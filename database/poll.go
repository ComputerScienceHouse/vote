package database

import (
	"context"
	"sort"
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
	if err := Client.Database("vote").Collection("polls").FindOne(ctx, map[string]interface{}{"_id": objId}).Decode(&poll); err != nil {
		return nil, err
	}

	return &poll, nil
}

func (poll *Poll) Close(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	objId, _ := primitive.ObjectIDFromHex(poll.Id)

	_, err := Client.Database("vote").Collection("polls").UpdateOne(ctx, map[string]interface{}{"_id": objId}, map[string]interface{}{"$set": map[string]interface{}{"open": false}})
	if err != nil {
		return err
	}

	return nil
}

func (poll *Poll) Hide(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	objId, _ := primitive.ObjectIDFromHex(poll.Id)

	_, err := Client.Database("vote").Collection("polls").UpdateOne(ctx, map[string]interface{}{"_id": objId}, map[string]interface{}{"$set": map[string]interface{}{"hidden": true}})
	if err != nil {
		return err
	}

	return nil
}

func (poll *Poll) Reveal(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	objId, _ := primitive.ObjectIDFromHex(poll.Id)

	_, err := Client.Database("vote").Collection("polls").UpdateOne(ctx, map[string]interface{}{"_id": objId}, map[string]interface{}{"$set": map[string]interface{}{"hidden": false}})
	if err != nil {
		return err
	}

	return nil
}

func CreatePoll(ctx context.Context, poll *Poll) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	result, err := Client.Database("vote").Collection("polls").InsertOne(ctx, poll)
	if err != nil {
		return "", err
	}

	return result.InsertedID.(primitive.ObjectID).Hex(), nil
}

func GetOpenPolls(ctx context.Context) ([]*Poll, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	cursor, err := Client.Database("vote").Collection("polls").Find(ctx, map[string]interface{}{"open": true})
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

	cursor, err := Client.Database("vote").Collection("polls").Find(ctx, map[string]interface{}{"createdBy": userId, "open": false})
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

	cursor, err := Client.Database("vote").Collection("votes").Aggregate(ctx, mongo.Pipeline{
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

func (poll *Poll) GetResult(ctx context.Context) (map[string]int, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	pollId, _ := primitive.ObjectIDFromHex(poll.Id)
	finalResult := make(map[string]int)

	if poll.VoteType == POLL_TYPE_SIMPLE {
		cursor, err := Client.Database("vote").Collection("votes").Aggregate(ctx, mongo.Pipeline{
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
			finalResult[opt] = 0
		}
		// Overwrite those with given votes and add write-ins
		for _, r := range results {
			finalResult[r.Option] = r.Count
		}
		return finalResult, nil
	} else if poll.VoteType == POLL_TYPE_RANKED {
		cursor, err := Client.Database("vote").Collection("votes").Aggregate(ctx, mongo.Pipeline{
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

		voteCount := len(votes)

		// ALRIGHT LETS GO INSTANT RUNOFF VOTE LOGIC GOES HERE
		for {
			// Create an empty result for counting at this iteration
			results := make(map[string]int)
			// Iterate through all cast votes
			for _, vote := range votes {
				// Create a list of the options in this vote and sort by preference
				options := make([]string, 0, len(vote.Options))
				for key := range vote.Options {
					options = append(options, key)
				}
				sort.SliceStable(options, func(i, j int) bool {
					return vote.Options[options[i]] < vote.Options[options[j]]
				})

				// Add a vote for the highest preference option
				for _, option := range options {
					// If that option has been eliminated, skip it and go to the next one
					if containsKey(finalResult, option) {
						continue
					}
					results[option] += 1
					break
				}
			}

			// Once we've gone through all votes, check if we have any options
			// that have received more than half of the possible votes
			for _, count := range results {
				// If so, we're done
				// This means we won't randomly mess with ties
				if count*2 >= voteCount {
					for k, c := range results {
						finalResult[k] = c
					}
					return finalResult, nil
				}
			}
			// If no option has won yet, find the option with the least votes and eliminate
			// it, noting the number of votes it recieved at the time
			options := make([]string, 0, len(finalResult))
			for key := range results {
				options = append(options, key)
			}
			sort.SliceStable(options, func(i, j int) bool {
				return results[options[i]] < results[options[j]]
			})

			finalResult[options[len(options)-1]] = results[options[len(options)-1]]
		}
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
