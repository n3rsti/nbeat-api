package db

import (
	"context"
	"fmt"
	"log"
	"nbeat-api/helper"
	"time"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func Connect() *mongo.Client {
	DbHost := helper.GetEnv("DB_HOST", "")
	DbPassword := helper.GetEnv("DB_PASSWORD", "")
	DbUser := helper.GetEnv("DB_USER", "")

	fmt.Println(DbHost, DbUser, DbPassword)
	uri := fmt.Sprintf("mongodb://%s:%s@%s", DbUser, DbPassword, DbHost)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)

	defer cancel()

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		panic(err)
	}

	err = client.Ping(ctx, nil)
	if err != nil {
		log.Fatal(err)
	}

	return client

}
