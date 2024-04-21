package database

import (
	"context"
	"time"
	"fmt"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

func HasVoted(ctx context.Context, pollId, userId string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	pId, err := primitive.ObjectIDFromHex(pollId)
	if err != nil {
		return false, err
	}

	count, err := Client.Database(db).Collection("voters").CountDocuments(ctx, map[string]interface{}{"pollId": pId, "userId": userId})
	if err != nil {
		fmt.Println(err)
		return false, err
	}
	fmt.Println(count)
	return count > 0, nil
}
