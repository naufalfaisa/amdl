// ============================================
// File: internal/converter/converter.go
package converter

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"main/utils/structs"
	"main/utils/task"
)

// Constants
const (
	mp3Quality  = "2" // VBR quality 2 ~ high quality
	opusBitrate = "192k"
)

// Supported formats
var supportedFormats = map[string]bool{
	"flac": true,
	"mp3":  true,
	"opus": true,
	"wav":  true,
	"copy": true,
}

// Lossy extensions
var lossyExtensions = map[string]bool{
	".mp3":  true,
	".opus": true,
	".ogg":  true,
}

// Lossless formats
var losslessFormats = map[string]bool{
	"flac": true,
	"wav":  true,
}

// IsLossySource determines if source codec is lossy (rough heuristic)
func IsLossySource(ext string, codec string) bool {
	ext = strings.ToLower(ext)

	// Check for M4A with AAC/ATMOS
	if ext == ".m4a" {
		codecUpper := strings.ToUpper(codec)
		if codecUpper == "AAC" || strings.Contains(codecUpper, "AAC") || strings.Contains(codecUpper, "ATMOS") {
			return true
		}
	}

	// Check known lossy extensions
	return lossyExtensions[ext]
}

// BuildFFmpegArgs builds ffmpeg arguments for desired target format
func BuildFFmpegArgs(ffmpegPath, inPath, outPath, targetFmt, extraArgs string) ([]string, error) {
	if !supportedFormats[targetFmt] {
		return nil, fmt.Errorf("unsupported convert-format: %s", targetFmt)
	}

	args := []string{"-y", "-i", inPath, "-vn"}

	switch targetFmt {
	case "flac":
		args = append(args, "-c:a", "flac")
	case "mp3":
		args = append(args, "-c:a", "libmp3lame", "-qscale:a", mp3Quality)
	case "opus":
		args = append(args, "-c:a", "libopus", "-b:a", opusBitrate, "-vbr", "on")
	case "wav":
		args = append(args, "-c:a", "pcm_s16le")
	case "copy":
		args = append(args, "-c", "copy")
	}

	// Add extra arguments if provided
	if extraArgs != "" {
		args = append(args, strings.Fields(extraArgs)...)
	}

	args = append(args, outPath)
	return args, nil
}

// ConvertIfNeeded performs conversion if enabled in config
func ConvertIfNeeded(track *task.Track, cfg *structs.ConfigSet) {
	if !shouldConvert(track, cfg) {
		return
	}

	srcPath := track.SavePath
	ext := strings.ToLower(filepath.Ext(srcPath))
	targetFmt := strings.ToLower(cfg.ConvertFormat)

	// Skip if already in target format
	if cfg.ConvertSkipIfSourceMatch && ext == "."+targetFmt {
		fmt.Printf("Conversion skipped (already %s)\n", targetFmt)
		return
	}

	outPath := buildOutputPath(srcPath, ext, targetFmt)

	// Handle lossy to lossless conversion
	if shouldSkipLossyToLossless(ext, targetFmt, track.Codec, cfg) {
		return
	}

	// Verify ffmpeg availability
	if !isFFmpegAvailable(cfg.FFmpegPath) {
		fmt.Printf("ffmpeg not found at '%s'; skipping conversion.\n", cfg.FFmpegPath)
		return
	}

	// Perform conversion
	if err := performConversion(cfg, srcPath, outPath, targetFmt); err != nil {
		fmt.Println("Conversion failed:", err)
		return
	}

	// Handle post-conversion cleanup
	handlePostConversion(track, srcPath, outPath, cfg.ConvertKeepOriginal)
}

// shouldConvert checks if conversion should proceed
func shouldConvert(track *task.Track, cfg *structs.ConfigSet) bool {
	if !cfg.ConvertAfterDownload {
		return false
	}
	if cfg.ConvertFormat == "" {
		return false
	}
	if track.SavePath == "" {
		return false
	}
	if strings.ToLower(cfg.ConvertFormat) == "copy" {
		fmt.Println("Convert (copy) requested; skipping because it produces no new format.")
		return false
	}
	return true
}

// buildOutputPath constructs the output file path
func buildOutputPath(srcPath, ext, targetFmt string) string {
	outBase := strings.TrimSuffix(srcPath, ext)
	return outBase + "." + targetFmt
}

// shouldSkipLossyToLossless determines if lossy->lossless conversion should be skipped
func shouldSkipLossyToLossless(ext, targetFmt, codec string, cfg *structs.ConfigSet) bool {
	if !losslessFormats[targetFmt] {
		return false
	}

	if !IsLossySource(ext, codec) {
		return false
	}

	if cfg.ConvertSkipLossyToLossless {
		fmt.Println("Skipping conversion: source appears lossy and target is lossless; configured to skip.")
		return true
	}

	if cfg.ConvertWarnLossyToLossless {
		fmt.Println("Warning: Converting lossy source to lossless container will not improve quality.")
	}

	return false
}

// isFFmpegAvailable checks if ffmpeg is available at the specified path
func isFFmpegAvailable(ffmpegPath string) bool {
	_, err := exec.LookPath(ffmpegPath)
	return err == nil
}

// performConversion executes the ffmpeg conversion
func performConversion(cfg *structs.ConfigSet, srcPath, outPath, targetFmt string) error {
	args, err := BuildFFmpegArgs(cfg.FFmpegPath, srcPath, outPath, targetFmt, cfg.ConvertExtraArgs)
	if err != nil {
		return fmt.Errorf("build ffmpeg args: %w", err)
	}

	fmt.Printf("Converting -> %s ...\n", targetFmt)

	cmd := exec.Command(cfg.FFmpegPath, args...)
	cmd.Stdout = nil
	cmd.Stderr = nil

	start := time.Now()
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg execution: %w", err)
	}

	duration := time.Since(start).Truncate(time.Millisecond)
	fmt.Printf("Conversion completed in %s: %s\n", duration, filepath.Base(outPath))

	return nil
}

// handlePostConversion manages the original file after successful conversion
func handlePostConversion(track *task.Track, srcPath, outPath string, keepOriginal bool) {
	// Update track to point to new file
	track.SavePath = outPath
	track.SaveName = filepath.Base(outPath)

	if !keepOriginal {
		if err := os.Remove(srcPath); err != nil {
			fmt.Println("Failed to remove original after conversion:", err)
		} else {
			fmt.Println("Original removed.")
		}
	}
}
