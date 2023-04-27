package database

import (
	"context"
	"time"
)

type Voter struct {
	Id     string   `bson:"_id"`
	PollId string	`bson:"pollId"`
	UserId string   `bson:"userId"`
}

func RecordVoter(voter *Voter) error {
	ctx, cancel := context.WithTimeout(context.TODO(), 10*time.Second)
	defer cancel()

	if voter.Id == "" {
		voter.Id = voter.PollId + "_" + voter.UserId
	}

	_, err := Client.Database("vote").Collection("voters").InsertOne(ctx, voter)
	if err != nil {
		return err
	}

	return nil
}

