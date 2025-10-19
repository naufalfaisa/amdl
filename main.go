package main

import (
	// Core and external dependencies
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"main/utils/ampapi"
	"main/utils/lyrics"
	"main/utils/runv2"
	"main/utils/runv3"
	"main/utils/structs"
	"main/utils/task"

	"github.com/AlecAivazis/survey/v2"
	"github.com/grafov/m3u8"
	"github.com/spf13/pflag"
	"github.com/zhaarey/go-mp4tag"
	"gopkg.in/yaml.v2"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

var (
	// Forbidden characters for filenames
	forbiddenNames = regexp.MustCompile(`[/\\<>:"|?*]`)

	// Regular expressions to detect Apple Music URL types
	regexAlbum    = regexp.MustCompile(`^(?:https:\/\/(?:beta\.music|music|classical\.music)\.apple\.com\/(\w{2})(?:\/album|\/album\/.+))\/(?:id)?(\d[^\D]+)(?:$|\?)`)
	regexMv       = regexp.MustCompile(`^(?:https:\/\/(?:beta\.music|music)\.apple\.com\/(\w{2})(?:\/music-video|\/music-video\/.+))\/(?:id)?(\d[^\D]+)(?:$|\?)`)
	regexSong     = regexp.MustCompile(`^(?:https:\/\/(?:beta\.music|music|classical\.music)\.apple\.com\/(\w{2})(?:\/song|\/song\/.+))\/(?:id)?(\d[^\D]+)(?:$|\?)`)
	regexPlaylist = regexp.MustCompile(`^(?:https:\/\/(?:beta\.music|music|classical\.music)\.apple\.com\/(\w{2})(?:\/playlist|\/playlist\/.+))\/(?:id)?(pl\.[\w-]+)(?:$|\?)`)
	regexStation  = regexp.MustCompile(`^(?:https:\/\/(?:beta\.music|music)\.apple\.com\/(\w{2})(?:\/station|\/station\/.+))\/(?:id)?(ra\.[\w-]+)(?:$|\?)`)
	regexArtist   = regexp.MustCompile(`^(?:https:\/\/(?:beta\.music|music|classical\.music)\.apple\.com\/(\w{2})(?:\/artist|\/artist\/.+))\/(?:id)?(\d[^\D]+)(?:$|\?)`)

	// Media extraction patterns
	aacRegex       = regexp.MustCompile(`audio-stereo-\d+`)
	videoSizeRegex = regexp.MustCompile(`_(\d+)x(\d+)`)
	videoRankRegex = regexp.MustCompile(`_gr(\d+)_`)

	// Global variables
	dl_atmos      bool
	dl_aac        bool
	dl_select     bool
	dl_song       bool
	artist_select bool
	debug_mode    bool
	alac_max      *int
	atmos_max     *int
	mv_max        *int
	mv_audio_type *string
	aac_type      *string
	Config        structs.ConfigSet
	counter       structs.Counter
	okDict        = make(map[string][]int)
)

// loadConfig reads config.yaml and fills the Config struct
func loadConfig() error {
	data, err := os.ReadFile("config.yaml")
	if err != nil {
		return err
	}
	err = yaml.Unmarshal(data, &Config)
	if err != nil {
		return err
	}
	if len(Config.Storefront) != 2 {
		Config.Storefront = "us"
	}
	return nil
}

// LimitString ensures the string does not exceed Config.LimitMax characters
func LimitString(s string) string {
	if len([]rune(s)) > Config.LimitMax {
		return string([]rune(s)[:Config.LimitMax])
	}
	return s
}

func isInArray(arr []int, target int) bool {
	return slices.Contains(arr, target)
}

// fileExists checks if a file exists and returns true/false
func fileExists(path string) (bool, error) {
	f, err := os.Stat(path)
	if err == nil {
		return !f.IsDir(), nil
	} else if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

// extractMatches extracts storefront and ID from a URL using a regex
func extractMatches(regex *regexp.Regexp, url string) (string, string) {
	matches := regex.FindStringSubmatch(url)
	if len(matches) < 3 {
		return "", ""
	}
	return matches[1], matches[2]
}

// =================== URL PARSING HELPERS ===================

// These functions identify which type of Apple Music content a URL refers to
func checkUrl(url string) (string, string)         { return extractMatches(regexAlbum, url) }
func checkUrlMv(url string) (string, string)       { return extractMatches(regexMv, url) }
func checkUrlSong(url string) (string, string)     { return extractMatches(regexSong, url) }
func checkUrlPlaylist(url string) (string, string) { return extractMatches(regexPlaylist, url) }
func checkUrlStation(url string) (string, string)  { return extractMatches(regexStation, url) }
func checkUrlArtist(url string) (string, string)   { return extractMatches(regexArtist, url) }

// =================== ARTIST AND ALBUM HANDLING ===================

// getUrlArtistName fetches artist name and ID from the AMP API using the given token
func getUrlArtistName(artistUrl string, token string) (string, string, error) {
	storefront, artistId := checkUrlArtist(artistUrl)
	req, err := http.NewRequest("GET", fmt.Sprintf("https://amp-api.music.apple.com/v1/catalog/%s/artists/%s", storefront, artistId), nil)
	if err != nil {
		return "", "", err
	}
	// Set headers to mimic browser requests
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36")
	req.Header.Set("Origin", "https://music.apple.com")
	query := url.Values{}
	query.Set("l", Config.Language)
	do, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer do.Body.Close()
	if do.StatusCode != http.StatusOK {
		return "", "", errors.New(do.Status)
	}
	// Decode the JSON response
	obj := new(structs.AutoGeneratedArtist)
	err = json.NewDecoder(do.Body).Decode(&obj)
	if err != nil {
		return "", "", err
	}
	return obj.Data[0].Attributes.Name, obj.Data[0].ID, nil
}

// checkArtist lists all albums/singles/videos for a given artist
// Allows CLI-based multiple selection or 'all' download
func checkArtist(artistUrl string, token string, relationship string) ([]string, error) {
	storefront, artistId := checkUrlArtist(artistUrl)
	Num := 0
	var args []string
	var urls []string
	var options [][]string
	for {
		// Fetch paginated artist content (albums, singles, etc.)
		req, err := http.NewRequest("GET",
			fmt.Sprintf("https://amp-api.music.apple.com/v1/catalog/%s/artists/%s/%s?limit=100&offset=%d&l=%s",
				storefront, artistId, relationship, Num, Config.Language), nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))
		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36")
		req.Header.Set("Origin", "https://music.apple.com")
		do, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, err
		}
		defer do.Body.Close()

		if do.StatusCode != http.StatusOK {
			return nil, errors.New(do.Status)
		}
		// Decode JSON response into struct
		obj := new(structs.AutoGeneratedArtist)
		err = json.NewDecoder(do.Body).Decode(&obj)
		if err != nil {
			return nil, err
		}
		// Collect all results
		for _, album := range obj.Data {
			options = append(options, []string{album.Attributes.Name, album.Attributes.ReleaseDate, album.ID, album.Attributes.URL})
		}
		// Stop when no "next" page exists
		Num += 100
		if len(obj.Next) == 0 {
			break
		}
	}

	// Sort by release date ascending
	sort.Slice(options, func(i, j int) bool {
		dateI, _ := time.Parse("2006-01-02", options[i][1])
		dateJ, _ := time.Parse("2006-01-02", options[j][1])
		return dateI.Before(dateJ)
	})

	// Display all available items
	c := cases.Title(language.English)
	fmt.Println("\nAvailable " + c.String(relationship) + ":")
	fmt.Printf("%-5s %-60s %-12s %-20s\n", "NO.", "NAME", "DATE", "ID")

	for i, v := range options {
		name := v[0]
		nameRunes := []rune(name)
		// Handle long names by wrapping
		if len(nameRunes) <= 60 {
			fmt.Printf("%-5d %-60s %-12s %-20s\n", i+1, name, v[1], v[2])
		} else {
			fmt.Printf("%-5d %-60s %-12s %-20s\n", i+1, string(nameRunes[:60]), v[1], v[2])

			remaining := nameRunes[60:]
			for len(remaining) > 0 {
				if len(remaining) <= 60 {
					fmt.Printf("%-5s %-60s\n", "", string(remaining))
					break
				} else {
					fmt.Printf("%-5s %-60s\n", "", string(remaining[:60]))
					remaining = remaining[60:]
				}
			}
		}

		urls = append(urls, v[3])
	}

	// Automatically select all if artist_select flag is true
	if artist_select {
		fmt.Println("You have selected all options:")
		return urls, nil
	}

	// Ask user input for selection (manual or range)
	reader := bufio.NewReader(os.Stdin)
	fmt.Println("\nSelect from the " + relationship + " options above (multiple options separated by commas, ranges supported, or type 'all' to select all)")
	fmt.Print("Enter your choice: ")
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)

	if input == "" {
		fmt.Println("No option selected, skipping...")
		return []string{}, nil
	}
	if input == "all" {
		fmt.Println("You have selected all options:")
		return urls, nil
	}

	// Parse numeric or range input
	selectedOptions := [][]string{}
	parts := strings.Split(input, ",")
	for _, part := range parts {
		if strings.Contains(part, "-") {
			rangeParts := strings.Split(part, "-")
			selectedOptions = append(selectedOptions, rangeParts)
		} else {
			selectedOptions = append(selectedOptions, []string{part})
		}
	}

	// Process user-selected indices
	fmt.Println("You have selected the following options:")
	for _, opt := range selectedOptions {
		if len(opt) == 1 {
			num, err := strconv.Atoi(opt[0])
			if err != nil {
				fmt.Println("Invalid option:", opt[0])
				continue
			}
			if num > 0 && num <= len(options) {
				fmt.Println(options[num-1])
				args = append(args, urls[num-1])
			} else {
				fmt.Println("Option out of range:", opt[0])
			}
		} else if len(opt) == 2 {
			start, err1 := strconv.Atoi(opt[0])
			end, err2 := strconv.Atoi(opt[1])
			if err1 != nil || err2 != nil {
				fmt.Println("Invalid range:", opt)
				continue
			}
			if start < 1 || end > len(options) || start > end {
				fmt.Println("Range out of range:", opt)
				continue
			}
			for i := start; i <= end; i++ {
				fmt.Println(options[i-1])
				args = append(args, urls[i-1])
			}
		} else {
			fmt.Println("Invalid option:", opt)
		}
	}
	return args, nil
}

// =================== FILE WRITING ===================

// writeCover downloads and saves cover art image in the configured format (jpg/png/original)
func writeCover(sanAlbumFolder, name string, url string) (string, error) {
	originalUrl := url
	var ext string
	var covPath string

	// Determine output extension and destination path
	if Config.CoverFormat == "original" {
		ext = strings.Split(url, "/")[len(strings.Split(url, "/"))-2]
		ext = ext[strings.LastIndex(ext, ".")+1:]
		covPath = filepath.Join(sanAlbumFolder, name+"."+ext)
	} else {
		covPath = filepath.Join(sanAlbumFolder, name+"."+Config.CoverFormat)
	}
	exists, err := fileExists(covPath)
	if err != nil {
		fmt.Println("Failed to check if cover exists.")
		return "", err
	}
	if exists {
		_ = os.Remove(covPath)
	}

	// Replace resolution tokens in URL (e.g., {w}x{h})
	if Config.CoverFormat == "png" {
		re := regexp.MustCompile(`\{w\}x\{h\}`)
		parts := re.Split(url, 2)
		url = parts[0] + "{w}x{h}" + strings.Replace(parts[1], ".jpg", ".png", 1)
	}
	url = strings.Replace(url, "{w}x{h}", Config.CoverSize, 1)
	if Config.CoverFormat == "original" {
		url = strings.Replace(url, "is1-ssl.mzstatic.com/image/thumb", "a5.mzstatic.com/us/r1000/0", 1)
		url = url[:strings.LastIndex(url, "/")]
	}

	// Request and download the image
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36")
	do, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer do.Body.Close()

	if do.StatusCode != http.StatusOK {
		if Config.CoverFormat == "original" {
			fmt.Println("Failed to get cover, falling back to " + ext + " url.")
			splitByDot := strings.Split(originalUrl, ".")
			last := splitByDot[len(splitByDot)-1]
			fallback := originalUrl[:len(originalUrl)-len(last)] + ext
			fallback = strings.Replace(fallback, "{w}x{h}", Config.CoverSize, 1)
			fmt.Println("Fallback URL:", fallback)
			req, err = http.NewRequest("GET", fallback, nil)
			if err != nil {
				fmt.Println("Failed to create request for fallback url.")
				return "", err
			}
			req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36")
			do, err = http.DefaultClient.Do(req)
			if err != nil {
				fmt.Println("Failed to get cover from fallback url.")
				return "", err
			}
			defer do.Body.Close()
			if do.StatusCode != http.StatusOK {
				fmt.Println(fallback)
				return "", errors.New(do.Status)
			}
		} else {
			return "", errors.New(do.Status)
		}
	}
	f, err := os.Create(covPath)
	if err != nil {
		return "", err
	}
	defer f.Close()
	_, err = io.Copy(f, do.Body)
	if err != nil {
		return "", err
	}
	return covPath, nil
}

