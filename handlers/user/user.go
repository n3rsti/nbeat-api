package user

import (
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/mongo"
)

type Handler struct {
	Db *mongo.Database
}

func (h *Handler) Login(c *gin.Context) {

}

func (h *Handler) Register(c *gin.Context) {
}
