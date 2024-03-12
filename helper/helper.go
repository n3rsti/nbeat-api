package helper

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"
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

func MatchBearerToken(authHeader string) string {
	pattern := `^Bearer\s+([a-zA-Z0-9_-]+\.[a-zA-Z0-9_-]+\.[a-zA-Z0-9_-]+)$`
	re := regexp.MustCompile(pattern)

	matches := re.FindStringSubmatch(authHeader)
	if len(matches) == 2 {
		return matches[1]
	}

	return ""
}

func ParseISODuration(isoDuration string) (time.Duration, error) {
	re := regexp.MustCompile(`P(?:(\d+)D)?T(?:(\d+)H)?(?:(\d+)M)?(?:(\d+)S)?`)
	matches := re.FindStringSubmatch(isoDuration)

	if len(matches) == 0 {
		return 0, fmt.Errorf("invalid ISO 8601 duration: %s", isoDuration)
	}

	// Matches indices:
	// 0: Full match
	// 1: Days
	// 2: Hours
	// 3: Minutes
	// 4: Seconds
	var durationStrBuilder strings.Builder
	for i, match := range matches[1:] {
		if match == "" {
			continue
		}
		switch i {
		case 0:
			durationStrBuilder.WriteString(match + "h24m")
		case 1:
			durationStrBuilder.WriteString(match + "h")
		case 2:
			durationStrBuilder.WriteString(match + "m")
		case 3:
			durationStrBuilder.WriteString(match + "s")
		}
	}

	durationStr := durationStrBuilder.String()
	return time.ParseDuration(durationStr)
}
