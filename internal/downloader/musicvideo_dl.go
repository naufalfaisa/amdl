// ============================================
// File: internal/downloader/musicvideo_dl.go
package downloader

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"main/internal/artwork"
	"main/internal/config"
	"main/internal/helpers"
	"main/internal/media"
	"main/pkg/ampapi"
	"main/pkg/runv3"
	"main/pkg/task"
)

// DownloadMusicVideo downloads a music video
func DownloadMusicVideo(adamID string, saveDir string, token string, storefront string,
	mediaUserToken string, track *task.Track, cfg *config.Config) error {

	MVInfo, err := ampapi.GetMusicVideoResp(storefront, adamID, cfg.Language, token)
	if err != nil {
		fmt.Println("\u26A0 Failed to get MV manifest:", err)
		return nil
	}

	if track == nil && cfg.MVSaveFolder != "" {
		artistName := helpers.SanitizeFilename(MVInfo.Data[0].Attributes.ArtistName)
		saveDir = filepath.Join(cfg.MVSaveFolder, artistName)
	}

	if strings.HasSuffix(saveDir, ".") {
		saveDir = strings.ReplaceAll(saveDir, ".", "")
	}
	saveDir = strings.TrimSpace(saveDir)

	var mvSaveName string

	if cfg.MVFolderFormat != "" {
		// Extract info for placeholders
		releaseYear := ""
		if len(MVInfo.Data[0].Attributes.ReleaseDate) >= 4 {
			releaseYear = MVInfo.Data[0].Attributes.ReleaseDate[:4]
		}

		quality := fmt.Sprintf("%dp", cfg.MVMax)

		// Build filename from template
		mvSaveName = strings.NewReplacer(
			"{ArtistName}", config.LimitString(MVInfo.Data[0].Attributes.ArtistName, cfg.LimitMax),
			"{MVName}", config.LimitString(MVInfo.Data[0].Attributes.Name, cfg.LimitMax),
			"{ReleaseYear}", releaseYear,
			"{ReleaseDate}", MVInfo.Data[0].Attributes.ReleaseDate,
			"{MVId}", adamID,
			"{Quality}", quality,
		).Replace(cfg.MVFolderFormat)

		// Add track-specific placeholders if MV is from album/playlist
		if track != nil {
			mvSaveName = strings.NewReplacer(
				"{TrackNumber}", fmt.Sprintf("%02d", track.TaskNum),
				"{AlbumName}", config.LimitString(track.AlbumData.Attributes.Name, cfg.LimitMax),
			).Replace(mvSaveName)
		}
	} else {
		// Fallback to default format
		if track != nil {
			mvSaveName = fmt.Sprintf("%02d. %s", track.TaskNum, MVInfo.Data[0].Attributes.Name)
		} else {
			mvSaveName = fmt.Sprintf("%s (%s)", MVInfo.Data[0].Attributes.Name, adamID)
		}
	}

	mvSaveName = helpers.SanitizeFilename(mvSaveName)

	vidPath := filepath.Join(saveDir, fmt.Sprintf("%s_vid.mp4", adamID))
	audPath := filepath.Join(saveDir, fmt.Sprintf("%s_aud.mp4", adamID))
	// mvSaveName := fmt.Sprintf("%s (%s)", MVInfo.Data[0].Attributes.Name, adamID)
	// if track != nil {
	// 	mvSaveName = fmt.Sprintf("%02d. %s", track.TaskNum, MVInfo.Data[0].Attributes.Name)
	// }
	mvOutPath := filepath.Join(saveDir, fmt.Sprintf("%s.mp4", mvSaveName))

	fmt.Println(MVInfo.Data[0].Attributes.Name)

	exists, _ := helpers.FileExists(mvOutPath)
	if exists {
		fmt.Println("MV already exists locally.")
		return nil
	}

	mvm3u8url, _, _, _ := runv3.GetWebplayback(adamID, token, mediaUserToken, true)
	if mvm3u8url == "" {
		return errors.New("media-user-token may wrong or expired")
	}

	os.MkdirAll(saveDir, os.ModePerm)

	videom3u8url, _ := media.ExtractVideo(mvm3u8url, cfg.MVMax)
	videokeyAndUrls, _ := runv3.Run(adamID, videom3u8url, token, mediaUserToken, true, "")
	_ = runv3.ExtMvData(videokeyAndUrls, vidPath)
	defer os.Remove(vidPath)

	audiom3u8url, _ := media.ExtractMvAudio(mvm3u8url, cfg.MVAudioType)
	audiokeyAndUrls, _ := runv3.Run(adamID, audiom3u8url, token, mediaUserToken, true, "")
	_ = runv3.ExtMvData(audiokeyAndUrls, audPath)
	defer os.Remove(audPath)

	tags := []string{
		"tool=",
		fmt.Sprintf("artist=%s", MVInfo.Data[0].Attributes.ArtistName),
		fmt.Sprintf("title=%s", MVInfo.Data[0].Attributes.Name),
		fmt.Sprintf("genre=%s", MVInfo.Data[0].Attributes.GenreNames[0]),
		fmt.Sprintf("created=%s", MVInfo.Data[0].Attributes.ReleaseDate),
		fmt.Sprintf("ISRC=%s", MVInfo.Data[0].Attributes.Isrc),
	}

	switch MVInfo.Data[0].Attributes.ContentRating {
	case "explicit":
		tags = append(tags, "rating=1")
	case "clean":
		tags = append(tags, "rating=2")
	default:
		tags = append(tags, "rating=0")
	}

	if track != nil {
		if track.PreType == "playlists" && !cfg.UseSongInfoForPlaylist {
			tags = append(tags, "disk=1/1")
			tags = append(tags, fmt.Sprintf("album=%s", track.PlaylistData.Attributes.Name))
			tags = append(tags, fmt.Sprintf("track=%d", track.TaskNum))
			tags = append(tags, fmt.Sprintf("tracknum=%d/%d", track.TaskNum, track.TaskTotal))
			tags = append(tags, fmt.Sprintf("album_artist=%s", track.PlaylistData.Attributes.ArtistName))
			tags = append(tags, fmt.Sprintf("performer=%s", track.Resp.Attributes.ArtistName))
		} else if track.PreType == "playlists" && cfg.UseSongInfoForPlaylist {
			tags = append(tags, fmt.Sprintf("album=%s", track.AlbumData.Attributes.Name))
			tags = append(tags, fmt.Sprintf("disk=%d/%d", track.Resp.Attributes.DiscNumber, track.DiscTotal))
			tags = append(tags, fmt.Sprintf("track=%d", track.Resp.Attributes.TrackNumber))
			tags = append(tags, fmt.Sprintf("tracknum=%d/%d", track.Resp.Attributes.TrackNumber, track.AlbumData.Attributes.TrackCount))
			tags = append(tags, fmt.Sprintf("album_artist=%s", track.AlbumData.Attributes.ArtistName))
			tags = append(tags, fmt.Sprintf("performer=%s", track.Resp.Attributes.ArtistName))
			tags = append(tags, fmt.Sprintf("copyright=%s", track.AlbumData.Attributes.Copyright))
			tags = append(tags, fmt.Sprintf("UPC=%s", track.AlbumData.Attributes.Upc))
		} else {
			tags = append(tags, fmt.Sprintf("album=%s", track.AlbumData.Attributes.Name))
			tags = append(tags, fmt.Sprintf("disk=%d/%d", track.Resp.Attributes.DiscNumber, track.DiscTotal))
			tags = append(tags, fmt.Sprintf("track=%d", track.Resp.Attributes.TrackNumber))
			tags = append(tags, fmt.Sprintf("tracknum=%d/%d", track.Resp.Attributes.TrackNumber, track.AlbumData.Attributes.TrackCount))
			tags = append(tags, fmt.Sprintf("album_artist=%s", track.AlbumData.Attributes.ArtistName))
			tags = append(tags, fmt.Sprintf("performer=%s", track.Resp.Attributes.ArtistName))
			tags = append(tags, fmt.Sprintf("copyright=%s", track.AlbumData.Attributes.Copyright))
			tags = append(tags, fmt.Sprintf("UPC=%s", track.AlbumData.Attributes.Upc))
		}
	} else {
		tags = append(tags, fmt.Sprintf("album=%s", MVInfo.Data[0].Attributes.AlbumName))
		tags = append(tags, fmt.Sprintf("disk=%d", MVInfo.Data[0].Attributes.DiscNumber))
		tags = append(tags, fmt.Sprintf("track=%d", MVInfo.Data[0].Attributes.TrackNumber))
		tags = append(tags, fmt.Sprintf("tracknum=%d", MVInfo.Data[0].Attributes.TrackNumber))
		tags = append(tags, fmt.Sprintf("performer=%s", MVInfo.Data[0].Attributes.ArtistName))
	}

	var covPath string
	if true {
		thumbURL := MVInfo.Data[0].Attributes.Artwork.URL
		baseThumbName := helpers.SanitizeFilename(mvSaveName) + "_thumbnail"
		covPath, err = artwork.WriteCover(saveDir, baseThumbName, thumbURL, cfg.CoverFormat, cfg.CoverSize)
		if err != nil {
			fmt.Println("Failed to save MV thumbnail:", err)
		} else {
			tags = append(tags, fmt.Sprintf("cover=%s", covPath))
		}
	}
	defer os.Remove(covPath)

	tagsString := strings.Join(tags, ":")
	muxCmd := exec.Command("MP4Box", "-itags", tagsString, "-quiet", "-add", vidPath, "-add", audPath, "-keep-utc", "-new", mvOutPath)
	fmt.Printf("MV Remuxing...")
	if err := muxCmd.Run(); err != nil {
		fmt.Printf("MV mux failed: %v\n", err)
		return err
	}
	fmt.Printf("\rMV Remuxed.   \n")
	return nil
}
