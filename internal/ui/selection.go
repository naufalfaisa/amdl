package ui

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"

	"main/internal/api"

	"github.com/fatih/color"
	"github.com/olekukonko/tablewriter"
)

// SelectArtistItems displays artist items and prompts user for selection.
func SelectArtistItems(items []api.ArtistItem, relationship string) ([]string, error) {
	var args []string
	var urls []string
	var options [][]string

	table := tablewriter.NewWriter(os.Stdout)
	switch relationship {
	case "albums":
		table.SetHeader([]string{"", "Album Name", "Date", "Album ID"})
	case "music-videos":
		table.SetHeader([]string{"", "MV Name", "Date", "MV ID"})
	}
	table.SetRowLine(false)
	table.SetHeaderColor(tablewriter.Colors{},
		tablewriter.Colors{tablewriter.FgRedColor, tablewriter.Bold},
		tablewriter.Colors{tablewriter.Bold, tablewriter.FgBlackColor},
		tablewriter.Colors{tablewriter.Bold, tablewriter.FgBlackColor})

	table.SetColumnColor(tablewriter.Colors{tablewriter.FgCyanColor},
		tablewriter.Colors{tablewriter.Bold, tablewriter.FgRedColor},
		tablewriter.Colors{tablewriter.Bold, tablewriter.FgBlackColor},
		tablewriter.Colors{tablewriter.Bold, tablewriter.FgBlackColor})

	for i, v := range items {
		urls = append(urls, v.URL)
		options = append(options, []string{v.Name, v.ReleaseDate, v.ID})
		row := append([]string{fmt.Sprint(i + 1)}, v.Name, v.ReleaseDate, v.ID)
		table.Append(row)
	}
	table.Render()

	// Logic for "all-album" flag should be handled by caller by passing a flag or checking it before calling this?
	// The original checkArtist had `if artist_select { return urls, nil }` where `artist_select` was a global.
	// We'll leave that decision to the caller or allow passing a "selectAll" boolean.

	reader := bufio.NewReader(os.Stdin)
	fmt.Println("Please select from the " + relationship + " options above (multiple options separated by commas, ranges supported, or type 'all' to select all)")
	cyanColor := color.New(color.FgCyan)
	cyanColor.Print("Enter your choice: ")
	input, _ := reader.ReadString('\n')

	input = strings.TrimSpace(input)
	if input == "all" {
		fmt.Println("You have selected all options:")
		return urls, nil
	}

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
