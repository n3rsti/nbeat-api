package db

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/joho/godotenv"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func Connect() *mongo.Client {
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}

	DbHost := os.Getenv("DB_HOST")
	DbPassword := os.Getenv("DB_PASSWORD")
	DbUser := os.Getenv("DB_USER")

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
