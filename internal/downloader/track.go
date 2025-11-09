// ============================================
// File: internal/downloader/track.go
package downloader

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"main/internal/artwork"
	"main/internal/config"
	"main/internal/converter"
	"main/internal/helpers"
	"main/internal/media"
	"main/internal/tagger"
	"main/utils/lyrics"
	"main/utils/runv2"
	"main/utils/runv3"
	"main/utils/structs"
	"main/utils/task"
)

// RipTrack downloads and processes a single track
func RipTrack(track *task.Track, token string, mediaUserToken string, cfg *config.Config,
	counter *structs.Counter, okDict map[string][]int, dlAtmos bool, dlAAC bool) {
	var err error
	counter.Total++
	fmt.Printf("Track %d of %d: %s\n", track.TaskNum, track.TaskTotal, track.Type)

	// Get album data for playlist tracks if needed
	if track.PreType == "playlists" && cfg.UseSongInfoForPlaylist {
		track.GetAlbumData(token)
	}

	// Handle music videos
	if track.Type == "music-videos" {
		if len(mediaUserToken) <= 50 {
			fmt.Println("meida-user-token is not set, skip MV dl")
			counter.Success++
			return
		}
		if _, err := exec.LookPath("mp4decrypt"); err != nil {
			fmt.Println("mp4decrypt is not found, skip MV dl")
			counter.Success++
			return
		}
		err := DownloadMusicVideo(track.ID, track.SaveDir, token, track.Storefront, mediaUserToken, track, cfg)
		if err != nil {
			fmt.Println("\u26A0 Failed to dl MV:", err)
			counter.Error++
			return
		}
		counter.Success++
		return
	}

	needDlAacLc := false
	if dlAAC && cfg.AacType == "aac-lc" {
		needDlAacLc = true
	}
	if track.WebM3u8 == "" && !needDlAacLc {
		if dlAtmos {
			fmt.Println("Unavailable")
			counter.Unavailable++
			return
		}
		fmt.Println("Unavailable, trying to dl aac-lc")
		needDlAacLc = true
	}

	needCheck := false
	if cfg.GetM3u8Mode == "all" {
		needCheck = true
	} else if cfg.GetM3u8Mode == "hires" && helpers.Contains(track.Resp.Attributes.AudioTraits, "hi-res-lossless") {
		needCheck = true
	}

	var EnhancedHls_m3u8 string
	if needCheck && !needDlAacLc {
		EnhancedHls_m3u8, _ = media.CheckM3u8(track.ID, "song", cfg.GetM3u8FromDevice, cfg.GetM3u8Port)
		if strings.HasSuffix(EnhancedHls_m3u8, ".m3u8") {
			track.DeviceM3u8 = EnhancedHls_m3u8
			track.M3u8 = EnhancedHls_m3u8
		}
	}

	var Quality string
	if strings.Contains(cfg.SongFileFormat, "Quality") {
		if dlAtmos {
			Quality = fmt.Sprintf("%dKbps", cfg.AtmosMax-2000)
		} else if needDlAacLc {
			Quality = "256Kbps"
		} else {
			_, Quality, err = media.ExtractMedia(track.M3u8, true, dlAtmos, dlAAC, cfg)
			if err != nil {
				fmt.Println("Failed to extract quality from manifest.\n", err)
				counter.Error++
				return
			}
		}
	}
	track.Quality = Quality

	stringsToJoin := []string{}
	if track.Resp.Attributes.IsAppleDigitalMaster {
		if cfg.AppleMasterChoice != "" {
			stringsToJoin = append(stringsToJoin, cfg.AppleMasterChoice)
		}
	}
	if track.Resp.Attributes.ContentRating == "explicit" {
		if cfg.ExplicitChoice != "" {
			stringsToJoin = append(stringsToJoin, cfg.ExplicitChoice)
		}
	}
	if track.Resp.Attributes.ContentRating == "clean" {
		if cfg.CleanChoice != "" {
			stringsToJoin = append(stringsToJoin, cfg.CleanChoice)
		}
	}
	Tag_string := strings.Join(stringsToJoin, " ")

	songName := strings.NewReplacer(
		"{SongId}", track.ID,
		"{SongNumer}", fmt.Sprintf("%02d", track.TaskNum),
		"{SongName}", config.LimitString(track.Resp.Attributes.Name, cfg.LimitMax),
		"{DiscNumber}", fmt.Sprintf("%0d", track.Resp.Attributes.DiscNumber),
		"{TrackNumber}", fmt.Sprintf("%0d", track.Resp.Attributes.TrackNumber),
		"{Quality}", Quality,
		"{Tag}", Tag_string,
		"{Codec}", track.Codec,
	).Replace(cfg.SongFileFormat)
	fmt.Println(songName)

	filename := fmt.Sprintf("%s.m4a", helpers.SanitizeFilename(songName))
	track.SaveName = filename
	trackPath := filepath.Join(track.SaveDir, track.SaveName)
	lrcFilename := fmt.Sprintf("%s.%s", helpers.SanitizeFilename(songName), cfg.LrcFormat)

	// Determine possible post-conversion target file
	var convertedPath string
	considerConverted := false
	if cfg.ConvertAfterDownload &&
		cfg.ConvertFormat != "" &&
		strings.ToLower(cfg.ConvertFormat) != "copy" &&
		!cfg.ConvertKeepOriginal {
		convertedPath = strings.TrimSuffix(trackPath, filepath.Ext(trackPath)) + "." + strings.ToLower(cfg.ConvertFormat)
		considerConverted = true
	}

	// Get lyrics
	var lrc string = ""
	if cfg.EmbedLrc || cfg.SaveLrcFile {
		lrcStr, err := lyrics.Get(track.Storefront, track.ID, cfg.LrcType, cfg.Language, cfg.LrcFormat, token, mediaUserToken)
		if err != nil {
			fmt.Println(err)
		} else {
			if cfg.SaveLrcFile {
				err := helpers.WriteLyrics(track.SaveDir, lrcFilename, lrcStr)
				if err != nil {
					fmt.Printf("Failed to write lyrics")
				}
			}
			if cfg.EmbedLrc {
				lrc = lrcStr
			}
		}
	}

	// Existence check
	existsOriginal, err := helpers.FileExists(trackPath)
	if err != nil {
		fmt.Println("Failed to check if track exists.")
	}
	if existsOriginal {
		fmt.Println("Track already exists locally.")
		counter.Success++
		okDict[track.PreID] = append(okDict[track.PreID], track.TaskNum)
		return
	}
	if considerConverted {
		existsConverted, err2 := helpers.FileExists(convertedPath)
		if err2 == nil && existsConverted {
			fmt.Println("Converted track already exists locally.")
			counter.Success++
			okDict[track.PreID] = append(okDict[track.PreID], track.TaskNum)
			return
		}
	}

	if needDlAacLc {
		if len(mediaUserToken) <= 50 {
			fmt.Println("Invalid media-user-token")
			counter.Error++
			return
		}
		_, err := runv3.Run(track.ID, trackPath, token, mediaUserToken, false, "")
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
		trackM3u8Url, _, err := media.ExtractMedia(track.M3u8, false, dlAtmos, dlAAC, cfg)
		if err != nil {
			fmt.Println("\u26A0 Failed to extract info from manifest:", err)
			counter.Unavailable++
			return
		}
		err = runv2.Run(track.ID, trackM3u8Url, trackPath, cfg)
		if err != nil {
			fmt.Println("Failed to run v2:", err)
			counter.Error++
			return
		}
	}

	// MP4Box tagging
	tags := []string{
		"tool=",
		"artist=AppleMusic",
	}
	if cfg.EmbedCover {
		if (strings.Contains(track.PreID, "pl.") || strings.Contains(track.PreID, "ra.")) && cfg.DlAlbumcoverForPlaylist {
			track.CoverPath, err = artwork.WriteCover(track.SaveDir, track.ID, track.Resp.Attributes.Artwork.URL, cfg.CoverFormat, cfg.CoverSize)
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
	if (strings.Contains(track.PreID, "pl.") || strings.Contains(track.PreID, "ra.")) && cfg.DlAlbumcoverForPlaylist {
		if err := os.Remove(track.CoverPath); err != nil {
			fmt.Printf("Error deleting file: %s\n", track.CoverPath)
			counter.Error++
			return
		}
	}
	track.SavePath = trackPath

	err = tagger.WriteMP4Tags(track, lrc, cfg.UseSongInfoForPlaylist)
	if err != nil {
		fmt.Println("\u26A0 Failed to write tags in media:", err)
		counter.Unavailable++
		return
	}

	// Conversion
	converter.ConvertIfNeeded(track, cfg)

	counter.Success++
	okDict[track.PreID] = append(okDict[track.PreID], track.TaskNum)
}
