package runv3

import (
	"context"
	"encoding/base64"
	"fmt"
	"path/filepath"

	"github.com/gospider007/requests"
	"google.golang.org/protobuf/proto"

	//"log/slog"
	cdm "main/utils/runv3/cdm"
	key "main/utils/runv3/key"
	"os"

	"bytes"
	"errors"
	"io"

	"github.com/Eyevinn/mp4ff/mp4"

	//"io/ioutil"
	"encoding/json"
	"net/http"
	"os/exec"
	"strings"
	"sync"

	//"time"

	"github.com/grafov/m3u8"
	"github.com/schollz/progressbar/v3"
)

type PlaybackLicense struct {
	ErrorCode  int    `json:"errorCode"`
	License    string `json:"license"`
	RenewAfter int    `json:"renew-after"`
	Status     int    `json:"status"`
}

//	func log() {
//		f, err := os.OpenFile("log.txt", os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
//		if err != nil {
//			slog.Error("error opening file: %s", err)
//		}
//		defer func(f *os.File) {
//			err := f.Close()
//			if err != nil {
//				slog.Error("error closing file: %s", err)
//			}
//		}(f)
//		opts := slog.HandlerOptions{
//			AddSource: true,
//			Level:     slog.LevelDebug,
//		}
//		logger := slog.New(slog.NewJSONHandler(os.Stdout, &opts))
//		slog.SetDefault(logger)
//	}
func getPSSH(contentId string, kidBase64 string) (string, error) {
	kidBytes, err := base64.StdEncoding.DecodeString(kidBase64)
	if err != nil {
		return "", fmt.Errorf("failed to decode base64 KID: %v", err)
	}
	contentIdEncoded := base64.StdEncoding.EncodeToString([]byte(contentId))
	algo := cdm.WidevineCencHeader_AESCTR
	widevineCencHeader := &cdm.WidevineCencHeader{
		KeyId:     [][]byte{kidBytes},
		Algorithm: &algo,
		Provider:  new(string),
		ContentId: []byte(contentIdEncoded),
		Policy:    new(string),
	}
	widevineCenc, err := proto.Marshal(widevineCencHeader)
	if err != nil {
		return "", fmt.Errorf("failed to marshal WidevineCencHeader: %v", err)
	}
	//add 32 bytes at the beginning
	widevineCenc = append([]byte("0123456789abcdef0123456789abcdef"), widevineCenc...)
	pssh := base64.StdEncoding.EncodeToString(widevineCenc)
	return pssh, nil
}
func BeforeRequest(cl *requests.Client, preCtx context.Context, method string, href string, options ...requests.RequestOption) (resp *requests.Response, err error) {
	data := options[0].Data
	jsondata := map[string]interface{}{
		"challenge":      base64.StdEncoding.EncodeToString(data.([]byte)),
		"key-system":     "com.widevine.alpha",
		"uri":            "data:;base64," + preCtx.Value("pssh").(string),
		"adamId":         preCtx.Value("adamId").(string),
		"isLibrary":      false,
		"user-initiated": true,
	}
	options[0].Data = nil
	options[0].Json = jsondata
	resp, err = cl.Request(preCtx, method, href, options...)
	if err != nil {
		fmt.Println(err)
	}

	return
}
func AfterRequest(Response *requests.Response) ([]byte, error) {
	var ResponseData PlaybackLicense
	_, err := Response.Json(&ResponseData)
	if err != nil {
		return nil, fmt.Errorf("failed to parse response: %v", err)
	}
	if ResponseData.ErrorCode != 0 || ResponseData.Status != 0 {
		return nil, fmt.Errorf("error code: %d", ResponseData.ErrorCode)
	}
	License, err := base64.StdEncoding.DecodeString(ResponseData.License)
	if err != nil {
		return nil, fmt.Errorf("failed to decode license: %v", err)
	}
	return License, nil
}
func GetWebplayback(adamId string, authtoken string, mutoken string, mvmode bool) (string, string, error) {
	url := "https://play.music.apple.com/WebObjects/MZPlay.woa/wa/webPlayback"
	postData := map[string]string{
		"salableAdamId": adamId,
	}
	jsonData, err := json.Marshal(postData)
	if err != nil {
		fmt.Println("Error encoding JSON:", err)
		return "", "", err
	}
	req, err := http.NewRequest("POST", url, bytes.NewBuffer([]byte(jsonData)))
	if err != nil {
		fmt.Println("Error creating request:", err)
		return "", "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://music.apple.com")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36")
	req.Header.Set("Referer", "https://music.apple.com/")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", authtoken))
	req.Header.Set("x-apple-music-user-token", mutoken)
	// create HTTP client
	//client := &http.Client{}
	resp, err := http.DefaultClient.Do(req)
	// send request
	//resp, err := client.Do(req)
	if err != nil {
		fmt.Println("Error sending request:", err)
		return "", "", err
	}
	defer resp.Body.Close()
	//fmt.Println("Response Status:", resp.Status)
	obj := new(Songlist)
	err = json.NewDecoder(resp.Body).Decode(&obj)
	if err != nil {
		fmt.Println("json err:", err)
		return "", "", err
	}
	if len(obj.List) > 0 {
		if mvmode {
			return obj.List[0].HlsPlaylistUrl, "", nil
		}
		// traverse Assets
		for i := range obj.List[0].Assets {
			if obj.List[0].Assets[i].Flavor == "28:ctrp256" {
				kidBase64, fileurl, err := extractKidBase64(obj.List[0].Assets[i].URL, false)
				if err != nil {
					return "", "", err
				}
				return fileurl, kidBase64, nil
			}
			continue
		}
	}
	return "", "", errors.New("Unavailable")
}

