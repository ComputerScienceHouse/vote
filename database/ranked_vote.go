package database

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type RankedVote struct {
	Id      string             `bson:"_id,omitempty"`
	PollId  primitive.ObjectID `bson:"pollId"`
	Options map[string]int     `bson:"options"`
}


func CastRankedVote(ctx context.Context, vote *RankedVote, voter *Voter) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	_, err := Client.Database(db).Collection("votes").InsertOne(ctx, vote)
	if err != nil {
		return err
	}
	_, err = Client.Database(db).Collection("voters").InsertOne(ctx, voter)
	if err != nil {
		return err
	}

	return nil
}
