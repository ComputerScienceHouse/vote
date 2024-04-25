package database

import (
	"context"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"time"
)

type Action struct {
	Id     string             `bson:"_id,omitempty"`
	PollId primitive.ObjectID `bson:"pollId"`
	Date   primitive.DateTime `bson:"date"`
	User   string             `bson:"user"`
	Action string             `bson:"action"`
}

func WriteAction(ctx context.Context, action *Action) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	_, err := Client.Database(db).Collection("actions").InsertOne(ctx, action)
	if err != nil {
		return err
	}

	return nil
}
