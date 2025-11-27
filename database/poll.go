package database

import (
	"context"
	"sort"
	"time"

	"github.com/sirupsen/logrus"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"

	"github.com/computersciencehouse/vote/logging"
)

type Poll struct {
	Id               string    `bson:"_id,omitempty"`
	CreatedBy        string    `bson:"createdBy"`
	ShortDescription string    `bson:"shortDescription"`
	LongDescription  string    `bson:"longDescription"`
	VoteType         string    `bson:"voteType"`
	Options          []string  `bson:"options"`
	OpenedTime       time.Time `bson:"openedTime"`
	Open             bool      `bson:"open"`
	Gatekeep         bool      `bson:"gatekeep"`
	QuorumType       float64   `bson:"quorumType"`
	AllowedUsers     []string  `bson:"allowedUsers"`
	AllowWriteIns    bool      `bson:"writeins"`

	// Prevent this poll from having progress displayed
	// This is important for events like elections where the results shouldn't be visible mid vote
	Hidden bool `bson:"hidden"`
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
	err = cursor.All(ctx, &polls)
	if err != nil {
		return nil, err
	}

	return polls, nil
}

func GetOpenGatekeepPolls(ctx context.Context) ([]*Poll, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	cursor, err := Client.Database(db).Collection("polls").Find(ctx, map[string]interface{}{"open": true, "gatekeep": true})
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

// calculateRankedResult determines a result for a ranked choice vote
// votesRaw is the RankedVote entries that are returned directly from the database
// The algorithm defined in the Constitution as of 26 Nov 2025 is as follows:
//
// > The winning option is selected outright if it gains more than half the votes
// > cast as a first preference. If not, the option with the fewest number of first
// > preference votes is eliminated and their votes move to the second preference
// > marked on the ballots. This process continues until one option has half of the
// > votes cast and is elected.
//
// The return value consists of a list of voting rounds. Each round contains a
// mapping of the vote options to their vote share for that round. If the vote
// is not decided in a given round, there will be a subsequent round with the
// option that had the fewest votes eliminated, and its votes redistributed.
//
// The last entry in this list is the final round, and the option with the most
// votes in this round is the winner. If all options have the same, then it is
// unfortunately a tie, and the vote is not resolvable, as there is no lowest
// option to eliminate.
func calculateRankedResult(ctx context.Context, votesRaw []RankedVote) ([]map[string]int, error) {
	// We want to store those that were eliminated so we don't accidentally reinclude them
	eliminated := make([]string, 0)
	votes := make([][]string, 0)
	finalResult := make([]map[string]int, 0)

	//change ranked votes from a map (which is unordered) to a slice of votes (which is ordered)
	//order is from first preference to last preference
	for _, vote := range votesRaw {
		optionList := orderOptions(ctx, vote.Options)
		votes = append(votes, optionList)
	}

	round := 0
	// Iterate until we have a winner
	for {
		round = round + 1
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

		// TODO this should probably include some poll identifier
		logging.Logger.WithFields(logrus.Fields{"round": round, "tallies": tallied, "threshold": voteCount / 2}).Debug("round report")

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
			}
		}
		if end {
			break
		}
	}
	return finalResult, nil

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
		return calculateRankedResult(ctx, votesRaw)
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

// orderOptions takes a RankedVote's options, and returns an ordered list of
// their choices
//
// it's invalid for a vote to list the same number multiple times, the output
// will vary based on the map ordering of the options, and so is not guaranteed
// to be deterministic
//
// ctx is no longer used, as this function is not expected to hang, but remains
// an argument per golang standards
//
// the return values is the option keys, ordered from lowest to highest
func orderOptions(ctx context.Context, options map[string]int) []string {
	// Figure out all the ranks they've listed
	var ranks []int = make([]int, len(options))
	reverse_map := make(map[int]string)
	i := 0
	for option, rank := range options {
		ranks[i] = rank
		reverse_map[rank] = option
		i += 1
	}

	sort.Ints(ranks)

	// normalise the ranks for counts that don't start at 1
	var choices []string = make([]string, len(ranks))
	for idx, rank := range ranks {
		choices[idx] = reverse_map[rank]
	}

	return choices
}
