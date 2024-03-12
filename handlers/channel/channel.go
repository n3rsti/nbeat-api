package channel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"nbeat-api/helper"
	"nbeat-api/middleware/auth"
	"nbeat-api/models"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
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

		if err := h.handleMessage(messageType, message, channelId, &userId); err != nil {
			log.Println(err)
		}

	}
}

func (h *Handler) handleMessage(messageType int, message []byte, channelId string, userId *string) error {
	if authToken := helper.MatchBearerToken(string(message)); authToken != "" {
		claims := auth.ExtractClaims(authToken)
		*userId = claims.Id
		return nil
	}

	if *userId == "" {
		return errors.New("user not authorized")
	}

	messageObject := models.Message{
		Author:  *userId,
		Content: string(message),
		Type:    "message",
		Id:      primitive.NewObjectID(),
	}

	if songId := helper.MatchSongUrl(string(message)); songId != "" {

		data, err := getSongData(songId)
		if err != nil {
			return err
		}

		song, err := models.BuildSongFromYoutubeData(data)

		if err != nil {
			return err
		}

		if err := h.PlaySong(song, channelId); err != nil {
			return err
		}

		messageObject.Type = "song"
		songData, err := json.Marshal(data)
		if err != nil {
			return err
		}
		messageObject.Content = string(songData)
	}

	jsonString, err := json.Marshal(messageObject)
	if err != nil {
		return err
	}

	broadcastMessage(messageType, []byte(jsonString), channelId)
	if err := h.saveMessage(messageObject, channelId); err != nil {
		return err
	}

	return nil

}

