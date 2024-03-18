package models

import (
	"github.com/go-playground/validator/v10"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

var validate = validator.New(validator.WithRequiredStructEnabled())

type User struct {
	Id               string               `json:"id" bson:"_id" validate:"min=1,max=30"`
	Password         string               `validate:"min=1"`
	FollowedChannels []primitive.ObjectID `json:"followedChannels" bson:"followed_channels"`
}

func (u User) Validate() error {
	err := validate.Struct(u)
	return err
}