// writeLyrics saves lyric data to a file in the specified folder and format
func writeLyrics(sanAlbumFolder, filename string, lrc string) error {
	lyricspath := filepath.Join(sanAlbumFolder, filename)
	f, err := os.Create(lyricspath)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(lrc)
	if err != nil {
		return err
	}
	return nil
}

func contains(slice []string, item string) bool {
	return slices.Contains(slice, item)
}

type SearchResultItem struct {
	Type   string
	Name   string
	Detail string
	URL    string
	ID     string
}

type QualityOption struct {
	ID          string
	Description string
}

// =================== SEARCH AND QUALITY SELECTION ===================

// setDlFlags sets the global flags for download quality
func setDlFlags(quality string) {
	dl_atmos = false
	dl_aac = false

	switch quality {
	case "atmos":
		dl_atmos = true
		fmt.Println("Quality set to: Dolby Atmos")
	case "aac":
		dl_aac = true
		*aac_type = "aac"
		fmt.Println("Quality set to: High-Quality (AAC)")
	case "alac":
		fmt.Println("Quality set to: Lossless (ALAC)")
	}
}

// promptForQuality displays a menu for user to select audio quality
func promptForQuality(item SearchResultItem) (string, error) {
	// Skip for artist type (no quality needed)
	if item.Type == "Artist" {
		fmt.Println("Artist selected. Proceeding to list all albums/videos.")
		return "default", nil
	}
	fmt.Printf("\nFetching available qualities for: %s\n", item.Name)

	// Define available qualities
	qualities := []QualityOption{
		{ID: "alac", Description: "Lossless (ALAC)"},
		{ID: "aac", Description: "High-Quality (AAC)"},
		{ID: "atmos", Description: "Dolby Atmos"},
	}
	qualityOptions := []string{}
	for _, q := range qualities {
		qualityOptions = append(qualityOptions, q.Description)
	}

	// CLI prompt using the survey library
	prompt := &survey.Select{
		Message:  "Select a quality to download:",
		Options:  qualityOptions,
		PageSize: 5,
	}
	selectedIndex := 0
	err := survey.AskOne(prompt, &selectedIndex)
	if err != nil {
		return "", nil
	}
	return qualities[selectedIndex].ID, nil
}

// handleSearch performs Apple Music search by album, song, or artist
// Provides a CLI interface for selecting one of the results
func handleSearch(searchType string, queryParts []string, token string) (string, error) {
	query := strings.Join(queryParts, " ")

	// Validate search type
	validTypes := map[string]bool{"album": true, "song": true, "artist": true}
	if !validTypes[searchType] {
		return "", fmt.Errorf("invalid search type: %s. Use 'album', 'song', or 'artist'", searchType)
	}
	fmt.Printf("Searching for %ss: \"%s\" in storefront \"%s\"\n", searchType, query, Config.Storefront)
	offset := 0
	limit := 15
	apiSearchType := searchType + "s"

	for {
		// Query AMP API search endpoint
		searchResp, err := ampapi.Search(Config.Storefront, query, apiSearchType, Config.Language, token, limit, offset)
		if err != nil {
			return "", fmt.Errorf("error fetching search results: %w", err)
		}
		var items []SearchResultItem
		var displayOptions []string
		hasNext := false

		// Paging opstions
		const prevPageOpt = "< Previous Page"
		const nextPageOpt = "> Next Page"
		if offset > 0 {
			displayOptions = append(displayOptions, prevPageOpt)
		}

		// Display result list depending on type
		switch searchType {
		case "album":
			if searchResp.Results.Albums != nil {
				for _, item := range searchResp.Results.Albums.Data {
					year := ""
					if len(item.Attributes.ReleaseDate) >= 4 {
						year = item.Attributes.ReleaseDate[:4]
					}
					trackInfo := fmt.Sprintf("%d tracks", item.Attributes.TrackCount)
					detail := fmt.Sprintf("%s (%s, %s)", item.Attributes.ArtistName, year, trackInfo)
					displayOptions = append(displayOptions, fmt.Sprintf("%s - %s", item.Attributes.Name, detail))
					items = append(items, SearchResultItem{Type: "Album", URL: item.Attributes.URL, ID: item.ID})
				}
				hasNext = searchResp.Results.Albums.Next != ""
			}
		case "song":
			if searchResp.Results.Songs != nil {
				for _, item := range searchResp.Results.Songs.Data {
					detail := fmt.Sprintf("%s (%s)", item.Attributes.ArtistName, item.Attributes.AlbumName)
					displayOptions = append(displayOptions, fmt.Sprintf("%s - %s", item.Attributes.Name, detail))
					items = append(items, SearchResultItem{Type: "Song", URL: item.Attributes.URL, ID: item.ID})
				}
				hasNext = searchResp.Results.Songs.Next != ""
			}
		case "artist":
			if searchResp.Results.Artists != nil {
				for _, item := range searchResp.Results.Artists.Data {
					detail := ""
					if len(item.Attributes.GenreNames) > 0 {
						detail = strings.Join(item.Attributes.GenreNames, ", ")
					}
					displayOptions = append(displayOptions, fmt.Sprintf("%s (%s)", item.Attributes.Name, detail))
					items = append(items, SearchResultItem{Type: "Artist", URL: item.Attributes.URL, ID: item.ID})
				}
				hasNext = searchResp.Results.Artists.Next != ""
			}
		}

		// If no result found
		if len(items) == 0 && offset == 0 {
			fmt.Println("No results found.")
			return "", nil
		}
		if hasNext {
			displayOptions = append(displayOptions, nextPageOpt)
		}

		// CLI selection prompt
		prompt := &survey.Select{
			Message:  "Use arrow keys to navigate, Enter to select:",
			Options:  displayOptions,
			PageSize: limit,
		}
		selectedIndex := 0
		err = survey.AskOne(prompt, &selectedIndex)
		if err != nil {
			return "", nil
		}
		selectedOption := displayOptions[selectedIndex]

		// Handle pagination
		if selectedOption == nextPageOpt {
			offset += limit
			continue
		}
		if selectedOption == prevPageOpt {
			offset -= limit
			continue
		}

		// Determine selected item index
		itemIndex := selectedIndex
		if offset > 0 {
			itemIndex--
		}
		selectedItem := items[itemIndex]

		// If selected item is a song, set download flag
		if selectedItem.Type == "Song" {
			dl_song = true
		}

		// Ask user for quality selection
		quality, err := promptForQuality(selectedItem)
		if err != nil {
			return "", fmt.Errorf("could not process quality selection: %w", err)
		}
		if quality == "" {
			fmt.Println("Selection cancelled.")
			return "", nil
		}
		// Apply global flags based on quality
		if quality != "default" {
			setDlFlags(quality)
		}
		return selectedItem.URL, nil
	}
}

// =================== CORE DOWNLOAD FUNCTIONS ===================

