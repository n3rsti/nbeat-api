package auth

import (
	"log"
	"nbeat-api/helper"
	"time"

	"github.com/gin-gonic/gin"
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

func ExtractClaims(signedToken string) *SignedClaims {
	token, err := jwt.ParseWithClaims(
		signedToken,
		&SignedClaims{},
		func(token *jwt.Token) (interface{}, error) {
			return []byte(SecretKey), nil
		},
	)
	if err != nil {
		log.Panic(err)
		return nil
	}

	claims, ok := token.Claims.(*SignedClaims)
	if !ok {
		return nil
	}

	return claims
}

func ExtractClaimsFromContext(c *gin.Context) *SignedClaims {
	token := c.GetHeader("Authorization")
	token = token[len("Bearer "):]

	return ExtractClaims(token)
}
