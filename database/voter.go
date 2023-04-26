package database

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type Voter struct {
	Id     string             `bson:"_id,omitempty"`
	PollId primitive.ObjectID `bson:"pollId"`
	UserId string             `bson:"userId"`
}

func RecordVoter(voter *Voter) error {
	ctx, cancel := context.WithTimeout(context.TODO(), 10*time.Second)
	defer cancel()

	_, err := Client.Database("vote").Collection("voters").InsertOne(ctx, voter)
	if err != nil {
		return err
	}

	return nil
}