// ripTrack handles the download of a single song or music video track.
// It detects available qualities, fetches metadata, and embeds cover art and lyrics.
func ripTrack(track *task.Track, token string, mediaUserToken string) {
	var err error
	counter.Total++
	fmt.Printf("\nTrack %d of %d: %s\n", track.TaskNum, track.TaskTotal, track.Type)

	// For playlist, optionally get album data if configured
	if track.PreType == "playlists" && Config.UseSongInfoForPlaylist {
		track.GetAlbumData(token)
	}

	// Handle music video downloads
	if track.Type == "music-videos" {
		if len(mediaUserToken) <= 50 {
			fmt.Println("media-user-token is not set, skip MV dl")
			counter.Success++
			return
		}
		if _, err := exec.LookPath("mp4decrypt"); err != nil {
			fmt.Println("mp4decrypt is not found, skip MV dl")
			counter.Success++
			return
		}
		err := mvDownloader(track.ID, track.SaveDir, token, track.Storefront, mediaUserToken, track)
		if err != nil {
			fmt.Println("Failed to dl MV:", err)
			counter.Error++
			return
		}
		counter.Success++
		return
	}

	// Determine if we need to download AAC-LC format
	needDlAacLc := false
	if dl_aac && Config.AacType == "aac-lc" {
		needDlAacLc = true
	}

	// Check availability and fallback to AAC-LC if needed
	if track.WebM3u8 == "" && !needDlAacLc {
		if dl_atmos {
			fmt.Println("Unavailable")
			counter.Unavailable++
			return
		}
		fmt.Println("Unavailable, trying to dl aac-lc")
		needDlAacLc = true
	}

	// Check if we need to get enhanced HLS m3u8
	needCheck := false
	if Config.GetM3u8Mode == "all" {
		needCheck = true
	} else if Config.GetM3u8Mode == "hires" && contains(track.Resp.Attributes.AudioTraits, "hi-res-lossless") {
		needCheck = true
	}

	var EnhancedHls_m3u8 string
	if needCheck && !needDlAacLc {
		EnhancedHls_m3u8, _ = checkM3u8(track.ID, "song")
		if strings.HasSuffix(EnhancedHls_m3u8, ".m3u8") {
			track.DeviceM3u8 = EnhancedHls_m3u8
			track.M3u8 = EnhancedHls_m3u8
		}
	}

	// Extract quality information for filename formatting
	var Quality string
	if strings.Contains(Config.SongFileFormat, "Quality") {
		if dl_atmos {
			Quality = fmt.Sprintf("%dKbps", Config.AtmosMax-2000)
		} else if needDlAacLc {
			Quality = "256Kbps"
		} else {
			_, Quality, err = extractMedia(track.M3u8, true)
			if err != nil {
				fmt.Println("Failed to extract quality from manifest.\n", err)
				counter.Error++
				return
			}
		}
	}
	track.Quality = Quality

	// Build tag string for explicit/clean and Apple Master indicators
	stringsToJoin := []string{}
	if track.Resp.Attributes.IsAppleDigitalMaster {
		if Config.AppleMasterChoice != "" {
			stringsToJoin = append(stringsToJoin, Config.AppleMasterChoice)
		}
	}
	if track.Resp.Attributes.ContentRating == "explicit" {
		if Config.ExplicitChoice != "" {
			stringsToJoin = append(stringsToJoin, Config.ExplicitChoice)
		}
	}
	if track.Resp.Attributes.ContentRating == "clean" {
		if Config.CleanChoice != "" {
			stringsToJoin = append(stringsToJoin, Config.CleanChoice)
		}
	}
	Tag_string := strings.Join(stringsToJoin, " ")

	// Generate filename using template replacement
	songName := strings.NewReplacer(
		"{SongId}", track.ID,
		"{SongNumer}", fmt.Sprintf("%02d", track.TaskNum),
		"{SongName}", LimitString(track.Resp.Attributes.Name),
		"{DiscNumber}", fmt.Sprintf("%0d", track.Resp.Attributes.DiscNumber),
		"{TrackNumber}", fmt.Sprintf("%0d", track.Resp.Attributes.TrackNumber),
		"{Quality}", Quality,
		"{Tag}", Tag_string,
		"{Codec}", track.Codec,
	).Replace(Config.SongFileFormat)
	fmt.Println(songName)
	filename := fmt.Sprintf("%s.m4a", forbiddenNames.ReplaceAllString(songName, "_"))
	track.SaveName = filename
	trackPath := filepath.Join(track.SaveDir, track.SaveName)
	lrcFilename := fmt.Sprintf("%s.%s", forbiddenNames.ReplaceAllString(songName, "_"), Config.LrcFormat)

	// Fetch and process lyrics
	var lrc string = ""
	if Config.EmbedLrc || Config.SaveLrcFile {
		lrcStr, err := lyrics.Get(track.Storefront, track.ID, Config.LrcType, Config.Language, Config.LrcFormat, token, mediaUserToken)
		if err != nil {
			fmt.Println(err)
		} else {
			if Config.SaveLrcFile {
				err := writeLyrics(track.SaveDir, lrcFilename, lrcStr)
				if err != nil {
					fmt.Printf("Failed to write lyrics")
				}
			}
			if Config.EmbedLrc {
				lrc = lrcStr
			}
		}
	}

	// Check if file already exists
	exists, err := fileExists(trackPath)
	if err != nil {
		fmt.Println("Failed to check if track exists.")
	}
	if exists {
		fmt.Println("Track already exists locally.")
		counter.Success++
		okDict[track.PreID] = append(okDict[track.PreID], track.TaskNum)
		return
	}

	// Download the track based on quality selection
	if needDlAacLc {
		if len(mediaUserToken) <= 50 {
			fmt.Println("Invalid media-user-token")
			counter.Error++
			return
		}
		_, err := runv3.Run(track.ID, trackPath, token, mediaUserToken, false)
		if err != nil {
			fmt.Println("Failed to dl aac-lc:", err)
			if err.Error() == "Unavailable" {
				counter.Unavailable++
				return
			}
			counter.Error++
			return
		}
	} else {
		trackM3u8Url, _, err := extractMedia(track.M3u8, false)
		if err != nil {
			fmt.Println("Failed to extract info from manifest:", err)
			counter.Unavailable++
			return
		}
		err = runv2.Run(track.ID, trackM3u8Url, trackPath, Config)
		if err != nil {
			fmt.Println("Failed to run v2:", err)
			counter.Error++
			return
		}
	}

	// Update total size counter
	fileInfo, err := os.Stat(trackPath)
	if err == nil {
		counter.TotalSize += fileInfo.Size()
	}

	// Prepare and embed metadata tags
	tags := []string{
		"tool=",
		fmt.Sprintf("artist=%s", track.Resp.Attributes.ArtistName),
	}
	if Config.EmbedCover {
		// Handle cover art for playlists
		if (strings.Contains(track.PreID, "pl.") || strings.Contains(track.PreID, "ra.")) && Config.DlAlbumcoverForPlaylist {
			track.CoverPath, err = writeCover(track.SaveDir, track.ID, track.Resp.Attributes.Artwork.URL)
			if err != nil {
				fmt.Println("Failed to write cover.")
			}
		}
		tags = append(tags, fmt.Sprintf("cover=%s", track.CoverPath))
	}
	tagsString := strings.Join(tags, ":")
	cmd := exec.Command("MP4Box", "-itags", tagsString, trackPath)
	if err := cmd.Run(); err != nil {
		fmt.Printf("Embed failed: %v\n", err)
		counter.Error++
		return
	}

	// Clean up temporary cover files for playlists
	if (strings.Contains(track.PreID, "pl.") || strings.Contains(track.PreID, "ra.")) && Config.DlAlbumcoverForPlaylist {
		if err := os.Remove(track.CoverPath); err != nil {
			fmt.Printf("Error deleting file: %s\n", track.CoverPath)
			counter.Error++
			return
		}
	}

	// Write comprehensive MP4 tags and update counters
	track.SavePath = trackPath
	err = writeMP4Tags(track, lrc)
	if err != nil {
		fmt.Println("Failed to write tags in media:", err)
		counter.Unavailable++
		return
	}
	counter.Success++
	okDict[track.PreID] = append(okDict[track.PreID], track.TaskNum)
}

// ripstation handles the download of Apple Musi radio stations
// Stations can be either live streams or curated station playlists
func ripStation(albumId string, token string, storefront string, mediaUserToken string) error {
	// Initialize station object and fetch metadata
	station := task.NewStation(storefront, albumId)
	err := station.GetResp(mediaUserToken, token, Config.Language)
	if err != nil {
		return err
	}
	fmt.Println(" -", station.Type)
	meta := station.Resp

	// Determine codec based on global download flags
	var Codec string
	if dl_atmos {
		Codec = "ATMOS"
	} else if dl_aac {
		Codec = "AAC"
	} else {
		Codec = "ALAC"
	}
	station.Codec = Codec

	// Create artist folder structure for station
	var singerFoldername string
	if Config.ArtistFolderFormat != "" {
		singerFoldername = strings.NewReplacer(
			"{ArtistName}", "Apple Music Station",
			"{ArtistId}", "",
			"{UrlArtistName}", "Apple Music Station",
		).Replace(Config.ArtistFolderFormat)
		if strings.HasSuffix(singerFoldername, ".") {
			singerFoldername = strings.ReplaceAll(singerFoldername, ".", "")
		}
		singerFoldername = strings.TrimSpace(singerFoldername)
		fmt.Println(singerFoldername)
	}

	// Determine save folder based on quality selection
	singerFolder := filepath.Join(Config.AlacSaveFolder, forbiddenNames.ReplaceAllString(singerFoldername, "_"))
	if dl_atmos {
		singerFolder = filepath.Join(Config.AtmosSaveFolder, forbiddenNames.ReplaceAllString(singerFoldername, "_"))
	}
	if dl_aac {
		singerFolder = filepath.Join(Config.AacSaveFolder, forbiddenNames.ReplaceAllString(singerFoldername, "_"))
	}
	os.MkdirAll(singerFolder, os.ModePerm)
	station.SaveDir = singerFolder

	// Generate playlist folder name using template
	playlistFolder := strings.NewReplacer(
		"{ArtistName}", "Apple Music Station",
		"{PlaylistName}", LimitString(station.Name),
		"{PlaylistId}", station.ID,
		"{Quality}", "",
		"{Codec}", Codec,
		"{Tag}", "",
	).Replace(Config.PlaylistFolderFormat)
	if strings.HasSuffix(playlistFolder, ".") {
		playlistFolder = strings.ReplaceAll(playlistFolder, ".", "")
	}
	playlistFolder = strings.TrimSpace(playlistFolder)
	playlistFolderPath := filepath.Join(singerFolder, forbiddenNames.ReplaceAllString(playlistFolder, "_"))
	os.MkdirAll(playlistFolderPath, os.ModePerm)
	station.SaveName = playlistFolder
	fmt.Println(playlistFolder)

	// Download and save station cover art
	covPath, err := writeCover(playlistFolderPath, "cover", meta.Data[0].Attributes.Artwork.URL)
	if err != nil {
		fmt.Println("Failed to write cover.")
	}
	station.CoverPath = covPath

	// Handle animated artwork if available and configured
	if Config.SaveAnimatedArtwork && meta.Data[0].Attributes.EditorialVideo.MotionSquare.Video != "" {
		fmt.Println("Found Animation Artwork.")

		// Download square animated artwork
		motionvideoUrlSquare, err := extractVideo(meta.Data[0].Attributes.EditorialVideo.MotionSquare.Video)
		if err != nil {
			fmt.Println("no motion video square.\n", err)
		} else {
			exists, err := fileExists(filepath.Join(playlistFolderPath, "square_animated_artwork.mp4"))
			if err != nil {
				fmt.Println("Failed to check if animated artwork square exists.")
			}
			if exists {
				fmt.Println("Animated artwork square already exists locally.")
			} else {
				fmt.Println("Animation Artwork Square Downloading...")
				cmd := exec.Command("ffmpeg", "-loglevel", "quiet", "-y", "-i", motionvideoUrlSquare, "-c", "copy", filepath.Join(playlistFolderPath, "square_animated_artwork.mp4"))
				if err := cmd.Run(); err != nil {
					fmt.Printf("animated artwork square dl err: %v\n", err)
				} else {
					fmt.Println("Animation Artwork Square Downloaded")
				}
			}
		}

		// Convert to GIF for Emby compatibility if configured
		if Config.EmbyAnimatedArtwork {
			cmd3 := exec.Command("ffmpeg", "-i", filepath.Join(playlistFolderPath, "square_animated_artwork.mp4"), "-vf", "scale=440:-1", "-r", "24", "-f", "gif", filepath.Join(playlistFolderPath, "folder.jpg"))
			if err := cmd3.Run(); err != nil {
				fmt.Printf("animated artwork square to gif err: %v\n", err)
			}
		}
	}

	// Handle live stream stations (single continuous stream)
	if station.Type == "stream" {
		counter.Total++
		// Check if already downloaded
		if isInArray(okDict[station.ID], 1) {
			counter.Success++
			return nil
		}

		// Generate stream filename
		songName := strings.NewReplacer(
			"{SongId}", station.ID,
			"{SongNumer}", "01",
			"{SongName}", LimitString(station.Name),
			"{DiscNumber}", "1",
			"{TrackNumber}", "1",
			"{Quality}", "256Kbps",
			"{Tag}", "",
			"{Codec}", "AAC",
		).Replace(Config.SongFileFormat)
		fmt.Println(songName)
		trackPath := filepath.Join(playlistFolderPath, fmt.Sprintf("%s.m4a", forbiddenNames.ReplaceAllString(songName, "_")))

		// Check if stream already exists
		exists, _ := fileExists(trackPath)
		if exists {
			counter.Success++
			okDict[station.ID] = append(okDict[station.ID], 1)

			fmt.Println("Radio already exists locally.")
			return nil
		}

		// Get station stream assets URL
		assetsUrl, err := ampapi.GetStationAssetsUrl(station.ID, mediaUserToken, token)
		if err != nil {
			fmt.Println("Failed to get station assets url.", err)
			counter.Error++
			return err
		}

		// Construct m3u8 URL for 256kbps stream
		trackM3U8 := strings.ReplaceAll(assetsUrl, "index.m3u8", "256/prog_index.m3u8")
		keyAndUrls, _ := runv3.Run(station.ID, trackM3U8, token, mediaUserToken, true)
		err = runv3.ExtMvData(keyAndUrls, trackPath)
		if err != nil {
			fmt.Println("Failed to download station stream.", err)
			counter.Error++
			return err
		}

		// Update file size statistics
		fileInfo, err := os.Stat(trackPath)
		if err == nil {
			counter.TotalSize += fileInfo.Size()
		}

		// Embed metadata tags
		tags := []string{
			"tool=",
			"disk=1/1",
			"track=1",
			"tracknum=1/1",
			fmt.Sprintf("artist=%s", "Apple Music Station"),
			fmt.Sprintf("performer=%s", "Apple Music Station"),
			fmt.Sprintf("album_artist=%s", "Apple Music Station"),
			fmt.Sprintf("album=%s", station.Name),
			fmt.Sprintf("title=%s", station.Name),
		}
		if Config.EmbedCover {
			tags = append(tags, fmt.Sprintf("cover=%s", station.CoverPath))
		}
		tagsString := strings.Join(tags, ":")
		cmd := exec.Command("MP4Box", "-itags", tagsString, trackPath)
		if err := cmd.Run(); err != nil {
			fmt.Printf("Embed failed: %v\n", err)
		}
		counter.Success++
		okDict[station.ID] = append(okDict[station.ID], 1)
		return nil
	}

	// Handle curated station playlists (multiple tracks)
	// Set cover path and codec for all tracks in the station
	for i := range station.Tracks {
		station.Tracks[i].CoverPath = covPath
		station.Tracks[i].SaveDir = playlistFolderPath
		station.Tracks[i].Codec = Codec
	}

	// Create track selection array
	trackTotal := len(station.Tracks)
	arr := make([]int, trackTotal)
	for i := 0; i < trackTotal; i++ {
		arr[i] = i + 1
	}
	var selected []int

	// For stations, always select all tracks (no selective download)
	if true {
		selected = arr
	}

	// Download each selected track
	for i := range station.Tracks {
		i++
		if isInArray(selected, i) {
			ripTrack(&station.Tracks[i-1], token, mediaUserToken)
		}
	}
	return nil
}

