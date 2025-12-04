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
	"time"

	"github.com/naufalfaisa/amdl/internal/helpers"
)

const (
	userAgent       = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36"
	placeholderSize = "{w}x{h}"
)

var httpClient = &http.Client{
	Timeout: 30 * time.Second,
}

// WriteCover downloads and saves cover art
func WriteCover(albumFolder, name string, coverUrl string, coverFormat string, coverSize string) (string, error) {
	originalUrl := coverUrl
	var ext string
	var covPath string

	// Determine file path and extension
	if coverFormat == "original" {
		parts := strings.Split(coverUrl, "/")
		if len(parts) < 2 {
			return "", fmt.Errorf("invalid cover URL format")
		}
		ext = parts[len(parts)-2]
		if idx := strings.LastIndex(ext, "."); idx != -1 {
			ext = ext[idx+1:]
		}
		covPath = filepath.Join(albumFolder, name+"."+ext)
	} else {
		covPath = filepath.Join(albumFolder, name+"."+coverFormat)
	}

	// Remove existing file if present
	exists, err := helpers.FileExists(covPath)
	if err != nil {
		return "", fmt.Errorf("check file existence: %w", err)
	}
	if exists {
		if err := os.Remove(covPath); err != nil {
			return "", fmt.Errorf("remove existing file: %w", err)
		}
	}

	// Build cover URL
	coverUrl = buildCoverURL(coverUrl, coverFormat, coverSize)

	// Download cover
	if err := downloadToFile(coverUrl, covPath); err != nil {
		// Try fallback for original format
		if coverFormat == "original" {
			return tryFallbackDownload(originalUrl, ext, coverSize, covPath)
		}
		return "", err
	}

	return covPath, nil
}

// buildCoverURL constructs the final cover URL
func buildCoverURL(coverUrl, format, size string) string {
	// Convert to PNG if requested
	if format == "png" {
		re := regexp.MustCompile(`\{w\}x\{h\}`)
		parts := re.Split(coverUrl, 2)
		if len(parts) == 2 {
			coverUrl = parts[0] + placeholderSize + strings.Replace(parts[1], ".jpg", ".png", 1)
		}
	}

	// Replace size placeholder
	coverUrl = strings.Replace(coverUrl, placeholderSize, size, 1)

	// Handle original format URL transformation
	if format == "original" {
		coverUrl = strings.Replace(coverUrl, "is1-ssl.mzstatic.com/image/thumb", "a5.mzstatic.com/us/r1000/0", 1)
		if idx := strings.LastIndex(coverUrl, "/"); idx != -1 {
			coverUrl = coverUrl[:idx]
		}
	}

	return coverUrl
}

// downloadToFile downloads content from URL to file
func downloadToFile(url, destPath string) error {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed: %s", resp.Status)
	}

	f, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("create file: %w", err)
	}
	defer f.Close()

	if _, err := io.Copy(f, resp.Body); err != nil {
		return fmt.Errorf("write file: %w", err)
	}

	return nil
}

// tryFallbackDownload attempts to download using fallback URL
func tryFallbackDownload(originalUrl, ext, size, destPath string) (string, error) {
	fmt.Println("Failed to get cover, falling back to", ext, "url.")

	splitByDot := strings.Split(originalUrl, ".")
	if len(splitByDot) == 0 {
		return "", errors.New("invalid original URL")
	}

	last := splitByDot[len(splitByDot)-1]
	fallback := originalUrl[:len(originalUrl)-len(last)] + ext
	fallback = strings.Replace(fallback, placeholderSize, size, 1)
	fmt.Println("Fallback URL:", fallback)

	if err := downloadToFile(fallback, destPath); err != nil {
		return "", fmt.Errorf("fallback download failed: %w", err)
	}

	return destPath, nil
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
		return fmt.Errorf("check file existence: %w", err)
	}

	if exists {
		fmt.Printf("Animated artwork %s already exists locally.\n", artworkType)
		return nil
	}

	fmt.Printf("Animation Artwork %s Downloading...\n", strings.Title(artworkType))

	cmd := exec.Command("ffmpeg", "-loglevel", "quiet", "-y", "-i", videoUrl, "-c", "copy", outputPath)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("animated artwork %s download failed: %w", artworkType, err)
	}

	fmt.Printf("Animation Artwork %s Downloaded\n", strings.Title(artworkType))
	return nil
}

// ConvertToEmbyFormat converts animated artwork to Emby-compatible GIF
func ConvertToEmbyFormat(folderPath string) error {
	inputPath := filepath.Join(folderPath, "square_animated_artwork.mp4")
	outputPath := filepath.Join(folderPath, "folder.jpg")

	exists, err := helpers.FileExists(inputPath)
	if err != nil {
		return fmt.Errorf("check input file: %w", err)
	}
	if !exists {
		return errors.New("source animated artwork not found")
	}

	cmd := exec.Command("ffmpeg", "-i", inputPath, "-vf", "scale=440:-1", "-r", "24", "-f", "gif", outputPath)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("convert to gif failed: %w", err)
	}

	return nil
}

// ProcessAnimatedArtwork handles the complete animated artwork workflow
func ProcessAnimatedArtwork(folderPath string, squareVideoUrl string, tallVideoUrl string, embyFormat bool) {
	if squareVideoUrl != "" {
		fmt.Println("Found Animation Artwork.")

		err := DownloadAnimatedArtwork(folderPath, squareVideoUrl, "square")
		if err == nil && embyFormat {
			if err := ConvertToEmbyFormat(folderPath); err != nil {
				fmt.Println("Failed to convert to Emby format:", err)
			}
		} else if err != nil {
			fmt.Println("Failed to download square animated artwork:", err)
		}
	}

	if tallVideoUrl != "" {
		if err := DownloadAnimatedArtwork(folderPath, tallVideoUrl, "tall"); err != nil {
			fmt.Println("Failed to download tall animated artwork:", err)
		}
	}
}
