package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"yaria/config"
	"yaria/downloader"
	"yaria/logger"
	"yaria/tui"
	"yaria/utils"

	"github.com/google/go-github/v62/github"
)

func main() {
	flag.Usage = func() {
		log := logger.NewConsoleLogger()
		log.Error("Error: No URL provided")
		log.Info("Usage: yaria <URL>")
	}
	flag.Parse()

	args := flag.Args()
	cfg := config.New()
	log := logger.NewConsoleLogger()
	tuiInstance := tui.New(cfg, log)

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
			log.Info("⬇️ Downloading yt-dlp from GitHub...")
			client := github.NewClient(nil)
			release, _, err := client.Repositories.GetLatestRelease(context.Background(), "yt-dlp", "yt-dlp")
			if err != nil {
				log.Error("Error: Failed to fetch yt-dlp release: %v", err)
				os.Exit(1)
			}
			var downloadURL string
			for _, asset := range release.Assets {
				if asset.GetName() == ytDlpBinary {
					downloadURL = asset.GetBrowserDownloadURL()
					break
				}
			}
			if downloadURL == "" {
				log.Error("Error: No suitable yt-dlp binary found")
				os.Exit(1)
			}
			resp, err := http.Get(downloadURL)
			if err != nil {
				log.Error("Error: Failed to download yt-dlp: %v", err)
				os.Exit(1)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				log.Error("Error: Failed to download yt-dlp: HTTP status %s", resp.Status)
				os.Exit(1)
			}
			out, err := os.Create(ytDlpPath)
			if err != nil {
				log.Error("Error: Failed to create yt-dlp binary: %v", err)
				os.Exit(1)
			}
			_, err = io.Copy(out, resp.Body)
			out.Close()
			if err != nil {
				log.Error("Error: Failed to save yt-dlp: %v", err)
				os.Exit(1)
			}
			if runtime.GOOS != "windows" {
				if err := os.Chmod(ytDlpPath, 0o755); err != nil {
					log.Error("Error: Failed to set permissions for yt-dlp: %v", err)
					os.Exit(1)
				}
			}
			log.Info("Downloaded yt-dlp to %s", ytDlpPath)
		} else {
			log.Info("Found yt-dlp in dependencies at %s", ytDlpPath)
		}
	} else {
		log.Info("Found yt-dlp in system PATH")
	}

	// Setup aria2
	aria2Binary := "aria2c"
	if runtime.GOOS == "windows" {
		aria2Binary = "aria2c.exe"
	}
	aria2Path := filepath.Join(depsDir, aria2Binary)
	if _, err := exec.LookPath(aria2Binary); err != nil {
		if _, err := os.Stat(aria2Path); err != nil {
			log.Info("Downloading aria2 from GitHub...")
			client := github.NewClient(nil)
			release, _, err := client.Repositories.GetLatestRelease(context.Background(), "aria2", "aria2")
			if err != nil {
				log.Warn("Warning: Failed to fetch aria2 release: %v", err)
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
					log.Warn("Warning: No suitable aria2 binary found")
					cfg.UseAria2c = false
				} else {
					resp, err := http.Get(downloadURL)
					if err != nil {
						log.Warn("Warning: Failed to download aria2: %v", err)
						cfg.UseAria2c = false
					} else {
						defer resp.Body.Close()
						if resp.StatusCode != http.StatusOK {
							log.Warn("Warning: Failed to download aria2: HTTP status %s", resp.Status)
							cfg.UseAria2c = false
						} else {
							out, err := os.Create(aria2Path)
							if err != nil {
								log.Warn("Warning: Failed to create aria2 binary: %v", err)
								cfg.UseAria2c = false
							} else {
								_, err = io.Copy(out, resp.Body)
								out.Close()
								if err != nil {
									log.Warn("Warning: Failed to save aria2: %v", err)
									cfg.UseAria2c = false
								} else if runtime.GOOS != "windows" {
									if err := os.Chmod(aria2Path, 0o755); err != nil {
										log.Warn("Warning: Failed to set permissions for aria2: %v", err)
										cfg.UseAria2c = false
									} else {
										log.Info("Downloaded aria2 to %s", aria2Path)
										cfg.UseAria2c = true
									}
								} else {
									log.Info("Downloaded aria2 to %s", aria2Path)
									cfg.UseAria2c = true
								}
							}
						}
					}
				}
			}
		} else {
			log.Info("Found aria2 in dependencies at %s", aria2Path)
			cfg.UseAria2c = true
		}
	} else {
		log.Info("Found aria2 in system PATH")
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
	tuiInstance.SetDownloader(dl)

	originalDir, err := os.Getwd()
	if err != nil {
		log.Error("Error: Failed to get current directory: %v", err)
		os.Exit(1)
	}

	var url string

	var playlistInfo, videoTitle string

	// SINGLE TUI RUN - Run TUI twice: first for selection, then for download
	if len(args) == 0 {
		// First run: Get URL, format, and resolution
		if err := tuiInstance.Run("", ""); err != nil {
			log.Error("Error: Failed to run TUI: %v", err)
			os.Exit(1)
		}
		// Check if TUI exited with an error message or user cancelled
		if tuiInstance.URL == "" {
			os.Exit(0)
		}
		if !tuiInstance.Confirmed {
			log.Info("Download cancelled")
			os.Exit(0)
		}
		url = tuiInstance.URL
		args = []string{url}
		// Use metadata already fetched by TUI
		playlistInfo = tuiInstance.PlaylistInfo
		videoTitle = tuiInstance.Title
		// If playlistInfo is empty, TUI exited with error
		if playlistInfo == "" {
			os.Exit(0)
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

		// Generate final name
		var finalName string
		if isSingleVideo {
			finalName = utils.SanitizeFilename(videoTitle)
			if finalName == "" {
				finalName = utils.GenerateTempDirName("Video")
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

		// Set download parameters in TUI
		tuiInstance.TempDir = tempDir
		tuiInstance.Args = args

		// Second run: Show download progress in TUI (skip confirmation)
		if err := tuiInstance.RunDownloadOnly(); err != nil {
			log.Error("Error: Failed to run TUI download: %v", err)
			os.Exit(1)
		}

		// TUI handled everything including download
		os.Exit(0)
	}

	// CLI MODE - fetch metadata and download
	url = args[0]
	playlistInfo, videoTitle, err = dl.GetMetadata(args)
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
			os.Exit(0)
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
	fmt.Println() // Add blank line for separation
	success, err := dl.Download(args, tempDir)
	if err != nil {
		log.Error("❌ Download failed: %v", err)
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
}
