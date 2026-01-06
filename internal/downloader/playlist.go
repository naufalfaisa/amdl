package downloader

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"main/internal/structs"
	"main/internal/tagger"
	"main/internal/task"

	"github.com/schollz/progressbar/v3"
)

func RipPlaylist(playlistId string, token string, storefront string, mediaUserToken string, cfg *structs.ConfigSet, counter *structs.Counter, okDict map[string][]int, dl_atmos bool, dl_aac bool, dl_select bool) error {
	playlist := task.NewPlaylist(storefront, playlistId)
	if err := playlist.GetResp(token, cfg.Language); err != nil {
		return err
	}
	fmt.Println(" -", playlist.Name)
	fmt.Println(" -", len(playlist.Tracks), "Tracks")

	// Filter forbidden chars
	forbiddenNames := regexp.MustCompile(`[/\\<>:"|?*]`)

	sanPlaylistFolder := strings.NewReplacer(
		"{ArtistName}", "Apple Music",
		"{PlaylistName}", forbiddenNames.ReplaceAllString(playlist.Name, "_"),
		"{PlaylistId}", playlistId,
	).Replace(cfg.PlaylistFolderFormat)

	if sanPlaylistFolder == "" {
		sanPlaylistFolder = forbiddenNames.ReplaceAllString(playlist.Name, "_")
	} else {
		sanPlaylistFolder = forbiddenNames.ReplaceAllString(sanPlaylistFolder, "_")
	}

	var saveDir string
	if dl_atmos {
		saveDir = filepath.Join(cfg.AtmosSaveFolder, sanPlaylistFolder)
	} else {
		saveDir = filepath.Join(cfg.AlacSaveFolder, sanPlaylistFolder)
	}
	os.MkdirAll(saveDir, os.ModePerm)

	if playlist.Resp.Data[0].Attributes.Artwork.URL != "" {
		_, err := tagger.WriteCover(saveDir, "cover", playlist.Resp.Data[0].Attributes.Artwork.URL, cfg)
		if err != nil {
			fmt.Println("Failed to write playlist cover.")
		}
	}

	bar := progressbar.Default(int64(len(playlist.Tracks)))

	for i := range playlist.Tracks {
		bar.Add(1)
		// Assuming logic: playlist tracks are just tracks.
		// Set SaveDir and other props
		playlist.Tracks[i].SaveDir = saveDir
		// Need to set Codec logic like in album (AAC/ALAC/ATMOS) - Wait, snippet logic might differ.
		// Assuming we pass dl_atmos/dl_aac to ripTrack.

		RipTrack(&playlist.Tracks[i], token, mediaUserToken, cfg, counter, okDict, dl_atmos, dl_aac)
	}
	return nil
}

func RipStation(stationId string, token string, storefront string, mediaUserToken string, cfg *structs.ConfigSet, counter *structs.Counter, okDict map[string][]int, dl_atmos bool, dl_aac bool, dl_select bool) error {
	station := task.NewStation(storefront, stationId)
	err := station.GetResp(mediaUserToken, token, cfg.Language)
	if err != nil {
		return err
	}
	fmt.Println(" -", station.Type)
	fmt.Println(" -", station.Name)

	// Similar logic for folder creation
	forbiddenNames := regexp.MustCompile(`[/\\<>:"|?*]`)
	sanStationFolder := forbiddenNames.ReplaceAllString(station.Name, "_")

	var saveDir string
	if dl_atmos {
		saveDir = filepath.Join(cfg.AtmosSaveFolder, sanStationFolder)
	} else {
		saveDir = filepath.Join(cfg.AlacSaveFolder, sanStationFolder)
	}
	os.MkdirAll(saveDir, os.ModePerm)

	bar := progressbar.Default(int64(len(station.Tracks)))
	for i := range station.Tracks {
		bar.Add(1)
		station.Tracks[i].SaveDir = saveDir
		RipTrack(&station.Tracks[i], token, mediaUserToken, cfg, counter, okDict, dl_atmos, dl_aac)
	}
	return nil
}
