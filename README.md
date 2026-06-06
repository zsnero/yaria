# Yaria

The fastest video and audio downloader for your terminal. Downloads from 1000+ sites with multi-connection acceleration.

## Features

- Downloads from **YouTube, Instagram, Twitter/X, TikTok, Vimeo, Reddit**, and 1000+ more sites
- **Blazing fast** -- aria2c multi-connection acceleration with 32 concurrent fragments
- **Interactive TUI** -- format selection, resolution picker, download progress
- **Playlist support** -- download entire playlists with one command
- **Audio extraction** -- download audio-only in MP3 or other formats
- **Container format** -- output as mp4, mkv, or webm (default: mp4)
- **Background daemon** -- queue downloads and let them run in the background
- **Auto-dependency management** -- downloads yt-dlp and aria2c automatically if not installed
- **Cookie support** -- auto-detects your browser for age-restricted and login-required content
- **Cross-platform** -- Linux, macOS, Windows

## Quick Start

```bash
# Download a video
yaria https://www.youtube.com/watch?v=dQw4w9WgXcQ

# Interactive mode
yaria download

# Download audio only
yaria download --extract-audio https://www.youtube.com/watch?v=dQw4w9WgXcQ
```

## Installation

### npm (recommended)

```bash
npm install -g @zsnero/yaria
```

### Build from source

```bash
git clone https://github.com/zsnero/yaria.git
cd yaria
make build
```

### Install to PATH

```bash
make install
```

## Usage

```
yaria                              Launch interactive menu
yaria <URL>                        Download video/audio (shortcut)
yaria download                     Interactive video downloader TUI
yaria download <URL>               Download video/audio from URL
yaria download <magnet-link>       Stream magnet link via webtorrent
yaria --help                       Show help
yaria --version                    Show version
```

## Commands

| Command                | Description                                           |
| ---------------------- | ----------------------------------------------------- |
| `yaria`                | Interactive menu                                      |
| `yaria download`       | Video downloader TUI with format/resolution selection |
| `yaria download <URL>` | Direct CLI download (all yt-dlp flags supported)      |
| `yaria <URL>`          | Shortcut for `yaria download <URL>`                   |
| `yaria <magnet>`       | Stream torrent via mpv/vlc                            |
| `yaria activate <key>` | Activate a Pro license key                            |
| `yaria deactivate`     | Remove stored license                                 |
| `yaria status`         | Show license and device info                          |
| `yaria --help`         | Help                                                  |
| `yaria --version`      | Version                                               |

## Supported Sites

Yaria supports all 1000+ sites that yt-dlp supports, including:

YouTube, Instagram, Twitter/X, TikTok, Facebook, Vimeo, Dailymotion, Twitch, Reddit, SoundCloud, Bilibili, NicoNico, PornHub, XVideos, XHamster, and many more.

Sites with special handling get optimized headers, cookie management, and retry logic automatically.

## How It Works

1. **Metadata** -- fetches video info in a single optimized call
2. **Format selection** -- lists available qualities (4K, 1080p, 720p, audio-only)
3. **Download** -- uses aria2c for multi-connection acceleration (32 splits, 16 connections per server, 128MB disk cache)
4. **Retry** -- automatic retries with fallback formats on failure

## Dependencies

Yaria auto-downloads these if not installed:

| Dependency | Purpose                                |
| ---------- | -------------------------------------- |
| yt-dlp     | Video extraction engine                |
| aria2c     | Multi-connection download acceleration |

If auto-download fails, install manually:

```bash
# Arch Linux
sudo pacman -S yt-dlp aria2

# Ubuntu/Debian
sudo apt install yt-dlp aria2

# macOS
brew install yt-dlp aria2

# Windows
winget install yt-dlp aria2
```

## Configuration

All settings are stored in `~/.config/yaria/app.yaml`:

```yaml
yaria:
  theme: Rainbow
```

The config file is created automatically on first run.

## Keyboard Shortcuts (TUI)

| Key          | Action        |
| ------------ | ------------- |
| `up` / `k`   | Navigate up   |
| `down` / `j` | Navigate down |
| `enter`      | Select        |
| `esc`        | Go back       |
| `ctrl+c`     | Quit          |

## Desktop App

**Yaria Desktop** is a cross-platform GUI app built with Wails (Go + WebView):

- Video & audio downloader with format selection and download queue
- Local media library with folder scanning, thumbnails, and TMDB metadata
- Remote media browsing via SSH/SFTP and SMB
- LAN media server (stream to phones, tablets, smart TVs)
- DLNA/UPnP for smart TV discovery
- Built-in video player with keyboard controls and subtitle support
- Inter font, page transitions, glassmorphism UI

Available at [yaria.live](https://yaria.live).

## Pro Features

Yaria Pro adds **Mantorex** -- a torrent search, streaming, and media center:

- Search 11 torrent providers simultaneously with mirror fallback
- Stream torrents directly in the built-in player with auto-transcode
- Background torrent download daemon with pause/resume
- Library with watch progress, Continue Watching, and episode tracking
- Local media library with FFprobe analysis and TMDB enrichment
- Remote sources (SSH/SFTP, SMB) with network device discovery
- Media server with embedded web UI and PIN authentication
- DLNA/UPnP server for smart TVs and game consoles
- Music library with embedded metadata and album art
- NFO file support (Kodi-compatible read/write)

## Build

```bash
make build          # Community edition
make build-all      # Cross-compile for all platforms
make tidy           # go mod tidy
make vet            # Static analysis
make clean          # Remove binaries
```

## License

[MIT](LICENSE)