func (h *Handler) saveMessage(message models.Message, channelId string) error {
	collection := h.Db.Collection("channel")

	channelObjectId, err := primitive.ObjectIDFromHex(channelId)
	if err != nil {
		return err
	}

	filter := bson.M{"_id": channelObjectId}
	update := bson.M{"$push": bson.M{"messages": message}}

	_, err = collection.UpdateOne(context.Background(), filter, update)
	if err != nil {
		return err
	}

	return nil

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

func (h *Handler) PlaySong(song models.Song, channelId string) error {
	collection := h.Db.Collection("channel")

	updateFormula := bson.D{
		{Key: "$set", Value: bson.D{
			{Key: "last_song", Value: song.SongId},
			{Key: "last_song_played_at", Value: time.Now().UnixMilli()},
		}}}

	channelObjectId, err := primitive.ObjectIDFromHex(channelId)
	if err != nil {
		return err
	}

	res, err := collection.UpdateByID(context.Background(), channelObjectId, updateFormula)
	if err != nil {
		log.Println(err)
		return err
	}

	log.Printf("Updated channel: %s, count: %d", channelId, res.ModifiedCount)

	// Add to queue

	var queue models.Queue

	collection = h.Db.Collection("queue")

	filter := bson.M{"channel_id": channelId}

	err = collection.FindOne(context.Background(), filter).Decode(&queue)

	if queue.ChannelId == "" {
		queue, err = h.insertNewQueue(channelId)
		if err != nil || queue.Id.Hex() == "" {
			return fmt.Errorf("couldn't insert queue (id: %s), error: %s", queue.Id, err)
		}
	}

	song.Id = primitive.NewObjectID()

	if len(queue.Songs) == 0 {
		song.SongStartTime = time.Now().UnixMilli()
		queue.Songs = []models.Song{song}
	} else {
		lastSong := queue.Songs[len(queue.Songs)-1]

		if lastSong.SongStartTime+int64(lastSong.Duration*1000) <= time.Now().UnixMilli() {
			song.SongStartTime = time.Now().UnixMilli()
		} else {
			song.SongStartTime = lastSong.SongStartTime + int64(lastSong.Duration*1000)
		}

		queue.Songs = append(queue.Songs, song)
	}

	if _, err := collection.ReplaceOne(context.Background(), filter, queue); err != nil {
		return err
	}

	return nil
}

func (h *Handler) insertNewQueue(channelId string) (models.Queue, error) {
	collection := h.Db.Collection("queue")

	queue := models.Queue{
		ChannelId: channelId,
		Songs:     []models.Song{},
	}

	res, err := collection.InsertOne(context.Background(), queue)
	if err != nil {
		return models.Queue{}, err
	}

	queue.Id = res.InsertedID.(primitive.ObjectID)

	return queue, nil

}

func (h *Handler) AddSongToQueue() {

}

func (h *Handler) CreateChannel(c *gin.Context) {
	var channel models.Channel

	if err := c.BindJSON(&channel); err != nil {
		c.Status(http.StatusBadRequest)
		return
	}

	userId := auth.ExtractClaimsFromContext(c).Id

	collection := h.Db.Collection("channel")

	channelBson := bson.D{
		{Key: "name", Value: channel.Name},
		{Key: "owner", Value: userId},
	}

	res, err := collection.InsertOne(context.TODO(), channelBson)
	if err != nil {
		log.Println(err)
		c.Status(http.StatusBadRequest)
		return
	}

	channel.Id = res.InsertedID.(primitive.ObjectID).Hex()
	channel.Owner = userId
	channel.Messages = []models.Message{}
	channel.LastSong = ""
	channel.LastSongPLayedAt = 0

	c.JSON(http.StatusCreated, channel)

}

func (h *Handler) GetChannel(c *gin.Context) {
	paramId := c.Param("id")
	channelId, err := primitive.ObjectIDFromHex(paramId)

	if err != nil {
		log.Println(err)
		c.Status(http.StatusNotFound)
		return
	}

	collection := h.Db.Collection("channel")

	var channelObject models.Channel

	filter := bson.D{{Key: "_id", Value: channelId}}
	if err := collection.FindOne(c, filter).Decode(&channelObject); err != nil {
		log.Println(err)
		c.Status(http.StatusBadRequest)
		return
	}

	c.JSON(http.StatusOK, channelObject)

}

func (h *Handler) GetChannel2(c *gin.Context) {
	// Extract channel ID from URL parameter
	channelID := c.Param("id")

	channelCollection := h.Db.Collection("channel")
	queueCollection := h.Db.Collection("queue")

	channelObjId, _ := primitive.ObjectIDFromHex(channelID)

	// Fetch channel data by ID
	var channel models.Channel
	err := channelCollection.FindOne(context.Background(), bson.M{"_id": channelObjId}).Decode(&channel)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Channel not found"})
		return
	}

	currentTimestamp := time.Now().UnixMilli()

	pipeline := []bson.M{
		{"$match": bson.M{"channel_id": channelID}},
		{"$unwind": "$songs"},
		{"$facet": bson.M{
			"lastPlayed": []bson.M{
				{"$match": bson.M{"songs.song_start_time": bson.M{"$lt": currentTimestamp}}},
				{"$sort": bson.M{"songs.song_start_time": -1}},
				{"$limit": 1},
			},
			"upcomingSongs": []bson.M{
				{"$match": bson.M{"songs.song_start_time": bson.M{"$gt": currentTimestamp}}},
				{"$sort": bson.M{"songs.song_start_time": 1}},
			},
		}},
	}

	cursor, err := queueCollection.Aggregate(context.Background(), pipeline)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch songs"})
		return
	}
	defer cursor.Close(context.Background())

	var results []bson.M
	if err = cursor.All(context.Background(), &results); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to decode song data"})
		return
	}

	response := struct {
		Channel models.Channel `json:"channel"`
		Songs   bson.M         `json:"songs"`
	}{
		Channel: channel,
		Songs:   results[0],
	}

	// Return the channel with attached songs
	c.JSON(http.StatusOK, response)
}

func getSongData(url string) (models.YoutubeVideoData, error) {

	var data models.YoutubeVideoData
	apiKey := helper.GetEnv("YOUTUBE_API_KEY", "")
	if apiKey == "" {
		return data, errors.New("no API key")
	}

	requestUrl := fmt.Sprintf("https://www.googleapis.com/youtube/v3/videos?id=%s&key=%s&part=snippet,contentDetails", url, apiKey)
	res, err := http.Get(requestUrl)
	if err != nil {
		log.Println(err)
		return data, err
	}

	body, err := io.ReadAll(res.Body)
	if err != nil {
		log.Println(err)
	}

	json.Unmarshal(body, &data)

	return data, nil

}

func (h *Handler) GetSongData(c *gin.Context) {
	id := c.Param("id")

	videoData, err := getSongData(id)
	if err != nil {
		c.Status(http.StatusNotFound)
	}

	c.JSON(http.StatusOK, videoData)
}