// ripAlbum handles the download of entire music albums
// It processes album metadata, creates folder structure, and downloads all tracks
func ripAlbum(albumId string, token string, storefront string, mediaUserToken string, urlArg_i string) error {
	// Initialize album object and fetch metadata from Apple Music API
	album := task.NewAlbum(storefront, albumId)
	err := album.GetResp(token, Config.Language)
	if err != nil {
		fmt.Println("Failed to get album response.")
		return err
	}
	meta := album.Resp

	// Debug mode: Display detailed track information without downloading
	if debug_mode {
		fmt.Println(meta.Data[0].Attributes.ArtistName)
		fmt.Println(meta.Data[0].Attributes.Name)

		// Iterate through all tracks in the album
		for trackNum, track := range meta.Data[0].Relationships.Tracks.Data {
			trackNum++
			fmt.Printf("\nTrack %d of %d:\n", trackNum, len(meta.Data[0].Relationships.Tracks.Data))
			fmt.Printf("%02d. %s\n", trackNum, track.Attributes.Name)

			// Fetch detailed manifest for each track
			manifest, err := ampapi.GetSongResp(storefront, track.ID, album.Language, token)
			if err != nil {
				fmt.Printf("Failed to get manifest for track %d: %v\n", trackNum, err)
				continue
			}

			// Determine the best available m3u8 URL
			var m3u8Url string
			if manifest.Data[0].Attributes.ExtendedAssetUrls.EnhancedHls != "" {
				m3u8Url = manifest.Data[0].Attributes.ExtendedAssetUrls.EnhancedHls
			}

			// Check if we need enhanced HLS streams
			needCheck := false
			if Config.GetM3u8Mode == "all" {
				needCheck = true
			} else if Config.GetM3u8Mode == "hires" && contains(track.Attributes.AudioTraits, "hi-res-lossless") {
				needCheck = true
			}

			// Attempt to get device-enhanced m3u8 if needed
			if needCheck {
				fullM3u8Url, err := checkM3u8(track.ID, "song")
				if err == nil && strings.HasSuffix(fullM3u8Url, ".m3u8") {
					m3u8Url = fullM3u8Url
				} else {
					fmt.Println("Failed to get best quality m3u8 from device m3u8 port, will use m3u8 from Web API")
				}
			}

			// Extract and display quality information
			_, _, err = extractMedia(m3u8Url, true)
			if err != nil {
				fmt.Printf("Failed to extract quality info for track %d: %v\n", trackNum, err)
				continue
			}
		}
		return nil
	}

	// Determine audio codec based on global download flags
	var Codec string
	if dl_atmos {
		Codec = "ATMOS"
	} else if dl_aac {
		Codec = "AAC"
	} else {
		Codec = "ALAC"
	}
	album.Codec = Codec

	// Create artist folder structure
	var singerFoldername string
	if Config.ArtistFolderFormat != "" {
		if len(meta.Data[0].Relationships.Artists.Data) > 0 {
			// Use actual artist information if available
			singerFoldername = strings.NewReplacer(
				"{UrlArtistName}", LimitString(meta.Data[0].Attributes.ArtistName),
				"{ArtistName}", LimitString(meta.Data[0].Attributes.ArtistName),
				"{ArtistId}", meta.Data[0].Relationships.Artists.Data[0].ID,
			).Replace(Config.ArtistFolderFormat)
		} else {
			// Fallback to album artist name
			singerFoldername = strings.NewReplacer(
				"{UrlArtistName}", LimitString(meta.Data[0].Attributes.ArtistName),
				"{ArtistName}", LimitString(meta.Data[0].Attributes.ArtistName),
				"{ArtistId}", "",
			).Replace(Config.ArtistFolderFormat)
		}
		// Clean up folder name (remove trailing dots and trim spaces)
		if strings.HasSuffix(singerFoldername, ".") {
			singerFoldername = strings.ReplaceAll(singerFoldername, ".", "")
		}
		singerFoldername = strings.TrimSpace(singerFoldername)
		fmt.Println(singerFoldername)
	}

	// Determine save folder based on audio quality
	singerFolder := filepath.Join(Config.AlacSaveFolder, forbiddenNames.ReplaceAllString(singerFoldername, "_"))
	if dl_atmos {
		singerFolder = filepath.Join(Config.AtmosSaveFolder, forbiddenNames.ReplaceAllString(singerFoldername, "_"))
	}
	if dl_aac {
		singerFolder = filepath.Join(Config.AacSaveFolder, forbiddenNames.ReplaceAllString(singerFoldername, "_"))
	}
	os.MkdirAll(singerFolder, os.ModePerm)
	album.SaveDir = singerFolder

	// Extract quality information for folder naming
	var Quality string
	if strings.Contains(Config.AlbumFolderFormat, "Quality") {
		if dl_atmos {
			Quality = fmt.Sprintf("%dKbps", Config.AtmosMax-2000)
		} else if dl_aac && Config.AacType == "aac-lc" {
			Quality = "256Kbps"
		} else {
			// Get quality info from the first track in the album
			manifest1, err := ampapi.GetSongResp(storefront, meta.Data[0].Relationships.Tracks.Data[0].ID, album.Language, token)
			if err != nil {
				fmt.Println("Failed to get manifest.\n", err)
			} else {
				if manifest1.Data[0].Attributes.ExtendedAssetUrls.EnhancedHls == "" {

					// Fallback to AAC if no enhanced HLS available
					Codec = "AAC"
					Quality = "256Kbps"
				} else {
					needCheck := false

					// Check if we need enhanced HLS streams
					if Config.GetM3u8Mode == "all" {
						needCheck = true
					} else if Config.GetM3u8Mode == "hires" && contains(meta.Data[0].Relationships.Tracks.Data[0].Attributes.AudioTraits, "hi-res-lossless") {
						needCheck = true
					}

					// Attempt to get device-enhanced m3u8
					var EnhancedHls_m3u8 string
					if needCheck {
						EnhancedHls_m3u8, _ = checkM3u8(meta.Data[0].Relationships.Tracks.Data[0].ID, "album")
						if strings.HasSuffix(EnhancedHls_m3u8, ".m3u8") {
							manifest1.Data[0].Attributes.ExtendedAssetUrls.EnhancedHls = EnhancedHls_m3u8
						}
					}

					// Extract quality information from the manifest
					_, Quality, err = extractMedia(manifest1.Data[0].Attributes.ExtendedAssetUrls.EnhancedHls, true)
					if err != nil {
						fmt.Println("Failed to extract quality from manifest.\n", err)
					}
				}
			}
		}
	}

	// Build tag string for album metadata (explicit/clean, Apple Master, etc.)
	stringsToJoin := []string{}
	if meta.Data[0].Attributes.IsAppleDigitalMaster || meta.Data[0].Attributes.IsMasteredForItunes {
		if Config.AppleMasterChoice != "" {
			stringsToJoin = append(stringsToJoin, Config.AppleMasterChoice)
		}
	}
	if meta.Data[0].Attributes.ContentRating == "explicit" {
		if Config.ExplicitChoice != "" {
			stringsToJoin = append(stringsToJoin, Config.ExplicitChoice)
		}
	}
	if meta.Data[0].Attributes.ContentRating == "clean" {
		if Config.CleanChoice != "" {
			stringsToJoin = append(stringsToJoin, Config.CleanChoice)
		}
	}
	Tag_string := strings.Join(stringsToJoin, " ")

	// Generate album folder name using template replacement
	var albumFolderName string
	albumFolderName = strings.NewReplacer(
		"{ReleaseDate}", meta.Data[0].Attributes.ReleaseDate,
		"{ReleaseYear}", meta.Data[0].Attributes.ReleaseDate[:4],
		"{ArtistName}", LimitString(meta.Data[0].Attributes.ArtistName),
		"{AlbumName}", LimitString(meta.Data[0].Attributes.Name),
		"{UPC}", meta.Data[0].Attributes.Upc,
		"{RecordLabel}", meta.Data[0].Attributes.RecordLabel,
		"{Copyright}", meta.Data[0].Attributes.Copyright,
		"{AlbumId}", albumId,
		"{Quality}", Quality,
		"{Codec}", Codec,
		"{Tag}", Tag_string,
	).Replace(Config.AlbumFolderFormat)

	// Clean up album folder name
	if strings.HasSuffix(albumFolderName, ".") {
		albumFolderName = strings.ReplaceAll(albumFolderName, ".", "")
	}
	albumFolderName = strings.TrimSpace(albumFolderName)
	albumFolderPath := filepath.Join(singerFolder, forbiddenNames.ReplaceAllString(albumFolderName, "_"))
	os.MkdirAll(albumFolderPath, os.ModePerm)
	album.SaveName = albumFolderName
	fmt.Println(albumFolderName)

	// Save artist cover image if configured
	if Config.SaveArtistCover {
		if len(meta.Data[0].Relationships.Artists.Data) > 0 {
			_, err = writeCover(singerFolder, "folder", meta.Data[0].Relationships.Artists.Data[0].Attributes.Artwork.Url)
			if err != nil {
				fmt.Println("Failed to write artist cover.")
			}
		}
	}

	// Download and save album cover art
	covPath, err := writeCover(albumFolderPath, "cover", meta.Data[0].Attributes.Artwork.URL)
	if err != nil {
		fmt.Println("Failed to write cover.")
	}

	// Handle animated artwork if available and configured
	if Config.SaveAnimatedArtwork && meta.Data[0].Attributes.EditorialVideo.MotionDetailSquare.Video != "" {
		fmt.Println("Found Animation Artwork.")

		// Download square animated artwork
		motionvideoUrlSquare, err := extractVideo(meta.Data[0].Attributes.EditorialVideo.MotionDetailSquare.Video)
		if err != nil {
			fmt.Println("no motion video square.\n", err)
		} else {
			exists, err := fileExists(filepath.Join(albumFolderPath, "square_animated_artwork.mp4"))
			if err != nil {
				fmt.Println("Failed to check if animated artwork square exists.")
			}
			if exists {
				fmt.Println("Animated artwork square already exists locally.")
			} else {
				fmt.Println("Animation Artwork Square Downloading...")
				cmd := exec.Command("ffmpeg", "-loglevel", "quiet", "-y", "-i", motionvideoUrlSquare, "-c", "copy", filepath.Join(albumFolderPath, "square_animated_artwork.mp4"))
				if err := cmd.Run(); err != nil {
					fmt.Printf("animated artwork square dl err: %v\n", err)
				} else {
					fmt.Println("Animation Artwork Square Downloaded")
				}
			}
		}

		// Convert to GIF for Emby compatibility
		if Config.EmbyAnimatedArtwork {
			cmd3 := exec.Command("ffmpeg", "-i", filepath.Join(albumFolderPath, "square_animated_artwork.mp4"), "-vf", "scale=440:-1", "-r", "24", "-f", "gif", filepath.Join(albumFolderPath, "folder.jpg"))
			if err := cmd3.Run(); err != nil {
				fmt.Printf("animated artwork square to gif err: %v\n", err)
			}
		}

		// Download tall animated artwork
		motionvideoUrlTall, err := extractVideo(meta.Data[0].Attributes.EditorialVideo.MotionDetailTall.Video)
		if err != nil {
			fmt.Println("no motion video tall.\n", err)
		} else {
			exists, err := fileExists(filepath.Join(albumFolderPath, "tall_animated_artwork.mp4"))
			if err != nil {
				fmt.Println("Failed to check if animated artwork tall exists.")
			}
			if exists {
				fmt.Println("Animated artwork tall already exists locally.")
			} else {
				fmt.Println("Animation Artwork Tall Downloading...")
				cmd := exec.Command("ffmpeg", "-loglevel", "quiet", "-y", "-i", motionvideoUrlTall, "-c", "copy", filepath.Join(albumFolderPath, "tall_animated_artwork.mp4"))
				if err := cmd.Run(); err != nil {
					fmt.Printf("animated artwork tall dl err: %v\n", err)
				} else {
					fmt.Println("Animation Artwork Tall Downloaded")
				}
			}
		}
	}

	// Set common track properties for all tracks in the album
	for i := range album.Tracks {
		album.Tracks[i].CoverPath = covPath
		album.Tracks[i].SaveDir = albumFolderPath
		album.Tracks[i].Codec = Codec
	}

	// Create track selection array
	trackTotal := len(meta.Data[0].Relationships.Tracks.Data)
	arr := make([]int, trackTotal)
	for i := range trackTotal {
		arr[i] = i + 1
	}

	// Handle single song download mode
	if dl_song {
		if urlArg_i == "" {
			// No specific track ID provided in URL
		} else {
			// Find and download the specific track
			for i := range album.Tracks {
				if urlArg_i == album.Tracks[i].ID {
					ripTrack(&album.Tracks[i], token, mediaUserToken)
					return nil
				}
			}
		}
		return nil
	}

	// Determine which tracks to download
	var selected []int
	if !dl_select {
		// Download all tracks if selective mode is not enabled
		selected = arr
	} else {
		// Show interactive track selection
		selected = album.ShowSelect()
	}

	// Download selected tracks
	for i := range album.Tracks {
		i++
		// Skip already downloaded tracks
		if isInArray(okDict[albumId], i) {
			counter.Total++
			counter.Success++
			continue
		}
		// Download selected tracks
		if isInArray(selected, i) {
			ripTrack(&album.Tracks[i-1], token, mediaUserToken)
		}
	}
	return nil
}

