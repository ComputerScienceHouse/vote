package database

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type SimpleVote struct {
	Id     string             `bson:"_id,omitempty"`
	PollId primitive.ObjectID `bson:"pollId"`
	UserId string             `bson:"userId"`
	Option string             `bson:"option"`
}

type SimpleResult struct {
	Option string `bson:"_id"`
	Count  int    `bson:"count"`
}

func CastSimpleVote(ctx context.Context, vote *SimpleVote) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	_, err := Client.Database(db).Collection("votes").InsertOne(ctx, vote)
	if err != nil {
		return err
	}

	return nil
}
