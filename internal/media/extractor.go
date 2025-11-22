// ============================================
// File: internal/media/extractor.go
package media

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"main/utils/structs"

	"github.com/grafov/m3u8"
	"github.com/olekukonko/tablewriter"
)

// Constants
const (
	codecAAC      = "mp4a.40.2"
	codecALAC     = "alac"
	codecAtmos    = "ec-3"
	codecDolbyAC3 = "ac-3"

	hiResSampleRate = 48000
)

// Errors
var (
	ErrNotMasterPlaylist = errors.New("m3u8 not of master type")
	ErrNoCodecFound      = errors.New("no codec found")
	ErrNoStreamFound     = errors.New("no suitable stream found")
)

// ExtractMedia parses master m3u8 and returns track URL and quality info
func ExtractMedia(masterUrl string, debugMode bool, dlAtmos bool, dlAAC bool, cfg *structs.ConfigSet) (string, string, error) {
	parsedUrl, err := url.Parse(masterUrl)
	if err != nil {
		return "", "", fmt.Errorf("parse master URL: %w", err)
	}

	master, err := fetchMasterPlaylist(masterUrl)
	if err != nil {
		return "", "", err
	}

	sortByBandwidth(master.Variants)

	if dlAtmos {
		return extractAtmosStream(master, parsedUrl, cfg, debugMode)
	} else if dlAAC {
		return extractAACStream(master, parsedUrl, cfg, debugMode)
	}
	return extractALACStream(master, parsedUrl, cfg, debugMode)
}

