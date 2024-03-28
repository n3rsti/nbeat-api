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
	"go.mongodb.org/mongo-driver/mongo/options"
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

		if err := h.handleMessage(messageType, message, channelId, &userId); err != nil {
			log.Println(err)
		}

	}
}

func (h *Handler) handleMessage(messageType int, message []byte, channelId string, userId *string) error {
	if authToken := helper.MatchBearerToken(string(message)); authToken != "" {
		return authorizeUser(authToken, userId)
	}

	if *userId == "" {
		return errors.New("user not authorized")
	}

	messageContent, err := h.processMessage(message, channelId, userId)
	if err != nil {
		return err
	}

	return broadcastMessage(messageType, messageContent, channelId)
}

func authorizeUser(authToken string, userId *string) error {
	if authToken == "" && *userId == "" {
		return errors.New("user not authorized")
	}

	if authToken != "" {
		claims, err := auth.ValidateToken(authToken)
		if err != nil {
			return err
		}
		*userId = claims.Id
	}

	return nil
}

func (h *Handler) processMessage(message []byte, channelId string, userId *string) ([]byte, error) {
	if songId := helper.MatchSongUrl(string(message)); songId != "" {
		return h.handleSongMessage(songId, channelId, userId)
	}

	return h.handleTextMessage(message, userId, channelId)
}

func (h *Handler) handleSongMessage(songId, channelId string, userId *string) ([]byte, error) {
	data, err := getSongData(songId)
	if err != nil {
		return nil, err
	}

	songData, err := models.BuildSongFromYoutubeData(data)
	if err != nil {
		return nil, err
	}

	newSongId := primitive.NewObjectID()
	songData.Id = newSongId

	songData, err = h.PlaySong(songData, channelId)
	if err != nil {
		return nil, err
	}

	response := map[string]interface{}{
		"author":  *userId,
		"content": songData,
		"type":    "song",
		"id":      newSongId,
	}

	messageToSave := models.Message{
		Author:  *userId,
		Type:    "song",
		Id:      newSongId,
		SongRef: newSongId,
	}

	if err := h.saveMessage(messageToSave, channelId); err != nil {
		return nil, err
	}

	return json.Marshal(response)
}

