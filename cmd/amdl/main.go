package main

import (
	"fmt"
	"log"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"main/internal/api"
	"main/internal/config"
	"main/internal/downloader"
	"main/internal/structs"
	"main/internal/ui"
	"main/internal/utils"

	"github.com/spf13/pflag"
)

var (
	forbiddenNames = regexp.MustCompile(`[/\\<>:"|?*]`)
	dl_atmos       bool
	dl_aac         bool
	dl_select      bool
	dl_song        bool
	artist_select  bool
	debug_mode     bool
	alac_max       *int
	atmos_max      *int
	mv_max         *int
	mv_audio_type  *string
	aac_type       *string

	// Config logic handled via internal/config package now, but internal APIs use global config?
	// The internal packages (downloader, etc.) mostly accept ConfigSet struct.
	// But internal/config package has LoadConfig() which loads into a variable?
	// internal/config/config.go has `var Config structs.ConfigSet`.
	// I should use that.

	counter structs.Counter
	okDict  = make(map[string][]int)
)

func main() {
	// 1. Load Config
	cfg, err := config.LoadConfig()
	if err != nil {
		fmt.Printf("load Config failed: %v", err)
		return
	}

	// 2. Auth logic
	token, err := api.GetToken()
	if err != nil {
		if cfg.AuthorizationToken != "" && cfg.AuthorizationToken != "your-authorization-token" {
			token = strings.Replace(cfg.AuthorizationToken, "Bearer ", "", -1)
		} else {
			fmt.Println("Failed to get token.")
			return
		}
	}

	// 3. Flags
	var search_type string
	pflag.StringVar(&search_type, "search", "", "Search for 'album', 'song', or 'artist'. Provide query after flags.")
	pflag.BoolVar(&dl_atmos, "atmos", false, "Enable atmos download mode")
	pflag.BoolVar(&dl_aac, "aac", false, "Enable adm-aac download mode")
	pflag.BoolVar(&dl_select, "select", false, "Enable selective download")
	pflag.BoolVar(&dl_song, "song", false, "Enable single song download mode")
	pflag.BoolVar(&artist_select, "all-album", false, "Download all artist albums")
	pflag.BoolVar(&debug_mode, "debug", false, "Enable debug mode to show audio quality information")
	alac_max = pflag.Int("alac-max", cfg.AlacMax, "Specify the max quality for download alac")
	atmos_max = pflag.Int("atmos-max", cfg.AtmosMax, "Specify the max quality for download atmos")
	aac_type = pflag.String("aac-type", cfg.AacType, "Select AAC type, aac aac-binaural aac-downmix")
	mv_audio_type = pflag.String("mv-audio-type", cfg.MVAudioType, "Select MV audio type, atmos ac3 aac")
	mv_max = pflag.Int("mv-max", cfg.MVMax, "Specify the max quality for download MV")

	pflag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [options] [url1 url2 ...]\n", "amdl")
		fmt.Fprintf(os.Stderr, "Search Usage: %s --search [album|song|artist] [query]\n", "amdl")
		fmt.Println("\nOptions:")
		pflag.PrintDefaults()
	}

	pflag.Parse()

	// Update Config with flags
	cfg.AlacMax = *alac_max
	cfg.AtmosMax = *atmos_max
	cfg.AacType = *aac_type
	cfg.MVAudioType = *mv_audio_type
	cfg.MVMax = *mv_max

	args := pflag.Args()

	// 4. Mode Selection
	if search_type != "" {
		if len(args) == 0 {
			fmt.Println("Error: --search flag requires a query.")
			pflag.Usage()
			return
		}
		// Call UI search handler
		selectedUrl, err := ui.HandleSearch(search_type, args, token, cfg.Storefront, cfg.Language)
		if err != nil {
			fmt.Printf("\nSearch process failed: %v\n", err)
			return
		}
		if selectedUrl == nil {
			fmt.Println("\nExiting.")
			return
		}
		// Replace args with result
		args = []string{selectedUrl.URL}
		// os.Args update is not needed if we use 'args' variable
	} else {
		if len(args) == 0 {
			fmt.Println("No URLs provided. Please provide at least one URL.")
			pflag.Usage()
			return
		}
	}

	// 5. Processing Loop
	// Handle /artist/ URL specifically (expands to albums/MVs)
	finalArgs := []string{}
	for _, rawUrl := range args {
		if strings.Contains(rawUrl, "/artist/") {
			urlArtistName, urlArtistID, err := api.GetUrlArtistName(rawUrl, token, cfg.Language)
			if err != nil {
				fmt.Println("Failed to get artistname.")
				continue
			}
			cfg.ArtistFolderFormat = strings.NewReplacer(
				"{UrlArtistName}", utils.LimitString(urlArtistName, cfg.LimitMax),
				"{ArtistId}", urlArtistID,
			).Replace(cfg.ArtistFolderFormat)

			// Fetch Albums
			albumUrls, err := api.FetchArtistItems(rawUrl, token, "albums", cfg.Language)
			if err != nil {
				fmt.Println("Failed to fetch albums.")
			} else {
				// Select
				selected, err := ui.SelectArtistItems(albumUrls, "albums")
				if err == nil {
					finalArgs = append(finalArgs, selected...)
				}
			}

			// Fetch MVs
			mvUrls, err := api.FetchArtistItems(rawUrl, token, "music-videos", cfg.Language)
			if err != nil {
				fmt.Println("Failed to fetch MVs.")
			} else {
				// Select
				selected, err := ui.SelectArtistItems(mvUrls, "music-videos")
				if err == nil {
					finalArgs = append(finalArgs, selected...)
				}
			}
		} else {
			finalArgs = append(finalArgs, rawUrl)
		}
	}

	// Reset counter
	counter = structs.Counter{}

	// Execution Loop
	execTotal := len(finalArgs)
	for {
		for i, urlRaw := range finalArgs {
			fmt.Printf("Queue %d of %d: ", i+1, execTotal)

			if strings.Contains(urlRaw, "/music-video/") {
				fmt.Println("Music Video")
				if debug_mode {
					continue
				}
				counter.Total++
				if len(cfg.MediaUserToken) <= 50 {
					fmt.Println(": media-user-token is not set, skip MV dl")
					counter.Success++
					continue
				}
				if _, err := exec.LookPath("mp4decrypt"); err != nil {
					fmt.Println(": mp4decrypt is not found, skip MV dl")
					counter.Success++
					continue
				}

				mvSaveDir := strings.NewReplacer(
					"{ArtistName}", "",
					"{UrlArtistName}", "",
					"{ArtistId}", "",
				).Replace(cfg.ArtistFolderFormat)

				if mvSaveDir != "" {
					mvSaveDir = filepath.Join(cfg.AlacSaveFolder, forbiddenNames.ReplaceAllString(mvSaveDir, "_"))
				} else {
					mvSaveDir = cfg.AlacSaveFolder
				}

				storefront, mvId := utils.CheckUrlMv(urlRaw)

				// Call MvDownloader
				err := downloader.MvDownloader(mvId, mvSaveDir, token, storefront, cfg.MediaUserToken, nil, cfg, &counter)
				if err != nil {
					fmt.Println("\u26A0 Failed to dl MV:", err)
					counter.Error++
					continue
				}
				counter.Success++
				continue
			}

			if strings.Contains(urlRaw, "/song/") {
				fmt.Printf("Song->")
				storefront, songId := utils.CheckUrlSong(urlRaw)
				if storefront == "" || songId == "" {
					fmt.Println("Invalid song URL format.")
					continue
				}
				err := downloader.RipSong(songId, token, storefront, cfg.MediaUserToken, cfg, &counter, okDict, dl_atmos, dl_aac)
				if err != nil {
					fmt.Println("Failed to rip song:", err)
				}
				continue
			}

			parse, err := url.Parse(urlRaw)
			if err != nil {
				log.Fatalf("Invalid URL: %v", err)
			}
			var urlArg_i = parse.Query().Get("i")

			if strings.Contains(urlRaw, "/album/") {
				fmt.Println("Album")
				storefront, albumId := utils.CheckUrl(urlRaw)
				err := downloader.RipAlbum(albumId, token, storefront, cfg.MediaUserToken, urlArg_i, cfg, &counter, okDict, dl_atmos, dl_aac, dl_select, debug_mode)
				if err != nil {
					fmt.Println("Failed to rip album:", err)
				}
			} else if strings.Contains(urlRaw, "/playlist/") {
				fmt.Println("Playlist")
				storefront, playlistId := utils.CheckUrlPlaylist(urlRaw)
				err := downloader.RipPlaylist(playlistId, token, storefront, cfg.MediaUserToken, cfg, &counter, okDict, dl_atmos, dl_aac, dl_select)
				if err != nil {
					fmt.Println("Failed to rip playlist:", err)
				}
			} else if strings.Contains(urlRaw, "/station/") {
				fmt.Printf("Station")
				storefront, stationId := utils.CheckUrlStation(urlRaw)
				if len(cfg.MediaUserToken) <= 50 {
					fmt.Println(": media-user-token is not set, skip station dl")
					continue
				}
				err := downloader.RipStation(stationId, token, storefront, cfg.MediaUserToken, cfg, &counter, okDict, dl_atmos, dl_aac, dl_select)
				if err != nil {
					fmt.Println("Failed to rip station:", err)
				}
			} else {
				fmt.Println("Invalid type")
			}
		}

		fmt.Printf("=======  [\u2714 ] Completed: %d/%d  |  [\u26A0 ] Warnings: %d  |  [\u2716 ] Errors: %d  =======\n", counter.Success, counter.Total, counter.Unavailable+counter.NotSong, counter.Error)
		if counter.Error == 0 {
			break
		}
		fmt.Println("Error detected, press Enter to try again...")
		fmt.Scanln()
		fmt.Println("Start trying again...")
		counter = structs.Counter{}
	}
}
