package models

import (
	"nbeat-api/helper"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type Queue struct {
	Id        primitive.ObjectID `json:"_id" bson:"_id,omitempty"`
	ChannelId primitive.ObjectID `json:"channel_id" bson:"channel_id"`
	Songs     []Song
}

type Song struct {
	Id            primitive.ObjectID `json:"id" bson:"id"`
	SongId        string             `json:"song_id" bson:"song_id"`
	Duration      float64            `json:"duration" bson:"duration"`
	Title         string             `json:"title" bson:"title"`
	Thumbnail     string             `json:"thumbnail" bson:"thumbnail"`
	SongStartTime int64              `json:"song_start_time" bson:"song_start_time"`
}

func BuildSongFromYoutubeData(data YoutubeVideoData) (Song, error) {
	d := data.Items[0]
	songDuration, err := helper.ParseISODuration(d.ContentDetails.Duration)
	if err != nil {
		return Song{}, err
	}

	return Song{
		SongId:    d.Id,
		Duration:  songDuration.Seconds(),
		Title:     d.Snippet.Title,
		Thumbnail: d.Snippet.Thumbnails.Default.Url,
	}, nil
}

type YoutubeVideoData struct {
	Items []struct {
		Id             string `json:"id"`
		ContentDetails struct {
			Duration string `json:"duration"`
		} `json:"contentDetails"`
		Snippet struct {
			Title      string `json:"title"`
			Thumbnails struct {
				Default struct {
					Url string `json:"url"`
				} `json:"default"`
			} `json:"thumbnails"`
		} `json:"snippet"`
	} `json:"items"`
}
