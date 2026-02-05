package downloader

import (
	"archive/zip"
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

// Interface for yt-dlp operations
type Downloader interface {
	GetMetadata(args []string) (string, string, error)
	GetOutputFilename(args []string, tempDir string) (string, error)
	GetFormats(url string) ([]Format, error)
	Download(args []string, tempDir string) (bool, error)
}

// Represents video/audio format
type Format struct {
	ID       string
	Height   int
	Ext      string
	IsAudio  bool
	Protocol string
	FileSize string
}

// Implements the Downloader interface
type YTDLPDownloader struct {
	cfg *config.Config
}

func New(cfg *config.Config) (*YTDLPDownloader, error) {
	// Create dependencies folder
	exePath, err := os.Executable()
	if err != nil {
		exePath, _ = os.Getwd() // Fallback to current directory
	}
	depsDir := filepath.Join(filepath.Dir(exePath), "dependencies")
	if err := os.MkdirAll(depsDir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create dependencies directory: %v", err)
	}

	// Check if version check is needed (every 24 hours)
	lastCheckFile := filepath.Join(depsDir, "last_check")
	shouldCheckVersions := true
	if info, err := os.Stat(lastCheckFile); err == nil {
		if time.Since(info.ModTime()) < 24*time.Hour {
			shouldCheckVersions = false
			fmt.Fprintf(cfg.Stderr, "Skipping version check, last checked at %s\n", info.ModTime().Format(time.RFC3339))
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
				fmt.Fprintf(cfg.Stderr, "Warning: Failed to check yt-dlp version: %v\n", err)
				shouldDownloadYTDLP = true
			} else {
				release, _, err := client.Repositories.GetLatestRelease(context.Background(), "yt-dlp", "yt-dlp")
				if err != nil {
					return nil, fmt.Errorf("failed to fetch yt-dlp release: %v", err)
				}
				latestVersion := strings.TrimPrefix(release.GetTagName(), "v")
				localVersionStr := strings.TrimSpace(string(localVersion))
				if localVersionStr != latestVersion {
					fmt.Fprintf(cfg.Stderr, "Local yt-dlp version %s is outdated, latest is %s\n", localVersionStr, latestVersion)
					shouldDownloadYTDLP = true
				} else {
					fmt.Fprintf(cfg.Stderr, "Found yt-dlp in dependencies at %s (version %s)\n", ytDlpPath, localVersionStr)
				}
			}
		} else {
			fmt.Fprintf(cfg.Stderr, "Found yt-dlp in dependencies at %s\n", ytDlpPath)
		}
	} else {
		fmt.Fprintf(cfg.Stderr, "Found yt-dlp in system PATH\n")
	}

	if shouldDownloadYTDLP {
		fmt.Fprintf(cfg.Stderr, "Downloading yt-dlp from GitHub...\n")
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
			fmt.Fprintf(cfg.Stderr, "Warning: Failed to remove outdated yt-dlp: %v\n", err)
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
			if err := os.Chmod(ytDlpPath, 0o755); err != nil {
				return nil, fmt.Errorf("failed to set permissions for yt-dlp: %v", err)
			}
		}
		fmt.Fprintf(cfg.Stderr, "Downloaded yt-dlp to %s\n", ytDlpPath)
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
				fmt.Fprintf(cfg.Stderr, "Warning: Failed to check aria2 version: %v\n", err)
				shouldDownloadAria2 = true
			} else {
				release, _, err := client.Repositories.GetLatestRelease(context.Background(), "aria2", "aria2")
				if err != nil {
					fmt.Fprintf(cfg.Stderr, "Warning: Failed to fetch aria2 release: %v\n", err)
					cfg.UseAria2c = false
				} else {
					latestVersion := strings.TrimPrefix(release.GetTagName(), "release-")
					localVersionStr := strings.TrimSpace(string(localVersion))
					if strings.Contains(localVersionStr, "aria2 ") {
						localVersionStr = strings.Split(localVersionStr, " ")[1]
					}
					if localVersionStr != latestVersion {
						fmt.Fprintf(cfg.Stderr, "Local aria2 version %s is outdated, latest is %s\n", localVersionStr, latestVersion)
						shouldDownloadAria2 = true
					} else {
						fmt.Fprintf(cfg.Stderr, "Found aria2 in dependencies at %s (version %s)\n", aria2Path, localVersionStr)
						cfg.UseAria2c = true
					}
				}
			}
		} else {
			fmt.Fprintf(cfg.Stderr, "Found aria2 in dependencies at %s\n", aria2Path)
			cfg.UseAria2c = true
		}
	} else {
		fmt.Fprintf(cfg.Stderr, "Found aria2 in system PATH\n")
		cfg.UseAria2c = true
	}

	if shouldDownloadAria2 {
		fmt.Fprintf(cfg.Stderr, "Downloading aria2 from GitHub...\n")
		if client == nil {
			client = github.NewClient(nil)
		}
		release, _, err := client.Repositories.GetLatestRelease(context.Background(), "aria2", "aria2")
		if err != nil {
			fmt.Fprintf(cfg.Stderr, "Warning: Failed to fetch aria2 release: %v\n", err)
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
				fmt.Fprintf(cfg.Stderr, "Warning: No suitable aria2 binary found\n")
				cfg.UseAria2c = false
			} else {
				resp, err := http.Get(downloadURL)
				if err != nil {
					fmt.Fprintf(cfg.Stderr, "Warning: Failed to download aria2: %v\n", err)
					cfg.UseAria2c = false
				} else {
					defer resp.Body.Close()
					if resp.StatusCode != http.StatusOK {
						fmt.Fprintf(cfg.Stderr, "Warning: Failed to download aria2: HTTP status %s\n", resp.Status)
						cfg.UseAria2c = false
					} else {
						if err := os.Remove(aria2Path); err != nil && !os.IsNotExist(err) {
							fmt.Fprintf(cfg.Stderr, "Warning: Failed to remove outdated aria2: %v\n", err)
						}
						out, err := os.Create(aria2Path)
						if err != nil {
							fmt.Fprintf(cfg.Stderr, "Warning: Failed to create aria2 binary: %v\n", err)
							cfg.UseAria2c = false
						} else {
							_, err = io.Copy(out, resp.Body)
							out.Close()
							if err != nil {
								fmt.Fprintf(cfg.Stderr, "Warning: Failed to save aria2: %v\n", err)
								cfg.UseAria2c = false
							} else if runtime.GOOS != "windows" {
								if err := os.Chmod(aria2Path, 0o755); err != nil {
									fmt.Fprintf(cfg.Stderr, "Warning: Failed to set permissions for aria2: %v\n", err)
									cfg.UseAria2c = false
								} else {
									fmt.Fprintf(cfg.Stderr, "Downloaded aria2 to %s\n", aria2Path)
									cfg.UseAria2c = true
								}
							} else {
								fmt.Fprintf(cfg.Stderr, "Downloaded aria2 to %s\n", aria2Path)
								cfg.UseAria2c = true
							}
						}
					}
				}
			}
		}
	}

	// Check and download deno for JavaScript challenge solving
	denoBinary := "deno"
	if runtime.GOOS == "windows" {
		denoBinary = "deno.exe"
	}
	denoPath := filepath.Join(depsDir, denoBinary)
	if _, err := exec.LookPath(denoBinary); err != nil {
		if _, err := os.Stat(denoPath); err != nil {
			fmt.Fprintf(cfg.Stderr, "Downloading deno for JavaScript challenge solving...\n")
			// Determine platform-specific download URL
			var denoURL string
			switch runtime.GOOS {
			case "linux":
				denoURL = "https://github.com/denoland/deno/releases/latest/download/deno-x86_64-unknown-linux-gnu.zip"
			case "darwin":
				if runtime.GOARCH == "arm64" {
					denoURL = "https://github.com/denoland/deno/releases/latest/download/deno-aarch64-apple-darwin.zip"
				} else {
					denoURL = "https://github.com/denoland/deno/releases/latest/download/deno-x86_64-apple-darwin.zip"
				}
			case "windows":
				denoURL = "https://github.com/denoland/deno/releases/latest/download/deno-x86_64-pc-windows-msvc.zip"
			default:
				fmt.Fprintf(cfg.Stderr, "Warning: Unsupported platform for deno auto-install. JavaScript challenges may fail.\n")
			}

			if denoURL != "" {
				resp, err := http.Get(denoURL)
				if err != nil {
					fmt.Fprintf(cfg.Stderr, "Warning: Failed to download deno: %v. JavaScript challenges may fail.\n", err)
				} else {
					defer resp.Body.Close()
					if resp.StatusCode == http.StatusOK {
						// Save zip file temporarily
						zipPath := filepath.Join(depsDir, "deno.zip")
						zipFile, err := os.Create(zipPath)
						if err == nil {
							_, err = io.Copy(zipFile, resp.Body)
							zipFile.Close()
							if err == nil {
								// Extract deno binary from zip
								if err := extractDenoFromZip(zipPath, denoPath); err != nil {
									fmt.Fprintf(cfg.Stderr, "Warning: Failed to extract deno: %v\n", err)
								} else {
									os.Remove(zipPath)
									if runtime.GOOS != "windows" {
										os.Chmod(denoPath, 0o755)
									}
									fmt.Fprintf(cfg.Stderr, "Downloaded deno to %s\n", denoPath)
								}
							}
						}
					}
				}
			}
		} else {
			fmt.Fprintf(cfg.Stderr, "Found deno in dependencies at %s\n", denoPath)
		}
	} else {
		fmt.Fprintf(cfg.Stderr, "Found deno in system PATH\n")
	}

	// Update last_check timestamp if versions were checked
	if shouldCheckVersions {
		if f, err := os.Create(lastCheckFile); err != nil {
			fmt.Fprintf(cfg.Stderr, "Warning: Failed to update last_check timestamp: %v\n", err)
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

// extractDenoFromZip extracts the deno binary from a zip archive
func extractDenoFromZip(zipPath, destPath string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer r.Close()

	for _, f := range r.File {
		// Look for the deno binary (might be in root or subdirectory)
		if strings.HasSuffix(f.Name, "deno") || strings.HasSuffix(f.Name, "deno.exe") {
			rc, err := f.Open()
			if err != nil {
				return err
			}
			defer rc.Close()

			outFile, err := os.Create(destPath)
			if err != nil {
				return err
			}
			defer outFile.Close()

			_, err = io.Copy(outFile, rc)
			return err
		}
	}
	return errors.New("deno binary not found in zip archive")
}

// readFile reads the content of a file
/*
func readFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}
*/

// Fetches playlist info and video title in one command
func (d *YTDLPDownloader) GetMetadata(args []string) (string, string, error) {
	ytDlpCmd := "yt-dlp"
	if runtime.GOOS == "windows" {
		ytDlpCmd = "yt-dlp.exe"
	}

	// Get title first
	titleArgs := []string{"--get-title", "--no-warnings"}
	if d.cfg.CookieBrowser != "" {
		titleArgs = append(titleArgs, "--cookies-from-browser", d.cfg.CookieBrowser)
	}
	titleArgs = append(titleArgs, args...)
	titleCmd := exec.Command(ytDlpCmd, titleArgs...)
	titleOutput, err := titleCmd.CombinedOutput()
	if err != nil {
		// Include stderr output in error message for better debugging
		if len(titleOutput) > 0 {
			errMsg := strings.TrimSpace(string(titleOutput))

			// Provide helpful hints for common errors
			if strings.Contains(errMsg, "Unsupported URL") {
				return "", "", fmt.Errorf("Invalid or unsupported URL. Please check the URL and try again")
			}
			if strings.Contains(errMsg, "Video unavailable") {
				return "", "", fmt.Errorf("Video is unavailable (may be private, deleted, or region-locked)")
			}
			if strings.Contains(errMsg, "Sign in") || strings.Contains(errMsg, "Age-restricted") {
				if d.cfg.CookieBrowser != "" {
					return "", "", fmt.Errorf("Age-restricted video. Please make sure you are logged into YouTube in %s browser", d.cfg.CookieBrowser)
				}
				return "", "", fmt.Errorf("Age-restricted video. Browser cookies will be requested")
			}
			if strings.Contains(errMsg, "HTTP Error 429") {
				return "", "", fmt.Errorf("Rate limited by YouTube. Please try again later")
			}
			if strings.Contains(errMsg, "Requested format is not available") {
				return "", "", fmt.Errorf("Video has no downloadable formats available. This may be due to regional restrictions, DRM protection, or YouTube's anti-bot measures. Try updating yt-dlp: pip install -U yt-dlp")
			}

			// Limit error message length
			if len(errMsg) > 300 {
				errMsg = errMsg[:300] + "..."
			}
			return "", "", fmt.Errorf("%s", errMsg)
		}
		return "", "", fmt.Errorf("Failed to execute yt-dlp: %v", err)
	}

	title := strings.TrimSpace(string(titleOutput))
	if title == "" {
		return "", "", errors.New("no title found")
	}

	// Check if it's a playlist by trying to get playlist info
	playlistArgs := []string{"--flat-playlist", "--print", "playlist,playlist_title,playlist_count", "--no-warnings"}
	if d.cfg.CookieBrowser != "" {
		playlistArgs = append(playlistArgs, "--cookies-from-browser", d.cfg.CookieBrowser)
	}
	playlistArgs = append(playlistArgs, args...)
	playlistCmd := exec.Command(ytDlpCmd, playlistArgs...)
	playlistOutput, _ := playlistCmd.Output()

	playlistData := strings.TrimSpace(string(playlistOutput))
	var playlist, playlistTitle, playlistCount string

	if playlistData != "" && playlistData != "NA" {
		parts := strings.Split(playlistData, ",")
		if len(parts) >= 3 {
			playlist = parts[0]
			playlistTitle = parts[1]
			playlistCount = parts[2]
		} else {
			playlist = "NA"
			playlistTitle = "NA"
			playlistCount = "1"
		}
	} else {
		playlist = "NA"
		playlistTitle = "NA"
		playlistCount = "1"
	}

	playlistInfo := fmt.Sprintf("%s&%s&%s", playlist, playlistTitle, playlistCount)
	return playlistInfo, title, nil
}

// Predicts the output filename
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

// Fetches available formats for a URL
func (d *YTDLPDownloader) GetFormats(url string) ([]Format, error) {
	ytDlpCmd := "yt-dlp"
	if runtime.GOOS == "windows" {
		ytDlpCmd = "yt-dlp.exe"
	}
	cmdArgs := []string{
		"--list-formats",
		"--no-warnings",
		"--extractor-retries", "2",
	}
	if d.cfg.CookieBrowser != "" {
		cmdArgs = append(cmdArgs, "--cookies-from-browser", d.cfg.CookieBrowser)
	}
	cmdArgs = append(cmdArgs, url)
	cmd := exec.Command(ytDlpCmd, cmdArgs...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		// Include stderr output in error message for better debugging
		if len(output) > 0 {
			errMsg := strings.TrimSpace(string(output))
			// Limit error message length
			if len(errMsg) > 200 {
				errMsg = errMsg[:200] + "..."
			}
			return nil, fmt.Errorf("%s", errMsg)
		}
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
			fileSize := ""
			for _, field := range fields {
				if strings.Contains(field, "x") && !isAudio {
					parts := strings.Split(field, "x")
					if len(parts) >= 2 {
						if res, err := strconv.Atoi(parts[1]); err == nil {
							height = res
						}
					}
				}
				if strings.Contains(field, "mp4") || strings.Contains(field, "webm") || strings.Contains(field, "m4a") || strings.Contains(field, "mp3") {
					ext = field
				}
				if strings.Contains(field, "http") || strings.Contains(field, "m3u8") {
					protocol = field
				}
				// Parse file size
				if strings.Contains(field, "iB") || strings.Contains(field, "B") {
					if len(field) > 2 && (field[len(field)-2:] == "iB" || field[len(field)-1:] == "B") {
						// Check if it's a valid size (starts with number)
						if len(field) > 0 && (field[0] >= '0' && field[0] <= '9') {
							fileSize = field
						}
					}
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
					FileSize: fileSize,
				})
			}
		}
	}
	// Deduplicate and filter formats - keep only the best format for each resolution
	uniqueFormats := make(map[int]Format) // map[height]bestFormat

	for _, f := range formats {
		if f.IsAudio {
			continue // Skip audio formats in video selection
		}

		existing, exists := uniqueFormats[f.Height]
		if !exists {
			uniqueFormats[f.Height] = f
			continue
		}

		// Prioritize: mp4 > webm, http > m3u8
		shouldReplace := false

		// Prefer mp4 over webm
		if f.Ext == "mp4" && existing.Ext != "mp4" {
			shouldReplace = true
		} else if f.Ext == existing.Ext {
			// Same extension, prefer http over m3u8
			if (f.Protocol == "http" || f.Protocol == "") && existing.Protocol != "http" && existing.Protocol != "" {
				shouldReplace = true
			}
		}

		if shouldReplace {
			uniqueFormats[f.Height] = f
		}
	}

	// Convert map to sorted slice (highest resolution first)
	sortedFormats := make([]Format, 0, len(uniqueFormats))
	for _, f := range uniqueFormats {
		sortedFormats = append(sortedFormats, f)
	}

	// Sort by height descending
	for i := 0; i < len(sortedFormats)-1; i++ {
		for j := i + 1; j < len(sortedFormats); j++ {
			if sortedFormats[i].Height < sortedFormats[j].Height {
				sortedFormats[i], sortedFormats[j] = sortedFormats[j], sortedFormats[i]
			}
		}
	}

	return sortedFormats, nil
}

