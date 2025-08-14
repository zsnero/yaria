package downloader

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
	"yaria/config"

	"github.com/google/go-github/v62/github"
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
	// Create dependencies folder
	exePath, err := os.Executable()
	if err != nil {
		exePath, _ = os.Getwd() // Fallback to current directory
	}
	depsDir := filepath.Join(filepath.Dir(exePath), "dependencies")
	if err := os.MkdirAll(depsDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create dependencies directory: %v", err)
	}

	// Check if version check is needed (every 24 hours)
	lastCheckFile := filepath.Join(depsDir, "last_check")
	shouldCheckVersions := true
	if info, err := os.Stat(lastCheckFile); err == nil {
		if time.Since(info.ModTime()) < 24*time.Hour {
			shouldCheckVersions = false
			fmt.Fprintf(cfg.Stderr, "ℹ️ Skipping version check, last checked at %s\n", info.ModTime().Format(time.RFC3339))
		}
	}

	// Initialize GitHub client
	var client *github.Client
	if shouldCheckVersions {
		client = github.NewClient(nil)
	}

	// Check and download yt-dlp
	ytDlpBinary := "yt-dlp"
	if runtime.GOOS == "windows" {
		ytDlpBinary = "yt-dlp.exe"
	}
	ytDlpPath := filepath.Join(depsDir, ytDlpBinary)
	shouldDownloadYTDLP := false
	if _, err := exec.LookPath(ytDlpBinary); err != nil {
		if _, err := os.Stat(ytDlpPath); err != nil {
			shouldDownloadYTDLP = true
		} else if shouldCheckVersions {
			// Check yt-dlp version
			cmd := exec.Command(ytDlpPath, "--version")
			localVersion, err := cmd.Output()
			if err != nil {
				fmt.Fprintf(cfg.Stderr, "⚠️ Warning: Failed to check yt-dlp version: %v\n", err)
				shouldDownloadYTDLP = true
			} else {
				release, _, err := client.Repositories.GetLatestRelease(context.Background(), "yt-dlp", "yt-dlp")
				if err != nil {
					return nil, fmt.Errorf("failed to fetch yt-dlp release: %v", err)
				}
				latestVersion := strings.TrimPrefix(release.GetTagName(), "v")
				localVersionStr := strings.TrimSpace(string(localVersion))
				if localVersionStr != latestVersion {
					fmt.Fprintf(cfg.Stderr, "ℹ️ Local yt-dlp version %s is outdated, latest is %s\n", localVersionStr, latestVersion)
					shouldDownloadYTDLP = true
				} else {
					fmt.Fprintf(cfg.Stderr, "✅ Found yt-dlp in dependencies at %s (version %s)\n", ytDlpPath, localVersionStr)
				}
			}
		} else {
			fmt.Fprintf(cfg.Stderr, "✅ Found yt-dlp in dependencies at %s\n", ytDlpPath)
		}
	} else {
		fmt.Fprintf(cfg.Stderr, "✅ Found yt-dlp in system PATH\n")
	}

	if shouldDownloadYTDLP {
		fmt.Fprintf(cfg.Stderr, "⬇️ Downloading yt-dlp from GitHub...\n")
		if client == nil {
			client = github.NewClient(nil)
		}
		release, _, err := client.Repositories.GetLatestRelease(context.Background(), "yt-dlp", "yt-dlp")
		if err != nil {
			return nil, fmt.Errorf("failed to fetch yt-dlp release: %v", err)
		}
		var downloadURL string
		for _, asset := range release.Assets {
			if asset.GetName() == ytDlpBinary {
				downloadURL = asset.GetBrowserDownloadURL()
				break
			}
		}
		if downloadURL == "" {
			return nil, errors.New("no suitable yt-dlp binary found")
		}
		resp, err := http.Get(downloadURL)
		if err != nil {
			return nil, fmt.Errorf("failed to download yt-dlp: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("failed to download yt-dlp: HTTP status %s", resp.Status)
		}
		if err := os.Remove(ytDlpPath); err != nil && !os.IsNotExist(err) {
			fmt.Fprintf(cfg.Stderr, "⚠️ Warning: Failed to remove outdated yt-dlp: %v\n", err)
		}
		out, err := os.Create(ytDlpPath)
		if err != nil {
			return nil, fmt.Errorf("failed to create yt-dlp binary: %v", err)
		}
		_, err = io.Copy(out, resp.Body)
		out.Close()
		if err != nil {
			return nil, fmt.Errorf("failed to save yt-dlp: %v", err)
		}
		if runtime.GOOS != "windows" {
			if err := os.Chmod(ytDlpPath, 0755); err != nil {
				return nil, fmt.Errorf("failed to set permissions for yt-dlp: %v", err)
			}
		}
		fmt.Fprintf(cfg.Stderr, "✅ Downloaded yt-dlp to %s\n", ytDlpPath)
	}

	// Check and download aria2
	aria2Binary := "aria2c"
	if runtime.GOOS == "windows" {
		aria2Binary = "aria2c.exe"
	}
	aria2Path := filepath.Join(depsDir, aria2Binary)
	shouldDownloadAria2 := false
	if _, err := exec.LookPath(aria2Binary); err != nil {
		if _, err := os.Stat(aria2Path); err != nil {
			shouldDownloadAria2 = true
		} else if shouldCheckVersions {
			// Check aria2 version
			cmd := exec.Command(aria2Path, "--version")
			localVersion, err := cmd.Output()
			if err != nil {
				fmt.Fprintf(cfg.Stderr, "⚠️ Warning: Failed to check aria2 version: %v\n", err)
				shouldDownloadAria2 = true
			} else {
				release, _, err := client.Repositories.GetLatestRelease(context.Background(), "aria2", "aria2")
				if err != nil {
					fmt.Fprintf(cfg.Stderr, "⚠️ Warning: Failed to fetch aria2 release: %v\n", err)
					cfg.UseAria2c = false
				} else {
					latestVersion := strings.TrimPrefix(release.GetTagName(), "release-")
					localVersionStr := strings.TrimSpace(string(localVersion))
					if strings.Contains(localVersionStr, "aria2 ") {
						localVersionStr = strings.Split(localVersionStr, " ")[1]
					}
					if localVersionStr != latestVersion {
						fmt.Fprintf(cfg.Stderr, "ℹ️ Local aria2 version %s is outdated, latest is %s\n", localVersionStr, latestVersion)
						shouldDownloadAria2 = true
					} else {
						fmt.Fprintf(cfg.Stderr, "✅ Found aria2 in dependencies at %s (version %s)\n", aria2Path, localVersionStr)
						cfg.UseAria2c = true
					}
				}
			}
		} else {
			fmt.Fprintf(cfg.Stderr, "✅ Found aria2 in dependencies at %s\n", aria2Path)
			cfg.UseAria2c = true
		}
	} else {
		fmt.Fprintf(cfg.Stderr, "✅ Found aria2 in system PATH\n")
		cfg.UseAria2c = true
	}

	if shouldDownloadAria2 {
		fmt.Fprintf(cfg.Stderr, "⬇️ Downloading aria2 from GitHub...\n")
		if client == nil {
			client = github.NewClient(nil)
		}
		release, _, err := client.Repositories.GetLatestRelease(context.Background(), "aria2", "aria2")
		if err != nil {
			fmt.Fprintf(cfg.Stderr, "⚠️ Warning: Failed to fetch aria2 release: %v\n", err)
			cfg.UseAria2c = false
		} else {
			assetPattern := fmt.Sprintf("aria2-[0-9.]+-%s-%s", runtime.GOOS, runtime.GOARCH)
			var downloadURL string
			for _, asset := range release.Assets {
				if strings.Contains(asset.GetName(), assetPattern) && !strings.Contains(asset.GetName(), ".tar.") && !strings.Contains(asset.GetName(), ".zip") {
					downloadURL = asset.GetBrowserDownloadURL()
					break
				}
			}
			if downloadURL == "" {
				fmt.Fprintf(cfg.Stderr, "⚠️ Warning: No suitable aria2 binary found\n")
				cfg.UseAria2c = false
			} else {
				resp, err := http.Get(downloadURL)
				if err != nil {
					fmt.Fprintf(cfg.Stderr, "⚠️ Warning: Failed to download aria2: %v\n", err)
					cfg.UseAria2c = false
				} else {
					defer resp.Body.Close()
					if resp.StatusCode != http.StatusOK {
						fmt.Fprintf(cfg.Stderr, "⚠️ Warning: Failed to download aria2: HTTP status %s\n", resp.Status)
						cfg.UseAria2c = false
					} else {
						if err := os.Remove(aria2Path); err != nil && !os.IsNotExist(err) {
							fmt.Fprintf(cfg.Stderr, "⚠️ Warning: Failed to remove outdated aria2: %v\n", err)
						}
						out, err := os.Create(aria2Path)
						if err != nil {
							fmt.Fprintf(cfg.Stderr, "⚠️ Warning: Failed to create aria2 binary: %v\n", err)
							cfg.UseAria2c = false
						} else {
							_, err = io.Copy(out, resp.Body)
							out.Close()
							if err != nil {
								fmt.Fprintf(cfg.Stderr, "⚠️ Warning: Failed to save aria2: %v\n", err)
								cfg.UseAria2c = false
							} else if runtime.GOOS != "windows" {
								if err := os.Chmod(aria2Path, 0755); err != nil {
									fmt.Fprintf(cfg.Stderr, "⚠️ Warning: Failed to set permissions for aria2: %v\n", err)
									cfg.UseAria2c = false
								} else {
									fmt.Fprintf(cfg.Stderr, "✅ Downloaded aria2 to %s\n", aria2Path)
									cfg.UseAria2c = true
								}
							} else {
								fmt.Fprintf(cfg.Stderr, "✅ Downloaded aria2 to %s\n", aria2Path)
								cfg.UseAria2c = true
							}
						}
					}
				}
			}
		}
	}

	// Update last_check timestamp if versions were checked
	if shouldCheckVersions {
		if f, err := os.Create(lastCheckFile); err != nil {
			fmt.Fprintf(cfg.Stderr, "⚠️ Warning: Failed to update last_check timestamp: %v\n", err)
		} else {
			f.Close()
		}
	}

	// Update PATH to include dependencies folder
	currentPath := os.Getenv("PATH")
	newPath := depsDir + string(os.PathListSeparator) + currentPath
	if err := os.Setenv("PATH", newPath); err != nil {
		return nil, fmt.Errorf("failed to update PATH: %v", err)
	}

	// Original dependency checks
	if _, err := exec.LookPath(ytDlpBinary); err != nil {
		return nil, errors.New("yt-dlp not installed")
	}
	if _, err := exec.LookPath(aria2Binary); err != nil {
		cfg.UseAria2c = false
	}
	return &YTDLPDownloader{cfg: cfg}, nil
}

