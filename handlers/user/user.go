package user

import (
	"context"
	"log"
	"nbeat-api/middleware/auth"
	"nbeat-api/models"
	"nbeat-api/utils/crypto"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
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
	user.FollowedChannels = []primitive.ObjectID{}

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

func (h *Handler) fetchUser(c context.Context, userId string, opts ...*options.FindOneOptions) (models.User, error) {

	collection := h.Db.Collection("user")

	filter := bson.M{"_id": userId}

	var user models.User

	if err := collection.FindOne(c, filter, opts...).Decode(&user); err != nil {
		return models.User{}, err
	}

	return user, nil
}

func (h *Handler) FetchFollowedChannelIDs(c *gin.Context) {
	userId := c.Param("id")

	opts := options.FindOne().SetProjection(bson.M{
		"_id":      0,
		"password": 0,
	})

	user, err := h.fetchUser(c, userId, opts)
	if err != nil {
		log.Println(err)
		c.Status(http.StatusNotFound)
		return
	}

	c.JSON(http.StatusOK, user)
}

func (h *Handler) FetchFollowedChannelsData(c *gin.Context) {
	userId := c.Param("id")

	currentTime := time.Now().UnixMilli()
	pipeline := []bson.M{
		{"$match": bson.M{"_id": userId}},
		{"$lookup": bson.M{
			"from":         "channel",
			"localField":   "followed_channels",
			"foreignField": "_id",
			"as":           "channels",
		}},
		{"$unwind": "$channels"},
		{"$lookup": bson.M{
			"from":         "queue",
			"localField":   "channels._id",
			"foreignField": "channel_id",
			"as":           "queueInfo",
		}},
		{"$unwind": bson.M{"path": "$queueInfo", "preserveNullAndEmptyArrays": true}},
		{"$set": bson.M{
			"queueInfo.songs": bson.M{
				"$filter": bson.M{
					"input": "$queueInfo.songs",
					"as":    "song",
					"cond":  bson.M{"$lt": []interface{}{"$$song.song_start_time", currentTime}},
				},
			},
		}},
		{"$set": bson.M{
			"channels.lastPlayedSong": bson.M{"$arrayElemAt": []interface{}{"$queueInfo.songs", -1}},
		}},
		{"$group": bson.M{
			"_id":      "$_id",
			"channels": bson.M{"$push": "$channels"},
		}},
		{"$project": bson.M{
			"channels.messages": 0,
			"password":          0,
			"_id":               0,
		}},
	}

	collection := h.Db.Collection("user")

	cursor, err := collection.Aggregate(c, pipeline)
	if err != nil {
		log.Println(err)
		c.Status(http.StatusNotFound)
		return
	}

	var results []bson.M

	if err := cursor.All(c, &results); err != nil {
		log.Println(err)
		c.Status(http.StatusInternalServerError)
		return
	}

	if len(results) == 0 {
		c.Status(http.StatusNotFound)
		return
	}

	c.JSON(http.StatusOK, results[0])
}
