// ============================================
// File: internal/downloader/playlist_dl.go
package downloader

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/naufalfaisa/amdl/internal/artwork"
	"github.com/naufalfaisa/amdl/internal/config"
	"github.com/naufalfaisa/amdl/internal/helpers"
	"github.com/naufalfaisa/amdl/internal/media"
	"github.com/naufalfaisa/amdl/pkg/ampapi"
	"github.com/naufalfaisa/amdl/pkg/structs"
	"github.com/naufalfaisa/amdl/pkg/task"
)

// RipPlaylist downloads an entire playlist
func RipPlaylist(playlistId string, token string, storefront string, mediaUserToken string,
	cfg *config.Config, counter *structs.Counter, okDict map[string][]int,
	dlAtmos bool, dlAAC bool, dlSelect bool, debugMode bool) error {

	playlist := task.NewPlaylist(storefront, playlistId)
	err := playlist.GetResp(token, cfg.Language)
	if err != nil {
		fmt.Println("Failed to get playlist response.")
		return err
	}

	meta := playlist.Resp

	if debugMode {
		fmt.Println(meta.Data[0].Attributes.ArtistName)
		fmt.Println(meta.Data[0].Attributes.Name)

		for trackNum, track := range meta.Data[0].Relationships.Tracks.Data {
			trackNum++
			fmt.Printf("Track %d of %d:\n", trackNum, len(meta.Data[0].Relationships.Tracks.Data))
			fmt.Printf("%02d. %s\n", trackNum, track.Attributes.Name)

			manifest, err := ampapi.GetSongResp(storefront, track.ID, playlist.Language, token)
			if err != nil {
				fmt.Printf("Failed to get manifest for track %d: %v\n", trackNum, err)
				continue
			}

			var m3u8Url string
			if manifest.Data[0].Attributes.ExtendedAssetUrls.EnhancedHls != "" {
				m3u8Url = manifest.Data[0].Attributes.ExtendedAssetUrls.EnhancedHls
			}

			needCheck := false
			if cfg.GetM3u8Mode == "all" {
				needCheck = true
			} else if cfg.GetM3u8Mode == "hires" && helpers.Contains(track.Attributes.AudioTraits, "hi-res-lossless") {
				needCheck = true
			}

			if needCheck {
				fullM3u8Url, err := media.CheckM3u8(track.ID, "song", cfg.GetM3u8FromDevice, cfg.GetM3u8Port)
				if err == nil && strings.HasSuffix(fullM3u8Url, ".m3u8") {
					m3u8Url = fullM3u8Url
				} else {
					fmt.Println("Failed to get best quality m3u8 from device m3u8 port, will use m3u8 from Web API")
				}
			}

			// Display debug info
			if m3u8Url != "" {
				err = media.DisplayDebugInfo(m3u8Url, cfg)
				if err != nil {
					fmt.Printf("Failed to display debug info: %v\n", err)
				}
			} else {
				fmt.Println("No m3u8 URL available")
			}
		}
		return nil
	}

	var Codec string
	if dlAtmos {
		Codec = "ATMOS"
	} else if dlAAC {
		Codec = "AAC"
	} else {
		Codec = "ALAC"
	}
	playlist.Codec = Codec

	var singerFoldername string
	if cfg.ArtistFolderFormat != "" {
		singerFoldername = strings.NewReplacer(
			"{ArtistName}", "Apple Music",
			"{ArtistId}", "",
			"{UrlArtistName}", "Apple Music",
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
	playlist.SaveDir = singerFolder

	var Quality string
	if strings.Contains(cfg.AlbumFolderFormat, "Quality") {
		if dlAtmos {
			Quality = fmt.Sprintf("%dKbps", cfg.AtmosMax-2000)
		} else if dlAAC && cfg.AacType == "aac-lc" {
			Quality = "256Kbps"
		} else {
			manifest1, err := ampapi.GetSongResp(storefront, meta.Data[0].Relationships.Tracks.Data[0].ID, playlist.Language, token)
			if err != nil {
				fmt.Println("Failed to get manifest.\n", err)
			} else {
				if manifest1.Data[0].Attributes.ExtendedAssetUrls.EnhancedHls == "" {
					Codec = "AAC"
					Quality = "256Kbps"
				} else {
					needCheck := false
					if cfg.GetM3u8Mode == "all" {
						needCheck = true
					} else if cfg.GetM3u8Mode == "hires" && helpers.Contains(meta.Data[0].Relationships.Tracks.Data[0].Attributes.AudioTraits, "hi-res-lossless") {
						needCheck = true
					}
					var EnhancedHls_m3u8 string
					if needCheck {
						EnhancedHls_m3u8, _ = media.CheckM3u8(meta.Data[0].Relationships.Tracks.Data[0].ID, "album", cfg.GetM3u8FromDevice, cfg.GetM3u8Port)
						if strings.HasSuffix(EnhancedHls_m3u8, ".m3u8") {
							manifest1.Data[0].Attributes.ExtendedAssetUrls.EnhancedHls = EnhancedHls_m3u8
						}
					}
					_, Quality, err = media.ExtractMedia(manifest1.Data[0].Attributes.ExtendedAssetUrls.EnhancedHls, true, dlAtmos, dlAAC, cfg)
					if err != nil {
						fmt.Println("Failed to extract quality from manifest.\n", err)
					}
				}
			}
		}
	}

	stringsToJoin := []string{}
	if meta.Data[0].Attributes.IsAppleDigitalMaster || meta.Data[0].Attributes.IsMasteredForItunes {
		if cfg.AppleMasterChoice != "" {
			stringsToJoin = append(stringsToJoin, cfg.AppleMasterChoice)
		}
	}
	if meta.Data[0].Attributes.ContentRating == "explicit" {
		if cfg.ExplicitChoice != "" {
			stringsToJoin = append(stringsToJoin, cfg.ExplicitChoice)
		}
	}
	if meta.Data[0].Attributes.ContentRating == "clean" {
		if cfg.CleanChoice != "" {
			stringsToJoin = append(stringsToJoin, cfg.CleanChoice)
		}
	}
	Tag_string := strings.Join(stringsToJoin, " ")

	playlistFolder := strings.NewReplacer(
		"{ArtistName}", "Apple Music",
		"{PlaylistName}", config.LimitString(meta.Data[0].Attributes.Name, cfg.LimitMax),
		"{PlaylistId}", playlistId,
		"{Quality}", Quality,
		"{Codec}", Codec,
		"{Tag}", Tag_string,
	).Replace(cfg.PlaylistFolderFormat)

	if strings.HasSuffix(playlistFolder, ".") {
		playlistFolder = strings.ReplaceAll(playlistFolder, ".", "")
	}
	playlistFolder = strings.TrimSpace(playlistFolder)
	playlistFolderPath := filepath.Join(singerFolder, helpers.SanitizeFilename(playlistFolder))
	os.MkdirAll(playlistFolderPath, os.ModePerm)
	playlist.SaveName = playlistFolder
	fmt.Println(playlistFolder)

	covPath, err := artwork.WriteCover(playlistFolderPath, "cover", meta.Data[0].Attributes.Artwork.URL, cfg.CoverFormat, cfg.CoverSize)
	if err != nil {
		fmt.Println("Failed to write cover.")
	}

	for i := range playlist.Tracks {
		playlist.Tracks[i].CoverPath = covPath
		playlist.Tracks[i].SaveDir = playlistFolderPath
		playlist.Tracks[i].Codec = Codec
	}

	if cfg.SaveAnimatedArtwork {
		var squareUrl, tallUrl string
		if meta.Data[0].Attributes.EditorialVideo.MotionDetailSquare.Video != "" {
			squareUrl, _ = media.ExtractVideo(meta.Data[0].Attributes.EditorialVideo.MotionDetailSquare.Video, 9999)
		}
		if meta.Data[0].Attributes.EditorialVideo.MotionDetailTall.Video != "" {
			tallUrl, _ = media.ExtractVideo(meta.Data[0].Attributes.EditorialVideo.MotionDetailTall.Video, 9999)
		}
		artwork.ProcessAnimatedArtwork(playlistFolderPath, squareUrl, tallUrl, cfg.EmbyAnimatedArtwork)
	}

	trackTotal := len(meta.Data[0].Relationships.Tracks.Data)
	arr := make([]int, trackTotal)
	for i := 0; i < trackTotal; i++ {
		arr[i] = i + 1
	}

	var selected []int
	if !dlSelect {
		selected = arr
	} else {
		selected = playlist.ShowSelect()
	}

	for i := range playlist.Tracks {
		i++
		if helpers.IsInArray(okDict[playlistId], i) {
			counter.Total++
			counter.Success++
			continue
		}
		if helpers.IsInArray(selected, i) {
			RipTrack(&playlist.Tracks[i-1], token, mediaUserToken, cfg, counter, okDict, dlAtmos, dlAAC)
		}
	}

	return nil
}