// ripPlaylist handles the download of Apple Music playlists
// Similiar to album but with playlist-specific metadata and organization
func ripPlaylist(playlistId string, token string, storefront string, mediaUserToken string) error {
	// initialize playlist object and fetch metadata
	playlist := task.NewPlaylist(storefront, playlistId)
	err := playlist.GetResp(token, Config.Language)
	if err != nil {
		fmt.Println("Failed to get playlist response.")
		return err
	}
	meta := playlist.Resp

	// Debug mode: Display track information without downloading
	if debug_mode {
		fmt.Println(meta.Data[0].Attributes.ArtistName)
		fmt.Println(meta.Data[0].Attributes.Name)

		// Display quality information for each track
		for trackNum, track := range meta.Data[0].Relationships.Tracks.Data {
			trackNum++
			fmt.Printf("\nTrack %d of %d:\n", trackNum, len(meta.Data[0].Relationships.Tracks.Data))
			fmt.Printf("%02d. %s\n", trackNum, track.Attributes.Name)

			// Get track manifest
			manifest, err := ampapi.GetSongResp(storefront, track.ID, playlist.Language, token)
			if err != nil {
				fmt.Printf("Failed to get manifest for track %d: %v\n", trackNum, err)
				continue
			}

			// Determine best m3u8 URL
			var m3u8Url string
			if manifest.Data[0].Attributes.ExtendedAssetUrls.EnhancedHls != "" {
				m3u8Url = manifest.Data[0].Attributes.ExtendedAssetUrls.EnhancedHls
			}

			// Check for enchanced HLS if needed
			needCheck := false
			if Config.GetM3u8Mode == "all" {
				needCheck = true
			} else if Config.GetM3u8Mode == "hires" && contains(track.Attributes.AudioTraits, "hi-res-lossless") {
				needCheck = true
			}

			if needCheck {
				fullM3u8Url, err := checkM3u8(track.ID, "song")
				if err == nil && strings.HasSuffix(fullM3u8Url, ".m3u8") {
					m3u8Url = fullM3u8Url
				} else {
					fmt.Println("Failed to get best quality m3u8 from device m3u8 port, will use m3u8 from Web API")
				}
			}

			// Extract and display quality info
			_, _, err = extractMedia(m3u8Url, true)
			if err != nil {
				fmt.Printf("Failed to extract quality info for track %d: %v\n", trackNum, err)
				continue
			}
		}
		return nil
	}

	// Determine audio codec
	var Codec string
	if dl_atmos {
		Codec = "ATMOS"
	} else if dl_aac {
		Codec = "AAC"
	} else {
		Codec = "ALAC"
	}
	playlist.Codec = Codec

	// Create artist folder for playlists (uses "Apple Music" as artist)
	var singerFoldername string
	if Config.ArtistFolderFormat != "" {
		singerFoldername = strings.NewReplacer(
			"{ArtistName}", "Apple Music",
			"{ArtistId}", "",
			"{UrlArtistName}", "Apple Music",
		).Replace(Config.ArtistFolderFormat)
		if strings.HasSuffix(singerFoldername, ".") {
			singerFoldername = strings.ReplaceAll(singerFoldername, ".", "")
		}
		singerFoldername = strings.TrimSpace(singerFoldername)
		fmt.Println(singerFoldername)
	}

	// Determine save folder based on quality
	singerFolder := filepath.Join(Config.AlacSaveFolder, forbiddenNames.ReplaceAllString(singerFoldername, "_"))
	if dl_atmos {
		singerFolder = filepath.Join(Config.AtmosSaveFolder, forbiddenNames.ReplaceAllString(singerFoldername, "_"))
	}
	if dl_aac {
		singerFolder = filepath.Join(Config.AacSaveFolder, forbiddenNames.ReplaceAllString(singerFoldername, "_"))
	}
	os.MkdirAll(singerFolder, os.ModePerm)
	playlist.SaveDir = singerFolder

	// Extract quality information for folder naming
	var Quality string
	if strings.Contains(Config.AlbumFolderFormat, "Quality") {
		if dl_atmos {
			Quality = fmt.Sprintf("%dKbps", Config.AtmosMax-2000)
		} else if dl_aac && Config.AacType == "aac-lc" {
			Quality = "256Kbps"
		} else {
			// Get quality from first track
			manifest1, err := ampapi.GetSongResp(storefront, meta.Data[0].Relationships.Tracks.Data[0].ID, playlist.Language, token)
			if err != nil {
				fmt.Println("Failed to get manifest.\n", err)
			} else {
				if manifest1.Data[0].Attributes.ExtendedAssetUrls.EnhancedHls == "" {
					Codec = "AAC"
					Quality = "256Kbps"
				} else {
					needCheck := false

					if Config.GetM3u8Mode == "all" {
						needCheck = true
					} else if Config.GetM3u8Mode == "hires" && contains(meta.Data[0].Relationships.Tracks.Data[0].Attributes.AudioTraits, "hi-res-lossless") {
						needCheck = true
					}
					var EnhancedHls_m3u8 string
					if needCheck {
						EnhancedHls_m3u8, _ = checkM3u8(meta.Data[0].Relationships.Tracks.Data[0].ID, "album")
						if strings.HasSuffix(EnhancedHls_m3u8, ".m3u8") {
							manifest1.Data[0].Attributes.ExtendedAssetUrls.EnhancedHls = EnhancedHls_m3u8
						}
					}
					_, Quality, err = extractMedia(manifest1.Data[0].Attributes.ExtendedAssetUrls.EnhancedHls, true)
					if err != nil {
						fmt.Println("Failed to extract quality from manifest.\n", err)
					}
				}
			}
		}
	}

	// Build tag string for playlist metadata
	stringsToJoin := []string{}
	if meta.Data[0].Attributes.IsAppleDigitalMaster || meta.Data[0].Attributes.IsMasteredForItunes {
		if Config.AppleMasterChoice != "" {
			stringsToJoin = append(stringsToJoin, Config.AppleMasterChoice)
		}
	}
	if meta.Data[0].Attributes.ContentRating == "explicit" {
		if Config.ExplicitChoice != "" {
			stringsToJoin = append(stringsToJoin, Config.ExplicitChoice)
		}
	}
	if meta.Data[0].Attributes.ContentRating == "clean" {
		if Config.CleanChoice != "" {
			stringsToJoin = append(stringsToJoin, Config.CleanChoice)
		}
	}
	Tag_string := strings.Join(stringsToJoin, " ")

	// Generate playlist folder name
	playlistFolder := strings.NewReplacer(
		"{ArtistName}", "Apple Music",
		"{PlaylistName}", LimitString(meta.Data[0].Attributes.Name),
		"{PlaylistId}", playlistId,
		"{Quality}", Quality,
		"{Codec}", Codec,
		"{Tag}", Tag_string,
	).Replace(Config.PlaylistFolderFormat)
	if strings.HasSuffix(playlistFolder, ".") {
		playlistFolder = strings.ReplaceAll(playlistFolder, ".", "")
	}
	playlistFolder = strings.TrimSpace(playlistFolder)
	playlistFolderPath := filepath.Join(singerFolder, forbiddenNames.ReplaceAllString(playlistFolder, "_"))
	os.MkdirAll(playlistFolderPath, os.ModePerm)
	playlist.SaveName = playlistFolder
	fmt.Println(playlistFolder)

	// Download playlist cover art
	covPath, err := writeCover(playlistFolderPath, "cover", meta.Data[0].Attributes.Artwork.URL)
	if err != nil {
		fmt.Println("Failed to write cover.")
	}

	// Set track properties
	for i := range playlist.Tracks {
		playlist.Tracks[i].CoverPath = covPath
		playlist.Tracks[i].SaveDir = playlistFolderPath
		playlist.Tracks[i].Codec = Codec
	}

	// Handle animated artwork for playlists
	if Config.SaveAnimatedArtwork && meta.Data[0].Attributes.EditorialVideo.MotionDetailSquare.Video != "" {
		fmt.Println("Found Animation Artwork.")

		motionvideoUrlSquare, err := extractVideo(meta.Data[0].Attributes.EditorialVideo.MotionDetailSquare.Video)
		if err != nil {
			fmt.Println("no motion video square.\n", err)
		} else {
			exists, err := fileExists(filepath.Join(playlistFolderPath, "square_animated_artwork.mp4"))
			if err != nil {
				fmt.Println("Failed to check if animated artwork square exists.")
			}
			if exists {
				fmt.Println("Animated artwork square already exists locally.")
			} else {
				fmt.Println("Animation Artwork Square Downloading...")
				cmd := exec.Command("ffmpeg", "-loglevel", "quiet", "-y", "-i", motionvideoUrlSquare, "-c", "copy", filepath.Join(playlistFolderPath, "square_animated_artwork.mp4"))
				if err := cmd.Run(); err != nil {
					fmt.Printf("animated artwork square dl err: %v\n", err)
				} else {
					fmt.Println("Animation Artwork Square Downloaded")
				}
			}
		}

		// Emby compatibility conversion
		if Config.EmbyAnimatedArtwork {
			cmd3 := exec.Command("ffmpeg", "-i", filepath.Join(playlistFolderPath, "square_animated_artwork.mp4"), "-vf", "scale=440:-1", "-r", "24", "-f", "gif", filepath.Join(playlistFolderPath, "folder.jpg"))
			if err := cmd3.Run(); err != nil {
				fmt.Printf("animated artwork square to gif err: %v\n", err)
			}
		}

		// Tall animated artwork
		motionvideoUrlTall, err := extractVideo(meta.Data[0].Attributes.EditorialVideo.MotionDetailTall.Video)
		if err != nil {
			fmt.Println("no motion video tall.\n", err)
		} else {
			exists, err := fileExists(filepath.Join(playlistFolderPath, "tall_animated_artwork.mp4"))
			if err != nil {
				fmt.Println("Failed to check if animated artwork tall exists.")
			}
			if exists {
				fmt.Println("Animated artwork tall already exists locally.")
			} else {
				fmt.Println("Animation Artwork Tall Downloading...")
				cmd := exec.Command("ffmpeg", "-loglevel", "quiet", "-y", "-i", motionvideoUrlTall, "-c", "copy", filepath.Join(playlistFolderPath, "tall_animated_artwork.mp4"))
				if err := cmd.Run(); err != nil {
					fmt.Printf("animated artwork tall dl err: %v\n", err)
				} else {
					fmt.Println("Animation Artwork Tall Downloaded")
				}
			}
		}
	}

	// Create track selection array
	trackTotal := len(meta.Data[0].Relationships.Tracks.Data)
	arr := make([]int, trackTotal)
	for i := 0; i < trackTotal; i++ {
		arr[i] = i + 1
	}
	var selected []int

	// Determine which tracks to download
	if !dl_select {
		selected = arr
	} else {
		selected = playlist.ShowSelect()
	}

	// Download selected tracks
	for i := range playlist.Tracks {
		i++
		// Skip already downloaded tracks
		if isInArray(okDict[playlistId], i) {
			counter.Total++
			counter.Success++
			continue
		}
		// Download selected tracks
		if isInArray(selected, i) {
			ripTrack(&playlist.Tracks[i-1], token, mediaUserToken)
		}
	}
	return nil
}

