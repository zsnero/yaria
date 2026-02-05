# yaria
Zip through video and audio downloads from tons of sites with yt-dlp's versatility and aria2's multi-connection speed. A slick TUI lets you pick formats and resolutions, skips duplicates, nails playlists, and shows real-time progress. No log files. Fast, fun, and fuss-free!

## Requirements

**None!** The app automatically downloads and manages its own dependencies:
- **yt-dlp** - Auto-downloaded from GitHub on first run
- **aria2c** - Auto-downloaded from GitHub on first run (optional, for faster downloads)

Both are automatically updated every 24 hours if outdated.

## Installation

1. Download the yaria binary for your platform
2. Make it executable (Linux/macOS): `chmod +x yaria`
3. Run: `./yaria` or `yaria.exe` (Windows)
4. Dependencies will be automatically downloaded to a `dependencies/` folder on first run

## Usage

**Interactive TUI mode:**
```bash
./yaria
```
Provides an interactive interface to select format, resolution, and manage downloads.

**CLI mode:**
```bash
./yaria <youtube-url>
```
Downloads with default settings (best quality).

**CLI mode with yt-dlp flags:**
```bash
./yaria <youtube-url> [yt-dlp-flags...]
```
All yt-dlp flags are supported and passed through directly. Examples:
```bash
# Download specific format
./yaria https://youtube.com/watch?v=... --format 137+140

# Download with subtitles
./yaria https://youtube.com/watch?v=... --write-subs --sub-lang en

# Download with metadata and thumbnail
./yaria https://youtube.com/watch?v=... --add-metadata --write-thumbnail --embed-thumbnail
```

## Troubleshooting

### "Failed to fetch metadata" error
- **Cause:** Network issue preventing GitHub API access, or firewall blocking downloads
- **Solution:** Check your internet connection and firewall settings. The app will automatically download yt-dlp on first run if it's missing

### "Age-restricted video" error
- **Cause:** Video requires login to view
- **Solution:** The app will automatically prompt you to select a browser to use cookies from

### "Video unavailable" error
- **Cause:** Video is private, deleted, or region-locked
- **Solution:** Check the URL and try a different video

### "Rate limited" error
- **Cause:** Too many requests to YouTube
- **Solution:** Wait a few minutes and try again. The app will automatically use browser cookies if needed
