# Apple Music ALAC / Dolby Atmos / AAC / MV Downloader
A command-line tool written in Go for downloading Apple Music content, including ALAC lossless audio, Dolby Atmos, AAC, and music videos.
Supports album, single, and playlist URLs, with options for lyric extraction and metadata handling.

## Required
- Make sure to install [MP4Box](https://gpac.io/downloads/gpac-nightly-builds/).
- Apple Music media-user-token for AAC-LC, MV, and Lyrics downloads.
- [mp4decrypt](https://www.bento4.com/downloads/) for MV Download.
- [wrapper](https://github.com/naufalfaisa/wrapper) for ALAC and Atmos downloads.

## Usage
```
Usage: [main|main.exe|go run main.go] [options] [url1 url2 ...]
Search Usage: [main|main.exe|go run main.go] --search [album|song|artist] [query]

Options:
      --aac                    Enable adm-aac download mode
      --aac-type string        Select AAC type, aac aac-binaural aac-downmix (default "aac-lc")
      --alac-max int           Specify the max quality for download alac (default 192000)
      --all-album              Download all artist albums
      --atmos                  Enable atmos download mode
      --atmos-max int          Specify the max quality for download atmos (default 2768)
      --debug                  Enable debug mode to show audio quality information
      --mv-audio-type string   Select MV audio type, atmos ac3 aac (default "atmos")
      --mv-max int             Specify the max quality for download MV (default 2160)
      --search string          Search for 'album', 'song', or 'artist'. Provide query after flags.
      --select                 Enable selective download
      --song                   Enable single song download mode
```

### Usage Example
1. Downloading some albums: ```go run main.go https://music.apple.com/us/album/whenever-you-need-somebody-2022-remaster/1624945511```.
2. Downloading single song: ```go run main.go --song https://music.apple.com/us/album/never-gonna-give-you-up-2022-remaster/1624945511?i=1624945512 or go run main.go https://music.apple.com/us/song/you-move-me-2022-remaster/1624945520```.
3. Downloading select: go run main.go --select ```https://music.apple.com/us/album/whenever-you-need-somebody-2022-remaster/1624945511```.
4. Downloading some playlists: ```go run main.go https://music.apple.com/us/playlist/taylor-swift-essentials/pl.3950454ced8c45a3b0cc693c2a7db97b or go run main.go https://music.apple.com/us/playlist/hi-res-lossless-24-bit-192khz/pl.u-MDAWvpjt38370N```.
5. For dolby atmos: ```go run main.go --atmos https://music.apple.com/us/album/1989-taylors-version-deluxe/1713845538```.
6. For aac: ```go run main.go --aac https://music.apple.com/us/album/1989-taylors-version-deluxe/1713845538```.
7. For see quality: ```go run main.go --debug https://music.apple.com/us/album/1989-taylors-version-deluxe/1713845538```.

## License
This project is modified from [zhaarey/apple-music-alac-atmos-downloader](https://github.com/zhaarey/apple-music-alac-atmos-downloader).