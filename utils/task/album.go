package task

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"main/utils/ampapi"

	"github.com/olekukonko/tablewriter"
)

type Album struct {
	Storefront string
	ID         string

	SaveDir   string
	SaveName  string
	Codec     string
	CoverPath string

	Language string
	Resp     ampapi.AlbumResp
	Name     string
	Tracks   []Track
}

func NewAlbum(st string, id string) *Album {
	a := new(Album)
	a.Storefront = st
	a.ID = id
	return a
}

func (a *Album) GetResp(token, l string) error {
	var err error
	a.Language = l
	resp, err := ampapi.GetAlbumResp(a.Storefront, a.ID, a.Language, token)
	if err != nil {
		return errors.New("error getting album response")
	}
	a.Resp = *resp
	a.Name = a.Resp.Data[0].Attributes.Name

	for i, trackData := range a.Resp.Data[0].Relationships.Tracks.Data {
		len := len(a.Resp.Data[0].Relationships.Tracks.Data)
		a.Tracks = append(a.Tracks, Track{
			ID:         trackData.ID,
			Type:       trackData.Type,
			Name:       trackData.Attributes.Name,
			Language:   a.Language,
			Storefront: a.Storefront,

			TaskNum:   i + 1,
			TaskTotal: len,
			M3u8:      trackData.Attributes.ExtendedAssetUrls.EnhancedHls,
			WebM3u8:   trackData.Attributes.ExtendedAssetUrls.EnhancedHls,

			Resp:      trackData,
			PreType:   "albums",
			DiscTotal: a.Resp.Data[0].Relationships.Tracks.Data[len-1].Attributes.DiscNumber,
			PreID:     a.ID,
			AlbumData: a.Resp.Data[0],
		})
	}
	return nil
}

func (a *Album) ShowSelect() []int {
	meta := a.Resp
	trackTotal := len(meta.Data[0].Relationships.Tracks.Data)
	arr := make([]int, trackTotal)
	for i := 0; i < trackTotal; i++ {
		arr[i] = i + 1
	}

	// Display all available tracks
	fmt.Println("\nAvailable Tracks:")

	// Create table with same styling as relationships table
	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader([]string{"NO.", "TRACK NAME", "RATING", "TYPE"})
	table.SetAutoWrapText(false)
	table.SetAutoFormatHeaders(true)
	table.SetHeaderAlignment(tablewriter.ALIGN_LEFT)
	table.SetAlignment(tablewriter.ALIGN_LEFT)
	table.SetColWidth(50)
	table.SetNoWhiteSpace(true)
	table.SetBorder(false)
	table.SetHeaderLine(false)
	table.SetColumnSeparator("  ")
	table.SetCenterSeparator("")
	table.SetTablePadding("\t")

	for trackNum, track := range meta.Data[0].Relationships.Tracks.Data {
		trackNum++

		// Format track name with truncation
		trackName := track.Attributes.Name
		if len(trackName) > 50 {
			trackName = trackName[:47] + "..."
		}

		// Format rating
		rating := "None"
		switch track.Attributes.ContentRating {
		case "explicit":
			rating = "E"
		case "clean":
			rating = "C"
		}

		// Format type
		trackType := "SONG"
		if track.Type == "music-videos" {
			trackType = "MV"
		}

		table.Append([]string{
			fmt.Sprintf("%d", trackNum),
			trackName,
			rating,
			trackType,
		})
	}

	table.Render()

	// Show missing tracks info
	missingTracks := meta.Data[0].Attributes.TrackCount - trackTotal
	if missingTracks > 0 {
		fmt.Printf("\nNote: %d tracks missing in storefront %s\n", missingTracks, strings.ToUpper(a.Storefront))
	}

	// Ask user input for selection (same style as main.go)
	reader := bufio.NewReader(os.Stdin)
	fmt.Println("\nSelect from the track options above (multiple options separated by commas, ranges supported, or type 'all' to select all)")
	fmt.Print("Enter your choice: ")
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)

	if input == "" {
		fmt.Println("No option selected, skipping...")
		return []int{}
	}

	if input == "all" {
		fmt.Println("You have selected all options:")
		return arr
	}

	// Parse numeric or range input (same logic as main.go)
	selected := []int{}
	parts := strings.Split(input, ",")
	selectedOptions := [][]string{}

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
			if num > 0 && num <= trackTotal {
				trackData := meta.Data[0].Relationships.Tracks.Data[num-1]
				trackName := fmt.Sprintf("%02d. %s", trackData.Attributes.TrackNumber, trackData.Attributes.Name)
				fmt.Printf("  %d. %s\n", num, trackName)
				selected = append(selected, num)
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
			if start < 1 || end > trackTotal || start > end {
				fmt.Println("Range out of range:", opt)
				continue
			}
			for i := start; i <= end; i++ {
				trackData := meta.Data[0].Relationships.Tracks.Data[i-1]
				trackName := fmt.Sprintf("%02d. %s", trackData.Attributes.TrackNumber, trackData.Attributes.Name)
				fmt.Printf("  %d. %s\n", i, trackName)
				selected = append(selected, i)
			}
		} else {
			fmt.Println("Invalid option:", opt)
		}
	}

	return selected
}
