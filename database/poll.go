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

func containsValue(slice []string, value string) bool {
	for _, item := range slice {
		if item == value {
			return true
		}
	}
	return false
}

func (poll *Poll) GetResult(ctx context.Context) ([]map[string]int, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	pollId, _ := primitive.ObjectIDFromHex(poll.Id)

	finalResult := make([]map[string]int, 0)

	if poll.VoteType == POLL_TYPE_SIMPLE {
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
	} else if poll.VoteType == POLL_TYPE_RANKED {
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
		var votes []RankedVote
		cursor.All(ctx, &votes)

		// Iterate until we have a winner
		for {
			// Contains candidates to number of votes in this route
			tallied := make(map[string]int)
			for _, vote := range votes {
				// Map a voter's ranked choice to an array
				// From First to last Choice
				picks := make([]string, 0)
				for i, _ := range poll.Options {
					for _, opt := range poll.Options {
						if vote.Options[opt] == i+1 {
							picks = append(picks, opt)
						}
					}
				}
				// Go over picks until we find a non-eliminated candidate
				for _, candidate := range picks {
					if !containsValue(eliminated, candidate) {
						if _, ok := tallied[candidate]; ok {
							tallied[candidate]++
						} else {
							tallied[candidate] = 1
						}
						break
					}
				}
			}
			// Eliminate lowest vote getter
			min_vote := 1000000
			min_person := make([]string, 0)
			for person, vote := range tallied {
				if vote < min_vote {
					min_vote = vote
					min_person = make([]string, 0)
					min_person = append(min_person, person)
				} else if vote == min_vote {
					min_person = append(min_person, person)
				}
			}
			eliminated = append(eliminated, min_person...)
			finalResult = append(finalResult, tallied)
			// If one person has all the votes, they win
			if len(tallied) == 1 {
				break
			}
			// Check if all values in tallied are the same
			// In that case, it's a tie?
			allSame := true
			for _, val := range tallied {
				if val != min_vote {
					allSame = false
				}
			}
			if allSame {
				break
			}
		}
		return finalResult, nil
	}
	return nil, nil
}

func containsKey(arr map[string]int, val string) bool {
	for key, _ := range arr {
		if key == val {
			return true
		}
	}
	return false
}
