package database

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

type Poll struct {
	Id               string   `bson:"_id,omitempty"`
	CreatedBy        string   `bson:"createdBy"`
	ShortDescription string   `bson:"shortDescription"`
	LongDescription  string   `bson:"longDescription"`
	VoteType         string   `bson:"voteType"`
	Options          []string `bson:"options"`
	Open             bool     `bson:"open"`
	Hidden           bool     `bson:"hidden"`
	AllowWriteIns    bool     `bson:"writeins"`
}

const POLL_TYPE_SIMPLE = "simple"
const POLL_TYPE_RANKED = "ranked"

func GetPoll(ctx context.Context, id string) (*Poll, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	objId, _ := primitive.ObjectIDFromHex(id)
	var poll Poll
	if err := Client.Database(db).Collection("polls").FindOne(ctx, map[string]interface{}{"_id": objId}).Decode(&poll); err != nil {
		return nil, err
	}

	return &poll, nil
}

func (poll *Poll) Close(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	objId, _ := primitive.ObjectIDFromHex(poll.Id)

	_, err := Client.Database(db).Collection("polls").UpdateOne(ctx, map[string]interface{}{"_id": objId}, map[string]interface{}{"$set": map[string]interface{}{"open": false}})
	if err != nil {
		return err
	}

	return nil
}

func (poll *Poll) Hide(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	objId, _ := primitive.ObjectIDFromHex(poll.Id)

	_, err := Client.Database(db).Collection("polls").UpdateOne(ctx, map[string]interface{}{"_id": objId}, map[string]interface{}{"$set": map[string]interface{}{"hidden": true}})
	if err != nil {
		return err
	}

	return nil
}

func (poll *Poll) Reveal(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	objId, _ := primitive.ObjectIDFromHex(poll.Id)

	_, err := Client.Database(db).Collection("polls").UpdateOne(ctx, map[string]interface{}{"_id": objId}, map[string]interface{}{"$set": map[string]interface{}{"hidden": false}})
	if err != nil {
		return err
	}

	return nil
}

func CreatePoll(ctx context.Context, poll *Poll) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	result, err := Client.Database(db).Collection("polls").InsertOne(ctx, poll)
	if err != nil {
		return "", err
	}

	return result.InsertedID.(primitive.ObjectID).Hex(), nil
}

func GetOpenPolls(ctx context.Context) ([]*Poll, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	cursor, err := Client.Database(db).Collection("polls").Find(ctx, map[string]interface{}{"open": true})
	if err != nil {
		return nil, err

	}

	var polls []*Poll
	cursor.All(ctx, &polls)

	return polls, nil
}

func GetClosedOwnedPolls(ctx context.Context, userId string) ([]*Poll, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	cursor, err := Client.Database(db).Collection("polls").Find(ctx, map[string]interface{}{"createdBy": userId, "open": false})
	if err != nil {
		return nil, err
	}

	var polls []*Poll
	cursor.All(ctx, &polls)

	return polls, nil
}

func GetClosedVotedPolls(ctx context.Context, userId string) ([]*Poll, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	cursor, err := Client.Database(db).Collection("votes").Aggregate(ctx, mongo.Pipeline{
		{{
			"$match", bson.D{
				{"userId", userId},
			},
		}},
		{{
			"$lookup", bson.D{
				{"from", "polls"},
				{"localField", "pollId"},
				{"foreignField", "_id"},
				{"as", "polls"},
			},
		}},
		{{
			"$unwind", bson.D{
				{"path", "$polls"},
				{"preserveNullAndEmptyArrays", false},
			},
		}},
		{{
			"$replaceRoot", bson.D{
				{"newRoot", "$polls"},
			},
		}},
		{{
			"$match", bson.D{
				{"open", false},
			},
		}},
	})
	if err != nil {
		return nil, err
	}

	var polls []*Poll
	cursor.All(ctx, &polls)

	return polls, nil
}

