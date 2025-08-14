package downloader

import (
	"errors"
	"os/exec"
	"strconv"
	"strings"
	"yaria/config"
)

// Downloader defines the interface for yt-dlp operations
type Downloader interface {
	GetMetadata(args []string) (string, string, error)
	GetOutputFilename(args []string, tempDir string) (string, error)
	GetFormats(url string) ([]Format, error)
	Download(args []string, tempDir string) (bool, error)
}

// Format represents a video/audio format
type Format struct {
	ID       string
	Height   int
	Ext      string
	IsAudio  bool
	Protocol string
}

// YTDLPDownloader implements the Downloader interface
type YTDLPDownloader struct {
	cfg *config.Config
}

// New creates a new YTDLPDownloader
func New(cfg *config.Config) (*YTDLPDownloader, error) {
	if _, err := exec.LookPath("yt-dlp"); err != nil {
		return nil, errors.New("yt-dlp not installed")
	}
	if _, err := exec.LookPath("aria2c"); err != nil {
		cfg.UseAria2c = false
	}
	return &YTDLPDownloader{cfg: cfg}, nil
}

// GetMetadata fetches playlist info and video title in one command
func (d *YTDLPDownloader) GetMetadata(args []string) (string, string, error) {
	cmd := exec.Command("yt-dlp", append([]string{"--flat-playlist", "--print", "%(playlist)s&%(playlist_title)s&%(playlist_count)s&%(title)s"}, args...)...)
	output, err := cmd.Output()
	if err != nil {
		return "", "", err
	}
	parts := splitLines(string(output))
	if len(parts) == 0 {
		return "", "", errors.New("no metadata found")
	}
	metadata := strings.SplitN(parts[0], "&", 4)
	if len(metadata) < 4 {
		return "", "", errors.New("incomplete metadata")
	}
	playlistInfo := strings.Join(metadata[:3], "&")
	title := metadata[3]
	return playlistInfo, title, nil
}

// GetOutputFilename predicts the output filename
func (d *YTDLPDownloader) GetOutputFilename(args []string, tempDir string) (string, error) {
	cmd := exec.Command("yt-dlp", append([]string{"--print", "filename", "--output", tempDir + "/" + d.cfg.OutputTemplate}, args...)...)
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	lines := splitLines(string(output))
	if len(lines) > 0 {
		return lines[0], nil
	}
	return "", errors.New("no filename found")
}

// GetFormats fetches available formats for a URL
func (d *YTDLPDownloader) GetFormats(url string) ([]Format, error) {
	cmd := exec.Command("yt-dlp", "--list-formats", url)
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var formats []Format
	lines := splitLines(string(output))
	for _, line := range lines {
		if strings.Contains(line, "video only") || strings.Contains(line, "audio only") {
			fields := strings.Fields(line)
			if len(fields) < 3 {
				continue
			}
			formatID := fields[0]
			isAudio := strings.Contains(line, "audio only")
			height := 0
			ext := ""
			protocol := ""
			for _, field := range fields {
				if strings.Contains(field, "x") && !isAudio {
					if res, err := strconv.Atoi(strings.Split(field, "x")[1]); err == nil {
						height = res
					}
				}
				if strings.Contains(field, "mp4") || strings.Contains(field, "webm") || strings.Contains(field, "m4a") || strings.Contains(field, "mp3") {
					ext = field
				}
				if strings.Contains(field, "http") || strings.Contains(field, "m3u8") {
					protocol = field
				}
			}
			// Include formats with m3u8 as a fallback, prioritize http
			if (isAudio && ext != "") || (!isAudio && height > 0) {
				formats = append(formats, Format{
					ID:       formatID,
					Height:   height,
					Ext:      ext,
					IsAudio:  isAudio,
					Protocol: protocol,
				})
			}
		}
	}
	// Sort formats to prioritize http over m3u8
	sortedFormats := make([]Format, 0, len(formats))
	httpFormats := make([]Format, 0)
	m3u8Formats := make([]Format, 0)
	for _, f := range formats {
		if f.Protocol == "http" || f.Protocol == "" {
			httpFormats = append(httpFormats, f)
		} else {
			m3u8Formats = append(m3u8Formats, f)
		}
	}
	sortedFormats = append(sortedFormats, httpFormats...)
	sortedFormats = append(sortedFormats, m3u8Formats...)
	return sortedFormats, nil
}

// Download executes the download process with retries and fallback
func (d *YTDLPDownloader) Download(args []string, tempDir string) (bool, error) {
	for attempt := 1; attempt <= d.cfg.MaxRetries; attempt++ {
		cmdArgs := []string{
			"--no-overwrites",
			"--geo-bypass",
			"--no-check-certificate",
			"--concurrent-fragments", "16",
			"--output", tempDir + "/" + d.cfg.OutputTemplate,
		}
		if d.cfg.IsAudioOnly {
			cmdArgs = append(cmdArgs, "--extract-audio", "--audio-format", d.cfg.AudioFormat)
		} else if d.cfg.Resolution != "" {
			cmdArgs = append(cmdArgs, "--format", d.cfg.Resolution)
		} else {
			cmdArgs = append(cmdArgs, "--format", "bestvideo+bestaudio/best")
		}
		cmdArgs = append(cmdArgs, args...)

		if d.cfg.UseAria2c && attempt <= 2 {
			cmdArgs = append(cmdArgs, "--downloader", "aria2c", "--downloader-args", "aria2c:"+d.cfg.Aria2cArgs)
		}

		cmd := exec.Command("yt-dlp", cmdArgs...)
		cmd.Stdout = d.cfg.Stdout
		cmd.Stderr = d.cfg.Stderr

		if err := cmd.Run(); err == nil {
			return true, nil
		} else {
			d.cfg.Stderr.Write([]byte("WARNING: Download failed with selected format, trying fallback format...\n"))
			// Try fallback format on last attempt
			if attempt == d.cfg.MaxRetries {
				fallbackArgs := []string{
					"--no-overwrites",
					"--geo-bypass",
					"--no-check-certificate",
					"--concurrent-fragments", "16",
					"--output", tempDir + "/" + d.cfg.OutputTemplate,
				}
				if d.cfg.IsAudioOnly {
					fallbackArgs = append(fallbackArgs, "--extract-audio", "--audio-format", d.cfg.AudioFormat)
				} else {
					fallbackArgs = append(fallbackArgs, "--format", "bestvideo[height<=1080]+bestaudio/best")
				}
				fallbackArgs = append(fallbackArgs, args...)
				if d.cfg.UseAria2c {
					fallbackArgs = append(fallbackArgs, "--downloader", "aria2c", "--downloader-args", "aria2c:"+d.cfg.Aria2cArgs)
				}
				cmd := exec.Command("yt-dlp", fallbackArgs...)
				cmd.Stdout = d.cfg.Stdout
				cmd.Stderr = d.cfg.Stderr
				if err := cmd.Run(); err == nil {
					return true, nil
				}
			}
			if attempt < d.cfg.MaxRetries {
				d.cfg.WaitBeforeRetry(attempt)
			}
		}
	}
	return false, errors.New("all download attempts failed, including fallback")
}

// splitLines splits a string into lines and trims whitespace
func splitLines(s string) []string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	for i, line := range lines {
		lines[i] = strings.TrimSpace(line)
	}
	return lines
}
