package auth

import (
	"errors"
	"log"
	"nbeat-api/helper"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v4"
	"github.com/joho/godotenv"
)

func loadSecretKey() string {
	err := godotenv.Load()
	if err != nil {
		log.Println("can't load .env'")
	}

	return helper.GetEnv("SECRET_KEY", "secret")
}

var (
	SecretKey = loadSecretKey()
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

func ValidateToken(signedToken string) (claims *SignedClaims, err error) {
	token, err := jwt.ParseWithClaims(
		signedToken,
		&SignedClaims{},
		func(token *jwt.Token) (interface{}, error) {
			return []byte(SecretKey), nil
		},
	)
	if err != nil {
		return
	}

	claims, ok := token.Claims.(*SignedClaims)
	if !ok {
		err = errors.New("couldn't parse claims")
		return
	}

	if claims.ExpiresAt.Unix() < time.Now().Local().Unix() {
		err = errors.New("token expired")
		return
	}

	if claims.Id == "" {
		err = errors.New("empty id")
		return
	}

	return
}

func Auth() gin.HandlerFunc {
	return func(c *gin.Context) {
		token := c.GetHeader("Authorization")

		if !strings.HasPrefix(token, "Bearer ") {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error": "no access token",
			})
			c.Header("WWW-Authenticate", "invalid access token")
			c.Abort()
			return
		}

		token = token[len("Bearer "):]

		claims, err := ValidateToken(token)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error": "invalid access token",
			})
			c.Header("WWW-Authenticate", "invalid access token")
			c.Abort()
			return
		}

		if claims.Token == "refresh" {
			c.JSON(http.StatusUnauthorized, gin.H{
				"error": "provided token is refresh token (should be access token)",
			})
			c.Header("WWW-Authenticate", "provided token is refresh token (should be access token)")
			c.Abort()
			return
		}

		c.Next()
	}
}
