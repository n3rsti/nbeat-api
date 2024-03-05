package helper

import (
	"os"
	"regexp"
)

func MatchSongUrl(url string) string {
	urlRegexStr := `(?:youtube\.com\/(?:[^/\n\s]+\/\S+\/|(?:v|e(?:mbed)?)\/|\S*?[?&]v=)|youtu\.be\/)([a-zA-Z0-9_-]{11})`
	urlRegex, err := regexp.Compile(urlRegexStr)

	if err != nil {
		return ""
	}

	match := urlRegex.FindStringSubmatch(url)
	if match != nil && len(match) > 1 {
		songId := match[1]
		return songId
	}

	return ""
}

func GetEnv(name, fallback string) string {
	if val, exists := os.LookupEnv(name); exists {
		return val
	}

	return fallback
}