func (h *Handler) handleTextMessage(message []byte, userId *string, channelId string) ([]byte, error) {
	messageToSave := models.Message{
		Author:  *userId,
		Content: string(message),
		Type:    "message",
		Id:      primitive.NewObjectID(),
	}

	if err := h.saveMessage(messageToSave, channelId); err != nil {
		return nil, err
	}

	return json.Marshal(messageToSave)
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

func broadcastMessage(messageType int, message []byte, channelId string) error {
	clientRegistry.m.Lock()
	defer clientRegistry.m.Unlock()
	for _, conn := range clientRegistry.conns[channelId] {
		if err := conn.WriteMessage(messageType, message); err != nil {
			return err
		}
	}

	return nil
}

func (h *Handler) PlaySong(song models.Song, channelId string) (models.Song, error) {
	// Add to queue

	var queue models.Queue

	collection := h.Db.Collection("queue")

	channelObjId, err := primitive.ObjectIDFromHex(channelId)
	if err != nil {
		return models.Song{}, fmt.Errorf("invalid id: %s", channelId)
	}

	filter := bson.M{"channel_id": channelObjId}

	err = collection.FindOne(context.Background(), filter).Decode(&queue)

	if queue.ChannelId == primitive.NilObjectID {
		queue, err = h.insertNewQueue(channelId)
		if err != nil || queue.Id.Hex() == "" {
			return models.Song{}, fmt.Errorf("couldn't insert queue (id: %s), error: %s", queue.Id, err)
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
		return models.Song{}, err
	}

	return song, nil
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

func (h *Handler) AddToFollowedChannels(userId, channelId string) error {
	channelObjId, err := primitive.ObjectIDFromHex(channelId)
	if err != nil {
		return err
	}

	collection := h.Db.Collection("user")

	filter := bson.M{"_id": userId}
	update := bson.M{"$push": bson.M{"followed_channels": channelObjId}}

	res, err := collection.UpdateOne(context.Background(), filter, update)
	if err != nil {
		return err
	}

	if res.MatchedCount == 0 {
		return errors.New("user not found")
	}

	return nil
}

func (h *Handler) CreateChannel(c *gin.Context) {
	var channel models.Channel

	if err := c.BindJSON(&channel); err != nil {
		log.Println(err)
		c.Status(http.StatusBadRequest)
		return
	}

	if err := channel.Validate(); err != nil {
		log.Println(err)
		c.Status(http.StatusBadRequest)
		return
	}

	userId := auth.ExtractClaimsFromContext(c).Id

	collection := h.Db.Collection("channel")

	channelBson := bson.D{
		{Key: "name", Value: channel.Name},
		{Key: "owner", Value: userId},
		{Key: "description", Value: channel.Description},
	}

	res, err := collection.InsertOne(context.TODO(), channelBson)
	if err != nil {
		log.Println(err)
		c.Status(http.StatusBadRequest)
		return
	}

	newChannelId := res.InsertedID.(primitive.ObjectID)
	if err := h.AddToFollowedChannels(userId, newChannelId.Hex()); err != nil {
		log.Println(err)
	}

	channel.Id = newChannelId.Hex()
	channel.Owner = userId
	channel.Messages = []models.Message{}
	channel.LastSong = ""
	channel.LastSongPLayedAt = 0

	c.JSON(http.StatusCreated, channel)

}

func (h *Handler) FetchChannelWithLastPlayedSong(c *gin.Context, channelID string) (bson.M, error) {
	channel, err := h.FetchChannelsWithLastPlayedSong(c, channelID)
	if err != nil {
		return bson.M{}, err
	}

	if len(channel) == 0 {
		return bson.M{}, errors.New("not found")
	}

	return channel[0], nil
}

func (h *Handler) FetchChannelsWithLastPlayedSong(c *gin.Context, channelID ...string) ([]bson.M, error) {
	channelCollection := h.Db.Collection("channel")

	channelObjIdList := make([]primitive.ObjectID, 0, len(channelID))
	for idx := range channelID {

		channelObjectId, err := primitive.ObjectIDFromHex(channelID[idx])
		if err != nil {
			return nil, err
		}

		channelObjIdList = append(channelObjIdList, channelObjectId)
	}

	currentTimestamp := time.Now().UnixMilli()

	pipeline := []bson.M{
		{"$match": bson.M{"_id": bson.M{"$in": channelObjIdList}}},
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
		{"$unwind": bson.M{"path": "$messages", "preserveNullAndEmptyArrays": true}},
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
		{"$group": bson.M{
			"_id":            "$_id",
			"lastPlayedSong": bson.M{"$first": "$lastPlayedSong"},
			"name":           bson.M{"$first": "$name"},
			"owner":          bson.M{"$first": "$owner"},
			"description":    bson.M{"$first": "$description"},
			"messages":       bson.M{"$push": "$messages"},
		},
		},
		{"$project": bson.M{
			"messages":       1,
			"name":           1,
			"owner":          1,
			"description":    1,
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
	return channels, nil

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

func (h *Handler) FollowChannel(c *gin.Context) {
	channelId := c.Param("id")
	channelObjId, err := primitive.ObjectIDFromHex(channelId)
	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}

	claims := auth.ExtractClaimsFromContext(c)
	userId := claims.Id

	filter := bson.M{"_id": channelObjId}

	opts := options.FindOne().SetProjection(
		bson.M{
			"_id":   1,
			"owner": 1,
		},
	)

	collection := h.Db.Collection("channel")

	var channel models.Channel
	if err := collection.FindOne(c, filter, opts).Decode(&channel); err != nil {
		log.Println(err)
		c.Status(http.StatusNotFound)
		return
	}

	if channel.Owner == userId {
		c.Status(http.StatusBadRequest)
		return
	}

	collection = h.Db.Collection("user")

	update := bson.M{"$addToSet": bson.M{"followed_channels": channelObjId}}

	if _, err := collection.UpdateByID(c, userId, update); err != nil {
		log.Println(err)
		c.Status(http.StatusInternalServerError)
		return
	}
}