// writeMP4Tags writes comprehensive metadata to downloaded MP4 files
func writeMP4Tags(track *task.Track, lrc string) error {
	// Initialize MP4 tags with basic track information
	t := &mp4tag.MP4Tags{
		Title:      track.Resp.Attributes.Name,
		TitleSort:  track.Resp.Attributes.Name,
		Artist:     track.Resp.Attributes.ArtistName,
		ArtistSort: track.Resp.Attributes.ArtistName,
		Custom: map[string]string{
			"PERFORMER":   track.Resp.Attributes.ArtistName,
			"RELEASETIME": track.Resp.Attributes.ReleaseDate,
			"ISRC":        track.Resp.Attributes.Isrc,
			"LABEL":       "",
			"UPC":         "",
		},
		Composer:     track.Resp.Attributes.ComposerName,
		ComposerSort: track.Resp.Attributes.ComposerName,
		CustomGenre:  track.Resp.Attributes.GenreNames[0],
		Lyrics:       lrc,
		TrackNumber:  int16(track.Resp.Attributes.TrackNumber),
		DiscNumber:   int16(track.Resp.Attributes.DiscNumber),
		Album:        track.Resp.Attributes.AlbumName,
		AlbumSort:    track.Resp.Attributes.AlbumName,
	}

	// Set iTunes album ID for albums
	if track.PreType == "albums" {
		albumID, err := strconv.ParseUint(track.PreID, 10, 32)
		if err != nil {
			return err
		}
		t.ItunesAlbumID = int32(albumID)
	}

	// Set iTunes artist ID if available
	if len(track.Resp.Relationships.Artists.Data) > 0 {
		artistID, err := strconv.ParseUint(track.Resp.Relationships.Artists.Data[0].ID, 10, 32)
		if err != nil {
			return err
		}
		t.ItunesArtistID = int32(artistID)
	}

	// Handle playlist and station specific tagging
	if (track.PreType == "playlists" || track.PreType == "stations") && !Config.UseSongInfoForPlaylist {
		// use playlist metadata
		t.DiscNumber = 1
		t.DiscTotal = 1
		t.TrackNumber = int16(track.TaskNum)
		t.TrackTotal = int16(track.TaskTotal)
		t.Album = track.PlaylistData.Attributes.Name
		t.AlbumSort = track.PlaylistData.Attributes.Name
		t.AlbumArtist = track.PlaylistData.Attributes.ArtistName
		t.AlbumArtistSort = track.PlaylistData.Attributes.ArtistName
	} else if (track.PreType == "playlists" || track.PreType == "stations") && Config.UseSongInfoForPlaylist {
		// Use original album metadata for playlists
		t.DiscTotal = int16(track.DiscTotal)
		t.TrackTotal = int16(track.AlbumData.Attributes.TrackCount)
		t.AlbumArtist = track.AlbumData.Attributes.ArtistName
		t.AlbumArtistSort = track.AlbumData.Attributes.ArtistName
		t.Custom["UPC"] = track.AlbumData.Attributes.Upc
		t.Custom["LABEL"] = track.AlbumData.Attributes.RecordLabel
		t.Date = track.AlbumData.Attributes.ReleaseDate
		t.Copyright = track.AlbumData.Attributes.Copyright
		t.Publisher = track.AlbumData.Attributes.RecordLabel
	} else {
		// use standard album metadata
		t.DiscTotal = int16(track.DiscTotal)
		t.TrackTotal = int16(track.AlbumData.Attributes.TrackCount)
		t.AlbumArtist = track.AlbumData.Attributes.ArtistName
		t.AlbumArtistSort = track.AlbumData.Attributes.ArtistName
		t.Custom["UPC"] = track.AlbumData.Attributes.Upc
		t.Date = track.AlbumData.Attributes.ReleaseDate
		t.Copyright = track.AlbumData.Attributes.Copyright
		t.Publisher = track.AlbumData.Attributes.RecordLabel
	}

	// Set content advisory rating
	switch track.Resp.Attributes.ContentRating {
	case "explicit":
		t.ItunesAdvisory = mp4tag.ItunesAdvisoryExplicit
	case "clean":
		t.ItunesAdvisory = mp4tag.ItunesAdvisoryClean
	default:
		t.ItunesAdvisory = mp4tag.ItunesAdvisoryNone
	}

	// Write tags to file
	mp4, err := mp4tag.Open(track.SavePath)
	if err != nil {
		return err
	}
	defer mp4.Close()
	err = mp4.Write(t, []string{})
	if err != nil {
		return err
	}
	return nil
}

// mvDownloader handles music video downloads with separate video/audio streams
func mvDownloader(adamID string, saveDir string, token string, storefront string, mediaUserToken string, track *task.Track) error {
	// Fetch music video metadata
	MVInfo, err := ampapi.GetMusicVideoResp(storefront, adamID, Config.Language, token)
	if err != nil {
		fmt.Println("Failed to get MV manifest:", err)
		return nil
	}

	// Clean up save directory name
	if strings.HasSuffix(saveDir, ".") {
		saveDir = strings.ReplaceAll(saveDir, ".", "")
	}
	saveDir = strings.TrimSpace(saveDir)

	// Define temporary file paths
	vidPath := filepath.Join(saveDir, fmt.Sprintf("%s_vid.mp4", adamID))
	audPath := filepath.Join(saveDir, fmt.Sprintf("%s_aud.mp4", adamID))

	// Generate output filename
	mvSaveName := fmt.Sprintf("%s (%s)", MVInfo.Data[0].Attributes.Name, adamID)
	if track != nil {
		mvSaveName = fmt.Sprintf("%02d. %s", track.TaskNum, MVInfo.Data[0].Attributes.Name)
	}

	mvOutPath := filepath.Join(saveDir, fmt.Sprintf("%s.mp4", forbiddenNames.ReplaceAllString(mvSaveName, "_")))

	fmt.Println(MVInfo.Data[0].Attributes.Name)

	// Handle playlist and station specific tagging
	exists, _ := fileExists(mvOutPath)
	if exists {
		fmt.Println("MV already exists locally.")
		return nil
	}

	// Get music video playback URL
	mvm3u8url, _, _ := runv3.GetWebplayback(adamID, token, mediaUserToken, true)
	if mvm3u8url == "" {
		return errors.New("media-user-token may wrong or expired")
	}

	// Create save directory
	os.MkdirAll(saveDir, os.ModePerm)

	// Download video stream
	videom3u8url, _ := extractVideo(mvm3u8url)
	videokeyAndUrls, _ := runv3.Run(adamID, videom3u8url, token, mediaUserToken, true)
	_ = runv3.ExtMvData(videokeyAndUrls, vidPath)

	// Download audio stream
	audiom3u8url, _ := extractMvAudio(mvm3u8url)
	audiokeyAndUrls, _ := runv3.Run(adamID, audiom3u8url, token, mediaUserToken, true)
	_ = runv3.ExtMvData(audiokeyAndUrls, audPath)

	// Prepare metadata tags
	tags := []string{
		"tool=",
		fmt.Sprintf("artist=%s", MVInfo.Data[0].Attributes.ArtistName),
		fmt.Sprintf("title=%s", MVInfo.Data[0].Attributes.Name),
		fmt.Sprintf("genre=%s", MVInfo.Data[0].Attributes.GenreNames[0]),
		fmt.Sprintf("created=%s", MVInfo.Data[0].Attributes.ReleaseDate),
		fmt.Sprintf("ISRC=%s", MVInfo.Data[0].Attributes.Isrc),
	}

	// Set content rating
	switch MVInfo.Data[0].Attributes.ContentRating {
	case "explicit":
		tags = append(tags, "rating=1")
	case "clean":
		tags = append(tags, "rating=2")
	default:
		tags = append(tags, "rating=0")
	}

	// Add track-specific metadata if available
	if track != nil {
		if track.PreType == "playlists" && !Config.UseSongInfoForPlaylist {
			// Use playlist metadata
			tags = append(tags, "disk=1/1")
			tags = append(tags, fmt.Sprintf("album=%s", track.PlaylistData.Attributes.Name))
			tags = append(tags, fmt.Sprintf("track=%d", track.TaskNum))
			tags = append(tags, fmt.Sprintf("tracknum=%d/%d", track.TaskNum, track.TaskTotal))
			tags = append(tags, fmt.Sprintf("album_artist=%s", track.PlaylistData.Attributes.ArtistName))
			tags = append(tags, fmt.Sprintf("performer=%s", track.Resp.Attributes.ArtistName))
		} else if track.PreType == "playlists" && Config.UseSongInfoForPlaylist {
			// Use original album metadata
			tags = append(tags, fmt.Sprintf("album=%s", track.AlbumData.Attributes.Name))
			tags = append(tags, fmt.Sprintf("disk=%d/%d", track.Resp.Attributes.DiscNumber, track.DiscTotal))
			tags = append(tags, fmt.Sprintf("track=%d", track.Resp.Attributes.TrackNumber))
			tags = append(tags, fmt.Sprintf("tracknum=%d/%d", track.Resp.Attributes.TrackNumber, track.AlbumData.Attributes.TrackCount))
			tags = append(tags, fmt.Sprintf("album_artist=%s", track.AlbumData.Attributes.ArtistName))
			tags = append(tags, fmt.Sprintf("performer=%s", track.Resp.Attributes.ArtistName))
			tags = append(tags, fmt.Sprintf("copyright=%s", track.AlbumData.Attributes.Copyright))
			tags = append(tags, fmt.Sprintf("UPC=%s", track.AlbumData.Attributes.Upc))
		} else {
			// Use standard album metadata
			tags = append(tags, fmt.Sprintf("album=%s", track.AlbumData.Attributes.Name))
			tags = append(tags, fmt.Sprintf("disk=%d/%d", track.Resp.Attributes.DiscNumber, track.DiscTotal))
			tags = append(tags, fmt.Sprintf("track=%d", track.Resp.Attributes.TrackNumber))
			tags = append(tags, fmt.Sprintf("tracknum=%d/%d", track.Resp.Attributes.TrackNumber, track.AlbumData.Attributes.TrackCount))
			tags = append(tags, fmt.Sprintf("album_artist=%s", track.AlbumData.Attributes.ArtistName))
			tags = append(tags, fmt.Sprintf("performer=%s", track.Resp.Attributes.ArtistName))
			tags = append(tags, fmt.Sprintf("copyright=%s", track.AlbumData.Attributes.Copyright))
			tags = append(tags, fmt.Sprintf("UPC=%s", track.AlbumData.Attributes.Upc))
		}
	} else {
		// Use music video metadata
		tags = append(tags, fmt.Sprintf("album=%s", MVInfo.Data[0].Attributes.AlbumName))
		tags = append(tags, fmt.Sprintf("disk=%d", MVInfo.Data[0].Attributes.DiscNumber))
		tags = append(tags, fmt.Sprintf("track=%d", MVInfo.Data[0].Attributes.TrackNumber))
		tags = append(tags, fmt.Sprintf("tracknum=%d", MVInfo.Data[0].Attributes.TrackNumber))
		tags = append(tags, fmt.Sprintf("performer=%s", MVInfo.Data[0].Attributes.ArtistName))
	}

	// Download and embed thumbnail
	var covPath string
	if true {
		thumbURL := MVInfo.Data[0].Attributes.Artwork.URL
		baseThumbName := forbiddenNames.ReplaceAllString(mvSaveName, "_") + "_thumbnail"
		covPath, err = writeCover(saveDir, baseThumbName, thumbURL)
		if err != nil {
			fmt.Println("Failed to save MV thumbnail:", err)
		} else {
			tags = append(tags, fmt.Sprintf("cover=%s", covPath))
		}
	}

	// Mux video and audio streams together
	tagsString := strings.Join(tags, ":")
	muxCmd := exec.Command("MP4Box", "-itags", tagsString, "-quiet", "-add", vidPath, "-add", audPath, "-keep-utc", "-new", mvOutPath)
	fmt.Printf("MV Remuxing...")
	if err := muxCmd.Run(); err != nil {
		fmt.Printf("MV mux failed: %v\n", err)
		return err
	}
	fmt.Printf("\rMV Remuxed.   \n")

	// Update file size statistics
	fileInfo, err := os.Stat(mvOutPath)
	if err == nil {
		counter.TotalSize += fileInfo.Size()
	}

	// Clean up temporary files
	defer os.Remove(vidPath)
	defer os.Remove(audPath)
	defer os.Remove(covPath)

	return nil
}

