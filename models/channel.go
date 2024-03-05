package models

import "go.mongodb.org/mongo-driver/bson"

type Channel struct {
	Id               string    `json:"_id" bson:"_id"`
	Name             string    `json:"name,omitempty"`
	LastSong         string    `json:"last_song,omitempty"`
	LastSongPLayedAt int64     `json:"last_song_played_at,omitempty"`
	Messages         []Message `json:"messages,omitempty"`
}

type Message struct {
	Author  string
	Content string
	Id      string
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

	return data
}
