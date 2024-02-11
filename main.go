package main

import (
	user "nbeat-api/handlers"

	"github.com/gin-gonic/gin"
)

func main() {
	router := gin.Default()

	userHandler := user.Handler{}

	router.POST("/api/login", userHandler.Login)
	router.POST("/api/register", userHandler.Register)
}
