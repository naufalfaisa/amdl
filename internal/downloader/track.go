package downloader

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"main/internal/api"
	"main/internal/converter"
	"main/internal/downloader/runv2"
	"main/internal/downloader/runv3"
	"main/internal/lyrics"
	"main/internal/structs"
	"main/internal/tagger"
	"main/internal/task"
	"main/internal/utils"
)

func RipSong(songId string, token string, storefront string, mediaUserToken string, cfg *structs.ConfigSet, counter *structs.Counter, okDict map[string][]int, dl_atmos bool, dl_aac bool) error {
	manifest, err := api.GetSongResp(storefront, songId, cfg.Language, token)
	if err != nil {
		fmt.Println("Failed to get song response.")
		return err
	}

	songData := manifest.Data[0]
	albumId := songData.Relationships.Albums.Data[0].ID

	// Use album approach but only download the specific song
	// dl_song in main implied by passing songId as urlArg_i
	err = RipAlbum(albumId, token, storefront, mediaUserToken, songId, cfg, counter, okDict, dl_atmos, dl_aac, false, false)
	if err != nil {
		fmt.Println("Failed to rip song:", err)
		return err
	}

	return nil
}

func RipTrack(track *task.Track, token string, mediaUserToken string, cfg *structs.ConfigSet, counter *structs.Counter, okDict map[string][]int, dl_atmos bool, dl_aac bool) {
	var err error
	counter.Total++
	fmt.Printf("Track %d of %d: %s\n", track.TaskNum, track.TaskTotal, track.Type)

	//提前获取到的播放列表下track所在的专辑信息
	if track.PreType == "playlists" && cfg.UseSongInfoForPlaylist {
		track.GetAlbumData(token)
	}

	//mv dl dev
	if track.Type == "music-videos" {
		if len(mediaUserToken) <= 50 {
			fmt.Println("meida-user-token is not set, skip MV dl")
			counter.Success++
			return
		}
		// check mp4decrypt using os/exec or similar? main.go used exec.LookPath
		// Moving that check to caller or here? Main check was inside ripTrack.
		// Assuming environment is checked or we check here.
		err := MvDownloader(track.ID, track.SaveDir, token, track.Storefront, mediaUserToken, track, cfg, counter)
		if err != nil {
			fmt.Println("\u26A0 Failed to dl MV:", err)
			counter.Error++
			return
		}
		counter.Success++
		return
	}

	needDlAacLc := false
	if dl_aac && cfg.AacType == "aac-lc" {
		needDlAacLc = true
	}
	if track.WebM3u8 == "" && !needDlAacLc {
		if dl_atmos {
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
	} else if cfg.GetM3u8Mode == "hires" && utils.Contains(track.Resp.Attributes.AudioTraits, "hi-res-lossless") {
		needCheck = true
	}
	var EnhancedHls_m3u8 string
	if needCheck && !needDlAacLc {
		EnhancedHls_m3u8, _ = CheckM3u8(track.ID, "song", cfg)
		if strings.HasSuffix(EnhancedHls_m3u8, ".m3u8") {
			track.DeviceM3u8 = EnhancedHls_m3u8
			track.M3u8 = EnhancedHls_m3u8
		}
	}
	var Quality string
	if strings.Contains(cfg.SongFileFormat, "Quality") {
		if dl_atmos {
			Quality = fmt.Sprintf("%dKbps", cfg.AtmosMax-2000)
		} else if needDlAacLc {
			Quality = "256Kbps"
		} else {
			_, Quality, err = ExtractMedia(track.M3u8, true, cfg, dl_atmos, dl_aac, false) // debug_mode false for now
			if err != nil {
				fmt.Println("Failed to extract quality from manifest.\n", err)
				counter.Error++
				return
			}
		}
	}
	track.Quality = Quality

	stringsToJoin := []string{}
	// IsAppleDigitalMaster field check
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
		"{SongName}", utils.LimitString(track.Resp.Attributes.Name, cfg.LimitMax),
		"{DiscNumber}", fmt.Sprintf("%0d", track.Resp.Attributes.DiscNumber),
		"{TrackNumber}", fmt.Sprintf("%0d", track.Resp.Attributes.TrackNumber),
		"{Quality}", Quality,
		"{Tag}", Tag_string,
		"{Codec}", track.Codec,
	).Replace(cfg.SongFileFormat)
	fmt.Println(songName)
	filename := fmt.Sprintf("%s.m4a", forbiddenNames.ReplaceAllString(songName, "_"))
	track.SaveName = filename
	trackPath := filepath.Join(track.SaveDir, track.SaveName)
	lrcFilename := fmt.Sprintf("%s.%s", forbiddenNames.ReplaceAllString(songName, "_"), cfg.LrcFormat)

	var convertedPath string
	considerConverted := false
	if cfg.ConvertAfterDownload &&
		cfg.ConvertFormat != "" &&
		strings.ToLower(cfg.ConvertFormat) != "copy" &&
		!cfg.ConvertKeepOriginal {
		convertedPath = strings.TrimSuffix(trackPath, filepath.Ext(trackPath)) + "." + strings.ToLower(cfg.ConvertFormat)
		considerConverted = true
	}
	//get lrc
	var lrc string = ""
	if cfg.EmbedLrc || cfg.SaveLrcFile {
		lrcStr, err := lyrics.Get(track.Storefront, track.ID, cfg.LrcType, cfg.Language, cfg.LrcFormat, token, mediaUserToken)
		if err != nil {
			fmt.Println(err)
		} else {
			if cfg.SaveLrcFile {
				err := tagger.WriteLyrics(track.SaveDir, lrcFilename, lrcStr)
				if err != nil {
					fmt.Printf("Failed to write lyrics")
				}
			}
			if cfg.EmbedLrc {
				lrc = lrcStr
			}
		}
	}

	existsOriginal, err := utils.FileExists(trackPath)
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
		existsConverted, err2 := utils.FileExists(convertedPath)
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
		trackM3u8Url, _, err := ExtractMedia(track.M3u8, false, cfg, dl_atmos, dl_aac, false)
		if err != nil {
			fmt.Println("\u26A0 Failed to extract info from manifest:", err)
			counter.Unavailable++
			return
		}
		//边下载边解密
		err = runv2.Run(track.ID, trackM3u8Url, trackPath, *cfg) // check runv2 signature to see if it accepts cfg
		if err != nil {
			fmt.Println("Failed to run v2:", err)
			counter.Error++
			return
		}
	}

	tags := []string{
		"tool=",
		"artist=AppleMusic",
	}
	if cfg.EmbedCover {
		if (strings.Contains(track.PreID, "pl.") || strings.Contains(track.PreID, "ra.")) && cfg.DlAlbumcoverForPlaylist {
			track.CoverPath, err = tagger.WriteCover(track.SaveDir, track.ID, track.Resp.Attributes.Artwork.URL, cfg)
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
	err = tagger.WriteMP4Tags(track, lrc, cfg)
	if err != nil {
		fmt.Println("\u26A0 Failed to write tags in media:", err)
		counter.Unavailable++
		return
	}

	converter.ConvertIfNeeded(track, cfg)

	counter.Success++
	okDict[track.PreID] = append(okDict[track.PreID], track.TaskNum)
}
