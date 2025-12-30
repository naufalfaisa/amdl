# Apple Music Downloader

Forked from [zhaarey](https://github.com/zhaarey/apple-music-downloader)

## Prerequisites

- Install [(MP4Box)](https://gpac.io/downloads/gpac-nightly-builds/) and ensure it is added to your environment variables.
- For MV download, install [mp4decrypt](https://www.bento4.com/downloads/).
- A valid `media-user-token` is required for lyrics and AAC-LC downloads.

## Features

1. Supports inline album covers and LRC lyrics.
2. Supports word-by-word and out-of-sync lyrics.
3. Download all albums of an artist:
```bash
go run main.go https://music.apple.com/us/artist/taylor-swift/159260351 --all-album
```
4. MV download support.
5. Interactive search with arrow-key navigation:
```bash
go run main.go --search [song/album/artist] "search_term"
```

## Audio Formats Supported:

- `alac` – ALAC stereo
- `ec3` – Dolby Atmos / EC3
- `aac` – Stereo AAC
- `aac-lc` – Stereo AAC-LC
- `aac-binaural` – Binaural AAC
- `aac-downmix` – Downmixed stereo AAC
- `MV` – Music videos

## Usage

Ensure the decryption wrapper is running: [WorldObservationLog/wrapper](https://github.com/WorldObservationLog/wrapper.git)

```bash
# Download an album: 
go run main.go <album_url>

# Download a single song:
go run main.go --song <song_url>

# Select specific tracks from an album:
go run main.go --select <album_url>

# Download playlists:
go run main.go <playlist_url>

# Dolby Atmos download:
go run main.go --atmos <album_url>

# AAC download:
go run main.go --aac <album_url>

# Debug/quality check:
go run main.go --debug <album_url>
```

## Downloading Lyrics
1. Log in to Apple Music.
2. Open Developer Tools → Application → Storage → Cookies → `https://3. music.apple.com.`
3. Copy the media-user-token cookie value.
4. Paste the value into `config.yaml` under `media-user-token`.
5. Start the script as usual.

## Translation and Pronunciation Lyrics (Beta)

1. Log in to Apple Music Beta.
2. Open Developer Tools → Network tab.
3. Play a song with translation/pronunciation support.
4. Sniff network requests (syllable-lyrics).
5. Copy the language value and paste into config.yaml.
6. Remove pronunciation if not needed.
7. Start the script as usual.
**Note:** These features are currently in beta.

## Configurable Options via `config.yaml`

### Authentication
- `media-user-token` – Needed for lyrics or AAC-LC downloads.
- `authorization-token` – Usually no change needed; automatically retrieves token.

### Language & Lyrics
- `language` – Supported language per storefront.
- `lrc-type` – Choose between `lyrics` or `syllable-lyrics`.
- `lrc-format` – Options: `lrc`, `ttml`.
- `embed-lrc` – Embed lyrics in audio file.
- `save-lrc-file` – Save lyrics as separate file.

### Cover & Artwork
- `save-artist-cover` – Save artist cover images.
- `save-animated-artwork` / `emby-animated-artwork` – Requires ffmpeg.
- `embed-cover` – Embed album cover in audio file.
- `cover-size` / `cover-format` – Control size and format.

### Download Folders
- `alac-save-folder`, `atmos-save-folder`, `aac-save-folder` – Output folders for each format.

### Memory & Port
- `max-memory-limit` – Maximum memory usage in MB.
- `decrypt-m3u8-port`, `get-m3u8-port` – Local ports for streaming/decryption.
- `get-m3u8-from-device`, `get-m3u8-mode` – Device mode and quality.

### Audio Settings
- `aac-type` – Choose AAC format.
- `alac-max` – Max sample rate.
- `atmos-max` – Max bitrate.
- `limit-max` – Maximum number of tracks to download.

### File & Folder Naming
- `album-folder-format`, `playlist-folder-format`, `song-file-format`, `artist-folder-format`.

### Explicit / Clean / Master Tags
- `explicit-choice`, `clean-choice`, `apple-master-choice`.

### Playlist Options
- `use-songinfo-for-playlist`, `dl-albumcover-for-playlist`.

### Music Video
- `mv-audio-type`, `mv-max` – Audio type and max resolution for MV download.

### Post-download Conversion
- `convert-after-download`, `convert-format`, `convert-keep-original`.
- `convert-skip-if-source-matches`, `ffmpeg-path`, `convert-extra-args`.
- `convert-warn-lossy-to-lossless`, `convert-skip-lossy-to-lossless`.