type Songlist struct {
	List []struct {
		Hlsurl         string `json:"hls-key-cert-url"`
		HlsPlaylistUrl string `json:"hls-playlist-url"`
		Assets         []struct {
			Flavor string `json:"flavor"`
			URL    string `json:"URL"`
		} `json:"assets"`
	} `json:"songList"`
	Status int `json:"status"`
}

func extractKidBase64(b string, mvmode bool) (string, string, error) {
	resp, err := http.Get(b)
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
	if err != nil {
		return "", "", err
	}
	var kidbase64 string
	var urlBuilder strings.Builder
	if listType == m3u8.MEDIA {
		mediaPlaylist := from.(*m3u8.MediaPlaylist)
		if mediaPlaylist.Key != nil {
			split := strings.Split(mediaPlaylist.Key.URI, ",")
			kidbase64 = split[1]
			lastSlashIndex := strings.LastIndex(b, "/")
			// intercept the part before the last slash
			urlBuilder.WriteString(b[:lastSlashIndex])
			urlBuilder.WriteString("/")
			urlBuilder.WriteString(mediaPlaylist.Map.URI)
			//fileurl = b[:lastSlashIndex] + "/" + mediaPlaylist.Map.URI
			//fmt.Println("Extracted URI:", mediaPlaylist.Map.URI)
			if mvmode {
				for _, segment := range mediaPlaylist.Segments {
					if segment != nil {
						//fmt.Println("Extracted URI:", segment.URI)
						urlBuilder.WriteString(";")
						urlBuilder.WriteString(b[:lastSlashIndex])
						urlBuilder.WriteString("/")
						urlBuilder.WriteString(segment.URI)
						//fileurl = fileurl + ";" + b[:lastSlashIndex] + "/" + segment.URI
					}
				}
			}
		} else {
			fmt.Println("No key information found")
		}
	} else {
		fmt.Println("Not a media playlist")
	}
	return kidbase64, urlBuilder.String(), nil
}
func extsong(b string) bytes.Buffer {
	resp, err := http.Get(b)
	if err != nil {
		fmt.Printf("Download file failed: %v\n", err)
	}
	defer resp.Body.Close()
	var buffer bytes.Buffer
	bar := progressbar.NewOptions64(
		resp.ContentLength,
		progressbar.OptionClearOnFinish(),
		progressbar.OptionSetElapsedTime(false),
		progressbar.OptionSetPredictTime(false),
		progressbar.OptionShowElapsedTimeOnFinish(),
		progressbar.OptionShowCount(),
		progressbar.OptionEnableColorCodes(true),
		progressbar.OptionShowBytes(true),
		progressbar.OptionSetDescription("Downloading..."),
		progressbar.OptionSetTheme(progressbar.Theme{
			Saucer:        "",
			SaucerHead:    "",
			SaucerPadding: "",
			BarStart:      "",
			BarEnd:        "",
		}),
	)
	io.Copy(io.MultiWriter(&buffer, bar), resp.Body)
	return buffer
}
func Run(adamId string, trackpath string, authtoken string, mutoken string, mvmode bool) (string, error) {
	var keystr string //for mv key
	var fileurl string
	var kidBase64 string
	var err error
	if mvmode {
		kidBase64, fileurl, err = extractKidBase64(trackpath, true)
		if err != nil {
			return "", err
		}
	} else {
		fileurl, kidBase64, err = GetWebplayback(adamId, authtoken, mutoken, false)
		if err != nil {
			return "", err
		}
	}
	ctx := context.Background()
	ctx = context.WithValue(ctx, "pssh", kidBase64)
	ctx = context.WithValue(ctx, "adamId", adamId)
	pssh, err := getPSSH("", kidBase64)
	//fmt.Println(pssh)
	if err != nil {
		fmt.Println(err)
		return "", err
	}
	headers := map[string]interface{}{
		"authorization":            "Bearer " + authtoken,
		"x-apple-music-user-token": mutoken,
	}
	client, _ := requests.NewClient(context.TODO(), requests.ClientOption{
		Headers: headers,
	})
	key := key.Key{
		ReqCli:        client,
		BeforeRequest: BeforeRequest,
		AfterRequest:  AfterRequest,
	}
	key.CdmInit()
	var keybt []byte
	if strings.Contains(adamId, "ra.") {
		keystr, keybt, err = key.GetKey(ctx, "https://play.itunes.apple.com/WebObjects/MZPlay.woa/web/radio/versions/1/license", pssh, nil)
		if err != nil {
			fmt.Println(err)
			return "", err
		}
	} else {
		keystr, keybt, err = key.GetKey(ctx, "https://play.itunes.apple.com/WebObjects/MZPlay.woa/wa/acquireWebPlaybackLicense", pssh, nil)
		if err != nil {
			fmt.Println(err)
			return "", err
		}
	}
	if mvmode {
		keyAndUrls := "1:" + keystr + ";" + fileurl
		return keyAndUrls, nil
	}
	body := extsong(fileurl)
	fmt.Print("Downloaded\n")
	//bodyReader := bytes.NewReader(body)
	var buffer bytes.Buffer

	err = DecryptMP4(&body, keybt, &buffer)
	if err != nil {
		fmt.Print("Decryption failed\n")
		return "", err
	} else {
		fmt.Print("Decrypted\n")
	}
	// create output file
	ofh, err := os.Create(trackpath)
	if err != nil {
		fmt.Printf("Create file Failed: %v\n", err)
		return "", err
	}
	defer ofh.Close()

	_, err = ofh.Write(buffer.Bytes())
	if err != nil {
		fmt.Printf("Write file Failed: %v\n", err)
		return "", err
	}
	return "", nil
}

