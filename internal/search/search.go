// ============================================
// File: internal/search/search.go
package search

import (
	"fmt"
	"strings"

	"main/pkg/ampapi"
	"main/pkg/structs"

	"github.com/AlecAivazis/survey/v2"
)

// Constants
const (
	prevPageOption = "<  Previous Page"
	nextPageOption = ">  Next Page"
	defaultLimit   = 15
)

// SearchResultItem is a unified struct to hold search results for display
type SearchResultItem struct {
	Type   string
	Name   string
	Detail string
	URL    string
	ID     string
}

// QualityOption holds information about a downloadable quality
type QualityOption struct {
	ID          string
	Description string
}

// SetDlFlags configures the global download flags based on the user's quality selection
func SetDlFlags(quality string, dlAtmos *bool, dlAAC *bool, aacType *string) {
	*dlAtmos = false
	*dlAAC = false

	switch quality {
	case "atmos":
		*dlAtmos = true
		fmt.Println("Quality set to: Dolby Atmos")
	case "aac":
		*dlAAC = true
		*aacType = "aac"
		fmt.Println("Quality set to: High-Quality (AAC)")
	case "alac":
		fmt.Println("Quality set to: Lossless (ALAC)")
	}
}

// PromptForQuality asks the user to select a download quality for the chosen media
// Skip prompt for Artist type - quality will be selected after choosing specific album
func PromptForQuality(item SearchResultItem, token string) (string, error) {
	// For Artist, skip quality selection here - it will be prompted later
	// after user selects specific albums in CheckArtist function
	if item.Type == "Artist" {
		fmt.Println("Artist selected. Proceeding to list albums...")
		return "skip", nil // Use "skip" instead of "default" to indicate deferred selection
	}

	fmt.Printf("\nFetching available qualities for: %s\n", item.Name)

	qualities := []QualityOption{
		{ID: "alac", Description: "Lossless (ALAC)"},
		{ID: "aac", Description: "High-Quality (AAC)"},
		{ID: "atmos", Description: "Dolby Atmos"},
	}

	qualityOptions := make([]string, len(qualities))
	for i, q := range qualities {
		qualityOptions[i] = q.Description
	}

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

// validateSearchType checks if the search type is valid
func validateSearchType(searchType string) error {
	validTypes := map[string]bool{"album": true, "song": true, "artist": true}
	if !validTypes[searchType] {
		return fmt.Errorf("invalid search type: %s. Use 'album', 'song', or 'artist'", searchType)
	}
	return nil
}

// extractYear safely extracts year from date string
func extractYear(dateStr string) string {
	if len(dateStr) >= 4 {
		return dateStr[:4]
	}
	return ""
}

// parseAlbumResults parses album search results
func parseAlbumResults(albums *ampapi.AlbumResults) ([]SearchResultItem, []string, bool) {
	var items []SearchResultItem
	var displayOptions []string
	hasNext := false

	if albums != nil {
		for _, item := range albums.Data {
			year := extractYear(item.Attributes.ReleaseDate)
			trackInfo := fmt.Sprintf("%d tracks", item.Attributes.TrackCount)
			detail := fmt.Sprintf("%s (%s, %s)", item.Attributes.ArtistName, year, trackInfo)
			displayOptions = append(displayOptions, fmt.Sprintf("%s - %s", item.Attributes.Name, detail))
			items = append(items, SearchResultItem{
				Type: "Album",
				URL:  item.Attributes.URL,
				ID:   item.ID,
			})
		}
		hasNext = albums.Next != ""
	}

	return items, displayOptions, hasNext
}

// parseSongResults parses song search results
func parseSongResults(songs *ampapi.SongResults) ([]SearchResultItem, []string, bool) {
	var items []SearchResultItem
	var displayOptions []string
	hasNext := false

	if songs != nil {
		for _, item := range songs.Data {
			detail := fmt.Sprintf("%s (%s)", item.Attributes.ArtistName, item.Attributes.AlbumName)
			displayOptions = append(displayOptions, fmt.Sprintf("%s - %s", item.Attributes.Name, detail))
			items = append(items, SearchResultItem{
				Type: "Song",
				URL:  item.Attributes.URL,
				ID:   item.ID,
			})
		}
		hasNext = songs.Next != ""
	}

	return items, displayOptions, hasNext
}

// parseArtistResults parses artist search results
func parseArtistResults(artists *ampapi.ArtistResults) ([]SearchResultItem, []string, bool) {
	var items []SearchResultItem
	var displayOptions []string
	hasNext := false

	if artists != nil {
		for _, item := range artists.Data {
			detail := ""
			if len(item.Attributes.GenreNames) > 0 {
				detail = strings.Join(item.Attributes.GenreNames, ", ")
			}
			displayOptions = append(displayOptions, fmt.Sprintf("%s (%s)", item.Attributes.Name, detail))
			items = append(items, SearchResultItem{
				Type: "Artist",
				URL:  item.Attributes.URL,
				ID:   item.ID,
			})
		}
		hasNext = artists.Next != ""
	}

	return items, displayOptions, hasNext
}

// parseSearchResults parses search results based on search type
func parseSearchResults(searchType string, searchResp *ampapi.SearchResp) ([]SearchResultItem, []string, bool) {
	switch searchType {
	case "album":
		return parseAlbumResults(searchResp.Results.Albums)
	case "song":
		return parseSongResults(searchResp.Results.Songs)
	case "artist":
		return parseArtistResults(searchResp.Results.Artists)
	default:
		return nil, nil, false
	}
}

// buildDisplayOptions adds pagination options to display
func buildDisplayOptions(options []string, offset int, hasNext bool) []string {
	result := []string{}

	if offset > 0 {
		result = append(result, prevPageOption)
	}

	result = append(result, options...)

	if hasNext {
		result = append(result, nextPageOption)
	}

	return result
}

// adjustItemIndex adjusts the selected index based on pagination
func adjustItemIndex(selectedIndex int, offset int) int {
	if offset > 0 {
		return selectedIndex - 1
	}
	return selectedIndex
}

// Handle manages the entire interactive search process
func Handle(searchType string, queryParts []string, token string, cfg *structs.ConfigSet, dlAtmos bool, dlAAC bool, dlSong *bool, aacType *string) (string, error) {
	query := strings.Join(queryParts, " ")

	if err := validateSearchType(searchType); err != nil {
		return "", err
	}

	fmt.Printf("Searching for %ss: \"%s\" in storefront \"%s\"\n", searchType, query, cfg.Storefront)

	offset := 0
	limit := defaultLimit
	apiSearchType := searchType + "s"

	for {
		searchResp, err := ampapi.Search(cfg.Storefront, query, apiSearchType, cfg.Language, token, limit, offset)
		if err != nil {
			return "", fmt.Errorf("error fetching search results: %w", err)
		}

		items, displayOptions, hasNext := parseSearchResults(searchType, searchResp)

		if len(items) == 0 && offset == 0 {
			fmt.Println("No results found.")
			return "", nil
		}

		displayOptions = buildDisplayOptions(displayOptions, offset, hasNext)

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
		if selectedOption == nextPageOption {
			offset += limit
			continue
		}
		if selectedOption == prevPageOption {
			offset -= limit
			continue
		}

		itemIndex := adjustItemIndex(selectedIndex, offset)
		selectedItem := items[itemIndex]

		// Automatically set single song download flag
		if selectedItem.Type == "Song" {
			*dlSong = true
		}

		quality, err := PromptForQuality(selectedItem, token)
		if err != nil {
			return "", fmt.Errorf("could not process quality selection: %w", err)
		}
		if quality == "" {
			fmt.Println("Selection cancelled.")
			return "", nil
		}

		// Only set quality flags if not skipped (i.e., not an Artist)
		// For Artist, quality will be set later after album selection
		if quality != "skip" {
			SetDlFlags(quality, &dlAtmos, &dlAAC, aacType)
		}

		return selectedItem.URL, nil
	}
}
