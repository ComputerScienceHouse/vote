package database

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type RankedVote struct {
	Id      string             `bson:"_id,omitempty"`
	PollId  primitive.ObjectID `bson:"pollId"`
	UserId  string             `bson:"userId"`
	Options map[string]int     `bson:"options"`
}

func CastRankedVote(ctx context.Context, vote *RankedVote) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	_, err := Client.Database(db).Collection("votes").InsertOne(ctx, vote)
	if err != nil {
		return err
	}

	return nil
}
