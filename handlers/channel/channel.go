package channel

import (
	"context"
	"fmt"
	"log"
	"nbeat-api/helper"
	"nbeat-api/middleware/auth"
	"nbeat-api/models"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"go.mongodb.org/mongo-driver/mongo"
)

var (
	// Define a structure to hold all connections and a mutex for safe access
	clientRegistry = struct {
		m     sync.Mutex
		conns map[string][]*websocket.Conn
	}{conns: make(map[string][]*websocket.Conn)}
)

type Handler struct {
	Db *mongo.Database
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func AddConnection(conn *websocket.Conn, channelId string) {
	clientRegistry.m.Lock()
	if _, exists := clientRegistry.conns[channelId]; exists {
		clientRegistry.conns[channelId] = append(clientRegistry.conns[channelId], conn)
	} else {
		clientRegistry.conns[channelId] = []*websocket.Conn{conn}
	}
	clientRegistry.m.Unlock()
}

func RemoveConnection(conn *websocket.Conn, channelId string) {
	clientRegistry.m.Lock()
	for i, c := range clientRegistry.conns[channelId] {
		if c == conn {
			clientRegistry.conns[channelId] = append(clientRegistry.conns[channelId][:i], clientRegistry.conns[channelId][i+1:]...)
			break
		}
	}
	clientRegistry.m.Unlock()
}

func (h *Handler) Channel(c *gin.Context) {
	w := c.Writer
	r := c.Request

	channelId := c.Param("id")

	log.Println(channelId)

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Print("Error during connection upgradation:", err)
		return
	}
	defer conn.Close()

	AddConnection(conn, channelId)

	defer RemoveConnection(conn, channelId)

	userId := ""

	for {
		messageType, message, err := conn.ReadMessage()
		if err != nil {
			log.Println("Error during message reading:", err)
			break
		}
		log.Printf("Received: %s, %d", message, messageType)

		h.handleMessage(messageType, message, channelId, &userId)
	}
}

func (h *Handler) handleMessage(messageType int, message []byte, channelId string, userId *string) {
	if authToken := helper.MatchBearerToken(string(message)); authToken != "" {
		claims := auth.ExtractClaims(authToken)
		*userId = claims.Id
		return
	}

	if *userId == "" {
		return
	}

	if songId := helper.MatchSongUrl(string(message)); songId != "" {
		log.Printf("Sond ID: %s", songId)
		h.PlaySong(songId, channelId)
	}

	broadcastMessage(messageType, []byte(fmt.Sprintf("%s: %s", *userId, message)), channelId)
}

func broadcastMessage(messageType int, message []byte, channelId string) {
	clientRegistry.m.Lock()
	defer clientRegistry.m.Unlock()
	for _, conn := range clientRegistry.conns[channelId] {
		if err := conn.WriteMessage(messageType, message); err != nil {
			log.Println("Error during message broadcasting:", err)
		}
	}
}

func (h *Handler) PlaySong(song string, channel string) {
	collection := h.Db.Collection("channel")
	updatedChannel := models.Channel{
		LastSong:         song,
		LastSongPLayedAt: time.Now().UnixMilli(),
		Id:               channel,
	}

	// TO FIX
	res, err := collection.UpdateOne(context.Background(), channel, updatedChannel.ToBsonOmitEmpty())
	if err != nil {
		log.Println(err)
		return
	}

	log.Printf("Updated channel: %s, count: %d", channel, res.ModifiedCount)
}
