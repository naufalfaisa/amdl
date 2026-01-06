package converter

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"main/internal/structs"
	"main/internal/task"
)

// IsLossySource Determine if source codec is lossy (rough heuristic by extension/codec name).
func IsLossySource(ext string, codec string) bool {
	ext = strings.ToLower(ext)
	if ext == ".m4a" && (codec == "AAC" || strings.Contains(codec, "AAC") || strings.Contains(codec, "ATMOS")) {
		return true
	}
	if ext == ".mp3" || ext == ".opus" || ext == ".ogg" {
		return true
	}
	return false
}

// BuildFFmpegArgs Build ffmpeg arguments for desired target.
func BuildFFmpegArgs(ffmpegPath, inPath, outPath, targetFmt, extraArgs string) ([]string, error) {
	args := []string{"-y", "-i", inPath, "-vn"}
	switch targetFmt {
	case "flac":
		args = append(args, "-c:a", "flac")
	case "mp3":
		// VBR quality 2 ~ high quality
		args = append(args, "-c:a", "libmp3lame", "-qscale:a", "2")
	case "opus":
		// Medium/high quality
		args = append(args, "-c:a", "libopus", "-b:a", "192k", "-vbr", "on")
	case "wav":
		args = append(args, "-c:a", "pcm_s16le")
	case "copy":
		// Just container copy (probably pointless for same container)
		args = append(args, "-c", "copy")
	default:
		return nil, fmt.Errorf("unsupported convert-format: %s", targetFmt)
	}
	if extraArgs != "" {
		// naive split; for complex quoting you could enhance
		args = append(args, strings.Fields(extraArgs)...)
	}
	args = append(args, outPath)
	return args, nil
}

// ConvertIfNeeded Perform conversion if enabled.
func ConvertIfNeeded(track *task.Track, cfg *structs.ConfigSet) {
	if !cfg.ConvertAfterDownload {
		return
	}
	if cfg.ConvertFormat == "" {
		return
	}
	srcPath := track.SavePath
	if srcPath == "" {
		return
	}
	ext := strings.ToLower(filepath.Ext(srcPath))
	targetFmt := strings.ToLower(cfg.ConvertFormat)

	// Map extension for output
	if targetFmt == "copy" {
		fmt.Println("Convert (copy) requested; skipping because it produces no new format.")
		return
	}

	if cfg.ConvertSkipIfSourceMatch {
		if ext == "."+targetFmt {
			fmt.Printf("Conversion skipped (already %s)\n", targetFmt)
			return
		}
	}

	outBase := strings.TrimSuffix(srcPath, ext)
	outPath := outBase + "." + targetFmt

	// Handle lossy -> lossless cases: optionally skip or warn
	if (targetFmt == "flac" || targetFmt == "wav") && IsLossySource(ext, track.Codec) {
		if cfg.ConvertSkipLossyToLossless {
			fmt.Println("Skipping conversion: source appears lossy and target is lossless; configured to skip.")
			return
		}
		if cfg.ConvertWarnLossyToLossless {
			fmt.Println("Warning: Converting lossy source to lossless container will not improve quality.")
		}
	}

	if _, err := exec.LookPath(cfg.FFmpegPath); err != nil {
		fmt.Printf("ffmpeg not found at '%s'; skipping conversion.\n", cfg.FFmpegPath)
		return
	}

	args, err := BuildFFmpegArgs(cfg.FFmpegPath, srcPath, outPath, targetFmt, cfg.ConvertExtraArgs)
	if err != nil {
		fmt.Println("Conversion config error:", err)
		return
	}

	fmt.Printf("Converting -> %s ...\n", targetFmt)
	cmd := exec.Command(cfg.FFmpegPath, args...)
	cmd.Stdout = nil
	cmd.Stderr = nil
	start := time.Now()
	if err := cmd.Run(); err != nil {
		fmt.Println("Conversion failed:", err)
		// leave original
		return
	}
	fmt.Printf("Conversion completed in %s: %s\n", time.Since(start).Truncate(time.Millisecond), filepath.Base(outPath))

	if !cfg.ConvertKeepOriginal {
		if err := os.Remove(srcPath); err != nil {
			fmt.Println("Failed to remove original after conversion:", err)
		} else {
			track.SavePath = outPath
			track.SaveName = filepath.Base(outPath)
			fmt.Println("Original removed.")
		}
	} else {
		// Keep both but point track to new file (optional decision)
		track.SavePath = outPath
		track.SaveName = filepath.Base(outPath)
	}
}
