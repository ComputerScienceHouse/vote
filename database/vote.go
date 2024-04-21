package database

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

func HasVoted(ctx context.Context, pollId, userId string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	pId, err := primitive.ObjectIDFromHex(pollId)
	if err != nil {
		return false, err
	}

	count, err := Client.Database(db).Collection("votes").CountDocuments(ctx, map[string]interface{}{"pollId": pId, "userId": userId})
	if err != nil {
		return false, err
	}
	return count > 0, nil
}