// Segment struct is used to pass segment data in Channel
type Segment struct {
	Index int
	Data  []byte
}

func downloadSegment(url string, index int, wg *sync.WaitGroup, segmentsChan chan<- Segment, client *http.Client, limiter chan struct{}) {
	// When the function exits, receive a value from limiter to release a concurrency slot
	defer func() {
		<-limiter
		wg.Done()
	}()

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		fmt.Printf("error (segment %d): failed to create request: %v\n", index, err)
		return
	}

	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("error (segment %d): download failed: %v\n", index, err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fmt.Printf("error (segment %d): server returned status code %d\n", index, resp.StatusCode)
		return
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Printf("error (segment %d): failed to read data: %v\n", index, err)
		return
	}

	// send the downloaded segment (including index and data) to Channel
	segmentsChan <- Segment{Index: index, Data: data}
}

// fileWriter receives segments from Channel and writes them sequentially to the file
func fileWriter(wg *sync.WaitGroup, segmentsChan <-chan Segment, outputFile io.Writer, totalSegments int) {
	defer wg.Done()

	// Buffer to store out-of-order segments
	// key is segment index, value is segment data
	segmentBuffer := make(map[int][]byte)
	nextIndex := 0 // The sequence number of the next segment expected to be written

	for segment := range segmentsChan {
		// Check if the received segment is the expected one
		if segment.Index == nextIndex {
			//fmt.Printf("write segment %d\n", segment.Index)
			_, err := outputFile.Write(segment.Data)
			if err != nil {
				fmt.Printf("error (segment %d): failed to write to file: %v\n", segment.Index, err)
			}
			nextIndex++

			// Check if there is the next consecutive segment in the buffer
			for {
				data, ok := segmentBuffer[nextIndex]
				if !ok {
					break // No next segment in buffer, wait for the next incoming one
				}

				//fmt.Printf("write segment %d from buffer\n", nextIndex)
				_, err := outputFile.Write(data)
				if err != nil {
					fmt.Printf("error (segment %d): failed to write from buffer: %v\n", nextIndex, err)
				}
				// Delete the written segment from the buffer and release memory
				delete(segmentBuffer, nextIndex)
				nextIndex++
			}
		} else {
			// If it's not the expected segment, store it in the buffer
			//fmt.Printf("buffered segment %d (waiting for %d)\n", segment.Index, nextIndex)
			segmentBuffer[segment.Index] = segment.Data
		}
	}

	// Ensure all segments are written
	if nextIndex != totalSegments {
		fmt.Printf("warning: writing complete, but some segments seem missing. Expected %d, wrote %d.\n", totalSegments, nextIndex)
	}
}