// fetchMasterPlaylist fetches and parses master m3u8 playlist
func fetchMasterPlaylist(masterUrl string) (*m3u8.MasterPlaylist, error) {
	resp, err := http.Get(masterUrl)
	if err != nil {
		return nil, fmt.Errorf("fetch master playlist: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch master playlist: %s", resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read master playlist: %w", err)
	}

	from, listType, err := m3u8.DecodeFrom(strings.NewReader(string(body)), true)
	if err != nil || listType != m3u8.MASTER {
		return nil, ErrNotMasterPlaylist
	}

	return from.(*m3u8.MasterPlaylist), nil
}

// sortByBandwidth sorts variants by bandwidth (highest first)
func sortByBandwidth(variants []*m3u8.Variant) {
	sort.Slice(variants, func(i, j int) bool {
		return variants[i].AverageBandwidth > variants[j].AverageBandwidth
	})
}

// extractAtmosStream finds and returns Atmos stream URL and quality
func extractAtmosStream(master *m3u8.MasterPlaylist, baseUrl *url.URL, cfg *structs.ConfigSet, debugMode bool) (string, string, error) {
	for _, variant := range master.Variants {
		// Prioritize Atmos
		if variant.Codecs == codecAtmos && strings.Contains(variant.Audio, "atmos") {
			bitrate, err := extractBitrateFromAudio(variant.Audio)
			if err != nil {
				continue
			}
			if bitrate <= cfg.AtmosMax {
				streamUrl, err := baseUrl.Parse(variant.URI)
				if err != nil {
					return "", "", fmt.Errorf("parse stream URL: %w", err)
				}
				if !debugMode {
					fmt.Printf("%s\n", variant.Audio)
				}
				return streamUrl.String(), fmt.Sprintf("%d Kbps", bitrate), nil
			}
		}
		// Fallback to AC-3
		if variant.Codecs == codecDolbyAC3 {
			streamUrl, err := baseUrl.Parse(variant.URI)
			if err != nil {
				return "", "", fmt.Errorf("parse stream URL: %w", err)
			}
			bitrate, _ := extractBitrateFromAudio(variant.Audio)
			return streamUrl.String(), fmt.Sprintf("%d Kbps", bitrate), nil
		}
	}
	return "", "", ErrNoCodecFound
}

// extractAACStream finds and returns AAC stream URL and quality
func extractAACStream(master *m3u8.MasterPlaylist, baseUrl *url.URL, cfg *structs.ConfigSet, debugMode bool) (string, string, error) {
	aacRegex := regexp.MustCompile(`audio-stereo-\d+`)

	for _, variant := range master.Variants {
		if variant.Codecs == codecAAC {
			replaced := aacRegex.ReplaceAllString(variant.Audio, "aac")
			if replaced == cfg.AacType {
				streamUrl, err := baseUrl.Parse(variant.URI)
				if err != nil {
					return "", "", fmt.Errorf("parse stream URL: %w", err)
				}
				if !debugMode {
					fmt.Printf("%s\n", variant.Audio)
				}

				split := strings.Split(variant.Audio, "-")
				quality := "Unknown"
				if len(split) >= 3 {
					quality = fmt.Sprintf("%s Kbps", split[2])
				}
				return streamUrl.String(), quality, nil
			}
		}
	}
	return "", "", ErrNoCodecFound
}

// extractALACStream finds and returns ALAC stream URL and quality
func extractALACStream(master *m3u8.MasterPlaylist, baseUrl *url.URL, cfg *structs.ConfigSet, debugMode bool) (string, string, error) {
	for _, variant := range master.Variants {
		if variant.Codecs == codecALAC {
			sampleRate, bitDepth, err := extractALACInfo(variant.Audio)
			if err != nil {
				continue
			}

			if sampleRate <= cfg.AlacMax {
				streamUrl, err := baseUrl.Parse(variant.URI)
				if err != nil {
					return "", "", fmt.Errorf("parse stream URL: %w", err)
				}
				if !debugMode {
					fmt.Printf("%s-bit / %d Hz\n", bitDepth, sampleRate)
				}
				khz := float64(sampleRate) / 1000.0
				quality := fmt.Sprintf("%sB-%.1fkHz", bitDepth, khz)
				return streamUrl.String(), quality, nil
			}
		}
	}
	return "", "", ErrNoCodecFound
}

// extractBitrateFromAudio extracts bitrate from audio string
func extractBitrateFromAudio(audio string) (int, error) {
	split := strings.Split(audio, "-")
	if len(split) == 0 {
		return 0, errors.New("invalid audio format")
	}
	bitrateStr := split[len(split)-1]
	return strconv.Atoi(bitrateStr)
}

// extractALACInfo extracts sample rate and bit depth from ALAC audio string
func extractALACInfo(audio string) (sampleRate int, bitDepth string, err error) {
	split := strings.Split(audio, "-")
	if len(split) < 3 {
		return 0, "", errors.New("invalid ALAC audio format")
	}

	bitDepth = split[len(split)-1]
	sampleRate, err = strconv.Atoi(split[len(split)-2])
	return
}

// ExtractVideo parses m3u8 and returns best video stream URL
func ExtractVideo(m3u8Url string, maxHeight int) (string, error) {
	mediaUrl, err := url.Parse(m3u8Url)
	if err != nil {
		return "", fmt.Errorf("parse video URL: %w", err)
	}

	master, err := fetchMasterPlaylist(m3u8Url)
	if err != nil {
		return "", err
	}

	sortByBandwidth(master.Variants)

	streamUrl, resolution, err := findBestVideoStream(master, mediaUrl, maxHeight)
	if err != nil {
		return "", err
	}

	fmt.Println("Video:", resolution)
	return streamUrl, nil
}

// findBestVideoStream finds the best video stream under maxHeight
func findBestVideoStream(master *m3u8.MasterPlaylist, baseUrl *url.URL, maxHeight int) (string, string, error) {
	re := regexp.MustCompile(`_(\d+)x(\d+)`)

	for _, variant := range master.Variants {
		matches := re.FindStringSubmatch(variant.URI)
		if len(matches) != 3 {
			continue
		}

		height, err := strconv.Atoi(matches[2])
		if err != nil {
			continue
		}

		if height <= maxHeight {
			streamUrl, err := baseUrl.Parse(variant.URI)
			if err != nil {
				return "", "", fmt.Errorf("parse stream URL: %w", err)
			}
			resolution := fmt.Sprintf("%s-%s", variant.Resolution, variant.VideoRange)
			return streamUrl.String(), resolution, nil
		}
	}

	return "", "", ErrNoStreamFound
}

// ExtractMvAudio parses m3u8 and returns best audio stream URL for music video
func ExtractMvAudio(m3u8Url string, audioType string) (string, error) {
	mediaUrl, err := url.Parse(m3u8Url)
	if err != nil {
		return "", fmt.Errorf("parse audio URL: %w", err)
	}

	master, err := fetchMasterPlaylist(m3u8Url)
	if err != nil {
		return "", err
	}

	audioPriority := getAudioPriority(audioType)
	audioStreams := extractAudioStreams(master, mediaUrl, audioPriority)

	if len(audioStreams) == 0 {
		return "", ErrNoStreamFound
	}

	sort.Slice(audioStreams, func(i, j int) bool {
		return audioStreams[i].Rank > audioStreams[j].Rank
	})

	fmt.Println("Audio:", audioStreams[0].GroupID)
	return audioStreams[0].URL, nil
}

// AudioStream represents an audio stream with ranking
type AudioStream struct {
	URL     string
	Rank    int
	GroupID string
}

// getAudioPriority returns audio priority list based on type
func getAudioPriority(audioType string) []string {
	switch audioType {
	case "ac3":
		return []string{"audio-ac3", "audio-stereo-256"}
	case "aac":
		return []string{"audio-stereo-256"}
	default:
		return []string{"audio-atmos", "audio-ac3", "audio-stereo-256"}
	}
}

// extractAudioStreams extracts all matching audio streams with ranking
func extractAudioStreams(master *m3u8.MasterPlaylist, baseUrl *url.URL, priority []string) []AudioStream {
	re := regexp.MustCompile(`_gr(\d+)_`)
	var streams []AudioStream

	for _, variant := range master.Variants {
		for _, audiov := range variant.Alternatives {
			if audiov.URI == "" {
				continue
			}

			for _, p := range priority {
				if audiov.GroupId == p {
					matches := re.FindStringSubmatch(audiov.URI)
					if len(matches) == 2 {
						rank, _ := strconv.Atoi(matches[1])
						streamUrl, err := baseUrl.Parse(audiov.URI)
						if err != nil {
							continue
						}
						streams = append(streams, AudioStream{
							URL:     streamUrl.String(),
							Rank:    rank,
							GroupID: audiov.GroupId,
						})
					}
				}
			}
		}
	}

	return streams
}

// FormatAvailability returns formatted availability string
func FormatAvailability(available bool, quality string) string {
	if !available {
		return "Not Available"
	}
	return quality
}

// DisplayDebugInfo shows detailed quality information for debugging
func DisplayDebugInfo(m3u8Url string, cfg *structs.ConfigSet) error {
	master, err := fetchMasterPlaylist(m3u8Url)
	if err != nil {
		return err
	}

	sortByBandwidth(master.Variants)

	displayVariantsTable(master.Variants)
	displayAvailableFormats(master.Variants)

	return nil
}

// displayVariantsTable shows all variants in a table
func displayVariantsTable(variants []*m3u8.Variant) {
	fmt.Println("Debug: All Available Variants:")

	var data [][]string
	for _, variant := range variants {
		data = append(data, []string{
			variant.Codecs,
			variant.Audio,
			fmt.Sprint(variant.Bandwidth),
		})
	}

	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader([]string{"Codec", "Audio", "Bandwidth"})
	table.SetAutoMergeCells(true)
	table.SetRowLine(true)
	table.AppendBulk(data)
	table.Render()
}

// displayAvailableFormats analyzes and displays available audio formats
func displayAvailableFormats(variants []*m3u8.Variant) {
	formats := analyzeAudioFormats(variants)

	fmt.Println("Available Audio Formats:")
	fmt.Println("------------------------")
	fmt.Printf("AAC             : %s\n", FormatAvailability(formats.HasAAC, formats.AACQuality))
	fmt.Printf("Lossless        : %s\n", FormatAvailability(formats.HasLossless, formats.LosslessQuality))
	fmt.Printf("Hi-Res Lossless : %s\n", FormatAvailability(formats.HasHiRes, formats.HiResQuality))
	fmt.Printf("Dolby Atmos     : %s\n", FormatAvailability(formats.HasAtmos, formats.AtmosQuality))
	fmt.Printf("Dolby Audio     : %s\n", FormatAvailability(formats.HasDolbyAudio, formats.DolbyAudioQuality))
	fmt.Println("------------------------")
}

// AudioFormats holds information about available audio formats
type AudioFormats struct {
	HasAAC            bool
	HasLossless       bool
	HasHiRes          bool
	HasAtmos          bool
	HasDolbyAudio     bool
	AACQuality        string
	LosslessQuality   string
	HiResQuality      string
	AtmosQuality      string
	DolbyAudioQuality string
}

// analyzeAudioFormats analyzes all variants and returns format information
func analyzeAudioFormats(variants []*m3u8.Variant) AudioFormats {
	formats := AudioFormats{}

	for _, variant := range variants {
		switch variant.Codecs {
		case codecAAC:
			analyzeAAC(variant, &formats)
		case codecAtmos:
			analyzeAtmos(variant, &formats)
		case codecALAC:
			analyzeALAC(variant, &formats)
		case codecDolbyAC3:
			analyzeDolbyAudio(variant, &formats)
		}
	}

	return formats
}

// analyzeAAC analyzes AAC variant
func analyzeAAC(variant *m3u8.Variant, formats *AudioFormats) {
	formats.HasAAC = true
	split := strings.Split(variant.Audio, "-")
	if len(split) < 3 {
		return
	}

	bitrate, err := strconv.Atoi(split[2])
	if err != nil {
		return
	}

	currentBitrate := extractCurrentBitrate(formats.AACQuality)
	if bitrate > currentBitrate {
		formats.AACQuality = fmt.Sprintf("AAC | 2 Channel | %d Kbps", bitrate)
	}
}

// analyzeAtmos analyzes Atmos variant
func analyzeAtmos(variant *m3u8.Variant, formats *AudioFormats) {
	if !strings.Contains(variant.Audio, "atmos") {
		return
	}

	formats.HasAtmos = true
	split := strings.Split(variant.Audio, "-")
	if len(split) == 0 {
		return
	}

	bitrateStr := split[len(split)-1]
	// Handle leading '2' in bitrate (e.g., "2768" -> "768")
	if len(bitrateStr) == 4 && bitrateStr[0] == '2' {
		bitrateStr = bitrateStr[1:]
	}

	bitrate, err := strconv.Atoi(bitrateStr)
	if err != nil {
		return
	}

	currentBitrate := extractCurrentBitrate(formats.AtmosQuality)
	if bitrate > currentBitrate {
		formats.AtmosQuality = fmt.Sprintf("E-AC-3 | 16 Channel | %d Kbps", bitrate)
	}
}

// analyzeALAC analyzes ALAC variant
func analyzeALAC(variant *m3u8.Variant, formats *AudioFormats) {
	split := strings.Split(variant.Audio, "-")
	if len(split) < 3 {
		return
	}

	bitDepth := split[len(split)-1]
	sampleRate, err := strconv.Atoi(split[len(split)-2])
	if err != nil {
		return
	}

	quality := fmt.Sprintf("ALAC | 2 Channel | %s-bit/%d kHz", bitDepth, sampleRate/1000)

	if sampleRate > hiResSampleRate {
		formats.HasHiRes = true
		formats.HiResQuality = quality
	} else {
		formats.HasLossless = true
		formats.LosslessQuality = quality
	}
}

// analyzeDolbyAudio analyzes Dolby Audio (AC-3) variant
func analyzeDolbyAudio(variant *m3u8.Variant, formats *AudioFormats) {
	formats.HasDolbyAudio = true
	split := strings.Split(variant.Audio, "-")
	if len(split) == 0 {
		return
	}

	bitrate, err := strconv.Atoi(split[len(split)-1])
	if err != nil {
		return
	}

	formats.DolbyAudioQuality = fmt.Sprintf("AC-3 | 16 Channel | %d Kbps", bitrate)
}

// extractCurrentBitrate extracts bitrate from quality string
func extractCurrentBitrate(quality string) int {
	if quality == "" {
		return 0
	}
	parts := strings.Split(quality, " | ")
	if len(parts) < 3 {
		return 0
	}
	bitrateStr := strings.Split(parts[2], " ")[0]
	bitrate, _ := strconv.Atoi(bitrateStr)
	return bitrate
}