func (poll *Poll) GetResult(ctx context.Context) ([]map[string]int, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	pollId, _ := primitive.ObjectIDFromHex(poll.Id)

	finalResult := make([]map[string]int, 0)
	switch poll.VoteType {

	case POLL_TYPE_SIMPLE:
		pollResult := make(map[string]int)
		cursor, err := Client.Database(db).Collection("votes").Aggregate(ctx, mongo.Pipeline{
			{{
				"$match", bson.D{
					{"pollId", pollId},
				},
			}},
			{{
				"$group", bson.D{
					{"_id", "$option"},
					{"count", bson.D{
						{"$sum", 1},
					}},
				},
			}},
		})
		if err != nil {
			return nil, err
		}

		var results []SimpleResult
		cursor.All(ctx, &results)

		// Start by setting all the results to zero
		for _, opt := range poll.Options {
			pollResult[opt] = 0
		}
		// Overwrite those with given votes and add write-ins
		for _, r := range results {
			pollResult[r.Option] = r.Count
		}
		finalResult = append(finalResult, pollResult)
		return finalResult, nil

	case POLL_TYPE_RANKED:
		// We want to store those that were eliminated
		eliminated := make([]string, 0)

		// Get all votes
		cursor, err := Client.Database(db).Collection("votes").Aggregate(ctx, mongo.Pipeline{
			{{
				"$match", bson.D{
					{"pollId", pollId},
				},
			}},
		})
		if err != nil {
			return nil, err
		}
		var votesRaw []RankedVote
		cursor.All(ctx, &votesRaw)

		votes := make([][]string, 0)

		//change ranked votes from a map (which is unordered) to a slice of votes (which is ordered)
		//order is from first preference to last preference
		for _, vote := range votesRaw {
			temp, cf := context.WithTimeout(context.Background(), 1*time.Second)
			optionList := orderOptions(vote.Options, temp)
			cf()
			votes = append(votes, optionList)
		}

		// Iterate until we have a winner
		for {
			// Contains candidates to number of votes in this round
			tallied := make(map[string]int)
			voteCount := 0
			for _, picks := range votes {
				// Go over picks until we find a non-eliminated candidate
				for _, candidate := range picks {
					if !containsValue(eliminated, candidate) {
						if _, ok := tallied[candidate]; ok {
							tallied[candidate]++
						} else {
							tallied[candidate] = 1
						}
						voteCount += 1
						break
					}
				}
			}
			// Eliminate lowest vote getter
			minVote := 1000000             //the smallest number of votes received thus far (to find who is in last)
			minPerson := make([]string, 0) //the person(s) with the least votes that need removed
			for person, vote := range tallied {
				if vote < minVote { // this should always be true round one, to set a true "who is in last"
					minVote = vote
					minPerson = make([]string, 0)
					minPerson = append(minPerson, person)
				} else if vote == minVote {
					minPerson = append(minPerson, person)
				}
			}
			eliminated = append(eliminated, minPerson...)
			finalResult = append(finalResult, tallied)
			// If one person has all the votes, they win
			if len(tallied) == 1 {
				break
			}

			end := true
			for str, val := range tallied {
				// if any particular entry is above half remaining votes, they win and it ends
				if val > (voteCount / 2) {
					finalResult = append(finalResult, map[string]int{str: val})
					end = true
					break
				}
				// Check if all values in tallied are the same
				// In that case, it's a tie?
				if val != minVote {
					end = false
					break
				}
			}
			if end {
				break
			}
		}
		return finalResult, nil
	}
	return nil, nil
}

func containsValue(slice []string, value string) bool {
	for _, item := range slice {
		if item == value {
			return true
		}
	}
	return false
}

func orderOptions(options map[string]int, ctx context.Context) []string {
	result := make([]string, 0, len(options))
	order := 1
	for order <= len(options) {
		for option, preference := range options {
			select {
			case <-ctx.Done():
				return make([]string, 0)
			default:
				if preference == order {
					result = append(result, option)
					order += 1
				}
			}
		}
	}
	return result
}
