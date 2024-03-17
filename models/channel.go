package models

import (
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type Channel struct {
	Id               string    `json:"_id" bson:"_id"`
	Name             string    `json:"name,omitempty"`
	LastSong         string    `json:"last_song,omitempty" bson:"last_song"`
	LastSongPLayedAt int64     `json:"last_song_played_at,omitempty" bson:"last_song_played_at"`
	Messages         []Message `json:"messages" bson:"messages"`
	Owner            string    `json:"owner,omitempty"`
}

type Message struct {
	Author  string             `json:"author"`
	Content string             `json:"content"`
	Id      primitive.ObjectID `json:"id,omitempty"`
	SongRef primitive.ObjectID `json:"song,omitempty" bson:"song"`
	Type    string             `json:"type,omitempty"`
}

func (c *Channel) ToBsonOmitEmpty() bson.D {

	var data bson.D

	if c.Id != "" {
		data = append(data, bson.E{Key: "_id", Value: c.Id})
	}

	if c.Name != "" {
		data = append(data, bson.E{Key: "name", Value: c.Name})
	}

	if c.LastSong != "" {
		data = append(data, bson.E{Key: "last_song", Value: c.LastSong})
	}

	if c.LastSongPLayedAt != 0 {
		data = append(data, bson.E{Key: "last_song_played_at", Value: c.LastSongPLayedAt})
	}

	if len(c.Messages) != 0 {
		data = append(data, bson.E{Key: "messages", Value: c.Messages})
	}

	if c.Owner != "" {
		data = append(data, bson.E{Key: "owner", Value: c.Owner})
	}

	return data
}
