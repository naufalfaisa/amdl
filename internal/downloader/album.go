// ============================================
// File: internal/downloader/album.go
package downloader

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"main/internal/artwork"
	"main/internal/config"
	"main/internal/helpers"
	"main/internal/media"
	"main/utils/ampapi"
	"main/utils/structs"
	"main/utils/task"
)

// RipAlbum downloads an entire album
func RipAlbum(albumId string, token string, storefront string, mediaUserToken string, urlArg_i string,
	cfg *config.Config, counter *structs.Counter, okDict map[string][]int,
	dlAtmos bool, dlAAC bool, dlSelect bool, dlSong bool, debugMode bool) error {

	album := task.NewAlbum(storefront, albumId)
	err := album.GetResp(token, cfg.Language)
	if err != nil {
		fmt.Println("Failed to get album response.")
		return err
	}

	meta := album.Resp

	if debugMode {
		fmt.Println(meta.Data[0].Attributes.ArtistName)
		fmt.Println(meta.Data[0].Attributes.Name)

		for trackNum, track := range meta.Data[0].Relationships.Tracks.Data {
			trackNum++
			fmt.Printf("Track %d of %d:\n", trackNum, len(meta.Data[0].Relationships.Tracks.Data))
			fmt.Printf("%02d. %s\n", trackNum, track.Attributes.Name)

			manifest, err := ampapi.GetSongResp(storefront, track.ID, album.Language, token)
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
	album.Codec = Codec

	var singerFoldername string
	if cfg.ArtistFolderFormat != "" {
		if len(meta.Data[0].Relationships.Artists.Data) > 0 {
			singerFoldername = strings.NewReplacer(
				"{UrlArtistName}", config.LimitString(meta.Data[0].Attributes.ArtistName, cfg.LimitMax),
				"{ArtistName}", config.LimitString(meta.Data[0].Attributes.ArtistName, cfg.LimitMax),
				"{ArtistId}", meta.Data[0].Relationships.Artists.Data[0].ID,
			).Replace(cfg.ArtistFolderFormat)
		} else {
			singerFoldername = strings.NewReplacer(
				"{UrlArtistName}", config.LimitString(meta.Data[0].Attributes.ArtistName, cfg.LimitMax),
				"{ArtistName}", config.LimitString(meta.Data[0].Attributes.ArtistName, cfg.LimitMax),
				"{ArtistId}", "",
			).Replace(cfg.ArtistFolderFormat)
		}
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
	album.SaveDir = singerFolder

	var Quality string
	if strings.Contains(cfg.AlbumFolderFormat, "Quality") {
		if dlAtmos {
			Quality = fmt.Sprintf("%dKbps", cfg.AtmosMax-2000)
		} else if dlAAC && cfg.AacType == "aac-lc" {
			Quality = "256Kbps"
		} else {
			manifest1, err := ampapi.GetSongResp(storefront, meta.Data[0].Relationships.Tracks.Data[0].ID, album.Language, token)
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

	switch Codec {
	case "ATMOS":
		singerFolder = filepath.Join(cfg.AtmosSaveFolder, helpers.SanitizeFilename(singerFoldername))
	case "AAC":
		singerFolder = filepath.Join(cfg.AacSaveFolder, helpers.SanitizeFilename(singerFoldername))
	default:
		singerFolder = filepath.Join(cfg.AlacSaveFolder, helpers.SanitizeFilename(singerFoldername))
	}
	os.MkdirAll(singerFolder, os.ModePerm)
	album.SaveDir = singerFolder

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

	var albumFolderName string
	albumFolderName = strings.NewReplacer(
		"{ReleaseDate}", meta.Data[0].Attributes.ReleaseDate,
		"{ReleaseYear}", meta.Data[0].Attributes.ReleaseDate[:4],
		"{ArtistName}", config.LimitString(meta.Data[0].Attributes.ArtistName, cfg.LimitMax),
		"{AlbumName}", config.LimitString(meta.Data[0].Attributes.Name, cfg.LimitMax),
		"{UPC}", meta.Data[0].Attributes.Upc,
		"{RecordLabel}", meta.Data[0].Attributes.RecordLabel,
		"{Copyright}", meta.Data[0].Attributes.Copyright,
		"{AlbumId}", albumId,
		"{Quality}", Quality,
		"{Codec}", Codec,
		"{Tag}", Tag_string,
	).Replace(cfg.AlbumFolderFormat)

	if strings.HasSuffix(albumFolderName, ".") {
		albumFolderName = strings.ReplaceAll(albumFolderName, ".", "")
	}
	albumFolderName = strings.TrimSpace(albumFolderName)
	albumFolderPath := filepath.Join(singerFolder, helpers.SanitizeFilename(albumFolderName))
	os.MkdirAll(albumFolderPath, os.ModePerm)
	album.SaveName = albumFolderName
	fmt.Println(albumFolderName)

	if cfg.SaveArtistCover && len(meta.Data[0].Relationships.Artists.Data) > 0 {
		if meta.Data[0].Relationships.Artists.Data[0].Attributes.Artwork.Url != "" {
			_, err = artwork.WriteCover(singerFolder, "folder", meta.Data[0].Relationships.Artists.Data[0].Attributes.Artwork.Url, cfg.CoverFormat, cfg.CoverSize)
			if err != nil {
				fmt.Println("Failed to write artist cover.")
			}
		}
	}

	covPath, err := artwork.WriteCover(albumFolderPath, "cover", meta.Data[0].Attributes.Artwork.URL, cfg.CoverFormat, cfg.CoverSize)
	if err != nil {
		fmt.Println("Failed to write cover.")
	}

	if cfg.SaveAnimatedArtwork {
		var squareUrl, tallUrl string
		if meta.Data[0].Attributes.EditorialVideo.MotionDetailSquare.Video != "" {
			squareUrl, _ = media.ExtractVideo(meta.Data[0].Attributes.EditorialVideo.MotionDetailSquare.Video, 9999)
		}
		if meta.Data[0].Attributes.EditorialVideo.MotionDetailTall.Video != "" {
			tallUrl, _ = media.ExtractVideo(meta.Data[0].Attributes.EditorialVideo.MotionDetailTall.Video, 9999)
		}
		artwork.ProcessAnimatedArtwork(albumFolderPath, squareUrl, tallUrl, cfg.EmbyAnimatedArtwork)
	}

	for i := range album.Tracks {
		album.Tracks[i].CoverPath = covPath
		album.Tracks[i].SaveDir = albumFolderPath
		album.Tracks[i].Codec = Codec
	}

	trackTotal := len(meta.Data[0].Relationships.Tracks.Data)
	arr := make([]int, trackTotal)
	for i := 0; i < trackTotal; i++ {
		arr[i] = i + 1
	}

	if dlSong {
		if urlArg_i == "" {
		} else {
			for i := range album.Tracks {
				if urlArg_i == album.Tracks[i].ID {
					RipTrack(&album.Tracks[i], token, mediaUserToken, cfg, counter, okDict, dlAtmos, dlAAC)
					return nil
				}
			}
		}
		return nil
	}

	var selected []int
	if !dlSelect {
		selected = arr
	} else {
		selected = album.ShowSelect()
	}

	for i := range album.Tracks {
		i++
		if helpers.IsInArray(okDict[albumId], i) {
			counter.Total++
			counter.Success++
			continue
		}
		if helpers.IsInArray(selected, i) {
			RipTrack(&album.Tracks[i-1], token, mediaUserToken, cfg, counter, okDict, dlAtmos, dlAAC)
		}
	}

	return nil
}
