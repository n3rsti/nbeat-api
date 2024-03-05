package auth

import (
	"nbeat-api/helper"
	"time"

	"github.com/golang-jwt/jwt/v4"
)

var (
	SecretKey = helper.GetEnv("SECRET_KEY", "secret")
)

type SignedClaims struct {
	Id    string `json:"id"`
	Token string `json:"token,omitempty"`
	jwt.RegisteredClaims
}

func GenerateAccessToken(userId string) (accessToken string, err error) {
	// Access token for 24 hours
	claims := &SignedClaims{
		Id: userId,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Local().Add(time.Hour * time.Duration(24))),
		},
	}

	newToken, err := jwt.NewWithClaims(jwt.SigningMethodHS512, claims).
		SignedString([]byte(SecretKey))
	if err != nil {
		return "", err
	}

	return newToken, nil
}
