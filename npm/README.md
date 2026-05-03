# Yaria

The fastest video and audio downloader for your terminal. Downloads from YouTube, Instagram, Twitter/X, TikTok, and 1000+ sites.

## Install

```bash
npm install -g yaria
```

## Usage

```bash
# Download a video
yaria https://www.youtube.com/watch?v=dQw4w9WgXcQ

# Interactive mode
yaria

# Audio only
yaria download --extract-audio https://www.youtube.com/watch?v=dQw4w9WgXcQ
```

## Features

- Downloads from **1000+ sites** via yt-dlp
- **Multi-connection acceleration** with aria2c (32 concurrent fragments)
- **Interactive TUI** with format and resolution selection
- **Background daemon** for queued downloads
- **Cookie support** for age-restricted content
- **Cross-platform** -- Linux, macOS, Windows

## Links

- [Website](https://yaria.live)
- [GitHub](https://github.com/zsnero/yaria)
- [Desktop App](https://yaria.live/download)
