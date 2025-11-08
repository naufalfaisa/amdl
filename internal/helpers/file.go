// ============================================
// File: internal/helpers/file.go
package helpers

import (
	"os"
	"path/filepath"
	"regexp"
)

var ForbiddenNames = regexp.MustCompile(`[/\\<>:"|?*]`)

func FileExists(path string) (bool, error) {
	f, err := os.Stat(path)
	if err == nil {
		return !f.IsDir(), nil
	} else if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func WriteLyrics(albumFolder, filename string, lrc string) error {
	lyricspath := filepath.Join(albumFolder, filename)
	f, err := os.Create(lyricspath)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(lrc)
	return err
}

func SanitizeFilename(name string) string {
	return ForbiddenNames.ReplaceAllString(name, "_")
}