// extractMvAudio extracts the audio stream URL from music video m3u8 manifest
func extractMvAudio(c string) (string, error) {
	MediaUrl, err := url.Parse(c)
	if err != nil {
		return "", err
	}

	// Fetch m3u8 manifest
	resp, err := http.Get(c)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", errors.New(resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	audioString := string(body)
	from, listType, err := m3u8.DecodeFrom(strings.NewReader(audioString), true)
	if err != nil || listType != m3u8.MASTER {
		return "", errors.New("m3u8 not of media type")
	}

	audio := from.(*m3u8.MasterPlaylist)

	// Define audio quality priority based on configuration
	var audioPriority = []string{"audio-atmos", "audio-ac3", "audio-stereo-256"}
	switch Config.MVAudioType {
	case "ac3":
		audioPriority = []string{"audio-ac3", "audio-stereo-256"}
	case "aac":
		audioPriority = []string{"audio-stereo-256"}
	}

	type AudioStream struct {
		URL     string
		Rank    int
		GroupID string
	}
	var audioStreams []AudioStream

	// Parse all available audio streams
	for _, variant := range audio.Variants {
		for _, audiov := range variant.Alternatives {
			if audiov.URI != "" {
				for _, priority := range audioPriority {
					if audiov.GroupId == priority {
						// Extract quality rank from URI
						matches := videoRankRegex.FindStringSubmatch(audiov.URI)
						if len(matches) == 2 {
							var rank int
							fmt.Sscanf(matches[1], "%d", &rank)
							streamUrl, _ := MediaUrl.Parse(audiov.URI)
							audioStreams = append(audioStreams, AudioStream{
								URL:     streamUrl.String(),
								Rank:    rank,
								GroupID: audiov.GroupId,
							})
						}
					}
				}
			}
		}
	}

	if len(audioStreams) == 0 {
		return "", errors.New("no suitable audio stream found")
	}

	// Select highest quality audio stream
	sort.Slice(audioStreams, func(i, j int) bool {
		return audioStreams[i].Rank > audioStreams[j].Rank
	})
	fmt.Println("Audio: " + audioStreams[0].GroupID)
	return audioStreams[0].URL, nil
}

// checkM3u8 retrieves enhanced m3u8 URL from external device/service
func checkM3u8(b string, f string) (string, error) {
	var EnhancedHls string
	if Config.GetM3u8FromDevice {
		adamID := b
		// Connect to external device/service on configured port
		conn, err := net.Dial("tcp", Config.GetM3u8Port)
		if err != nil {
			fmt.Println("Error connecting to device:", err)
			return "none", err
		}
		defer conn.Close()
		if f == "song" {
			fmt.Println("Connected to device")
		}

		// Send track ID to device
		adamIDBuffer := []byte(adamID)
		lengthBuffer := []byte{byte(len(adamIDBuffer))}

		_, err = conn.Write(lengthBuffer)
		if err != nil {
			fmt.Println("Error writing length to device:", err)
			return "none", err
		}

		_, err = conn.Write(adamIDBuffer)
		if err != nil {
			fmt.Println("Error writing adamID to device:", err)
			return "none", err
		}

		// Receive enhanced m3u8 URL from device
		response, err := bufio.NewReader(conn).ReadBytes('\n')
		if err != nil {
			fmt.Println("Error reading response from device:", err)
			return "none", err
		}

		response = bytes.TrimSpace(response)
		if len(response) > 0 {
			if f == "song" {
				fmt.Println("Received URL:", string(response))
			}
			EnhancedHls = string(response)
		} else {
			fmt.Println("Received an empty response")
		}
	}
	return EnhancedHls, nil
}

// formatAvailability formats availability string for debug output
func formatAvailability(available bool, quality string) string {
	if !available {
		return "Not Available"
	}
	return quality
}

// extractMedia parses m3u8 manifest and extracts stream URL and quality information
func extractMedia(b string, more_mode bool) (string, string, error) {
	masterUrl, err := url.Parse(b)
	if err != nil {
		return "", "", err
	}

	// Fetch m3u8 manifest
	resp, err := http.Get(b)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", errors.New(resp.Status)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", err
	}
	masterString := string(body)

	// Parse m3u8 manifest
	from, listType, err := m3u8.DecodeFrom(strings.NewReader(masterString), true)
	if err != nil || listType != m3u8.MASTER {
		return "", "", errors.New("m3u8 not of master type")
	}
	master := from.(*m3u8.MasterPlaylist)
	var streamUrl *url.URL

	// Sort variants by bandwidth (highest first)
	sort.Slice(master.Variants, func(i, j int) bool {
		return master.Variants[i].AverageBandwidth > master.Variants[j].AverageBandwidth
	})

	// Debug mode: Display all available variants
	if debug_mode && more_mode {
		fmt.Println("\nDebug: All Available Variants:")
		fmt.Printf("%-10s %-40s %10s\n", "CODEC", "AUDIO", "BANDWIDTH")
		for _, variant := range master.Variants {
			fmt.Printf("%-10s %-40s %10d\n", variant.Codecs, variant.Audio, variant.Bandwidth)
		}

		// Analyze available audio formats
		var hasAAC, hasLossless, hasHiRes, hasAtmos, hasDolbyAudio bool
		var aacQuality, losslessQuality, hiResQuality, atmosQuality, dolbyAudioQuality string

		for _, variant := range master.Variants {
			if variant.Codecs == "mp4a.40.2" {
				hasAAC = true
				split := strings.Split(variant.Audio, "-")
				if len(split) >= 3 {
					bitrate, _ := strconv.Atoi(split[2])
					currentBitrate := 0
					if aacQuality != "" {
						current := strings.Split(aacQuality, " | ")[2]
						current = strings.Split(current, " ")[0]
						currentBitrate, _ = strconv.Atoi(current)
					}
					if bitrate > currentBitrate {
						aacQuality = fmt.Sprintf("AAC | 2 Channel | %d Kbps", bitrate)
					}
				}
			} else if variant.Codecs == "ec-3" && strings.Contains(variant.Audio, "atmos") {
				hasAtmos = true
				split := strings.Split(variant.Audio, "-")
				if len(split) > 0 {
					bitrateStr := split[len(split)-1]
					if len(bitrateStr) == 4 && bitrateStr[0] == '2' {
						bitrateStr = bitrateStr[1:]
					}
					bitrate, _ := strconv.Atoi(bitrateStr)
					currentBitrate := 0
					if atmosQuality != "" {
						current := strings.Split(strings.Split(atmosQuality, " | ")[2], " ")[0]
						currentBitrate, _ = strconv.Atoi(current)
					}
					if bitrate > currentBitrate {
						atmosQuality = fmt.Sprintf("E-AC-3 | 16 Channel | %d Kbps", bitrate)
					}
				}
			} else if variant.Codecs == "alac" {
				split := strings.Split(variant.Audio, "-")
				if len(split) >= 3 {
					bitDepth := split[len(split)-1]
					sampleRate := split[len(split)-2]
					sampleRateInt, _ := strconv.Atoi(sampleRate)
					if sampleRateInt > 48000 {
						hasHiRes = true
						hiResQuality = fmt.Sprintf("ALAC | 2 Channel | %s-bit/%d kHz", bitDepth, sampleRateInt/1000)
					} else {
						hasLossless = true
						losslessQuality = fmt.Sprintf("ALAC | 2 Channel | %s-bit/%d kHz", bitDepth, sampleRateInt/1000)
					}
				}
			} else if variant.Codecs == "ac-3" {
				hasDolbyAudio = true
				split := strings.Split(variant.Audio, "-")
				if len(split) > 0 {
					bitrate, _ := strconv.Atoi(split[len(split)-1])
					dolbyAudioQuality = fmt.Sprintf("AC-3 |  16 Channel | %d Kbps", bitrate)
				}
			}
		}

		// Display available formats
		fmt.Println("\nAvailable Audio Formats:")
		fmt.Printf("%-15s %-35s\n", "FORMAT", "DETAILS")
		for _, f := range []struct {
			name, detail string
		}{
			{"AAC", formatAvailability(hasAAC, aacQuality)},
			{"LOSSLESS", formatAvailability(hasLossless, losslessQuality)},
			{"HI-RES", formatAvailability(hasHiRes, hiResQuality)},
			{"ATMOS", formatAvailability(hasAtmos, atmosQuality)},
			{"DOLBY", formatAvailability(hasDolbyAudio, dolbyAudioQuality)},
		} {
			fmt.Printf("%-15s %-35s\n", f.name, f.detail)
		}

		return "", "", nil
	}

	var Quality string
	// Select appropriate stream based on quality preferences
	for _, variant := range master.Variants {
		if dl_atmos {
			if variant.Codecs == "ec-3" && strings.Contains(variant.Audio, "atmos") {
				if debug_mode && !more_mode {
					fmt.Printf("Debug: Found Dolby Atmos variant - %s (Bitrate: %d Kbps)\n",
						variant.Audio, variant.Bandwidth/1000)
				}
				split := strings.Split(variant.Audio, "-")
				length := len(split)
				length_int, err := strconv.Atoi(split[length-1])
				if err != nil {
					return "", "", err
				}
				if length_int <= Config.AtmosMax {
					if !debug_mode && !more_mode {
						fmt.Printf("%s\n", variant.Audio)
					}
					streamUrlTemp, err := masterUrl.Parse(variant.URI)
					if err != nil {
						return "", "", err
					}
					streamUrl = streamUrlTemp
					Quality = fmt.Sprintf("%s Kbps", split[len(split)-1])
					break
				}
			} else if variant.Codecs == "ac-3" {
				if debug_mode && !more_mode {
					fmt.Printf("Debug: Found Dolby Audio variant - %s (Bitrate: %d Kbps)\n",
						variant.Audio, variant.Bandwidth/1000)
				}
				streamUrlTemp, err := masterUrl.Parse(variant.URI)
				if err != nil {
					return "", "", err
				}
				streamUrl = streamUrlTemp
				split := strings.Split(variant.Audio, "-")
				Quality = fmt.Sprintf("%s Kbps", split[len(split)-1])
				break
			}
		} else if dl_aac {
			if variant.Codecs == "mp4a.40.2" {
				if debug_mode && !more_mode {
					fmt.Printf("Debug: Found AAC variant - %s (Bitrate: %d)\n", variant.Audio, variant.Bandwidth)
				}
				replaced := aacRegex.ReplaceAllString(variant.Audio, "aac")
				if replaced == Config.AacType {
					if !debug_mode && !more_mode {
						fmt.Printf("%s\n", variant.Audio)
					}
					streamUrlTemp, err := masterUrl.Parse(variant.URI)
					if err != nil {
						panic(err)
					}
					streamUrl = streamUrlTemp
					split := strings.Split(variant.Audio, "-")
					Quality = fmt.Sprintf("%s Kbps", split[2])
					break
				}
			}
		} else {
			if variant.Codecs == "alac" {
				split := strings.Split(variant.Audio, "-")
				length := len(split)
				length_int, err := strconv.Atoi(split[length-2])
				if err != nil {
					return "", "", err
				}
				if length_int <= Config.AlacMax {
					if !debug_mode && !more_mode {
						fmt.Printf("%s-bit / %s Hz\n", split[length-1], split[length-2])
					}
					streamUrlTemp, err := masterUrl.Parse(variant.URI)
					if err != nil {
						panic(err)
					}
					streamUrl = streamUrlTemp
					KHZ := float64(length_int) / 1000.0
					Quality = fmt.Sprintf("%sB-%.1fkHz", split[length-1], KHZ)
					break
				}
			}
		}
	}
	if streamUrl == nil {
		return "", "", errors.New("no codec found")
	}
	return streamUrl.String(), Quality, nil
}

