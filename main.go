package main

import (
	"context"
	"nbeat-api/db"
	user "nbeat-api/handlers"

	"github.com/gin-gonic/gin"
)

func main() {
	dbConn := db.Connect()
	defer dbConn.Close(context.Background())

	router := gin.Default()

	userHandler := user.Handler{}

	router.POST("/api/login", userHandler.Login)
	router.POST("/api/register", userHandler.Register)

	router.Run("0.0.0.0:8080")
}
