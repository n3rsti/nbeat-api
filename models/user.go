package models

import "github.com/go-playground/validator/v10"

var validate = validator.New(validator.WithRequiredStructEnabled())

type User struct {
	Id       string `validate:"min=1,max=30"`
	Password string
}

func (u User) Validate() error {
	err := validate.Struct(u)
	return err
}
