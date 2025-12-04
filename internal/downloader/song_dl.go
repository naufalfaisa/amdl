// ============================================
// File: internal/downloader/song_dl.go
package downloader

import (
	"fmt"

	"github.com/naufalfaisa/amdl/internal/config"
	"github.com/naufalfaisa/amdl/internal/helpers"
	"github.com/naufalfaisa/amdl/pkg/ampapi"
	"github.com/naufalfaisa/amdl/pkg/structs"
)

// GetUrlSong converts song URL to album URL
func GetUrlSong(songUrl string, token string, cfg *config.Config) (string, error) {
	storefront, songId := helpers.CheckURLSong(songUrl)
	manifest, err := ampapi.GetSongResp(storefront, songId, cfg.Language, token)
	if err != nil {
		fmt.Println("\u26A0 Failed to get manifest:", err)
		return "", err
	}
	albumId := manifest.Data[0].Relationships.Albums.Data[0].ID
	songAlbumUrl := fmt.Sprintf("https://music.apple.com/%s/album/1/%s?i=%s", storefront, albumId, songId)
	return songAlbumUrl, nil
}

// RipSong downloads a single song (converts to album approach)
func RipSong(songId string, token string, storefront string, mediaUserToken string, cfg *config.Config,
	counter *structs.Counter, okDict map[string][]int, dlAtmos bool, dlAAC bool, dlSelect bool, debugMode bool) error {
	// Get song info to find album ID
	manifest, err := ampapi.GetSongResp(storefront, songId, cfg.Language, token)
	if err != nil {
		fmt.Println("Failed to get song response.")
		return err
	}

	songData := manifest.Data[0]
	albumId := songData.Relationships.Albums.Data[0].ID

	// Use album approach but only download the specific song
	err = RipAlbum(albumId, token, storefront, mediaUserToken, songId, cfg, counter, okDict, dlAtmos, dlAAC, dlSelect, true, debugMode)
	if err != nil {
		fmt.Println("Failed to rip song:", err)
		return err
	}

	return nil
}
