// ============================================
// File: internal/downloader/station.go
package downloader

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"main/internal/artwork"
	"main/internal/config"
	"main/internal/helpers"
	"main/internal/media"
	"main/utils/ampapi"
	"main/utils/runv3"
	"main/utils/structs"
	"main/utils/task"
)

// RipStation downloads station/radio content
func RipStation(albumId string, token string, storefront string, mediaUserToken string,
	cfg *config.Config, counter *structs.Counter, okDict map[string][]int,
	dlAtmos bool, dlAAC bool) error {

	station := task.NewStation(storefront, albumId)
	err := station.GetResp(mediaUserToken, token, cfg.Language)
	if err != nil {
		return err
	}
	fmt.Println(" -", station.Type)
	meta := station.Resp

	var Codec string
	if dlAtmos {
		Codec = "ATMOS"
	} else if dlAAC {
		Codec = "AAC"
	} else {
		Codec = "ALAC"
	}
	station.Codec = Codec

	var singerFoldername string
	if cfg.ArtistFolderFormat != "" {
		singerFoldername = strings.NewReplacer(
			"{ArtistName}", "Apple Music Station",
			"{ArtistId}", "",
			"{UrlArtistName}", "Apple Music Station",
		).Replace(cfg.ArtistFolderFormat)
		if strings.HasSuffix(singerFoldername, ".") {
			singerFoldername = strings.ReplaceAll(singerFoldername, ".", "")
		}
		singerFoldername = strings.TrimSpace(singerFoldername)
		fmt.Println(singerFoldername)
	}

	singerFolder := filepath.Join(cfg.AlacSaveFolder, helpers.SanitizeFilename(singerFoldername))
	if dlAtmos {
		singerFolder = filepath.Join(cfg.AtmosSaveFolder, helpers.SanitizeFilename(singerFoldername))
	}
	if dlAAC {
		singerFolder = filepath.Join(cfg.AacSaveFolder, helpers.SanitizeFilename(singerFoldername))
	}
	os.MkdirAll(singerFolder, os.ModePerm)
	station.SaveDir = singerFolder

	playlistFolder := strings.NewReplacer(
		"{ArtistName}", "Apple Music Station",
		"{PlaylistName}", config.LimitString(station.Name, cfg.LimitMax),
		"{PlaylistId}", station.ID,
		"{Quality}", "",
		"{Codec}", Codec,
		"{Tag}", "",
	).Replace(cfg.PlaylistFolderFormat)

	if strings.HasSuffix(playlistFolder, ".") {
		playlistFolder = strings.ReplaceAll(playlistFolder, ".", "")
	}
	playlistFolder = strings.TrimSpace(playlistFolder)
	playlistFolderPath := filepath.Join(singerFolder, helpers.SanitizeFilename(playlistFolder))
	os.MkdirAll(playlistFolderPath, os.ModePerm)
	station.SaveName = playlistFolder
	fmt.Println(playlistFolder)

	covPath, err := artwork.WriteCover(playlistFolderPath, "cover", meta.Data[0].Attributes.Artwork.URL, cfg.CoverFormat, cfg.CoverSize)
	if err != nil {
		fmt.Println("Failed to write cover.")
	}
	station.CoverPath = covPath

	if cfg.SaveAnimatedArtwork {
		var squareUrl string
		if meta.Data[0].Attributes.EditorialVideo.MotionSquare.Video != "" {
			squareUrl, _ = media.ExtractVideo(meta.Data[0].Attributes.EditorialVideo.MotionSquare.Video, 9999)
		}
		artwork.ProcessAnimatedArtwork(playlistFolderPath, squareUrl, "", cfg.EmbyAnimatedArtwork)
	}

	if station.Type == "stream" {
		counter.Total++
		if helpers.IsInArray(okDict[station.ID], 1) {
			counter.Success++
			return nil
		}

		songName := strings.NewReplacer(
			"{SongId}", station.ID,
			"{SongNumer}", "01",
			"{SongName}", config.LimitString(station.Name, cfg.LimitMax),
			"{DiscNumber}", "1",
			"{TrackNumber}", "1",
			"{Quality}", "256Kbps",
			"{Tag}", "",
			"{Codec}", "AAC",
		).Replace(cfg.SongFileFormat)
		fmt.Println(songName)

		trackPath := filepath.Join(playlistFolderPath, fmt.Sprintf("%s.m4a", helpers.SanitizeFilename(songName)))
		exists, _ := helpers.FileExists(trackPath)
		if exists {
			counter.Success++
			okDict[station.ID] = append(okDict[station.ID], 1)
			fmt.Println("Radio already exists locally.")
			return nil
		}

		assetsUrl, serverUrl, err := ampapi.GetStationAssetsUrlAndServerUrl(station.ID, mediaUserToken, token)
		if err != nil {
			fmt.Println("Failed to get station assets url.", err)
			counter.Error++
			return err
		}

		trackM3U8 := strings.ReplaceAll(assetsUrl, "index.m3u8", "256/prog_index.m3u8")
		keyAndUrls, _ := runv3.Run(station.ID, trackM3U8, token, mediaUserToken, true, serverUrl)
		err = runv3.ExtMvData(keyAndUrls, trackPath)
		if err != nil {
			fmt.Println("Failed to download station stream.", err)
			counter.Error++
			return err
		}

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
		if cfg.EmbedCover {
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

	for i := range station.Tracks {
		station.Tracks[i].CoverPath = covPath
		station.Tracks[i].SaveDir = playlistFolderPath
		station.Tracks[i].Codec = Codec
	}

	trackTotal := len(station.Tracks)
	arr := make([]int, trackTotal)
	for i := range trackTotal {
		arr[i] = i + 1
	}

	selected := arr

	for i := range station.Tracks {
		i++
		if helpers.IsInArray(selected, i) {
			RipTrack(&station.Tracks[i-1], token, mediaUserToken, cfg, counter, okDict, dlAtmos, dlAAC)
		}
	}

	return nil
}