// readFile reads the content of a file
func readFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}

// GetMetadata fetches playlist info and video title in one command
func (d *YTDLPDownloader) GetMetadata(args []string) (string, string, error) {
	ytDlpCmd := "yt-dlp"
	if runtime.GOOS == "windows" {
		ytDlpCmd = "yt-dlp.exe"
	}
	cmd := exec.Command(ytDlpCmd, append([]string{"--flat-playlist", "--print", "%(playlist)s&%(playlist_title)s&%(playlist_count)s&%(title)s"}, args...)...)
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
	ytDlpCmd := "yt-dlp"
	if runtime.GOOS == "windows" {
		ytDlpCmd = "yt-dlp.exe"
	}
	cmd := exec.Command(ytDlpCmd, append([]string{"--print", "filename", "--output", tempDir + "/" + d.cfg.OutputTemplate}, args...)...)
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
	ytDlpCmd := "yt-dlp"
	if runtime.GOOS == "windows" {
		ytDlpCmd = "yt-dlp.exe"
	}
	cmd := exec.Command(ytDlpCmd, "--list-formats", url)
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
	ytDlpCmd := "yt-dlp"
	if runtime.GOOS == "windows" {
		ytDlpCmd = "yt-dlp.exe"
	}
	for attempt := 1; attempt <= d.cfg.MaxRetries; attempt++ {
		cmdArgs := []string{
			"--no-overwrites",
			"--geo-bypass",
			"--no-check-certificate",
			"--concurrent-fragments", "16",
			"--downloader", "aria2c",
			"--downloader-args", "aria2c:--max-connection-per-server=16 --split=16 --max-concurrent-downloads=16",
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
			aria2Cmd := "aria2c"
			if runtime.GOOS == "windows" {
				aria2Cmd = "aria2c.exe"
			}
			cmdArgs = append(cmdArgs, "--downloader", aria2Cmd, "--downloader-args", "aria2c:"+d.cfg.Aria2cArgs)
		}

		cmd := exec.Command(ytDlpCmd, cmdArgs...)
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
					aria2Cmd := "aria2c"
					if runtime.GOOS == "windows" {
						aria2Cmd = "aria2c.exe"
					}
					fallbackArgs = append(fallbackArgs, "--downloader", aria2Cmd, "--downloader-args", "aria2c:"+d.cfg.Aria2cArgs)
				}
				cmd := exec.Command(ytDlpCmd, fallbackArgs...)
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
