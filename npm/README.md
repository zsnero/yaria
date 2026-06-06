# Yaria

The fastest video and audio downloader for your terminal. Downloads from YouTube, Instagram, Twitter/X, TikTok, and 1000+ sites.

## Install

```bash
npm install -g @zsnero/yaria
```

## Usage

```bash
# Download a video
yaria https://www.youtube.com/watch?v=dQw4w9WgXcQ

# Interactive mode
yaria

# Audio only
yaria download -x --audio-format mp3 https://www.youtube.com/watch?v=dQw4w9WgXcQ

# List formats
yaria download -F https://www.youtube.com/watch?v=dQw4w9WgXcQ

# Specific resolution
yaria download -f "bestvideo[height<=720]+bestaudio" https://www.youtube.com/watch?v=dQw4w9WgXcQ
```

All yt-dlp flags are supported and passed through directly.

## Features

- Downloads from **1000+ sites** via yt-dlp
- **Multi-connection acceleration** with aria2c (16 connections, 32 splits)
- **Container format selection** -- output as mp4, mkv, or webm
- **Interactive TUI** with format and resolution selection
- **Background daemon** for queued downloads
- **Cookie support** for age-restricted content
- **Cross-platform** -- Linux, macOS, Windows

## Links

- [Website](https://yaria.live)
- [GitHub](https://github.com/zsnero/yaria)
- [Desktop App](https://yaria.live/download)
