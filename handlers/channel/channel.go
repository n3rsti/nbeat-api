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

	var jsonResponse []byte

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

		newSongId := primitive.NewObjectID()
		song.Id = newSongId

		if err != nil {
			return err
		}

		if err := h.PlaySong(song, channelId); err != nil {
			return err
		}

		response := map[string]interface{}{
			"author":  *userId,
			"content": song,
			"type":    "song",
			"id":      newSongId,
		}

		messageObject.Type = "song"
		messageObject.SongRef = newSongId.Hex()

		log.Println(response)

		jsonResponse, err = json.Marshal(response)
		if err != nil {
			return err
		}
	} else {

		var err error

		jsonResponse, err = json.Marshal(messageObject)
		if err != nil {
			return err
		}
	}

	broadcastMessage(messageType, []byte(jsonResponse), channelId)
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

	log.Println(message)

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
	// Add to queue

	var queue models.Queue

	collection := h.Db.Collection("queue")

	channelObjId, err := primitive.ObjectIDFromHex(channelId)
	if err != nil {
		return fmt.Errorf("invalid id: %s", channelId)
	}

	filter := bson.M{"channel_id": channelObjId}

	err = collection.FindOne(context.Background(), filter).Decode(&queue)

	if queue.ChannelId == primitive.NilObjectID {
		queue, err = h.insertNewQueue(channelId)
		if err != nil || queue.Id.Hex() == "" {
			return fmt.Errorf("couldn't insert queue (id: %s), error: %s", queue.Id, err)
		}
	}

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

	channelObjId, err := primitive.ObjectIDFromHex(channelId)
	if err != nil {
		return models.Queue{}, err
	}

	queue := models.Queue{
		ChannelId: channelObjId,
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

func (h *Handler) FetchChannelWithLastPlayedSong(c *gin.Context, channelID string) (bson.M, error) {
	channelCollection := h.Db.Collection("channel")

	channelObjectId, err := primitive.ObjectIDFromHex(channelID)
	if err != nil {
		return nil, err
	}

	currentTimestamp := time.Now().UnixMilli()

	pipeline := []bson.M{
		{"$match": bson.M{"_id": channelObjectId}},
		{"$lookup": bson.M{
			"from": "queue",
			"let":  bson.M{"channel_id": "$_id"},
			"pipeline": []bson.M{
				{"$match": bson.M{"$expr": bson.M{"$eq": []interface{}{"$channel_id", "$$channel_id"}}}},
				{"$unwind": "$songs"},
				{"$match": bson.M{"songs.song_start_time": bson.M{"$lt": currentTimestamp}}},
				{"$sort": bson.M{"songs.song_start_time": -1}},
				{"$limit": 1},
			},
			"as": "lastPlayedSong",
		}},
		{
			"$unwind": "$messages",
		},
		{"$lookup": bson.M{
			"from": "queue",
			"let":  bson.M{"song_id": "$messages.song"},
			"pipeline": []bson.M{
				{"$unwind": "$songs"},
				{"$match": bson.M{"$expr": bson.M{"$eq": []interface{}{"$songs.id", "$$song_id"}}}},
				{"$replaceRoot": bson.M{"newRoot": "$songs"}},
			},
			"as": "messages.songDetails",
		},
		},
		{
			"$group": bson.M{
				"_id":      "$_id",
				"name":     bson.M{"$first": "$name"},
				"owner":    bson.M{"$first": "$owner"},
				"messages": bson.M{"$push": "$messages"},
			},
		},
		{"$project": bson.M{
			"messages":       1,
			"name":           1,
			"owner":          1,
			"lastPlayedSong": bson.M{"$arrayElemAt": []interface{}{"$lastPlayedSong.songs", 0}},
		}},
	}

	var channels []bson.M
	cursor, err := channelCollection.Aggregate(c, pipeline)

	if err != nil {
		return nil, err
	}

	defer cursor.Close(c)

	if err := cursor.All(c, &channels); err != nil {
		return nil, err
	}
	if len(channels) == 0 {
		return nil, errors.New("channel not found")
	}
	return channels[0], nil

}

func (h *Handler) FetchUpcomingSongsForChannel(c *gin.Context, channelID string) (bson.M, error) {
	channelObjId, err := primitive.ObjectIDFromHex(channelID)
	if err != nil {
		return nil, err
	}

	queueCollection := h.Db.Collection("queue")

	currentTimestamp := time.Now().UnixMilli()
	pipeline := []bson.M{
		{"$match": bson.M{"channel_id": channelObjId}},
		{
			"$unwind": "$songs",
		},
		{
			"$match": bson.M{"songs.song_start_time": bson.M{"$gt": currentTimestamp}},
		},
		{
			"$group": bson.M{
				"_id":   "$_id",
				"songs": bson.M{"$push": "$songs"},
			},
		},
		{"$project": bson.M{
			"_id": 0,
		}},
	}

	cursor, err := queueCollection.Aggregate(context.Background(), pipeline)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(context.Background())

	var result []bson.M

	if err := cursor.All(c, &result); err != nil {
		return nil, err
	}

	if len(result) == 0 {
		return bson.M{"songs": []interface{}{}}, nil
	}

	return result[0], nil

}

func (h *Handler) GetChannel(c *gin.Context) {
	channelID := c.Param("id")

	channel, err := h.FetchChannelWithLastPlayedSong(c, channelID)
	if err != nil {
		log.Println(err)
		c.Status(http.StatusNotFound)
		return
	}

	queue, err := h.FetchUpcomingSongsForChannel(c, channelID)
	if err != nil {
		log.Println(err)
		c.Status(http.StatusNotFound)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"channel": channel,
		"queue":   queue,
	})
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
