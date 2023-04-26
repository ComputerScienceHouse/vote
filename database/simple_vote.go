package database

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type SimpleVote struct {
	Id     string             `bson:"_id,omitempty"`
	PollId primitive.ObjectID `bson:"pollId"`
	Option string             `bson:"option"`
}

type SimpleResult struct {
	Option string `bson:"_id"`
	Count  int    `bson:"count"`
}

func CastSimpleVote(vote *SimpleVote) error {
	ctx, cancel := context.WithTimeout(context.TODO(), 10*time.Second)
	defer cancel()

	_, err := Client.Database("vote").Collection("votes").InsertOne(ctx, vote)
	if err != nil {
		return err
	}

	return nil
}
