package main

import (
	"flag"
	"os"
	"path/filepath"
	"yaria/config"
	"yaria/downloader"
	"yaria/logger"
	"yaria/tui"
	"yaria/utils"
)

func main() {
	flag.Usage = func() {
		log := logger.NewConsoleLogger()
		log.Info("❌ Error: No URL provided")
		log.Info("ℹ️ Usage: yaria <URL>")
	}
	flag.Parse()

	args := flag.Args()
	cfg := config.New()
	log := logger.NewConsoleLogger()
	tui := tui.New(cfg, log)

	// Check dependencies
	dl, err := downloader.New(cfg)
	if err != nil {
		log.Error("❌ Error: %v", err)
		os.Exit(1)
	}
	tui.SetDownloader(dl)

	originalDir, err := os.Getwd()
	if err != nil {
		log.Error("❌ Error: Failed to get current directory: %v", err)
		os.Exit(1)
	}

	var url string
	var isSingleVideo bool
	var tempDir string
	var videoTitle string

	if len(args) == 0 {
		// Run TUI to get URL
		if err := tui.Run("", ""); err != nil {
			log.Error("❌ Error: Failed to run TUI: %v", err)
			os.Exit(1)
		}
		if !tui.Confirmed || tui.URL == "" {
			log.Info("ℹ️ No URL provided or download cancelled")
			os.Exit(0)
		}
		url = tui.URL
		args = []string{url}
	} else {
		url = args[0]
	}

	// Fetch playlist info and title in one command
	playlistInfo, title, err := dl.GetMetadata(args)
	if err != nil {
		log.Error("❌ Error: Failed to fetch metadata: %v", err)
		os.Exit(1)
	}
	videoTitle = title

	// Playlist or single video handling
	parts := utils.SplitN(playlistInfo, "&", 3)
	isPlaylist := parts[0]
	playlistTitle := parts[1]
	playlistCountStr := parts[2]

	if isPlaylist != "NA" {
		playlistCount, err := utils.ParseInt(playlistCountStr)
		if err == nil && playlistCount > 1 {
			tempDir = utils.SanitizeFilename(playlistTitle)
			if tempDir == "" {
				tempDir = utils.GenerateTempDirName("Playlist")
			}
		} else {
			isSingleVideo = true
		}
	} else {
		isSingleVideo = true
	}

	if isSingleVideo {
		tempDir = utils.SanitizeFilename(videoTitle)
		if tempDir == "" {
			tempDir = utils.GenerateTempDirName("Video")
		}

		// Check if file already exists in originalDir
		filename, err := dl.GetOutputFilename(args, tempDir)
		if err == nil {
			destPath := filepath.Join(originalDir, filepath.Base(filename))
			if utils.FileExists(destPath) {
				log.Info("ℹ️ File already downloaded: %s", filepath.Base(destPath))
				os.Exit(0)
			}
		} else {
			log.Warn("⚠️ Warning: Failed to get output filename: %v", err)
		}

		// Run TUI for format, resolution, and confirmation
		if err := tui.Run(url, videoTitle); err != nil {
			log.Error("❌ Error: Failed to run TUI: %v", err)
			os.Exit(1)
		}
		if !tui.Confirmed {
			log.Info("ℹ️ Download cancelled by user")
			os.Exit(0)
		}
	}

	// Ensure unique temporary directory
	tempDir, err = utils.CreateUniqueTempDir(tempDir)
	if err != nil {
		log.Error("❌ Failed to create directory: %s: %v", tempDir, err)
		os.Exit(1)
	}
	defer func() {
		if isSingleVideo && utils.FileExists(tempDir) {
			_ = os.RemoveAll(tempDir)
		}
	}()

	// Download
	success, err := dl.Download(args, tempDir)
	if err != nil {
		log.Error("❌ Download failed: %v", err)
		_ = os.RemoveAll(tempDir)
		os.Exit(1)
	}
	if !success {
		log.Error("❌ All download attempts failed")
		_ = os.RemoveAll(tempDir)
		os.Exit(1)
	}

	// Move single video file
	if isSingleVideo {
		videoFile, err := utils.FindVideoFile(tempDir)
		if err != nil {
			log.Warn("⚠️ Warning: No video file found in %s: %v", tempDir, err)
			_ = os.RemoveAll(tempDir)
		} else {
			dest := filepath.Join(originalDir, filepath.Base(videoFile))
			if utils.FileExists(dest) {
				log.Warn("⚠️ Warning: File already exists in destination: %s, keeping temporary files", filepath.Base(dest))
			} else if err := utils.MoveFile(videoFile, dest); err != nil {
				log.Warn("⚠️ Warning: Failed to move %s (error: %v)", filepath.Base(videoFile), err)
			} else {
				log.Info("Moved: %s", filepath.Base(videoFile))
				_ = os.RemoveAll(tempDir)
			}
		}
	} else {
		log.Info("ℹ️ Playlist download complete. Files in: %s", tempDir)
	}

	log.Info("Download completed!")
}