func ExtMvData(keyAndUrls string, savePath string) error {
	segments := strings.Split(keyAndUrls, ";")
	key := segments[0]
	urls := segments[1:]

	tempFile, err := os.CreateTemp("", "enc_mv_data-*.mp4")
	if err != nil {
		fmt.Printf("Failed to create temporary file: %v\n", err)
		return err
	}
	defer os.Remove(tempFile.Name())
	defer tempFile.Close()

	var downloadWg, writerWg sync.WaitGroup
	segmentsChan := make(chan Segment, len(urls))

	// Define maximum concurrency
	const maxConcurrency = 10
	// Create buffered channel as a semaphore
	limiter := make(chan struct{}, maxConcurrency)
	client := &http.Client{}

	// Initialize progress bar
	bar := progressbar.DefaultBytes(-1, "Downloading...")
	barWriter := io.MultiWriter(tempFile, bar)

	// Start writer goroutine
	writerWg.Add(1)
	go fileWriter(&writerWg, segmentsChan, barWriter, len(urls))

	// Start download goroutines
	for i, url := range urls {
		// Acquire a slot from limiter before starting a goroutine
		// If limiter is full (10 running), this blocks until another finishes
		limiter <- struct{}{}
		downloadWg.Add(1)
		go downloadSegment(url, i, &downloadWg, segmentsChan, client, limiter)
	}

	// Wait for all download tasks to finish
	downloadWg.Wait()

	// Close channel after all downloads; writer will exit after processing all segments
	close(segmentsChan)

	// Wait for writer to complete all writing and buffer handling
	writerWg.Wait()

	// Explicitly close the temp file (safe even if defer closes again)
	if err := tempFile.Close(); err != nil {
		fmt.Printf("Failed to close temporary file: %v\n", err)
		return err
	}

	fmt.Println("\nDownloaded.")

	cmd1 := exec.Command("mp4decrypt", "--key", key, tempFile.Name(), filepath.Base(savePath))
	cmd1.Dir = filepath.Dir(savePath) // Set working directory to avoid path issues with non-ASCII characters
	outlog, err := cmd1.CombinedOutput()
	if err != nil {
		fmt.Printf("Decrypt failed: %v\n", err)
		fmt.Printf("Output:\n%s\n", outlog)
		return err
	}

	fmt.Println("Decrypted.")
	return nil
}

// DecryptMP4 decrypts a fragmented MP4 file using keys from a Widevine license.
// Supports both CENC and CBCS encryption schemes.
func DecryptMP4(r io.Reader, key []byte, w io.Writer) error {
	inMp4, err := mp4.DecodeFile(r)
	if err != nil {
		return fmt.Errorf("failed to decode file: %w", err)
	}
	if !inMp4.IsFragmented() {
		return errors.New("file is not fragmented")
	}
	if inMp4.Init == nil {
		return errors.New("no init part of file")
	}

	decryptInfo, err := mp4.DecryptInit(inMp4.Init)
	if err != nil {
		return fmt.Errorf("failed to decrypt init: %w", err)
	}
	if err = inMp4.Init.Encode(w); err != nil {
		return fmt.Errorf("failed to write init: %w", err)
	}

	for _, seg := range inMp4.Segments {
		if err = mp4.DecryptSegment(seg, decryptInfo, key); err != nil {
			if err.Error() == "no senc box in traf" {
				// No SENC box; skip decryption for this segment
				err = nil
			} else {
				return fmt.Errorf("failed to decrypt segment: %w", err)
			}
		}
		if err = seg.Encode(w); err != nil {
			return fmt.Errorf("failed to encode segment: %w", err)
		}
	}
	return nil
}
