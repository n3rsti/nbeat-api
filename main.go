package main

import (
	"context"
	"nbeat-api/db"
	"nbeat-api/handlers/channel"
	"nbeat-api/handlers/user"
	"nbeat-api/middleware/cors"

	"github.com/gin-gonic/gin"
)

func main() {
	router := gin.Default()

	mongoClient := db.Connect()

	defer func() {
		if err := mongoClient.Disconnect(context.TODO()); err != nil {
			panic(err)
		}
	}()

	db := mongoClient.Database("nbeat")

	userHandler := user.Handler{Db: db}
	channelHandler := channel.Handler{Db: db}

	router.Use(cors.Middleware())

	router.POST("/api/login", userHandler.Login)
	router.POST("/api/register", userHandler.Register)
	router.POST("/api/channel", channelHandler.CreateChannel)
	router.GET("/api/channel/:id", channelHandler.GetChannel)
	router.GET("/api/channel2/:id", channelHandler.GetChannel2)
	router.GET("/api/song/:id", channelHandler.GetSongData)
	router.GET("/ws/channel/:id", channelHandler.Channel)

	router.Run("0.0.0.0:8080")
}
