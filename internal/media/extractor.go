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

// ExtractMedia parses master m3u8 and returns track URL and quality info
func ExtractMedia(masterUrl string, debugMode bool, dlAtmos bool, dlAAC bool, cfg *structs.ConfigSet) (string, string, error) {
	parsedUrl, err := url.Parse(masterUrl)
	if err != nil {
		return "", "", err
	}

	resp, err := http.Get(masterUrl)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", "", errors.New(resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", err
	}

	masterString := string(body)
	from, listType, err := m3u8.DecodeFrom(strings.NewReader(masterString), true)
	if err != nil || listType != m3u8.MASTER {
		return "", "", errors.New("m3u8 not of master type")
	}

	master := from.(*m3u8.MasterPlaylist)
	var streamUrl *url.URL

	sort.Slice(master.Variants, func(i, j int) bool {
		return master.Variants[i].AverageBandwidth > master.Variants[j].AverageBandwidth
	})

	var Quality string
	for _, variant := range master.Variants {
		if dlAtmos {
			if variant.Codecs == "ec-3" && strings.Contains(variant.Audio, "atmos") {
				split := strings.Split(variant.Audio, "-")
				length := len(split)
				lengthInt, err := strconv.Atoi(split[length-1])
				if err != nil {
					return "", "", err
				}
				if lengthInt <= cfg.AtmosMax {
					if !debugMode {
						fmt.Printf("%s\n", variant.Audio)
					}
					streamUrlTemp, err := parsedUrl.Parse(variant.URI)
					if err != nil {
						return "", "", err
					}
					streamUrl = streamUrlTemp
					Quality = fmt.Sprintf("%s Kbps", split[len(split)-1])
					break
				}
			} else if variant.Codecs == "ac-3" {
				streamUrlTemp, err := parsedUrl.Parse(variant.URI)
				if err != nil {
					return "", "", err
				}
				streamUrl = streamUrlTemp
				split := strings.Split(variant.Audio, "-")
				Quality = fmt.Sprintf("%s Kbps", split[len(split)-1])
				break
			}
		} else if dlAAC {
			if variant.Codecs == "mp4a.40.2" {
				aacregex := regexp.MustCompile(`audio-stereo-\d+`)
				replaced := aacregex.ReplaceAllString(variant.Audio, "aac")
				if replaced == cfg.AacType {
					if !debugMode {
						fmt.Printf("%s\n", variant.Audio)
					}
					streamUrlTemp, err := parsedUrl.Parse(variant.URI)
					if err != nil {
						return "", "", err
					}
					streamUrl = streamUrlTemp
					split := strings.Split(variant.Audio, "-")
					Quality = fmt.Sprintf("%s Kbps", split[2])
					break
				}
			}
		} else {
			if variant.Codecs == "alac" {
				split := strings.Split(variant.Audio, "-")
				length := len(split)
				lengthInt, err := strconv.Atoi(split[length-2])
				if err != nil {
					return "", "", err
				}
				if lengthInt <= cfg.AlacMax {
					if !debugMode {
						fmt.Printf("%s-bit / %s Hz\n", split[length-1], split[length-2])
					}
					streamUrlTemp, err := parsedUrl.Parse(variant.URI)
					if err != nil {
						return "", "", err
					}
					streamUrl = streamUrlTemp
					KHZ := float64(lengthInt) / 1000.0
					Quality = fmt.Sprintf("%sB-%.1fkHz", split[length-1], KHZ)
					break
				}
			}
		}
	}

	if streamUrl == nil {
		return "", "", errors.New("no codec found")
	}

	return streamUrl.String(), Quality, nil
}

