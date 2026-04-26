package yaria

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
	"strings"

	"yaria/internal/yaria/config"
	"yaria/internal/yaria/daemon"
	"yaria/internal/yaria/downloader"
	"yaria/internal/yaria/logger"
	"yaria/internal/yaria/tui"
	"yaria/internal/yaria/utils"

	"github.com/google/go-github/v62/github"
)

// Run launches Yaria with the given command-line arguments.
// Pass nil or empty slice for interactive TUI mode.
func Run(args []string) {
	// Handle daemon subcommand
	if len(args) > 0 && args[0] == "daemon" {
		if err := daemon.RunDaemon(); err != nil {
			fmt.Fprintf(os.Stderr, "daemon error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	cfg := config.New()
	log := logger.NewConsoleLogger()

	// Initialize dependencies directory
	exePath, err := os.Executable()
	if err != nil {
		exePath, _ = os.Getwd()
	}
	depsDir := filepath.Join(filepath.Dir(exePath), "dependencies")
	if err := os.MkdirAll(depsDir, 0o755); err != nil {
		log.Error("Error: Failed to create dependencies directory: %v", err)
		os.Exit(1)
	}

	// Setup yt-dlp
	ytDlpBinary := "yt-dlp"
	if runtime.GOOS == "windows" {
		ytDlpBinary = "yt-dlp.exe"
	}
	ytDlpPath := filepath.Join(depsDir, ytDlpBinary)
	if _, err := exec.LookPath(ytDlpBinary); err != nil {
		if _, err := os.Stat(ytDlpPath); err != nil {
			// yt-dlp not installed -- auto-download it
			fmt.Fprintln(os.Stderr, "yt-dlp not found. Downloading automatically...")
			if downloadErr := autoDownloadYtDlp(ytDlpBinary, ytDlpPath); downloadErr != nil {
				fmt.Fprintln(os.Stderr, "")
				fmt.Fprintln(os.Stderr, "Failed to download yt-dlp:", downloadErr)
				fmt.Fprintln(os.Stderr, "")
				fmt.Fprintln(os.Stderr, "Please install yt-dlp manually:")
				switch runtime.GOOS {
				case "linux":
					distro := utils.DetectDistro()
					switch distro {
					case "arch", "manjaro", "endeavouros":
						fmt.Fprintln(os.Stderr, "  sudo pacman -S yt-dlp")
					case "fedora":
						fmt.Fprintln(os.Stderr, "  sudo dnf install yt-dlp")
					default:
						fmt.Fprintln(os.Stderr, "  pip install yt-dlp")
						fmt.Fprintln(os.Stderr, "  or: sudo apt install yt-dlp")
					}
				case "darwin":
					fmt.Fprintln(os.Stderr, "  brew install yt-dlp")
				case "windows":
					fmt.Fprintln(os.Stderr, "  winget install yt-dlp")
					fmt.Fprintln(os.Stderr, "  or: pip install yt-dlp")
				default:
					fmt.Fprintln(os.Stderr, "  pip install yt-dlp")
				}
				os.Exit(1)
			}
			fmt.Fprintln(os.Stderr, "yt-dlp installed successfully")
		}
	}

	// Setup aria2
	aria2Binary := "aria2c"
	if runtime.GOOS == "windows" {
		aria2Binary = "aria2c.exe"
	}
	aria2Path := filepath.Join(depsDir, aria2Binary)
	if _, err := exec.LookPath(aria2Binary); err != nil {
		if _, err := os.Stat(aria2Path); err == nil {
			cfg.UseAria2c = true
		} else if runtime.GOOS == "windows" {
			fmt.Fprintln(os.Stderr, "aria2 not found. Downloading...")
			if err := downloadAria2Windows(depsDir, aria2Path); err != nil {
				cfg.UseAria2c = false
				fmt.Fprintf(os.Stderr, "aria2 download failed. Downloads will be slower. Install: %s\n", utils.Aria2InstallCmd())
			} else {
				cfg.UseAria2c = true
			}
		} else {
			cfg.UseAria2c = false
			fmt.Fprintf(os.Stderr, "aria2 not found, downloads will be slower. Install: %s\n", utils.Aria2InstallCmd())
		}
	} else {
		cfg.UseAria2c = true
	}

	// Update PATH
	currentPath := os.Getenv("PATH")
	newPath := depsDir + string(os.PathListSeparator) + currentPath
	if err := os.Setenv("PATH", newPath); err != nil {
		log.Error("Error: Failed to update PATH: %v", err)
		os.Exit(1)
	}

	// Initialize downloader
	dl, err := downloader.New(cfg)
	if err != nil {
		log.Error("Error: %v", err)
		os.Exit(1)
	}

	originalDir, err := os.Getwd()
	if err != nil {
		log.Error("Error: Failed to get current directory: %v", err)
		os.Exit(1)
	}

	// Check if first argument is a magnet link (torrent streaming - CLI only)
	if len(args) > 0 && strings.HasPrefix(args[0], "magnet:") {
		log.Info("Detected magnet link - streaming torrent...")
		if err := dl.StreamTorrent(args[0]); err != nil {
			log.Error("Error: Failed to stream torrent: %v", err)
			os.Exit(1)
		}
		return
	}

	// TUI MODE - single run, downloads go to background daemon
	if len(args) == 0 {
		tuiInstance := tui.New(cfg, log)
		tuiInstance.SetDownloader(dl)
		if err := tuiInstance.Run("", ""); err != nil {
			log.Error("Error: Failed to run TUI: %v", err)
			os.Exit(1)
		}
		return
	}

	// CLI MODE - fetch metadata and download directly
	url := args[0]
	// Clean shell-escaped URLs
	url = strings.ReplaceAll(url, "\\?", "?")
	url = strings.ReplaceAll(url, "\\=", "=")
	url = strings.ReplaceAll(url, "\\&", "&")
	url = strings.ReplaceAll(url, "\\#", "#")
	args[0] = url
	playlistInfo, videoTitle, err := dl.GetMetadata(args)
	if err != nil {
		log.Error("Error: Failed to fetch metadata: %v", err)
		os.Exit(1)
	}

	// Determine playlist or single video
	parts := utils.SplitN(playlistInfo, "&", 3)
	if len(parts) < 3 {
		log.Error("Error: Invalid metadata format")
		os.Exit(1)
	}
	isPlaylist := parts[0]
	playlistTitle := parts[1]
	playlistCountStr := parts[2]

	isSingleVideo := isPlaylist == "NA" || utils.MustParseInt(playlistCountStr) <= 1

	// Generate final name and check duplicates
	var finalName string
	if isSingleVideo {
		finalName = utils.SanitizeFilename(videoTitle)
		if finalName == "" {
			finalName = utils.GenerateTempDirName("Video")
		}
		videoFileName := finalName + ".mp4"
		destPath := filepath.Join(originalDir, videoFileName)
		if utils.FileExists(destPath) {
			log.Warn("Video already exists: %s, skipping download", videoFileName)
			return
		}
	} else {
		finalName = utils.SanitizeFilename(playlistTitle)
		if finalName == "" {
			finalName = utils.GenerateTempDirName("Playlist")
		}
	}

	// Create unique temp directory
	tempDir, err := utils.CreateUniqueTempDir(finalName)
	if err != nil {
		log.Error("Failed to create directory: %s: %v", tempDir, err)
		os.Exit(1)
	}
	defer func() {
		if isSingleVideo && utils.FileExists(tempDir) {
			_ = os.RemoveAll(tempDir)
		}
	}()

	// Download (CLI mode only)
	log.Info("Starting download...")
	fmt.Println()
	success, err := dl.Download(args, tempDir)
	if err != nil {
		log.Error("Download failed: %v", err)
		_ = os.RemoveAll(tempDir)
		os.Exit(1)
	}
	if !success {
		log.Error("All download attempts failed")
		_ = os.RemoveAll(tempDir)
		os.Exit(1)
	}

	// Move single video
	if isSingleVideo {
		videoFile, err := utils.FindVideoFile(tempDir)
		if err != nil {
			log.Warn("Warning: No video file found in %s: %v", tempDir, err)
			_ = os.RemoveAll(tempDir)
		} else {
			dest := filepath.Join(originalDir, filepath.Base(videoFile))
			if utils.FileExists(dest) {
				log.Warn("Warning: Video already exists in destination: %s, keeping temporary files", filepath.Base(dest))
			} else if err := utils.MoveFile(videoFile, dest); err != nil {
				log.Warn("Warning: Failed to move %s (error: %v)", filepath.Base(videoFile), err)
			} else {
				log.Info("Moved: %s", filepath.Base(videoFile))
				_ = os.RemoveAll(tempDir)
			}
		}
	} else {
		log.Info("Playlist download complete. Files in: %s", tempDir)
	}

	_ = url
}

// autoDownloadYtDlp downloads yt-dlp from GitHub releases.
func autoDownloadYtDlp(binaryName, destPath string) error {
	ghClient := github.NewClient(nil)
	release, _, err := ghClient.Repositories.GetLatestRelease(context.Background(), "yt-dlp", "yt-dlp")
	if err != nil {
		return fmt.Errorf("cannot reach GitHub: %w", err)
	}
	var downloadURL string
	for _, asset := range release.Assets {
		if asset.GetName() == binaryName {
			downloadURL = asset.GetBrowserDownloadURL()
			break
		}
	}
	if downloadURL == "" {
		return errors.New("no matching binary found in latest release")
	}
	resp, err := http.Get(downloadURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %s", resp.Status)
	}
	out, err := os.Create(destPath)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, resp.Body)
	out.Close()
	if err != nil {
		os.Remove(destPath) // clean up partial file
		return err
	}
	if runtime.GOOS != "windows" {
		os.Chmod(destPath, 0o755)
	}
	return nil
}

// downloadAria2Windows downloads the pre-built aria2 binary from GitHub releases.
// Only works on Windows since aria2 only publishes Windows and Android pre-built binaries.
func downloadAria2Windows(depsDir, dest string) error {
	client := github.NewClient(nil)
	release, _, err := client.Repositories.GetLatestRelease(context.Background(), "aria2", "aria2")
	if err != nil {
		return fmt.Errorf("failed to fetch aria2 release: %w", err)
	}

	// Find the win-64bit zip asset
	var downloadURL string
	for _, asset := range release.Assets {
		name := strings.ToLower(asset.GetName())
		if strings.Contains(name, "win") && strings.Contains(name, "64bit") && strings.HasSuffix(name, ".zip") {
			downloadURL = asset.GetBrowserDownloadURL()
			break
		}
	}
	if downloadURL == "" {
		return errors.New("no Windows 64-bit aria2 binary found in GitHub releases")
	}

	// Download the zip
	zipPath := filepath.Join(depsDir, "aria2.zip")
	defer os.Remove(zipPath)

	resp, err := http.Get(downloadURL)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed: HTTP %s", resp.Status)
	}

	out, err := os.Create(zipPath)
	if err != nil {
		return err
	}
	if _, err = io.Copy(out, resp.Body); err != nil {
		out.Close()
		return err
	}
	out.Close()

	// Extract aria2c.exe from the zip
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("failed to open zip: %w", err)
	}
	defer r.Close()

	for _, f := range r.File {
		name := filepath.Base(f.Name)
		if strings.EqualFold(name, "aria2c.exe") {
			rc, err := f.Open()
			if err != nil {
				return err
			}
			out, err := os.Create(dest)
			if err != nil {
				rc.Close()
				return err
			}
			if _, err = io.Copy(out, rc); err != nil {
				out.Close()
				rc.Close()
				return err
			}
			out.Close()
			rc.Close()
			return nil
		}
	}

	return errors.New("aria2c.exe not found in zip archive")
}
