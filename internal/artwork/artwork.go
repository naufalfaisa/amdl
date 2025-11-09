// ============================================
// File: internal/artwork/artwork.go
package artwork

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"main/internal/helpers"
)

// WriteCover downloads and saves cover art
func WriteCover(albumFolder, name string, coverUrl string, coverFormat string, coverSize string) (string, error) {
	originalUrl := coverUrl
	var ext string
	var covPath string

	if coverFormat == "original" {
		ext = strings.Split(coverUrl, "/")[len(strings.Split(coverUrl, "/"))-2]
		ext = ext[strings.LastIndex(ext, ".")+1:]
		covPath = filepath.Join(albumFolder, name+"."+ext)
	} else {
		covPath = filepath.Join(albumFolder, name+"."+coverFormat)
	}

	exists, err := helpers.FileExists(covPath)
	if err != nil {
		fmt.Println("Failed to check if cover exists.")
		return "", err
	}
	if exists {
		_ = os.Remove(covPath)
	}

	if coverFormat == "png" {
		re := regexp.MustCompile(`\{w\}x\{h\}`)
		parts := re.Split(coverUrl, 2)
		coverUrl = parts[0] + "{w}x{h}" + strings.Replace(parts[1], ".jpg", ".png", 1)
	}

	coverUrl = strings.Replace(coverUrl, "{w}x{h}", coverSize, 1)

	if coverFormat == "original" {
		coverUrl = strings.Replace(coverUrl, "is1-ssl.mzstatic.com/image/thumb", "a5.mzstatic.com/us/r1000/0", 1)
		coverUrl = coverUrl[:strings.LastIndex(coverUrl, "/")]
	}

	req, err := http.NewRequest("GET", coverUrl, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36")

	do, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer do.Body.Close()

	if do.StatusCode != http.StatusOK {
		if coverFormat == "original" {
			fmt.Println("Failed to get cover, falling back to " + ext + " url.")
			splitByDot := strings.Split(originalUrl, ".")
			last := splitByDot[len(splitByDot)-1]
			fallback := originalUrl[:len(originalUrl)-len(last)] + ext
			fallback = strings.Replace(fallback, "{w}x{h}", coverSize, 1)
			fmt.Println("Fallback URL:", fallback)

			req, err = http.NewRequest("GET", fallback, nil)
			if err != nil {
				fmt.Println("Failed to create request for fallback url.")
				return "", err
			}
			req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36")

			do, err = http.DefaultClient.Do(req)
			if err != nil {
				fmt.Println("Failed to get cover from fallback url.")
				return "", err
			}
			defer do.Body.Close()

			if do.StatusCode != http.StatusOK {
				fmt.Println(fallback)
				return "", errors.New(do.Status)
			}
		} else {
			return "", errors.New(do.Status)
		}
	}

	f, err := os.Create(covPath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	_, err = io.Copy(f, do.Body)
	if err != nil {
		return "", err
	}

	return covPath, nil
}

// DownloadAnimatedArtwork downloads and processes animated artwork
func DownloadAnimatedArtwork(folderPath string, videoUrl string, artworkType string) error {
	var filename string
	if artworkType == "square" {
		filename = "square_animated_artwork.mp4"
	} else {
		filename = "tall_animated_artwork.mp4"
	}

	outputPath := filepath.Join(folderPath, filename)

	exists, err := helpers.FileExists(outputPath)
	if err != nil {
		fmt.Println("Failed to check if animated artwork exists.")
		return err
	}

	if exists {
		fmt.Printf("Animated artwork %s already exists locally.\n", artworkType)
		return nil
	}

	fmt.Printf("Animation Artwork %s Downloading...\n", strings.Title(artworkType))

	cmd := exec.Command("ffmpeg", "-loglevel", "quiet", "-y", "-i", videoUrl, "-c", "copy", outputPath)
	if err := cmd.Run(); err != nil {
		fmt.Printf("animated artwork %s dl err: %v\n", artworkType, err)
		return err
	}

	fmt.Printf("Animation Artwork %s Downloaded\n", strings.Title(artworkType))
	return nil
}

// ConvertToEmbyFormat converts animated artwork to Emby-compatible GIF
func ConvertToEmbyFormat(folderPath string) error {
	inputPath := filepath.Join(folderPath, "square_animated_artwork.mp4")
	outputPath := filepath.Join(folderPath, "folder.jpg")

	exists, err := helpers.FileExists(inputPath)
	if err != nil || !exists {
		return errors.New("source animated artwork not found")
	}

	cmd := exec.Command("ffmpeg", "-i", inputPath, "-vf", "scale=440:-1", "-r", "24", "-f", "gif", outputPath)
	if err := cmd.Run(); err != nil {
		fmt.Printf("animated artwork square to gif err: %v\n", err)
		return err
	}

	return nil
}

// ProcessAnimatedArtwork handles the complete animated artwork workflow
func ProcessAnimatedArtwork(folderPath string, squareVideoUrl string, tallVideoUrl string, embyFormat bool) {
	if squareVideoUrl != "" {
		fmt.Println("Found Animation Artwork.")

		err := DownloadAnimatedArtwork(folderPath, squareVideoUrl, "square")
		if err == nil && embyFormat {
			err = ConvertToEmbyFormat(folderPath)
			if err != nil {
				fmt.Println("Failed to convert to Emby format:", err)
			}
		}
	}

	if tallVideoUrl != "" {
		err := DownloadAnimatedArtwork(folderPath, tallVideoUrl, "tall")
		if err != nil {
			fmt.Println("Failed to download tall animated artwork:", err)
		}
	}
}