// ExtractVideo parses m3u8 and returns best video stream URL
func ExtractVideo(m3u8Url string, maxHeight int) (string, error) {
	mediaUrl, err := url.Parse(m3u8Url)
	if err != nil {
		return "", err
	}

	resp, err := http.Get(m3u8Url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", errors.New(resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	videoString := string(body)
	from, listType, err := m3u8.DecodeFrom(strings.NewReader(videoString), true)
	if err != nil || listType != m3u8.MASTER {
		return "", errors.New("m3u8 not of media type")
	}

	video := from.(*m3u8.MasterPlaylist)
	re := regexp.MustCompile(`_(\d+)x(\d+)`)

	var streamUrl *url.URL
	sort.Slice(video.Variants, func(i, j int) bool {
		return video.Variants[i].AverageBandwidth > video.Variants[j].AverageBandwidth
	})

	for _, variant := range video.Variants {
		matches := re.FindStringSubmatch(variant.URI)
		if len(matches) == 3 {
			height := matches[2]
			var h int
			_, err := fmt.Sscanf(height, "%d", &h)
			if err != nil {
				continue
			}
			if h <= maxHeight {
				streamUrl, err = mediaUrl.Parse(variant.URI)
				if err != nil {
					return "", err
				}
				fmt.Println("Video: " + variant.Resolution + "-" + variant.VideoRange)
				break
			}
		}
	}

	if streamUrl == nil {
		return "", errors.New("no suitable video stream found")
	}

	return streamUrl.String(), nil
}

// ExtractMvAudio parses m3u8 and returns best audio stream URL for music video
func ExtractMvAudio(m3u8Url string, audioType string) (string, error) {
	mediaUrl, err := url.Parse(m3u8Url)
	if err != nil {
		return "", err
	}

	resp, err := http.Get(m3u8Url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", errors.New(resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	audioString := string(body)
	from, listType, err := m3u8.DecodeFrom(strings.NewReader(audioString), true)
	if err != nil || listType != m3u8.MASTER {
		return "", errors.New("m3u8 not of media type")
	}

	audio := from.(*m3u8.MasterPlaylist)

	var audioPriority = []string{"audio-atmos", "audio-ac3", "audio-stereo-256"}
	if audioType == "ac3" {
		audioPriority = []string{"audio-ac3", "audio-stereo-256"}
	} else if audioType == "aac" {
		audioPriority = []string{"audio-stereo-256"}
	}

	re := regexp.MustCompile(`_gr(\d+)_`)

	type AudioStream struct {
		URL     string
		Rank    int
		GroupID string
	}
	var audioStreams []AudioStream

	for _, variant := range audio.Variants {
		for _, audiov := range variant.Alternatives {
			if audiov.URI != "" {
				for _, priority := range audioPriority {
					if audiov.GroupId == priority {
						matches := re.FindStringSubmatch(audiov.URI)
						if len(matches) == 2 {
							var rank int
							fmt.Sscanf(matches[1], "%d", &rank)
							streamUrl, _ := mediaUrl.Parse(audiov.URI)
							audioStreams = append(audioStreams, AudioStream{
								URL:     streamUrl.String(),
								Rank:    rank,
								GroupID: audiov.GroupId,
							})
						}
					}
				}
			}
		}
	}

	if len(audioStreams) == 0 {
		return "", errors.New("no suitable audio stream found")
	}

	sort.Slice(audioStreams, func(i, j int) bool {
		return audioStreams[i].Rank > audioStreams[j].Rank
	})

	fmt.Println("Audio: " + audioStreams[0].GroupID)
	return audioStreams[0].URL, nil
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
	_, err := url.Parse(m3u8Url)
	if err != nil {
		return err
	}

	resp, err := http.Get(m3u8Url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return errors.New(resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	masterString := string(body)
	from, listType, err := m3u8.DecodeFrom(strings.NewReader(masterString), true)
	if err != nil || listType != m3u8.MASTER {
		return errors.New("m3u8 not of master type")
	}

	master := from.(*m3u8.MasterPlaylist)

	sort.Slice(master.Variants, func(i, j int) bool {
		return master.Variants[i].AverageBandwidth > master.Variants[j].AverageBandwidth
	})

	// Display table of all variants
	fmt.Println("Debug: All Available Variants:")
	var data [][]string
	for _, variant := range master.Variants {
		data = append(data, []string{variant.Codecs, variant.Audio, fmt.Sprint(variant.Bandwidth)})
	}

	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader([]string{"Codec", "Audio", "Bandwidth"})
	table.SetAutoMergeCells(true)
	table.SetRowLine(true)
	table.AppendBulk(data)
	table.Render()

	// Analyze available formats
	var hasAAC, hasLossless, hasHiRes, hasAtmos, hasDolbyAudio bool
	var aacQuality, losslessQuality, hiResQuality, atmosQuality, dolbyAudioQuality string

	for _, variant := range master.Variants {
		if variant.Codecs == "mp4a.40.2" { // AAC
			hasAAC = true
			split := strings.Split(variant.Audio, "-")
			if len(split) >= 3 {
				bitrate, _ := strconv.Atoi(split[2])
				currentBitrate := 0
				if aacQuality != "" {
					current := strings.Split(aacQuality, " | ")[2]
					current = strings.Split(current, " ")[0]
					currentBitrate, _ = strconv.Atoi(current)
				}
				if bitrate > currentBitrate {
					aacQuality = fmt.Sprintf("AAC | 2 Channel | %d Kbps", bitrate)
				}
			}
		} else if variant.Codecs == "ec-3" && strings.Contains(variant.Audio, "atmos") { // Dolby Atmos
			hasAtmos = true
			split := strings.Split(variant.Audio, "-")
			if len(split) > 0 {
				bitrateStr := split[len(split)-1]
				if len(bitrateStr) == 4 && bitrateStr[0] == '2' {
					bitrateStr = bitrateStr[1:]
				}
				bitrate, _ := strconv.Atoi(bitrateStr)
				currentBitrate := 0
				if atmosQuality != "" {
					current := strings.Split(strings.Split(atmosQuality, " | ")[2], " ")[0]
					currentBitrate, _ = strconv.Atoi(current)
				}
				if bitrate > currentBitrate {
					atmosQuality = fmt.Sprintf("E-AC-3 | 16 Channel | %d Kbps", bitrate)
				}
			}
		} else if variant.Codecs == "alac" { // ALAC (Lossless or Hi-Res)
			split := strings.Split(variant.Audio, "-")
			if len(split) >= 3 {
				bitDepth := split[len(split)-1]
				sampleRate := split[len(split)-2]
				sampleRateInt, _ := strconv.Atoi(sampleRate)
				if sampleRateInt > 48000 { // Hi-Res
					hasHiRes = true
					hiResQuality = fmt.Sprintf("ALAC | 2 Channel | %s-bit/%d kHz", bitDepth, sampleRateInt/1000)
				} else { // Standard Lossless
					hasLossless = true
					losslessQuality = fmt.Sprintf("ALAC | 2 Channel | %s-bit/%d kHz", bitDepth, sampleRateInt/1000)
				}
			}
		} else if variant.Codecs == "ac-3" { // Dolby Audio
			hasDolbyAudio = true
			split := strings.Split(variant.Audio, "-")
			if len(split) > 0 {
				bitrate, _ := strconv.Atoi(split[len(split)-1])
				dolbyAudioQuality = fmt.Sprintf("AC-3 | 16 Channel | %d Kbps", bitrate)
			}
		}
	}

	fmt.Println("Available Audio Formats:")
	fmt.Println("------------------------")
	fmt.Printf("AAC             : %s\n", FormatAvailability(hasAAC, aacQuality))
	fmt.Printf("Lossless        : %s\n", FormatAvailability(hasLossless, losslessQuality))
	fmt.Printf("Hi-Res Lossless : %s\n", FormatAvailability(hasHiRes, hiResQuality))
	fmt.Printf("Dolby Atmos     : %s\n", FormatAvailability(hasAtmos, atmosQuality))
	fmt.Printf("Dolby Audio     : %s\n", FormatAvailability(hasDolbyAudio, dolbyAudioQuality))
	fmt.Println("------------------------")

	return nil
}