// Executes the download process with retries and fallback
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
			"--concurrent-fragments", "32",
			"--buffer-size", "64K",
			"--http-chunk-size", "10M",
			"--no-warnings",
			"--progress",
			"--newline",
			"--extractor-retries", "2",
			"--fragment-retries", "3",
			"--output", tempDir + "/" + d.cfg.OutputTemplate,
		}
		if d.cfg.CookieBrowser != "" {
			cmdArgs = append(cmdArgs, "--cookies-from-browser", d.cfg.CookieBrowser)
		}
		if d.cfg.IsAudioOnly {
			cmdArgs = append(cmdArgs, "--extract-audio", "--audio-format", d.cfg.AudioFormat)
		} else if d.cfg.Resolution != "" {
			cmdArgs = append(cmdArgs, "--format", d.cfg.Resolution+"+bestaudio/best")
		} else {
			cmdArgs = append(cmdArgs, "--format", "bestvideo+bestaudio/best")
		}
		cmdArgs = append(cmdArgs, args...)

		if d.cfg.UseAria2c {
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
					"--no-warnings",
					"--progress",
					"--newline",
					"--output", tempDir + "/" + d.cfg.OutputTemplate,
				}
				if d.cfg.CookieBrowser != "" {
					fallbackArgs = append(fallbackArgs, "--cookies-from-browser", d.cfg.CookieBrowser)
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

// Splits a string into lines and trims whitespace
func splitLines(s string) []string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	for i, line := range lines {
		lines[i] = strings.TrimSpace(line)
	}
	return lines
}
