package user

import (
	"log"
	"nbeat-api/middleware/auth"
	"nbeat-api/models"
	"nbeat-api/utils/crypto"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

var validate = validator.New(validator.WithRequiredStructEnabled())

type Handler struct {
	Db *mongo.Database
}

func (h *Handler) Login(c *gin.Context) {
	var user models.User

	if err := c.BindJSON(&user); err != nil {
		log.Println(err)
		c.Status(http.StatusBadRequest)
		return
	}

	EMPTY_DATA := user.Id == "" || user.Password == ""
	if EMPTY_DATA {
		c.Status(http.StatusBadRequest)
		return
	}

	var result models.User

	collection := h.Db.Collection("user")
	err := collection.FindOne(c, bson.D{{Key: "_id", Value: user.Id}}).Decode(&result)
	if err != nil {
		log.Println(err)
		c.Status(http.StatusForbidden)
		return
	}

	isValidPassword := crypto.ComparePasswordAndHash(user.Password, result.Password)
	if !isValidPassword {
		log.Println(err)
		c.Status(http.StatusForbidden)
		return
	}

	accessToken, err := auth.GenerateAccessToken(user.Id)
	if err != nil {
		c.Status(http.StatusBadRequest)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"access_token": accessToken,
	})

}

func (h *Handler) Register(c *gin.Context) {
	var user models.User

	if err := c.BindJSON(&user); err != nil {
		c.Status(http.StatusBadRequest)
		return
	}

	if err := validate.Struct(&user); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Error in validation",
		})
		return
	}

	// Insert to DB
	collection := h.Db.Collection("user")

	// hash password
	passwordHash, err := crypto.GenerateHash(user.Password)
	if err != nil {
		c.Status(http.StatusInternalServerError)
		log.Println("Error while creating password hash")
		return
	}

	user.Password = passwordHash

	_, err = collection.InsertOne(c, user)
	if err != nil {
		log.Println(err)
		c.JSON(http.StatusConflict, gin.H{
			"error": "user already exists",
		})
		return
	}

	c.Status(http.StatusCreated)
}