// extractVideo extracts the video stream URL from music video m3u8 manifest
func extractVideo(c string) (string, error) {
	MediaUrl, err := url.Parse(c)
	if err != nil {
		return "", err
	}

	// Fetch m3u8 manifest
	resp, err := http.Get(c)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", errors.New(resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	videoString := string(body)

	// Parse m3u8 manifest
	from, listType, err := m3u8.DecodeFrom(strings.NewReader(videoString), true)
	if err != nil || listType != m3u8.MASTER {
		return "", errors.New("m3u8 not of media type")
	}

	video := from.(*m3u8.MasterPlaylist)

	var streamUrl *url.URL
	// Sort variants by bandwidth (highest quality first)
	sort.Slice(video.Variants, func(i, j int) bool {
		return video.Variants[i].AverageBandwidth > video.Variants[j].AverageBandwidth
	})

	maxHeight := Config.MVMax

	// Find the best video stream that meets quality requirements
	for _, variant := range video.Variants {
		matches := videoSizeRegex.FindStringSubmatch(variant.URI)
		if len(matches) == 3 {
			height := matches[2]
			var h int
			_, err := fmt.Sscanf(height, "%d", &h)
			if err != nil {
				continue
			}
			// Select the highest quality video within configured maximum
			if h <= maxHeight {
				streamUrl, err = MediaUrl.Parse(variant.URI)
				if err != nil {
					return "", err
				}
				fmt.Println("Video: " + variant.Resolution + "-" + variant.VideoRange)
				break
			}
		}
	}

	if streamUrl == nil {
		return "", errors.New("no suitable video stream found")
	}

	return streamUrl.String(), nil
}

// ripSong handles downloading individual songs
func ripSong(songId string, token string, storefront string, mediaUserToken string) error {
	// Fetch song metadata
	manifest, err := ampapi.GetSongResp(storefront, songId, Config.Language, token)
	if err != nil {
		fmt.Println("Failed to get song response.")
		return err
	}

	songData := manifest.Data[0]
	albumId := songData.Relationships.Albums.Data[0].ID

	// Set single song download flag and use album downloader for the specific song
	dl_song = true
	err = ripAlbum(albumId, token, storefront, mediaUserToken, songId)
	if err != nil {
		fmt.Println("Failed to rip song:", err)
		return err
	}

	return nil
}

// formatFileSize converts bytes to human readable file size
func formatFileSize(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.2f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

// main is the entry point of the application
func main() {
	// Load configuration from config.yaml
	err := loadConfig()
	if err != nil {
		fmt.Printf("load Config failed: %v", err)
		return
	}

	// Get authorization token for Apple Music API
	token, err := ampapi.GetToken()
	if err != nil {
		// Fallback to configured token if automatic token fetch fails
		if Config.AuthorizationToken != "" && Config.AuthorizationToken != "your-authorization-token" {
			token = strings.ReplaceAll(Config.AuthorizationToken, "Bearer ", "")
		} else {
			fmt.Println("Failed to get token.")
			return
		}
	}

	// Define command line flags
	var search_type string
	pflag.StringVar(&search_type, "search", "", "Search for 'album', 'song', or 'artist'. Provide query after flags.")
	pflag.BoolVar(&dl_atmos, "atmos", false, "Enable atmos download mode")
	pflag.BoolVar(&dl_aac, "aac", false, "Enable adm-aac download mode")
	pflag.BoolVar(&dl_select, "select", false, "Enable selective download")
	pflag.BoolVar(&dl_song, "song", false, "Enable single song download mode")
	pflag.BoolVar(&artist_select, "all-album", false, "Download all artist albums")
	pflag.BoolVar(&debug_mode, "debug", false, "Enable debug mode to show audio quality information")
	alac_max = pflag.Int("alac-max", Config.AlacMax, "Specify the max quality for download alac")
	atmos_max = pflag.Int("atmos-max", Config.AtmosMax, "Specify the max quality for download atmos")
	aac_type = pflag.String("aac-type", Config.AacType, "Select AAC type, aac aac-binaural aac-downmix")
	mv_audio_type = pflag.String("mv-audio-type", Config.MVAudioType, "Select MV audio type, atmos ac3 aac")
	mv_max = pflag.Int("mv-max", Config.MVMax, "Specify the max quality for download MV")

	// Custom usage function
	pflag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [options] [url1 url2 ...]\n", "[main|main.exe|go run main.go]")
		fmt.Fprintf(os.Stderr, "Search Usage: %s --search [album|song|artist] [query]\n", "[main|main.exe|go run main.go]")
		fmt.Println("\nOptions:")
		pflag.PrintDefaults()
	}

	// Parse command line flags
	pflag.Parse()

	// Update configuration with command line values
	Config.AlacMax = *alac_max
	Config.AtmosMax = *atmos_max
	Config.AacType = *aac_type
	Config.MVAudioType = *mv_audio_type
	Config.MVMax = *mv_max

	args := pflag.Args()

	// Handle search functionality
	if search_type != "" {
		if len(args) == 0 {
			fmt.Println("Error: --search flag requires a query.")
			pflag.Usage()
			return
		}
		// Perfom search and get selected URL
		selectedUrl, err := handleSearch(search_type, args, token)
		if err != nil {
			fmt.Printf("\nSearch process failed: %v\n", err)
			return
		}
		// Replace command line arguments with selected URL
		if selectedUrl == "" {
			fmt.Println("\nExiting.")
			return
		}
		// Replace command line arguments with selected URL
		os.Args = []string{selectedUrl}
	} else {
		// Check if URLs are provided for direct download
		if len(args) == 0 {
			fmt.Println("No URLs provided. Please provide at least one URL.")
			pflag.Usage()
			return
		}
		os.Args = args
	}

	// Handle artist URLs - fetch all albums and music videos
	if strings.Contains(os.Args[0], "/artist/") {
		urlArtistName, urlArtistID, err := getUrlArtistName(os.Args[0], token)
		if err != nil {
			fmt.Println("Failed to get artistname.")
			return
		}
		// Update artist folder format with actual artist name
		Config.ArtistFolderFormat = strings.NewReplacer(
			"{UrlArtistName}", LimitString(urlArtistName),
			"{ArtistId}", urlArtistID,
		).Replace(Config.ArtistFolderFormat)

		// Get all albums for the artist
		albumArgs, err := checkArtist(os.Args[0], token, "albums")
		if err != nil {
			fmt.Println("Failed to get artist albums.")
			return
		}

		// Get all music videos for the artist
		mvArgs, err := checkArtist(os.Args[0], token, "music-videos")
		if err != nil {
			fmt.Println("Failed to get artist music-videos.")
		}
		// Combine albums and music videos into download queue
		os.Args = append(albumArgs, mvArgs...)
	}

	// Process all items in download queue
	albumTotal := len(os.Args)
	for {
		// Iterate through all URLs in the queue
		for albumNum, urlRaw := range os.Args {
			fmt.Printf("\nQueue %d of %d: ", albumNum+1, albumTotal)
			var storefront, albumId string

			// Handle music video URLs
			if strings.Contains(urlRaw, "/music-video/") {
				fmt.Println("Music Video")
				if debug_mode {
					continue
				}
				counter.Total++

				// Check if media-user-token is available
				if len(Config.MediaUserToken) <= 50 {
					fmt.Println(": media-user-token is not set, skip MV dl")
					counter.Success++
					continue
				}

				// Check if mp4decrypt is available
				if _, err := exec.LookPath("mp4decrypt"); err != nil {
					fmt.Println(": mp4decrypt is not found, skip MV dl")
					counter.Success++
					continue
				}

				// Determine save directory for music videos
				mvSaveDir := strings.NewReplacer(
					"{ArtistName}", "",
					"{UrlArtistName}", "",
					"{ArtistId}", "",
				).Replace(Config.ArtistFolderFormat)
				if mvSaveDir != "" {
					mvSaveDir = filepath.Join(Config.AlacSaveFolder, forbiddenNames.ReplaceAllString(mvSaveDir, "_"))
				} else {
					mvSaveDir = Config.AlacSaveFolder
				}

				// Extract storefront and video ID from URL
				storefront, albumId = checkUrlMv(urlRaw)
				err := mvDownloader(albumId, mvSaveDir, token, storefront, Config.MediaUserToken, nil)
				if err != nil {
					fmt.Println("Failed to dl MV:", err)
					counter.Error++
					continue
				}
				counter.Success++
				continue
			}

			// Handle single song URLs
			if strings.Contains(urlRaw, "/song/") {
				fmt.Printf("Song->")
				storefront, songId := checkUrlSong(urlRaw)
				if storefront == "" || songId == "" {
					fmt.Println("Invalid song URL format.")
					continue
				}
				err := ripSong(songId, token, storefront, Config.MediaUserToken)
				if err != nil {
					fmt.Println("Failed to rip song:", err)
				}
				continue
			}

			// Parse URL to extract query parameters
			parse, err := url.Parse(urlRaw)
			if err != nil {
				log.Fatalf("Invalid URL: %v", err)
			}
			var urlArg_i = parse.Query().Get("i")

			// Handle album URLs
			if strings.Contains(urlRaw, "/album/") {
				fmt.Println("Album")
				storefront, albumId = checkUrl(urlRaw)
				err := ripAlbum(albumId, token, storefront, Config.MediaUserToken, urlArg_i)
				if err != nil {
					fmt.Println("Failed to rip album:", err)
				}
			} else if strings.Contains(urlRaw, "/playlist/") {
				// Handle playlist URLs
				fmt.Println("Playlist")
				storefront, albumId = checkUrlPlaylist(urlRaw)
				err := ripPlaylist(albumId, token, storefront, Config.MediaUserToken)
				if err != nil {
					fmt.Println("Failed to rip playlist:", err)
				}
			} else if strings.Contains(urlRaw, "/station/") {
				// Handle radio station URLs
				fmt.Printf("Station")
				storefront, albumId = checkUrlStation(urlRaw)
				if len(Config.MediaUserToken) <= 50 {
					fmt.Println(": media-user-token is not set, skip station dl")
					continue
				}
				err := ripStation(albumId, token, storefront, Config.MediaUserToken)
				if err != nil {
					fmt.Println("Failed to rip station:", err)
				}
			} else {
				fmt.Println("Invalid type")
			}
		}

		// Display donwload summary
		fmt.Printf("\n%-11s: %d / %d\n", "Completed", counter.Success, counter.Total)
		fmt.Printf("%-11s: %d\n", "Warnings", counter.Unavailable+counter.NotSong)
		fmt.Printf("%-11s: %d\n", "Errors", counter.Error)
		fmt.Printf("%-11s: %s\n", "Total Size", formatFileSize(counter.TotalSize))

		// Exit if no errors, otherwise retry
		if counter.Error == 0 {
			break
		}

		// Prompt for retry on errors
		fmt.Println("\nError detected, press Enter to try again...")
		fmt.Scanln()
		fmt.Println("Start trying again...")

		// Reset counters for retry
		counter = structs.Counter{}
	}
}
