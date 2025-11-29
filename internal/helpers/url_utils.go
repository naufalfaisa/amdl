// ============================================
// File: internal/helpers/url_utils.go
package helpers

import (
	"fmt"
	"regexp"
)

// URL type identifiers
const (
	URLTypeAlbum    = "album"
	URLTypeSong     = "song"
	URLTypeMV       = "music-video"
	URLTypePlaylist = "playlist"
	URLTypeStation  = "station"
	URLTypeArtist   = "artist"
)

// Regex patterns for Apple Music URLs
var (
	// Numeric ID pattern (album, song, music-video, artist)
	numericIDPattern = `(?:id)?(\d[^\D]+)`

	// Playlist ID pattern (pl.xxx)
	playlistIDPattern = `(?:id)?(pl\.[\w-]+)`

	// Station ID pattern (ra.xxx)
	stationIDPattern = `(?:id)?(ra\.[\w-]+)`

	// Compiled regex patterns (lazy initialization for better performance)
	urlPatterns = make(map[string]*regexp.Regexp)
)

// init compiles all regex patterns once at startup
func init() {
	urlPatterns[URLTypeAlbum] = buildURLPattern(URLTypeAlbum, numericIDPattern)
	urlPatterns[URLTypeSong] = buildURLPattern(URLTypeSong, numericIDPattern)
	urlPatterns[URLTypeMV] = buildURLPattern(URLTypeMV, numericIDPattern)
	urlPatterns[URLTypePlaylist] = buildURLPattern(URLTypePlaylist, playlistIDPattern)
	urlPatterns[URLTypeStation] = buildURLPattern(URLTypeStation, stationIDPattern)
	urlPatterns[URLTypeArtist] = buildURLPattern(URLTypeArtist, numericIDPattern)
}

// buildURLPattern constructs regex pattern for Apple Music URLs
func buildURLPattern(urlType, idPattern string) *regexp.Regexp {
	// Base domains: beta.music, music, classical.music
	domains := `(?:beta\.music|music|classical\.music)`

	// For station URLs, don't include classical.music
	if urlType == URLTypeStation || urlType == URLTypeMV {
		domains = `(?:beta\.music|music)`
	}

	pattern := fmt.Sprintf(
		`^(?:https:\/\/%s\.apple\.com\/(\w{2})(?:\/%s|\/[^\/]+\/.+))\/%s(?:$|\?)`,
		domains,
		urlType,
		idPattern,
	)

	return regexp.MustCompile(pattern)
}

// parseURL extracts country code and ID from Apple Music URL
func parseURL(url string, pattern *regexp.Regexp) (countryCode string, id string) {
	matches := pattern.FindStringSubmatch(url)
	if len(matches) < 3 {
		return "", ""
	}
	return matches[1], matches[2]
}

// CheckURL validates and extracts album URL info
func CheckURL(url string) (string, string) {
	return parseURL(url, urlPatterns[URLTypeAlbum])
}

// CheckURLMv validates and extracts music video URL info
func CheckURLMv(url string) (string, string) {
	return parseURL(url, urlPatterns[URLTypeMV])
}

// CheckURLSong validates and extracts song URL info
func CheckURLSong(url string) (string, string) {
	return parseURL(url, urlPatterns[URLTypeSong])
}

// CheckURLPlaylist validates and extracts playlist URL info
func CheckURLPlaylist(url string) (string, string) {
	return parseURL(url, urlPatterns[URLTypePlaylist])
}

// CheckURLStation validates and extracts station URL info
func CheckURLStation(url string) (string, string) {
	return parseURL(url, urlPatterns[URLTypeStation])
}

// CheckURLArtist validates and extracts artist URL info
func CheckURLArtist(url string) (string, string) {
	return parseURL(url, urlPatterns[URLTypeArtist])
}

// ParseAppleMusicURL attempts to parse any Apple Music URL and returns type, country, and ID
func ParseAppleMusicURL(url string) (urlType, countryCode, id string) {
	for t, pattern := range urlPatterns {
		country, itemID := parseURL(url, pattern)
		if country != "" && itemID != "" {
			return t, country, itemID
		}
	}
	return "", "", ""
}
