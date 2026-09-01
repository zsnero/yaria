package downloader

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"encoding/json"
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

	"yaria/internal/yaria/config"
	"yaria/internal/yaria/cookies"
	"yaria/internal/yaria/procexec"

	"github.com/google/go-github/v62/github"
)

// Current User-Agent string - kept up to date
const userAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"

// Interface for yt-dlp operations
type Downloader interface {
	GetMetadata(args []string) (string, string, error)
	GetOutputFilename(args []string, tempDir string) (string, error)
	GetFormats(url string) ([]Format, error)
	GetVideoInfo(url string) (*VideoInfo, error)
	GetThumbnail(args []string, tempDir string) (string, error)
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

// VideoInfo is metadata + formats from a single yt-dlp -J call.
type VideoInfo struct {
	Title     string
	Uploader  string
	Duration  int
	Thumbnail string
	Formats   []Format
}

// Implements the Downloader interface
type YTDLPDownloader struct {
	cfg        *config.Config
	ffmpegPath string // directory or binary path for --ffmpeg-location
	depsDir    string
}

// safeWriter never returns nil — fmt.Fprintf panics on a nil io.Writer.
func safeWriter(w io.Writer) io.Writer {
	if w == nil {
		return io.Discard
	}
	return w
}

func New(cfg *config.Config) (*YTDLPDownloader, error) {
	if cfg == nil {
		return nil, errors.New("config is nil")
	}
	// Pin non-nil writers for the whole setup path. Callers (and concurrent
	// InitDeps races) must never leave Stdout/Stderr nil mid-flight.
	cfg.Stdout = safeWriter(cfg.Stdout)
	cfg.Stderr = safeWriter(cfg.Stderr)
	logw := cfg.Stderr

	// Create dependencies folder in a persistent location
	var depsDir string

	// Try to use user's home directory for dependencies
	homeDir, err := os.UserHomeDir()
	if err == nil {
		// Use ~/.yaria/dependencies for persistent storage
		depsDir = filepath.Join(homeDir, ".yaria", "dependencies")
	} else {
		// Fallback to current working directory
		cwd, _ := os.Getwd()
		depsDir = filepath.Join(cwd, "dependencies")
	}

	if err := os.MkdirAll(depsDir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create dependencies directory: %v", err)
	}

	// Check if version check is needed (every 7 days)
	lastCheckFile := filepath.Join(depsDir, "last_check")
	shouldCheckVersions := true
	if info, err := os.Stat(lastCheckFile); err == nil {
		if time.Since(info.ModTime()) < 7*24*time.Hour {
			shouldCheckVersions = false
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
			procexec.HideConsole(cmd)
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

	// aria2 is required — auto-download a portable build on first launch
	if _, err := EnsureAria2(depsDir, logw); err != nil {
		return nil, fmt.Errorf("aria2 setup failed: %w", err)
	}
	cfg.UseAria2c = true

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
	}

	// Check and download yazi for file explorer integration (optional)
	yaziBinary := "yazi"
	if runtime.GOOS == "windows" {
		yaziBinary = "yazi.exe"
	}
	yaziPath := filepath.Join(depsDir, yaziBinary)
	if _, err := exec.LookPath(yaziBinary); err != nil {
		if _, err := os.Stat(yaziPath); err != nil {
			fmt.Fprintf(cfg.Stderr, "Downloading yazi for file explorer (optional)...\n")
			// Yazi download URLs - using specific version for stability
			var yaziURL string
			switch runtime.GOOS {
			case "linux":
				yaziURL = "https://github.com/sxyazi/yazi/releases/latest/download/yazi-x86_64-unknown-linux-gnu.zip"
			case "darwin":
				if runtime.GOARCH == "arm64" {
					yaziURL = "https://github.com/sxyazi/yazi/releases/latest/download/yazi-aarch64-apple-darwin.zip"
				} else {
					yaziURL = "https://github.com/sxyazi/yazi/releases/latest/download/yazi-x86_64-apple-darwin.zip"
				}
			case "windows":
				yaziURL = "https://github.com/sxyazi/yazi/releases/latest/download/yazi-x86_64-pc-windows-msvc.zip"
			}

			if yaziURL != "" {
				resp, err := http.Get(yaziURL)
				if err == nil {
					defer resp.Body.Close()
					if resp.StatusCode == http.StatusOK {
						zipPath := filepath.Join(depsDir, "yazi.zip")
						zipFile, err := os.Create(zipPath)
						if err == nil {
							_, err = io.Copy(zipFile, resp.Body)
							zipFile.Close()
							if err == nil {
								// Extract yazi binary
								if err := extractYaziFromZip(zipPath, yaziPath); err == nil {
									os.Remove(zipPath)
									if runtime.GOOS != "windows" {
										os.Chmod(yaziPath, 0o755)
									}
									fmt.Fprintf(cfg.Stderr, "Downloaded yazi to %s\n", yaziPath)
								}
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
			fmt.Fprintf(cfg.Stderr, "Warning: Failed to update last_check timestamp: %v\n", err)
		} else {
			f.Close()
		}
	}

	// Install webtorrent-cli for torrent streaming support
	webtorrentBinary := "webtorrent"
	if runtime.GOOS == "windows" {
		webtorrentBinary = "webtorrent.cmd"
	}

	// Check if webtorrent-cli is available
	webtorrentInstalled := false
	if _, err := exec.LookPath("webtorrent"); err == nil {
		webtorrentInstalled = true
	} else {
		// Check in dependencies folder
		webtorrentPath := filepath.Join(depsDir, "bin", webtorrentBinary)
		if _, err := os.Stat(webtorrentPath); err == nil {
			webtorrentInstalled = true
		}
	}

	if !webtorrentInstalled {
		fmt.Fprintf(cfg.Stderr, "Installing webtorrent-cli for torrent streaming...\n")

		// Use npm for installation (deno has issues with Node-API addons)
		if _, err := exec.LookPath("npm"); err == nil {
			fmt.Fprintf(cfg.Stderr, "Installing webtorrent-cli via npm...\n")

			// Install to dependencies folder
			installCmd := exec.Command("npm", "install", "-g", "--prefix", depsDir, "webtorrent-cli")
			procexec.HideConsole(installCmd)
			installCmd.Stdout = cfg.Stderr
			installCmd.Stderr = cfg.Stderr
			err := installCmd.Run()
			if err == nil {
				fmt.Fprintf(cfg.Stderr, "Installed webtorrent-cli successfully\n")
				webtorrentInstalled = true
			} else {
				fmt.Fprintf(cfg.Stderr, "npm install failed: %v\n", err)
			}
		} else {
			fmt.Fprintf(cfg.Stderr, "npm not found, skipping webtorrent-cli installation\n")
		}

		if !webtorrentInstalled {
			fmt.Fprintf(cfg.Stderr, "Warning: webtorrent-cli installation failed. Torrent streaming will not be available.\n")
			fmt.Fprintf(cfg.Stderr, "You can install it manually: npm install -g webtorrent-cli\n")
		}
	}

	// Ensure FFmpeg is available (required to merge video+audio into one file)
	ffmpegDir := ensureFFmpeg(depsDir, cfg)

	// Update PATH to include dependencies folder and bin directory
	currentPath := os.Getenv("PATH")
	binDir := filepath.Join(depsDir, "bin")
	newPath := depsDir + string(os.PathListSeparator) + binDir + string(os.PathListSeparator) + currentPath
	if err := os.Setenv("PATH", newPath); err != nil {
		return nil, fmt.Errorf("failed to update PATH: %v", err)
	}

	// Original dependency checks
	if _, err := exec.LookPath(ytDlpBinary); err != nil {
		return nil, errors.New("yt-dlp not installed")
	}
	aria2Binary := "aria2c"
	if runtime.GOOS == "windows" {
		aria2Binary = "aria2c.exe"
	}
	if _, err := exec.LookPath(aria2Binary); err != nil {
		return nil, errors.New("aria2c not installed")
	}
	cfg.UseAria2c = true
	return &YTDLPDownloader{cfg: cfg, ffmpegPath: ffmpegDir, depsDir: depsDir}, nil
}

// EnsureAria2 finds or downloads a portable aria2c into depsDir.
// aria2 is required for multi-connection downloads — this must not silently skip.
func EnsureAria2(depsDir string, logw io.Writer) (string, error) {
	logw = safeWriter(logw)
	bin := "aria2c"
	if runtime.GOOS == "windows" {
		bin = "aria2c.exe"
	}
	dest := filepath.Join(depsDir, bin)

	// Prefer a working binary already on PATH (system package)
	if p, err := exec.LookPath(bin); err == nil {
		fmt.Fprintf(logw, "Found aria2 on PATH at %s\n", p)
		return p, nil
	}
	// Prefer previously downloaded copy
	if st, err := os.Stat(dest); err == nil && !st.IsDir() {
		cmd := exec.Command(dest, "--version")
		procexec.HideConsole(cmd)
		if out, err := cmd.Output(); err == nil && len(out) > 0 {
			fmt.Fprintf(logw, "Found aria2 in dependencies at %s\n", dest)
			return dest, nil
		}
		// Corrupt/stale binary — re-download
		_ = os.Remove(dest)
	}

	fmt.Fprintf(logw, "Downloading aria2 (required)...\n")
	if err := downloadAria2Binary(depsDir, dest, logw); err != nil {
		return "", err
	}
	if runtime.GOOS != "windows" {
		_ = os.Chmod(dest, 0o755)
	}
	// Verify it runs
	cmd := exec.Command(dest, "--version")
	procexec.HideConsole(cmd)
	if out, err := cmd.Output(); err != nil {
		return "", fmt.Errorf("aria2 downloaded but failed to run: %w", err)
	} else {
		line := strings.TrimSpace(string(out))
		if i := strings.Index(line, "\n"); i > 0 {
			line = line[:i]
		}
		fmt.Fprintf(logw, "Downloaded aria2 to %s (%s)\n", dest, line)
	}
	return dest, nil
}

// downloadAria2Binary fetches a portable aria2c for the current OS/arch.
// Sources (in order):
//  1. abcfy2/aria2-static-build — static musl/mingw builds (Linux + Windows)
//  2. Official aria2/aria2 Windows zip
//  3. Homebrew bottles for macOS (and Linux fallback)
func downloadAria2Binary(depsDir, dest string, logw io.Writer) error {
	var errs []string

	if url, asset := aria2StaticBuildURL(); url != "" {
		fmt.Fprintf(logw, "Fetching aria2 static build (%s)...\n", asset)
		if err := downloadAndExtractAria2(url, depsDir, dest); err == nil {
			return nil
		} else {
			errs = append(errs, fmt.Sprintf("static build: %v", err))
			fmt.Fprintf(logw, "Warning: static build failed: %v\n", err)
		}
	}

	if runtime.GOOS == "windows" {
		fmt.Fprintf(logw, "Fetching official aria2 Windows release...\n")
		if err := downloadAria2OfficialWindows(depsDir, dest); err == nil {
			return nil
		} else {
			errs = append(errs, fmt.Sprintf("official windows: %v", err))
		}
	}

	if runtime.GOOS == "darwin" || runtime.GOOS == "linux" {
		fmt.Fprintf(logw, "Fetching aria2 via Homebrew bottle...\n")
		if err := downloadAria2BrewBottle(depsDir, dest); err == nil {
			return nil
		} else {
			errs = append(errs, fmt.Sprintf("brew bottle: %v", err))
		}
	}

	if len(errs) == 0 {
		return fmt.Errorf("no aria2 download source for %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	return fmt.Errorf("all aria2 download sources failed: %s", strings.Join(errs, "; "))
}

// aria2StaticBuildURL returns a direct zip URL from abcfy2/aria2-static-build.
func aria2StaticBuildURL() (url, asset string) {
	// Prefer latest release; fall back to known-good tag.
	const repoOwner, repoName = "abcfy2", "aria2-static-build"
	want := ""
	switch {
	case runtime.GOOS == "linux" && runtime.GOARCH == "amd64":
		want = "aria2-x86_64-linux-musl_static.zip"
	case runtime.GOOS == "linux" && runtime.GOARCH == "arm64":
		want = "aria2-aarch64-linux-musl_static.zip"
	case runtime.GOOS == "linux" && (runtime.GOARCH == "arm" || runtime.GOARCH == "armv7"):
		want = "aria2-armv7-linux-musleabihf_static.zip"
	case runtime.GOOS == "windows" && runtime.GOARCH == "amd64":
		want = "aria2-x86_64-w64-mingw32_static.zip"
	case runtime.GOOS == "windows" && runtime.GOARCH == "386":
		want = "aria2-i686-w64-mingw32_static.zip"
	default:
		return "", ""
	}

	client := github.NewClient(nil)
	release, _, err := client.Repositories.GetLatestRelease(context.Background(), repoOwner, repoName)
	if err == nil {
		for _, a := range release.Assets {
			if a.GetName() == want {
				return a.GetBrowserDownloadURL(), want
			}
		}
	}
	// Pinned fallback
	return fmt.Sprintf("https://github.com/%s/%s/releases/download/1.37.0/%s", repoOwner, repoName, want), want
}

func downloadAndExtractAria2(url, depsDir, dest string) error {
	tmp := filepath.Join(depsDir, "aria2_download.tmp")
	defer os.Remove(tmp)
	if err := downloadFileHTTP(url, tmp, nil); err != nil {
		return err
	}
	// abcfy2 zips contain a bare aria2c / aria2c.exe
	binBase := filepath.Base(dest)
	if err := extractBinariesFromZip(tmp, map[string]string{binBase: dest}); err != nil {
		// Also try without .exe mismatch / nested paths
		if err2 := extractBinaryNamed(tmp, dest, "aria2c"); err2 != nil {
			return err
		}
	}
	if runtime.GOOS != "windows" {
		_ = os.Chmod(dest, 0o755)
	}
	return nil
}

func downloadAria2OfficialWindows(depsDir, dest string) error {
	client := github.NewClient(nil)
	release, _, err := client.Repositories.GetLatestRelease(context.Background(), "aria2", "aria2")
	if err != nil {
		return err
	}
	var downloadURL string
	for _, asset := range release.Assets {
		n := strings.ToLower(asset.GetName())
		if strings.Contains(n, "win") && strings.Contains(n, "64bit") && strings.HasSuffix(n, ".zip") {
			downloadURL = asset.GetBrowserDownloadURL()
			break
		}
	}
	if downloadURL == "" {
		return errors.New("no Windows 64-bit aria2 asset in official release")
	}
	tmp := filepath.Join(depsDir, "aria2_official.zip")
	defer os.Remove(tmp)
	if err := downloadFileHTTP(downloadURL, tmp, nil); err != nil {
		return err
	}
	return extractBinaryNamed(tmp, dest, "aria2c")
}

// downloadAria2BrewBottle pulls a Homebrew bottle (works for macOS; Linux fallback).
func downloadAria2BrewBottle(depsDir, dest string) error {
	req, err := http.NewRequest(http.MethodGet, "https://formulae.brew.sh/api/formula/aria2.json", nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("brew api HTTP %s", resp.Status)
	}
	var meta struct {
		Bottle struct {
			Stable struct {
				Files map[string]struct {
					URL string `json:"url"`
				} `json:"files"`
			} `json:"stable"`
		} `json:"bottle"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&meta); err != nil {
		return err
	}
	files := meta.Bottle.Stable.Files
	// Prefer keys matching current platform
	candidates := []string{}
	switch {
	case runtime.GOOS == "darwin" && runtime.GOARCH == "arm64":
		candidates = []string{"arm64_tahoe", "arm64_sequoia", "arm64_sonoma", "arm64_ventura", "arm64_monterey"}
	case runtime.GOOS == "darwin" && runtime.GOARCH == "amd64":
		candidates = []string{"sonoma", "ventura", "monterey", "big_sur"}
	case runtime.GOOS == "linux" && runtime.GOARCH == "amd64":
		candidates = []string{"x86_64_linux"}
	case runtime.GOOS == "linux" && runtime.GOARCH == "arm64":
		candidates = []string{"arm64_linux"}
	}
	var bottleURL string
	for _, k := range candidates {
		if f, ok := files[k]; ok && f.URL != "" {
			bottleURL = f.URL
			break
		}
	}
	if bottleURL == "" {
		// any available
		for _, f := range files {
			if f.URL != "" {
				bottleURL = f.URL
				break
			}
		}
	}
	if bottleURL == "" {
		return errors.New("no brew bottle URL for this platform")
	}

	tmp := filepath.Join(depsDir, "aria2_bottle.tar.gz")
	defer os.Remove(tmp)
	req2, err := http.NewRequest(http.MethodGet, bottleURL, nil)
	if err != nil {
		return err
	}
	// Anonymous GHCR pull (Homebrew convention)
	req2.Header.Set("Authorization", "Bearer QQ==")
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		return err
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		return fmt.Errorf("bottle download HTTP %s", resp2.Status)
	}
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, resp2.Body)
	out.Close()
	if err != nil {
		return err
	}
	return extractAria2FromTarGz(tmp, dest)
}

func extractAria2FromTarGz(archivePath, dest string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	want := "aria2c"
	if runtime.GOOS == "windows" {
		want = "aria2c.exe"
	}
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if hdr.FileInfo().IsDir() {
			continue
		}
		base := filepath.Base(hdr.Name)
		if base != want && base != "aria2c" {
			continue
		}
		out, err := os.Create(dest)
		if err != nil {
			return err
		}
		_, err = io.Copy(out, tr)
		out.Close()
		if err != nil {
			return err
		}
		if runtime.GOOS != "windows" {
			_ = os.Chmod(dest, 0o755)
		}
		return nil
	}
	return errors.New("aria2c not found in brew bottle")
}

// extractBinaryNamed extracts the first zip entry whose base name is binBase or binBase.exe.
func extractBinaryNamed(zipPath, dest, binBase string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer r.Close()
	want := []string{binBase, binBase + ".exe", filepath.Base(dest)}
	for _, f := range r.File {
		if f.FileInfo().IsDir() {
			continue
		}
		base := filepath.Base(f.Name)
		match := false
		for _, w := range want {
			if strings.EqualFold(base, w) {
				match = true
				break
			}
		}
		if !match {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.Create(dest)
		if err != nil {
			rc.Close()
			return err
		}
		_, err = io.Copy(out, rc)
		out.Close()
		rc.Close()
		if err != nil {
			return err
		}
		if runtime.GOOS != "windows" {
			_ = os.Chmod(dest, 0o755)
		}
		return nil
	}
	return fmt.Errorf("%s not found in archive", binBase)
}

func downloadFileHTTP(url, dest string, headers map[string]string) error {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %s", resp.Status)
	}
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, resp.Body)
	return err
}

// ensureFFmpeg finds or downloads a static FFmpeg binary into depsDir.
// Returns the directory containing ffmpeg (for yt-dlp --ffmpeg-location).
func ensureFFmpeg(depsDir string, cfg *config.Config) string {
	ffmpegName := "ffmpeg"
	if runtime.GOOS == "windows" {
		ffmpegName = "ffmpeg.exe"
	}
	bundled := filepath.Join(depsDir, ffmpegName)
	if _, err := os.Stat(bundled); err == nil {
		return depsDir
	}
	if p, err := exec.LookPath("ffmpeg"); err == nil {
		return filepath.Dir(p)
	}

	// Download static build
	fmt.Fprintf(cfg.Stderr, "FFmpeg not found — downloading (needed to merge video+audio)...\n")
	var downloadURL string
	switch {
	case runtime.GOOS == "windows" && runtime.GOARCH == "amd64":
		downloadURL = "https://github.com/BtbN/FFmpeg-Builds/releases/download/latest/ffmpeg-master-latest-win64-gpl.zip"
	case runtime.GOOS == "linux" && runtime.GOARCH == "amd64":
		downloadURL = "https://github.com/BtbN/FFmpeg-Builds/releases/download/latest/ffmpeg-master-latest-linux64-gpl.tar.xz"
	case runtime.GOOS == "linux" && runtime.GOARCH == "arm64":
		downloadURL = "https://github.com/BtbN/FFmpeg-Builds/releases/download/latest/ffmpeg-master-latest-linuxarm64-gpl.tar.xz"
	case runtime.GOOS == "darwin" && runtime.GOARCH == "amd64":
		downloadURL = "https://github.com/eugeneware/ffmpeg-static/releases/latest/download/ffmpeg-darwin-x64.gz"
	case runtime.GOOS == "darwin" && runtime.GOARCH == "arm64":
		downloadURL = "https://github.com/eugeneware/ffmpeg-static/releases/latest/download/ffmpeg-darwin-arm64.gz"
	default:
		fmt.Fprintf(cfg.Stderr, "WARNING: no FFmpeg build for %s/%s — downloads may stay as separate video/audio files\n", runtime.GOOS, runtime.GOARCH)
		return ""
	}

	tmpFile := filepath.Join(depsDir, "ffmpeg_download.tmp")
	if err := downloadFile(downloadURL, tmpFile, cfg); err != nil {
		fmt.Fprintf(cfg.Stderr, "WARNING: FFmpeg download failed: %v\n", err)
		return ""
	}
	defer os.Remove(tmpFile)

	ffmpegDest := filepath.Join(depsDir, ffmpegName)
	ffprobeName := "ffprobe"
	if runtime.GOOS == "windows" {
		ffprobeName = "ffprobe.exe"
	}
	ffprobeDest := filepath.Join(depsDir, ffprobeName)

	var extractErr error
	switch {
	case strings.HasSuffix(downloadURL, ".zip"):
		extractErr = extractBinariesFromZip(tmpFile, map[string]string{ffmpegName: ffmpegDest, ffprobeName: ffprobeDest})
	case strings.HasSuffix(downloadURL, ".tar.xz"):
		extractErr = extractBinariesFromTarXz(tmpFile, map[string]string{"ffmpeg": ffmpegDest, "ffprobe": ffprobeDest})
	case strings.HasSuffix(downloadURL, ".gz"):
		extractErr = extractGzipFile(tmpFile, ffmpegDest)
	}
	if extractErr != nil {
		fmt.Fprintf(cfg.Stderr, "WARNING: FFmpeg extract failed: %v\n", extractErr)
		return ""
	}
	os.Chmod(ffmpegDest, 0755)
	if _, err := os.Stat(ffprobeDest); err == nil {
		os.Chmod(ffprobeDest, 0755)
	}
	fmt.Fprintf(cfg.Stderr, "FFmpeg installed to %s\n", depsDir)
	return depsDir
}

func downloadFile(url, dest string, cfg *config.Config) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, resp.Body)
	return err
}

func extractBinariesFromZip(zipPath string, want map[string]string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer r.Close()
	found := map[string]bool{}
	for _, f := range r.File {
		if f.FileInfo().IsDir() {
			continue
		}
		base := filepath.Base(f.Name)
		dest, ok := want[base]
		if !ok || found[base] {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.Create(dest)
		if err != nil {
			rc.Close()
			return err
		}
		_, err = io.Copy(out, rc)
		out.Close()
		rc.Close()
		if err != nil {
			return err
		}
		found[base] = true
		if len(found) == len(want) {
			break
		}
	}
	for name := range want {
		if !found[name] {
			return fmt.Errorf("%s not found in zip", name)
		}
	}
	return nil
}

func extractBinariesFromTarXz(archivePath string, want map[string]string) error {
	xzCmd := exec.Command("xz", "-d", "-c", archivePath)
	procexec.HideConsole(xzCmd)
	stdout, err := xzCmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := xzCmd.Start(); err != nil {
		return fmt.Errorf("xz not found: %w", err)
	}
	found := map[string]bool{}
	tr := tar.NewReader(stdout)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			xzCmd.Wait()
			return err
		}
		if hdr.FileInfo().IsDir() {
			continue
		}
		base := filepath.Base(hdr.Name)
		dest, ok := want[base]
		if !ok || found[base] {
			continue
		}
		out, err := os.Create(dest)
		if err != nil {
			xzCmd.Wait()
			return err
		}
		_, err = io.Copy(out, tr)
		out.Close()
		if err != nil {
			xzCmd.Wait()
			return err
		}
		found[base] = true
		if len(found) == len(want) {
			break
		}
	}
	xzCmd.Wait()
	if !found["ffmpeg"] {
		return fmt.Errorf("ffmpeg not found in archive")
	}
	return nil
}

func extractGzipFile(gzPath, dest string) error {
	f, err := os.Open(gzPath)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, gz)
	return err
}

// ffmpegArgs returns yt-dlp flags so merges/postprocessing use our FFmpeg.
func (d *YTDLPDownloader) ffmpegArgs() []string {
	if d.ffmpegPath == "" {
		// Try again at runtime
		if p, err := exec.LookPath("ffmpeg"); err == nil {
			return []string{"--ffmpeg-location", filepath.Dir(p)}
		}
		return nil
	}
	return []string{"--ffmpeg-location", d.ffmpegPath}
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

// extractYaziFromZip extracts yazi binary from zip archive
func extractYaziFromZip(zipPath, destPath string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer r.Close()

	for _, f := range r.File {
		// Look for yazi binary in the zip (usually in yazi-*/yazi or yazi-*/yazi.exe)
		if strings.Contains(f.Name, "yazi") && (strings.HasSuffix(f.Name, "/yazi") || strings.HasSuffix(f.Name, "\\yazi") || strings.HasSuffix(f.Name, "/yazi.exe") || strings.HasSuffix(f.Name, "\\yazi.exe")) {
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
	return errors.New("yazi binary not found in zip archive")
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

// Fetches playlist info and video title in a SINGLE yt-dlp call.
// Tries without cookies first (fast; works for public videos), then with
// cookies only if yt-dlp reports auth/bot/age-gate issues.
func (d *YTDLPDownloader) GetMetadata(args []string) (string, string, error) {
	ytDlpCmd := "yt-dlp"
	if runtime.GOOS == "windows" {
		ytDlpCmd = "yt-dlp.exe"
	}

	url := ""
	if len(args) > 0 {
		url = args[0]
	}

	isProblematic := isProblematicSite(url)
	directFile := IsDirectMediaURL(url)

	// Single call: get title + playlist info at once
	metaArgs := []string{
		"--print", "%(title)s|||%(playlist)s|||%(playlist_title)s|||%(playlist_count)s",
		"--no-warnings",
		"--no-playlist",
		"--socket-timeout", "20",
		"--user-agent", userAgent,
		"--legacy-server-connect",
	}

	// Direct CDN/file URLs: don't use site extractors (often broken / wrong match)
	if directFile {
		metaArgs = append(metaArgs, "--force-generic-extractor")
	}

	// Add site-specific headers
	if isProblematic {
		metaArgs = append(metaArgs, getSiteHeaders(url)...)
	}

	metaArgs = append(metaArgs, args...)

	// Fast path: no cookies (avoids kooky/Edge DB hangs on Windows)
	output, err := runYTDLPTimeout(ytDlpCmd, metaArgs, 45*time.Second)

	// Auth/bot/Instagram empty-media → try with browser cookies (LibreWolf/Firefox preferred)
	if err != nil && (needsCookies(string(output), err) || isInstagramURL(url)) {
		cookieBrowser := d.cfg.CookieBrowser
		if cookieBrowser == "" {
			cookieBrowser = DetectBrowser()
		}
		cookies.ClearCache()
		cookieArgs := cookies.GetYTDLPCookieArgs(url, cookieBrowser)
		if len(cookieArgs) > 0 {
			withCookies := append([]string{}, metaArgs[:len(metaArgs)-len(args)]...)
			withCookies = append(withCookies, cookieArgs...)
			withCookies = append(withCookies, args...)
			output2, err2 := runYTDLPTimeout(ytDlpCmd, withCookies, 45*time.Second)
			if err2 == nil {
				output, err = output2, nil
			} else if isCookieDBError(string(output2)) || strings.Contains(strings.ToLower(string(output2)), "cannot decrypt") {
				// Cookie DB locked / Brave decrypt failed — try fresh export once more
				cookies.ClearCache()
				if retry := cookies.GetYTDLPCookieArgs(url, cookieBrowser); len(retry) > 0 {
					withCookies2 := append([]string{}, metaArgs[:len(metaArgs)-len(args)]...)
					withCookies2 = append(withCookies2, retry...)
					withCookies2 = append(withCookies2, args...)
					if output3, err3 := runYTDLPTimeout(ytDlpCmd, withCookies2, 45*time.Second); err3 == nil {
						output, err = output3, nil
					}
				}
			} else {
				output, err = output2, err2
			}
		}
	}

	if err != nil {
		if len(output) > 0 {
			errMsg := strings.TrimSpace(string(output))
			// Helpful hints for common errors
			switch {
			case isCookieDBError(errMsg):
				return "", "", fmt.Errorf("could not read browser cookies (close Chrome/Edge or log into the site in the browser, then try again)")
			case strings.Contains(errMsg, "Unsupported URL"):
				return "", "", fmt.Errorf("unsupported URL")
			case strings.Contains(errMsg, "Video unavailable"):
				return "", "", fmt.Errorf("video unavailable (private, deleted, or region-locked)")
			case strings.Contains(errMsg, "empty media response"):
				return "", "", fmt.Errorf("Instagram blocked anonymous access. Log into Instagram in Firefox/Chrome, then retry (yaria will use browser cookies)")
			case strings.Contains(errMsg, "Sign in"), strings.Contains(errMsg, "sign-in"),
				strings.Contains(errMsg, "Age-restricted"), strings.Contains(errMsg, "confirm your age"),
				strings.Contains(errMsg, "confirm you're not a bot"):
				return "", "", fmt.Errorf("sign-in required. Log into YouTube in Firefox/LibreWolf (Brave cookies often can't be read), then retry")
			case strings.Contains(errMsg, "HTTP Error 429"):
				return "", "", fmt.Errorf("rate limited, try again later")
			case strings.Contains(errMsg, "HTTP Error 413"), strings.Contains(errMsg, "Request Entity Too Large"):
				// Clear cookie cache and retry without cookies
				cookies.ClearCache()
				return "", "", fmt.Errorf("request too large (cookies file may be oversized). Cache cleared -- please try again")
			case strings.Contains(errMsg, "SSLError"), strings.Contains(errMsg, "Connection reset"),
				strings.Contains(errMsg, "curl: (35)"):
				return "", "", fmt.Errorf("SSL/TLS connection failed. Your network may be blocking this site. Try using a VPN or proxy")
			case strings.Contains(errMsg, "No video formats found"), strings.Contains(errMsg, "unsupported URL format"):
				return "", "", fmt.Errorf("no video formats found. Try updating yt-dlp: pip install -U yt-dlp (or: sudo pacman -S yt-dlp)")
			case strings.Contains(errMsg, "Requested format is not available"):
				return "", "", fmt.Errorf("requested format unavailable")
			case strings.Contains(errMsg, "timed out"), strings.Contains(errMsg, "context deadline"):
				return "", "", fmt.Errorf("metadata fetch timed out. Check your network and try again")
			case strings.Contains(errMsg, "Traceback (most recent call last)"):
				return "", "", fmt.Errorf("yt-dlp crashed (update it: yt-dlp -U or pip install -U yt-dlp). Detail: %s", summarizeYTDLPError(errMsg))
			}
			return "", "", fmt.Errorf("%s", summarizeYTDLPError(errMsg))
		}
		return "", "", fmt.Errorf("yt-dlp failed: %v", err)
	}

	// Parse combined output: "title|||playlist|||playlist_title|||playlist_count"
	// yt-dlp may still print progress/log lines even with --print; also some
	// extractors return empty title or "NA" — never hard-fail the whole download.
	lines := strings.Split(string(output), "\n")
	var title string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "ERROR:") ||
			strings.HasPrefix(trimmed, "WARNING:") || strings.HasPrefix(trimmed, "DEPRECATED") {
			continue
		}
		// Skip pure log tags like [youtube] but keep titles that start with [
		// only if they look like the print template (contain |||) or aren't extractor tags
		if strings.HasPrefix(trimmed, "[") && !strings.Contains(trimmed, "|||") {
			// e.g. [info] ..., [download] ...
			if len(trimmed) > 2 && trimmed[1] >= 'a' && trimmed[1] <= 'z' {
				continue
			}
		}
		title = trimmed
		break
	}

	if title == "" || title == "NA" || title == "None" || strings.HasPrefix(title, "|||") {
		// Soft fallback: keep download going with a path-based name
		fallback := fallbackTitleFromURL(args)
		if fallback == "" {
			fallback = "Download"
		}
		return "NA&NA&1", fallback, nil
	}

	// Parse the combined output: "title|||playlist|||playlist_title|||playlist_count"
	var videoTitle, playlist, playlistTitle, playlistCount string
	parts := strings.SplitN(title, "|||", 4)
	if len(parts) >= 4 {
		videoTitle = strings.TrimSpace(parts[0])
		playlist = strings.TrimSpace(parts[1])
		playlistTitle = strings.TrimSpace(parts[2])
		playlistCount = strings.TrimSpace(parts[3])
	} else {
		videoTitle = title
		playlist = "NA"
		playlistTitle = "NA"
		playlistCount = "1"
	}
	title = strings.TrimSpace(videoTitle)
	if title == "" || title == "NA" || title == "None" {
		if fb := fallbackTitleFromURL(args); fb != "" {
			title = fb
		} else {
			title = "Download"
		}
	}

	if playlist == "" || playlist == "NA" || playlist == "None" {
		playlist = "NA"
		playlistTitle = "NA"
		playlistCount = "1"
	}

	playlistInfo := fmt.Sprintf("%s&%s&%s", playlist, playlistTitle, playlistCount)
	return playlistInfo, title, nil
}

// fallbackTitleFromURL picks a readable name from the last URL in yt-dlp args.
func fallbackTitleFromURL(args []string) string {
	var raw string
	for i := len(args) - 1; i >= 0; i-- {
		a := strings.TrimSpace(args[i])
		if strings.HasPrefix(a, "http://") || strings.HasPrefix(a, "https://") {
			raw = a
			break
		}
	}
	if raw == "" {
		return ""
	}
	path := raw
	if i := strings.Index(path, "://"); i >= 0 {
		path = path[i+3:]
		if j := strings.Index(path, "/"); j >= 0 {
			path = path[j:]
		}
	}
	if i := strings.IndexAny(path, "?#"); i >= 0 {
		path = path[:i]
	}
	base := filepath.Base(path)
	base = strings.TrimSuffix(base, filepath.Ext(base))
	if base == "" || base == "/" || base == "." {
		// YouTube watch?v=…
		if idx := strings.Index(raw, "v="); idx >= 0 {
			id := raw[idx+2:]
			if amp := strings.IndexAny(id, "&#"); amp >= 0 {
				id = id[:amp]
			}
			if id != "" {
				return "youtube-" + id
			}
		}
		return "Download"
	}
	// Unescape common encodings lightly
	base = strings.ReplaceAll(base, "+", " ")
	base = strings.ReplaceAll(base, "%20", " ")
	return base
}

// StreamTorrent streams a torrent magnet link using webtorrent-cli with mpv or vlc
func (d *YTDLPDownloader) StreamTorrent(magnetLink string) error {
	// Check for media players (mpv has priority)
	var player string
	if _, err := exec.LookPath("mpv"); err == nil {
		player = "mpv"
	} else if _, err := exec.LookPath("vlc"); err == nil {
		player = "vlc"
	} else {
		return errors.New("no media player found (install mpv or vlc)")
	}

	fmt.Fprintf(d.cfg.Stdout, "Streaming torrent with %s...\n", player)
	fmt.Fprintf(d.cfg.Stdout, "Press Ctrl+C to stop streaming\n\n")

	// Find webtorrent-cli
	webtorrentPath := ""

	// Try system PATH first
	if path, err := exec.LookPath("webtorrent"); err == nil {
		webtorrentPath = path
	} else {
		// Try dependencies/bin directory (npm install location)
		// Use same persistent location as New() function
		var depsDir string
		homeDir, err := os.UserHomeDir()
		if err == nil {
			depsDir = filepath.Join(homeDir, ".yaria", "dependencies")
		} else {
			cwd, _ := os.Getwd()
			depsDir = filepath.Join(cwd, "dependencies")
		}
		binDir := filepath.Join(depsDir, "bin")

		webtorrentBinary := "webtorrent"
		if runtime.GOOS == "windows" {
			webtorrentBinary = "webtorrent.cmd"
		}

		// Check bin directory
		binPath := filepath.Join(binDir, webtorrentBinary)
		if _, err := os.Stat(binPath); err == nil {
			webtorrentPath = binPath
		} else {
			// Check dependencies directory
			depsPath := filepath.Join(depsDir, webtorrentBinary)
			if _, err := os.Stat(depsPath); err == nil {
				webtorrentPath = depsPath
			}
		}
	}

	if webtorrentPath == "" {
		return errors.New("webtorrent-cli not installed. Install it with: npm install -g webtorrent-cli")
	}

	// Stream with webtorrent
	cmd := exec.Command(webtorrentPath, magnetLink, "--"+player)
	procexec.HideConsole(cmd)
	cmd.Stdout = d.cfg.Stdout
	cmd.Stderr = d.cfg.Stderr
	cmd.Stdin = os.Stdin

	return cmd.Run()
}

// Extracts video thumbnail to a temporary file
func (d *YTDLPDownloader) GetThumbnail(args []string, tempDir string) (string, error) {
	ytDlpCmd := "yt-dlp"
	if runtime.GOOS == "windows" {
		ytDlpCmd = "yt-dlp.exe"
	}

	// Create base thumbnail file path (yt-dlp will append video ID)
	thumbnailBase := filepath.Join(tempDir, "yaria_thumb")

	// Extract thumbnail
	thumbnailArgs := []string{
		"--write-thumbnail",
		"--skip-download",
		"--convert-thumbnails", "jpg",
		"--no-warnings",
		"--output", thumbnailBase + ".%(ext)s",
	}

	if d.cfg.CookieBrowser != "" {
		thumbnailArgs = append(thumbnailArgs, "--cookies-from-browser", d.cfg.CookieBrowser)
	}
	thumbnailArgs = append(thumbnailArgs, args...)

	cmd := exec.Command(ytDlpCmd, thumbnailArgs...)
	procexec.HideConsole(cmd)
	err := cmd.Run()
	if err != nil {
		// If thumbnail extraction fails, return empty path (not critical error)
		return "", nil
	}

	// Find the created thumbnail file (yt-dlp appends video ID and extension)
	// Look for files matching the pattern
	files, err := filepath.Glob(thumbnailBase + "*")
	if err != nil || len(files) == 0 {
		return "", nil
	}

	// Return the first matching file
	return files[0], nil
}

// Predicts the output filename
func (d *YTDLPDownloader) GetOutputFilename(args []string, tempDir string) (string, error) {
	ytDlpCmd := "yt-dlp"
	if runtime.GOOS == "windows" {
		ytDlpCmd = "yt-dlp.exe"
	}
	cmd := exec.Command(ytDlpCmd, append([]string{"--print", "filename", "--output", tempDir + "/" + d.cfg.OutputTemplate}, args...)...)
	procexec.HideConsole(cmd)
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
	// Extract cookies: kooky first, yt-dlp fallback
	cookieBrowser := d.cfg.CookieBrowser
	if cookieBrowser == "" {
		cookieBrowser = DetectBrowser()
	}
	cookieArgs := cookies.GetYTDLPCookieArgs(url, cookieBrowser)
	cmdArgs = append(cmdArgs, cookieArgs...)
	cmdArgs = append(cmdArgs, url)
	output, err := runYTDLP(ytDlpCmd, cmdArgs)
	if err != nil && isCookieDBError(string(output)) && hasCookieArgs(cmdArgs) {
		output, err = runYTDLP(ytDlpCmd, stripCookieArgs(cmdArgs))
	}
	if err != nil {
		// Include stderr output in error message for better debugging
		if len(output) > 0 {
			errMsg := strings.TrimSpace(string(output))
			if isCookieDBError(errMsg) {
				return nil, fmt.Errorf("could not read browser cookies (close Chrome/Edge and try again)")
			}
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
		// Skip header lines and empty lines
		if strings.HasPrefix(line, "[") || strings.HasPrefix(line, " ") || len(strings.TrimSpace(line)) == 0 {
			continue
		}

		// Look for format lines - more flexible matching
		if strings.Contains(line, "video only") || strings.Contains(line, "audio only") ||
			(strings.Contains(line, "mp4") || strings.Contains(line, "webm")) ||
			(len(strings.Fields(line)) > 2 && strings.Fields(line)[0] != "ID" && !strings.Contains(line, "extension")) {
			fields := strings.Fields(line)
			if len(fields) < 2 {
				continue
			}
			formatID := fields[0]
			isAudio := strings.Contains(line, "audio only")
			height := 0
			ext := ""
			protocol := ""
			fileSize := ""
			for _, field := range fields {
				// Try to extract height from various formats
				if strings.Contains(field, "x") && !isAudio {
					parts := strings.Split(field, "x")
					if len(parts) >= 2 {
						// Remove any non-numeric characters from the second part
						heightStr := strings.TrimSuffix(parts[1], "p")
						heightStr = strings.TrimSuffix(heightStr, "i")
						if res, err := strconv.Atoi(heightStr); err == nil {
							height = res
						}
					}
				} else if strings.HasSuffix(field, "p") && !isAudio {
					// Handle formats like "720p"
					heightStr := strings.TrimSuffix(field, "p")
					if res, err := strconv.Atoi(heightStr); err == nil {
						height = res
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
			// For non-YouTube sites, include formats even without explicit height
			includeFormat := (isAudio && ext != "") || (!isAudio && height > 0 && ext != "")
			if !includeFormat && !isAudio && !strings.Contains(url, "youtube.com") && ext != "" && protocol != "" {
				// For non-YouTube sites, include video formats even without height
				includeFormat = true
				height = 720 // Default height for unknown formats
			}

			// Filter out invalid formats with missing critical info
			if includeFormat && !isAudio {
				// Must have extension and either height > 0 or protocol
				if ext == "" || (height == 0 && protocol == "") {
					includeFormat = false
				}
				// Filter out extremely low resolutions that are likely errors
				if height > 0 && height < 144 {
					includeFormat = false
				}
			}

			if includeFormat {
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

// GetVideoInfo fetches title, uploader, duration, thumbnail, and formats in one yt-dlp -J call.
func (d *YTDLPDownloader) GetVideoInfo(url string) (*VideoInfo, error) {
	ytDlpCmd := "yt-dlp"
	if runtime.GOOS == "windows" {
		ytDlpCmd = "yt-dlp.exe"
	}

	isProblematic := isProblematicSite(url)
	cmdArgs := []string{
		"-J",
		"--no-warnings",
		"--no-playlist",
		"--socket-timeout", "20",
		"--user-agent", userAgent,
		"--legacy-server-connect",
		"--extractor-retries", "2",
	}
	if isProblematic {
		cmdArgs = append(cmdArgs, getSiteHeaders(url)...)
	}
	cmdArgs = append(cmdArgs, url)

	// Fast path: no cookies first (avoids browser DB hangs)
	output, err := runYTDLPTimeout(ytDlpCmd, cmdArgs, 45*time.Second)

	if err != nil && (needsCookies(string(output), err) || isInstagramURL(url)) {
		cookieBrowser := d.cfg.CookieBrowser
		if cookieBrowser == "" {
			cookieBrowser = DetectBrowser()
		}
		cookieArgs := cookies.GetYTDLPCookieArgs(url, cookieBrowser)
		if len(cookieArgs) > 0 {
			withCookies := append([]string{}, cmdArgs[:len(cmdArgs)-1]...)
			withCookies = append(withCookies, cookieArgs...)
			withCookies = append(withCookies, url)
			output2, err2 := runYTDLPTimeout(ytDlpCmd, withCookies, 45*time.Second)
			if err2 == nil {
				output, err = output2, nil
			} else if !isCookieDBError(string(output2)) {
				output, err = output2, err2
			}
		}
	}

	if err != nil {
		if len(output) > 0 {
			return nil, fmt.Errorf("%s", summarizeYTDLPError(strings.TrimSpace(string(output))))
		}
		return nil, fmt.Errorf("yt-dlp failed: %v", err)
	}

	var raw struct {
		Title     string  `json:"title"`
		Uploader  string  `json:"uploader"`
		Channel   string  `json:"channel"`
		Duration  float64 `json:"duration"`
		Thumbnail string  `json:"thumbnail"`
		Thumbnails []struct {
			URL string `json:"url"`
		} `json:"thumbnails"`
		Formats []struct {
			FormatID       string  `json:"format_id"`
			Height         int     `json:"height"`
			Ext            string  `json:"ext"`
			VCodec         string  `json:"vcodec"`
			ACodec         string  `json:"acodec"`
			Protocol       string  `json:"protocol"`
			Filesize       int64   `json:"filesize"`
			FilesizeApprox int64   `json:"filesize_approx"`
		} `json:"formats"`
	}
	if err := json.Unmarshal(output, &raw); err != nil {
		return nil, fmt.Errorf("parse yt-dlp json: %v", err)
	}

	info := &VideoInfo{
		Title:    raw.Title,
		Uploader: raw.Uploader,
		Duration: int(raw.Duration),
	}
	if info.Uploader == "" {
		info.Uploader = raw.Channel
	}
	info.Thumbnail = raw.Thumbnail
	if info.Thumbnail == "" && len(raw.Thumbnails) > 0 {
		info.Thumbnail = raw.Thumbnails[len(raw.Thumbnails)-1].URL
	}

	// Prefer best format per height (video) similar to GetFormats
	uniqueVideo := make(map[int]Format)
	var audioFmts []Format
	for _, f := range raw.Formats {
		vNone := f.VCodec == "none" || f.VCodec == ""
		aNone := f.ACodec == "none" || f.ACodec == ""
		isAudio := vNone && !aNone
		isVideo := !vNone && f.Height > 0
		if !isAudio && !isVideo {
			continue
		}
		if isVideo && f.Height < 144 {
			continue
		}

		ext := strings.ToLower(f.Ext)
		if ext == "" {
			continue
		}
		proto := f.Protocol
		if strings.Contains(proto, "m3u8") {
			proto = "m3u8"
		} else if strings.HasPrefix(proto, "http") {
			proto = "http"
		}

		size := f.Filesize
		if size <= 0 {
			size = f.FilesizeApprox
		}
		entry := Format{
			ID:       f.FormatID,
			Height:   f.Height,
			Ext:      ext,
			IsAudio:  isAudio,
			Protocol: proto,
			FileSize: formatBytes(size),
		}

		if isAudio {
			audioFmts = append(audioFmts, entry)
			continue
		}

		existing, exists := uniqueVideo[f.Height]
		if !exists {
			uniqueVideo[f.Height] = entry
			continue
		}
		shouldReplace := false
		if ext == "mp4" && existing.Ext != "mp4" {
			shouldReplace = true
		} else if ext == existing.Ext {
			if (proto == "http" || proto == "") && existing.Protocol != "http" && existing.Protocol != "" {
				shouldReplace = true
			}
		}
		if shouldReplace {
			uniqueVideo[f.Height] = entry
		}
	}

	sorted := make([]Format, 0, len(uniqueVideo))
	for _, f := range uniqueVideo {
		sorted = append(sorted, f)
	}
	for i := 0; i < len(sorted)-1; i++ {
		for j := i + 1; j < len(sorted); j++ {
			if sorted[i].Height < sorted[j].Height {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}
	info.Formats = append(sorted, audioFmts...)
	return info, nil
}

func formatBytes(n int64) string {
	if n <= 0 {
		return ""
	}
	const (
		kb = 1024
		mb = 1024 * kb
		gb = 1024 * mb
	)
	switch {
	case n >= gb:
		return fmt.Sprintf("%.1fGiB", float64(n)/float64(gb))
	case n >= mb:
		return fmt.Sprintf("%.1fMiB", float64(n)/float64(mb))
	case n >= kb:
		return fmt.Sprintf("%.1fKiB", float64(n)/float64(kb))
	default:
		return fmt.Sprintf("%dB", n)
	}
}

// Executes the download process with retries and fallback
func (d *YTDLPDownloader) Download(args []string, tempDir string) (bool, error) {
	ytDlpCmd := "yt-dlp"
	if runtime.GOOS == "windows" {
		ytDlpCmd = "yt-dlp.exe"
	}
	for attempt := 1; attempt <= d.cfg.MaxRetries; attempt++ {
		argURL := strings.Join(args, " ")
		// Slow mode: adult hosts only. Social/normal sites use full speed.
		adultSlow := IsAdultSlowSite(argURL)
		wantHeaders := NeedsSiteHeaders(argURL)
		directFile := IsDirectMediaURL(argURL)

		var cmdArgs []string
		if adultSlow {
			cmdArgs = []string{
				"--no-overwrites",
				"--geo-bypass",
				"--concurrent-fragments", "8",
				"--buffer-size", "32K",
				"--http-chunk-size", "5M",
				"--no-warnings",
				"--progress",
				"--newline",
				"--extractor-retries", "5",
				"--fragment-retries", "10",
				"--retries", "10",
				"--retry-sleep", "3",
				"--socket-timeout", "60",
				"--sleep-interval", "1",
				"--max-sleep-interval", "2",
			}
		} else {
			// Maximum speed settings for normal + social sites
			cmdArgs = []string{
				"--no-overwrites",
				"--geo-bypass",
				"--concurrent-fragments", "32",
				"--buffer-size", "128K",
				"--http-chunk-size", "10M",
				"--no-warnings",
				"--progress",
				"--newline",
				"--extractor-retries", "3",
				"--fragment-retries", "5",
				"--retries", "3",
				"--socket-timeout", "20",
			}
		}
		// Browser-sniffed file URLs (e.g. eporner /dload/) must use generic HTTP,
		// not the site extractor (often outdated / broken).
		if directFile {
			cmdArgs = append(cmdArgs, "--force-generic-extractor")
		}

		// Add common arguments for both cases
		cmdArgs = append(cmdArgs,
			"--no-mtime",
			"--no-playlist",
			"--user-agent", userAgent,
			"--legacy-server-connect",
			"--output", filepath.Join(tempDir, d.cfg.OutputTemplate),
		)
		// Attach browser cookies only if kooky can export quickly.
		// Avoids Windows hangs from --cookies-from-browser / locked Edge DB.
		// Public YouTube works without cookies; auth failures retry later without them.
		cookieURL := argURL
		if d.cfg.Referer != "" {
			// Prefer cookies for the watch page host (CDN file hosts often share parent site cookies)
			cookieURL = d.cfg.Referer
		}
		dlCookieArgs := cookies.GetYTDLPCookieArgs(cookieURL, d.cfg.CookieBrowser)
		cmdArgs = append(cmdArgs, dlCookieArgs...)

		// Headers only when needed (adult + a few picky hosts) — not full slow mode
		if wantHeaders {
			cmdArgs = append(cmdArgs, "--add-header", "Accept-Language:en-US,en;q=0.9")
			cmdArgs = append(cmdArgs, getSiteHeaders(argURL)...)
		}
		// Extension / caller-provided page referer (critical for /dload/ hotlinks)
		if ref := strings.TrimSpace(d.cfg.Referer); ref != "" {
			cmdArgs = append(cmdArgs, "--referer", ref)
			cmdArgs = append(cmdArgs, "--add-header", "Referer:"+ref)
		}
		if directFile {
			// Single progressive file — no format merge gymnastics
			if d.cfg.IsAudioOnly {
				cmdArgs = append(cmdArgs, "--extract-audio", "--audio-format", d.cfg.AudioFormat)
			} else {
				cmdArgs = append(cmdArgs, "--format", "best")
			}
		} else if d.cfg.IsAudioOnly {
			cmdArgs = append(cmdArgs, "--extract-audio", "--audio-format", d.cfg.AudioFormat)
		} else if d.cfg.Resolution != "" {
			cmdArgs = append(cmdArgs, "--format", d.cfg.Resolution+"+bestaudio/best")
		} else {
			// Check if user already provided --format, use that
			userProvidedFormat := false
			for _, arg := range args {
				if arg == "--format" || arg == "-f" {
					userProvidedFormat = true
					break
				}
			}
			if !userProvidedFormat {
				// HLS-first only for adult CDNs; social uses normal best
				if PreferHLSFormat(argURL) {
					cmdArgs = append(cmdArgs, "--format", "bestvideo[protocol=m3u8]+bestaudio[protocol=m3u8]/best[protocol=m3u8]/best[height<=1080]/best")
				} else {
					cmdArgs = append(cmdArgs, "--format", "bestvideo+bestaudio/best")
				}
			}
		}
		// Merge into user-selected container format (default: mp4)
		if !d.cfg.IsAudioOnly && !directFile {
			fmt := d.cfg.ContainerFormat
			if fmt == "" {
				fmt = "mp4"
			}
			cmdArgs = append(cmdArgs, "--merge-output-format", fmt)
		}

		// Tell yt-dlp where FFmpeg lives so it can merge video+audio
		cmdArgs = append(cmdArgs, d.ffmpegArgs()...)

		cmdArgs = append(cmdArgs, args...)

		// Prefer aria2 when enabled (faster multi-conn). Direct/hotlink CDNs that
		// reject it are handled by an immediate retry without aria2 below.
		useAria := d.cfg.UseAria2c
		if useAria {
			aria2Cmd := "aria2c"
			if runtime.GOOS == "windows" {
				aria2Cmd = "aria2c.exe"
			}
			cmdArgs = append(cmdArgs, "--downloader", aria2Cmd, "--downloader-args", "aria2c:"+d.cfg.Aria2cArgs)
		}

		stderrBuf := &strings.Builder{}
		err := runYTDLPStreaming(ytDlpCmd, cmdArgs, d.cfg.Stdout, io.MultiWriter(d.cfg.Stderr, stderrBuf))
		if err == nil {
			return true, nil
		}
		errText := stderrBuf.String() + " " + err.Error()

		// aria2 first failed → retry same args without aria2 (common on hotlink CDNs)
		if useAria && (directFile || isAriaHostileError(errText)) {
			fmt.Fprintf(d.cfg.Stderr, "WARNING: aria2 download failed; retrying without aria2...\n")
			noAriaArgs := stripAria2Args(cmdArgs)
			stderrBuf.Reset()
			if err2 := runYTDLPStreaming(ytDlpCmd, noAriaArgs, d.cfg.Stdout, io.MultiWriter(d.cfg.Stderr, stderrBuf)); err2 == nil {
				return true, nil
			} else {
				err = err2
				errText = stderrBuf.String() + " " + err2.Error()
			}
		}

		// Cookie DB locked (Chrome open) — drop cookies and retry this attempt once.
		if isCookieDBError(errText) && hasCookieArgs(cmdArgs) {
			fmt.Fprintf(d.cfg.Stderr, "WARNING: browser cookie DB locked; retrying download without cookies\n")
			noCookieArgs := stripAria2Args(stripCookieArgs(cmdArgs))
			// Keep aria2 only when it wasn't the failure mode
			if useAria && !isAriaHostileError(errText) && !directFile {
				aria2Cmd := "aria2c"
				if runtime.GOOS == "windows" {
					aria2Cmd = "aria2c.exe"
				}
				noCookieArgs = append(noCookieArgs, "--downloader", aria2Cmd, "--downloader-args", "aria2c:"+d.cfg.Aria2cArgs)
			}
			stderrBuf.Reset()
			if errNoCookie := runYTDLPStreaming(ytDlpCmd, noCookieArgs, d.cfg.Stdout, io.MultiWriter(d.cfg.Stderr, stderrBuf)); errNoCookie == nil {
				return true, nil
			} else {
				err = errNoCookie
			}
		}

		if attempt < d.cfg.MaxRetries {
			fmt.Fprintf(d.cfg.Stderr, "WARNING: Download attempt %d/%d failed: %v. Retrying...\n", attempt, d.cfg.MaxRetries, err)
			d.cfg.WaitBeforeRetry(attempt)
			continue
		}
		fmt.Fprintf(d.cfg.Stderr, "WARNING: All %d attempts failed. Trying fallback format...\n", d.cfg.MaxRetries)
		// Fallback keeps cookies — age-restricted YouTube fails without them.
		if attempt == d.cfg.MaxRetries {
			fallbackArgs := []string{
				"--no-overwrites",
				"--geo-bypass",
				"--concurrent-fragments", "8",
				"--buffer-size", "32K",
				"--http-chunk-size", "4M",
				"--no-warnings",
				"--progress",
				"--newline",
				"--extractor-retries", "3",
				"--fragment-retries", "5",
				"--retries", "3",
				"--socket-timeout", "30",
				"--no-mtime",
				"--no-playlist",
				"--user-agent", userAgent,
				"--legacy-server-connect",
				"--output", tempDir + "/" + d.cfg.OutputTemplate,
			}
			if directFile {
				fallbackArgs = append(fallbackArgs, "--force-generic-extractor")
			}
			if ref := strings.TrimSpace(d.cfg.Referer); ref != "" {
				fallbackArgs = append(fallbackArgs, "--referer", ref, "--add-header", "Referer:"+ref)
			}
			// Re-resolve cookies (may pick LibreWolf after Brave decrypt failed)
			cookies.ClearCache()
			fbCookieURL := argURL
			if d.cfg.Referer != "" {
				fbCookieURL = d.cfg.Referer
			}
			fbBrowser := d.cfg.CookieBrowser
			if fbBrowser == "" {
				fbBrowser = DetectBrowser()
			}
			fallbackArgs = append(fallbackArgs, cookies.GetYTDLPCookieArgs(fbCookieURL, fbBrowser)...)
			if d.cfg.IsAudioOnly {
				fallbackArgs = append(fallbackArgs, "--extract-audio", "--audio-format", d.cfg.AudioFormat)
			} else if directFile {
				fallbackArgs = append(fallbackArgs, "--format", "best")
			} else {
				// Prefer progressive "best" so age-gated videos with only format 18 still work
				fallbackArgs = append(fallbackArgs, "--format", "bestvideo+bestaudio/best/b", "--merge-output-format", "mp4")
			}
			fallbackArgs = append(fallbackArgs, d.ffmpegArgs()...)
			fallbackArgs = append(fallbackArgs, args...)

			// Fallback: try aria2 once, then native yt-dlp
			tryFallback := func(withAria bool) error {
				args := append([]string{}, fallbackArgs...)
				if withAria && d.cfg.UseAria2c {
					aria2Cmd := "aria2c"
					if runtime.GOOS == "windows" {
						aria2Cmd = "aria2c.exe"
					}
					args = append(args, "--downloader", aria2Cmd, "--downloader-args", "aria2c:"+d.cfg.Aria2cArgs)
				}
				return runYTDLPStreaming(ytDlpCmd, args, d.cfg.Stdout, d.cfg.Stderr)
			}
			if d.cfg.UseAria2c {
				if err := tryFallback(true); err == nil {
					return true, nil
				}
				fmt.Fprintf(d.cfg.Stderr, "WARNING: fallback with aria2 failed; trying without aria2...\n")
			}
			if err := tryFallback(false); err == nil {
				return true, nil
			} else if isCookieDBError(stderrBuf.String()) {
				return false, errors.New("could not read browser cookies (close Chrome/Edge and try again, or continue without signing in)")
			}
		}
		if attempt < d.cfg.MaxRetries {
			d.cfg.WaitBeforeRetry(attempt)
		}
	}
	return false, errors.New("all download attempts failed, including fallback")
}

// runYTDLP runs yt-dlp and returns combined output (no timeout).
func runYTDLP(ytDlpCmd string, args []string) ([]byte, error) {
	return runYTDLPTimeout(ytDlpCmd, args, 0)
}

// runYTDLPTimeout runs yt-dlp with an optional overall timeout.
// timeout <= 0 means no deadline.
func runYTDLPTimeout(ytDlpCmd string, args []string, timeout time.Duration) ([]byte, error) {
	var cmd *exec.Cmd
	if timeout > 0 {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		cmd = exec.CommandContext(ctx, ytDlpCmd, args...)
	} else {
		cmd = exec.Command(ytDlpCmd, args...)
	}
	procexec.HideConsole(cmd)
	cmd.Env = append(os.Environ(), "CURL_CFFI_DISABLE=1")
	out, err := cmd.CombinedOutput()
	if err != nil && timeout > 0 && errors.Is(err, context.DeadlineExceeded) {
		return out, fmt.Errorf("timed out after %s", timeout)
	}
	// CommandContext wraps deadline as "signal: killed" / ExitError — detect via ctx if needed
	if err != nil && timeout > 0 && cmd.ProcessState != nil && !cmd.ProcessState.Success() {
		if len(out) == 0 && strings.Contains(err.Error(), "killed") {
			return out, fmt.Errorf("timed out after %s", timeout)
		}
	}
	return out, err
}

// runYTDLPStreaming runs yt-dlp with live stdout/stderr (for progress UI).
func runYTDLPStreaming(ytDlpCmd string, args []string, stdout, stderr io.Writer) error {
	cmd := exec.Command(ytDlpCmd, args...)
	procexec.HideConsole(cmd)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.Env = append(os.Environ(),
		"PYTHONDONTWRITEBYTECODE=1",
		"PYTHONUNBUFFERED=1",
		"CURL_CFFI_DISABLE=1",
	)
	return cmd.Run()
}

// needsCookies reports whether yt-dlp failed in a way that cookies might fix.
func needsCookies(output string, err error) bool {
	s := strings.ToLower(output)
	if err != nil {
		s += " " + strings.ToLower(err.Error())
	}
	return strings.Contains(s, "sign in") ||
		strings.Contains(s, "sign-in") ||
		strings.Contains(s, "not a bot") ||
		strings.Contains(s, "age-restricted") ||
		strings.Contains(s, "age restricted") ||
		strings.Contains(s, "age-verification") ||
		strings.Contains(s, "login required") ||
		strings.Contains(s, "private video") ||
		strings.Contains(s, "confirm your age") ||
		strings.Contains(s, "empty media response") ||
		strings.Contains(s, "logged-in") ||
		strings.Contains(s, "use --cookies") ||
		strings.Contains(s, "cookies-from-browser")
}

func isInstagramURL(url string) bool {
	return urlMatchesAnyHost(url, []string{"instagram.com", "ddinstagram.com"})
}

// summarizeYTDLPError picks the useful ERROR line from noisy yt-dlp/python output.
func summarizeYTDLPError(errMsg string) string {
	errMsg = strings.TrimSpace(errMsg)
	if errMsg == "" {
		return "unknown yt-dlp error"
	}
	// Prefer last ERROR: line
	lines := strings.Split(errMsg, "\n")
	var lastErr string
	for _, line := range lines {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "ERROR:") || strings.Contains(t, "ERROR: ") {
			lastErr = t
		}
	}
	if lastErr != "" {
		lastErr = strings.TrimPrefix(lastErr, "ERROR:")
		lastErr = strings.TrimSpace(lastErr)
		if len(lastErr) > 280 {
			lastErr = lastErr[:280] + "..."
		}
		return lastErr
	}
	// Collapse python tracebacks
	if strings.Contains(errMsg, "Traceback (most recent call last)") {
		if len(errMsg) > 200 {
			return "yt-dlp internal error (update yt-dlp). " + errMsg[len(errMsg)-120:]
		}
		return "yt-dlp internal error — try: yt-dlp -U"
	}
	if len(errMsg) > 300 {
		return errMsg[:300] + "..."
	}
	return errMsg
}

func isCookieDBError(s string) bool {
	s = strings.ToLower(s)
	return (strings.Contains(s, "could not copy") && strings.Contains(s, "cookie")) ||
		strings.Contains(s, "could not copy chrome cookie") ||
		strings.Contains(s, "could not copy edge cookie") ||
		(strings.Contains(s, "cookie database") && strings.Contains(s, "could not"))
}

func hasCookieArgs(args []string) bool {
	for _, a := range args {
		if a == "--cookies" || a == "--cookies-from-browser" {
			return true
		}
	}
	return false
}

func stripCookieArgs(args []string) []string {
	out := make([]string, 0, len(args))
	skipNext := false
	for _, a := range args {
		if skipNext {
			skipNext = false
			continue
		}
		if a == "--cookies" || a == "--cookies-from-browser" {
			skipNext = true
			continue
		}
		out = append(out, a)
	}
	return out
}

// isAriaHostileError detects failures where multi-connection aria2 often breaks
// (hotlink CDNs, TLS resets) and a native yt-dlp retry is worth trying.
func isAriaHostileError(msg string) bool {
	m := strings.ToLower(msg)
	switch {
	case strings.Contains(m, "connection reset"),
		strings.Contains(m, "connection aborted"),
		strings.Contains(m, "connection refused"),
		strings.Contains(m, "broken pipe"),
		strings.Contains(m, "transport error"),
		strings.Contains(m, "ssl"),
		strings.Contains(m, "tls"),
		strings.Contains(m, "http error 403"),
		strings.Contains(m, "http error 429"),
		strings.Contains(m, "unable to download"),
		strings.Contains(m, "failed to open for writing"), // partial multi-conn mess
		strings.Contains(m, "403"),
		strings.Contains(m, "status code: 403"):
		return true
	default:
		return false
	}
}

func stripAria2Args(args []string) []string {
	out := make([]string, 0, len(args))
	skipNext := false
	for _, a := range args {
		if skipNext {
			skipNext = false
			continue
		}
		if a == "--downloader" || a == "--downloader-args" {
			skipNext = true
			continue
		}
		out = append(out, a)
	}
	return out
}

// Splits a string into lines and trims whitespace
func splitLines(s string) []string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	for i, line := range lines {
		lines[i] = strings.TrimSpace(line)
	}
	return lines
}

// DetectBrowser finds an installed browser for cookie extraction.
// Checks installed browsers and their profile directories to find
// Returns the yt-dlp --cookies-from-browser value.
//
// Supports:
//   - Firefox forks: Librewolf, Waterfox, Floorp, Zen, Mercury, Mullvad, Pale Moon
//   - Chromium forks: Thorium, Ungoogled Chromium, Arc
//   - Standard: Firefox, Chrome, Chromium, Brave, Edge, Vivaldi, Opera, Safari, Whale
//   - Install methods: native, Flatpak, Snap
//   - Platforms: Linux, macOS, Windows
//
// Firefox forks use "firefox:/path/to/profile" syntax.
// Chromium forks use "chromium:/path/to/profile" syntax.
// DetectBrowser auto-detects an installed browser for cookie extraction.
func DetectBrowser() string {
	home, _ := os.UserHomeDir()
	if home == "" {
		return ""
	}

	// --- Phase 1: Firefox-based forks (checked first, most likely to have real cookies) ---
	type forkDef struct {
		binaries   []string // binary names to check in PATH
		cookieDirs []string // profile directories (contain subdirs with cookies.sqlite)
		ytdlpBase  string   // "firefox" -- all Firefox forks use this
	}

	firefoxForks := []forkDef{
		// Librewolf
		{[]string{"librewolf"},
			[]string{
				filepath.Join(home, ".librewolf"),
				filepath.Join(home, "Library", "Application Support", "LibreWolf"),
				filepath.Join(home, ".var", "app", "io.gitlab.librewolf-community", ".librewolf"),
			}, "firefox"},
		// Waterfox
		{[]string{"waterfox"},
			[]string{
				filepath.Join(home, ".waterfox"),
				filepath.Join(home, "Library", "Application Support", "Waterfox"),
			}, "firefox"},
		// Floorp
		{[]string{"floorp"},
			[]string{
				filepath.Join(home, ".floorp"),
				filepath.Join(home, "Library", "Application Support", "Floorp"),
				filepath.Join(home, ".var", "app", "one.ablaze.floorp", ".floorp"),
			}, "firefox"},
		// Zen Browser
		{[]string{"zen-browser", "zen"},
			[]string{
				filepath.Join(home, ".zen"),
			}, "firefox"},
		// Mercury Browser
		{[]string{"mercury-browser"},
			[]string{
				filepath.Join(home, ".mercury"),
			}, "firefox"},
		// Mullvad Browser
		{[]string{"mullvad-browser"},
			[]string{
				filepath.Join(home, ".mullvad"),
			}, "firefox"},
		// Pale Moon
		{[]string{"palemoon"},
			[]string{
				filepath.Join(home, ".moonchild productions", "pale moon"),
			}, "firefox"},
	}

	for _, fork := range firefoxForks {
		installed := false
		for _, bin := range fork.binaries {
			if _, err := exec.LookPath(bin); err == nil {
				installed = true
				break
			}
		}
		if !installed {
			continue
		}
		for _, dir := range fork.cookieDirs {
			if profile := findFirefoxProfile(dir); profile != "" {
				return fork.ytdlpBase + ":" + profile
			}
		}
	}

	// --- Phase 2: Chromium-based forks (not natively in yt-dlp) ---
	type chromeForkDef struct {
		binaries   []string
		cookieDirs []string // contain "Default/Cookies" or similar
		ytdlpBase  string   // "chrome" or "chromium"
	}

	chromeForks := []chromeForkDef{
		// Thorium
		{[]string{"thorium-browser", "thorium"},
			[]string{
				filepath.Join(home, ".config", "thorium"),
			}, "chromium"},
	}

	for _, fork := range chromeForks {
		installed := false
		for _, bin := range fork.binaries {
			if _, err := exec.LookPath(bin); err == nil {
				installed = true
				break
			}
		}
		if !installed {
			continue
		}
		for _, dir := range fork.cookieDirs {
			if profile := findChromiumProfile(dir); profile != "" {
				return fork.ytdlpBase + ":" + profile
			}
		}
	}

	// --- Phase 3: Standard browsers (yt-dlp native support) ---
	// Check in order of likelihood of having logged-in sessions.
	type stdBrowser struct {
		binaries []string
		name     string
	}

	var standard []stdBrowser

	switch runtime.GOOS {
	case "linux":
		standard = []stdBrowser{
			// Native installs
			{[]string{"firefox"}, "firefox"},
			{[]string{"google-chrome-stable", "google-chrome"}, "chrome"},
			{[]string{"chromium", "chromium-browser"}, "chromium"},
			{[]string{"brave-browser", "brave"}, "brave"},
			{[]string{"microsoft-edge-stable", "microsoft-edge"}, "edge"},
			{[]string{"vivaldi-stable", "vivaldi"}, "vivaldi"},
			{[]string{"opera"}, "opera"},
			{[]string{"whale"}, "whale"},
			// Snap installs
			{[]string{"/snap/bin/firefox"}, "firefox"},
			{[]string{"/snap/bin/chromium"}, "chromium"},
		}
		// Also check Flatpak Firefox profile dir even if binary is in PATH as "firefox"
		flatpakFirefox := filepath.Join(home, ".var", "app", "org.mozilla.firefox", ".mozilla", "firefox")
		if profile := findFirefoxProfile(flatpakFirefox); profile != "" {
			// Flatpak Firefox exists with cookies -- use it
			return "firefox:" + profile
		}

	case "darwin":
		standard = []stdBrowser{
			{[]string{"/Applications/Firefox.app/Contents/MacOS/firefox", "firefox"}, "firefox"},
			{[]string{"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome", "google-chrome"}, "chrome"},
			{[]string{"/Applications/Chromium.app/Contents/MacOS/Chromium", "chromium"}, "chromium"},
			{[]string{"/Applications/Brave Browser.app/Contents/MacOS/Brave Browser", "brave-browser"}, "brave"},
			{[]string{"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge"}, "edge"},
			{[]string{"/Applications/Vivaldi.app/Contents/MacOS/Vivaldi"}, "vivaldi"},
			{[]string{"/Applications/Opera.app/Contents/MacOS/Opera"}, "opera"},
			{[]string{"safari"}, "safari"},
		}

	case "windows":
		localAppData := os.Getenv("LOCALAPPDATA")
		programFiles := os.Getenv("ProgramFiles")
		programFilesX86 := os.Getenv("ProgramFiles(x86)")
		if programFiles == "" {
			programFiles = `C:\Program Files`
		}
		if programFilesX86 == "" {
			programFilesX86 = `C:\Program Files (x86)`
		}
		// Edge first: every Windows install has it; Chrome is optional.
		// Check both Program Files and Program Files (x86) for Edge/Chrome.
		standard = []stdBrowser{
			{[]string{
				filepath.Join(programFilesX86, "Microsoft", "Edge", "Application", "msedge.exe"),
				filepath.Join(programFiles, "Microsoft", "Edge", "Application", "msedge.exe"),
			}, "edge"},
			{[]string{
				filepath.Join(programFiles, "Google", "Chrome", "Application", "chrome.exe"),
				filepath.Join(programFilesX86, "Google", "Chrome", "Application", "chrome.exe"),
			}, "chrome"},
			{[]string{filepath.Join(programFiles, "Mozilla Firefox", "firefox.exe")}, "firefox"},
			{[]string{filepath.Join(programFiles, "BraveSoftware", "Brave-Browser", "Application", "brave.exe")}, "brave"},
			{[]string{filepath.Join(localAppData, "Vivaldi", "Application", "vivaldi.exe")}, "vivaldi"},
			{[]string{filepath.Join(programFiles, "Opera", "opera.exe")}, "opera"},
		}

	default:
		standard = []stdBrowser{
			{[]string{"firefox"}, "firefox"},
			{[]string{"chromium"}, "chromium"},
		}
	}

	for _, b := range standard {
		for _, bin := range b.binaries {
			if filepath.IsAbs(bin) {
				if _, err := os.Stat(bin); err == nil {
					return b.name
				}
			} else {
				if _, err := exec.LookPath(bin); err == nil {
					return b.name
				}
			}
		}
	}

	return ""
}

// findFirefoxProfile finds the most recently used profile directory
// containing cookies.sqlite in a Firefox-compatible browser's profile directory.
func findFirefoxProfile(profilesDir string) string {
	if _, err := os.Stat(profilesDir); err != nil {
		return ""
	}

	var bestProfile string
	var bestTime int64

	entries, err := os.ReadDir(profilesDir)
	if err != nil {
		return ""
	}

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		cookiePath := filepath.Join(profilesDir, e.Name(), "cookies.sqlite")
		info, err := os.Stat(cookiePath)
		if err != nil {
			continue
		}
		if info.ModTime().Unix() > bestTime {
			bestTime = info.ModTime().Unix()
			bestProfile = filepath.Join(profilesDir, e.Name())
		}
	}

	return bestProfile
}

// findChromiumProfile finds a Chromium-based browser profile with cookies.
func findChromiumProfile(configDir string) string {
	cookiePath := filepath.Join(configDir, "Default", "Cookies")
	if _, err := os.Stat(cookiePath); err == nil {
		return filepath.Join(configDir, "Default")
	}
	// Try "Profile 1", "Profile 2", etc.
	entries, err := os.ReadDir(configDir)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		cp := filepath.Join(configDir, e.Name(), "Cookies")
		if _, err := os.Stat(cp); err == nil {
			return filepath.Join(configDir, e.Name())
		}
	}
	return ""
}

// adultSlowHosts: true "problematic" sites — lower concurrency, more retries, optional sleep.
// Do NOT put normal social/video hosts here (that only slows downloads).
var adultSlowHosts = []string{
	"pornhub.com", "xvideos.com", "xhamster.com", "xhamster.desi", "xhamster.one",
	"youporn.com", "redtube.com", "spankbang.com", "eporner.com",
	"tube8.com", "tnaflix.com", "keezmovies.com",
}

// headerOnlyHosts: full-speed downloads, but add Referer/Origin when helpful.
// Kept small — most sites work with yt-dlp defaults.
var headerOnlyHosts = []string{
	"twitter.com", "x.com", "instagram.com", "ddinstagram.com",
	"facebook.com", "fb.watch", "tiktok.com",
	"vimeo.com", "bilibili.com", "b23.tv", "nicovideo.jp",
	"vk.com", "ok.ru",
}

// firstURLHost returns the hostname of the first http(s) URL in s (lowercased, no www.).
func firstURLHost(s string) string {
	lower := strings.ToLower(s)
	idx := strings.Index(lower, "https://")
	if idx < 0 {
		idx = strings.Index(lower, "http://")
	}
	if idx < 0 {
		return ""
	}
	rest := lower[idx:]
	// strip scheme
	if i := strings.Index(rest, "://"); i >= 0 {
		rest = rest[i+3:]
	}
	// host ends at / ? # or space
	end := len(rest)
	for i, c := range rest {
		if c == '/' || c == '?' || c == '#' || c == ' ' || c == '\t' || c == '"' || c == '\'' {
			end = i
			break
		}
	}
	host := rest[:end]
	// strip userinfo and port
	if at := strings.LastIndex(host, "@"); at >= 0 {
		host = host[at+1:]
	}
	if colon := strings.Index(host, ":"); colon >= 0 {
		host = host[:colon]
	}
	return strings.TrimPrefix(host, "www.")
}

// hostMatchesDomain reports whether host is domain or a subdomain of domain.
// Avoids false positives like substring "x.com" inside unrelated hosts.
func hostMatchesDomain(host, domain string) bool {
	host = strings.TrimPrefix(strings.ToLower(host), "www.")
	domain = strings.TrimPrefix(strings.ToLower(domain), "www.")
	if host == "" || domain == "" {
		return false
	}
	return host == domain || strings.HasSuffix(host, "."+domain)
}

func urlMatchesAnyHost(raw string, domains []string) bool {
	host := firstURLHost(raw)
	if host == "" {
		return false
	}
	for _, d := range domains {
		if hostMatchesDomain(host, d) {
			return true
		}
	}
	return false
}

// IsAdultSlowSite is true for adult hosts that need reduced concurrency / retries.
func IsAdultSlowSite(url string) bool {
	return urlMatchesAnyHost(url, adultSlowHosts)
}

// NeedsSiteHeaders is true when extra Referer/Origin headers help (adult + a few picky hosts).
func NeedsSiteHeaders(url string) bool {
	return IsAdultSlowSite(url) || urlMatchesAnyHost(url, headerOnlyHosts)
}

// PreferHLSFormat is true only for adult sites where progressive HTTPS is often blocked.
func PreferHLSFormat(url string) bool {
	return IsAdultSlowSite(url)
}

// IsDirectMediaURL reports whether url looks like a progressive file/stream URL
// (mp4/m3u8/dload/…) rather than a site watch page. Used to skip broken site
// extractors and force the generic downloader.
func IsDirectMediaURL(raw string) bool {
	u := strings.ToLower(strings.TrimSpace(raw))
	if u == "" || (!strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://")) {
		return false
	}
	// Watch / site pages are never "direct files"
	if strings.Contains(u, "/watch?") || strings.Contains(u, "youtube.com/shorts/") {
		return false
	}
	// eporner HTML pages: /video-ID/slug  (dload paths are direct)
	if strings.Contains(u, "eporner.com/video-") && !strings.Contains(u, "/dload/") {
		return false
	}
	if strings.Contains(u, "/dload/") || strings.Contains(u, "/download/") {
		return true
	}
	if strings.Contains(u, ".m3u8") || strings.Contains(u, ".mpd") {
		return true
	}
	path := u
	if i := strings.Index(path, "?"); i >= 0 {
		path = path[:i]
	}
	if i := strings.Index(path, "#"); i >= 0 {
		path = path[:i]
	}
	for _, ext := range []string{".mp4", ".m4v", ".webm", ".mkv", ".mov", ".mp3", ".m4a", ".flac", ".ts"} {
		if strings.HasSuffix(path, ext) {
			return true
		}
	}
	return false
}

// isProblematicSite kept as alias for metadata path (headers only, not slow mode).
func isProblematicSite(url string) bool {
	return NeedsSiteHeaders(url)
}

// GetSiteHeaders returns yt-dlp args for site-specific Referer/Origin headers.
func GetSiteHeaders(url string) []string {
	return getSiteHeaders(url)
}

// getSiteHeaders returns yt-dlp args for site-specific headers.
func getSiteHeaders(url string) []string {
	if !NeedsSiteHeaders(url) {
		return nil
	}

	var args []string
	host := firstURLHost(url)

	// Extract origin from URL for generic Referer
	origin := ""
	if idx := strings.Index(url, "://"); idx != -1 {
		rest := url[idx+3:]
		if slashIdx := strings.Index(rest, "/"); slashIdx != -1 {
			origin = url[:idx+3+slashIdx]
		} else {
			origin = url
		}
	}

	switch {
	case hostMatchesDomain(host, "pornhub.com"):
		args = append(args, "--add-header", "Referer:https://www.pornhub.com/")
		args = append(args, "--add-header", "Origin:https://www.pornhub.com")
	case hostMatchesDomain(host, "xvideos.com"):
		args = append(args, "--add-header", "Referer:https://www.xvideos.com/")
		args = append(args, "--add-header", "Origin:https://www.xvideos.com")
	case hostMatchesDomain(host, "xhamster.com"), hostMatchesDomain(host, "xhamster.desi"), hostMatchesDomain(host, "xhamster.one"):
		ref := origin
		if ref == "" {
			ref = "https://xhamster.com"
		}
		args = append(args, "--add-header", "Referer:"+ref+"/")
		args = append(args, "--add-header", "Origin:"+ref)
	case hostMatchesDomain(host, "twitter.com"):
		args = append(args, "--add-header", "Referer:https://twitter.com/")
		args = append(args, "--add-header", "Origin:https://twitter.com")
	case hostMatchesDomain(host, "x.com"):
		args = append(args, "--add-header", "Referer:https://x.com/")
		args = append(args, "--add-header", "Origin:https://x.com")
	case hostMatchesDomain(host, "instagram.com"), hostMatchesDomain(host, "ddinstagram.com"):
		args = append(args, "--add-header", "Referer:https://www.instagram.com/")
		args = append(args, "--add-header", "Origin:https://www.instagram.com")
	case hostMatchesDomain(host, "facebook.com"), hostMatchesDomain(host, "fb.watch"):
		args = append(args, "--add-header", "Referer:https://www.facebook.com/")
		args = append(args, "--add-header", "Origin:https://www.facebook.com")
	case hostMatchesDomain(host, "tiktok.com"):
		args = append(args, "--add-header", "Referer:https://www.tiktok.com/")
		args = append(args, "--add-header", "Origin:https://www.tiktok.com")
	case hostMatchesDomain(host, "bilibili.com"), hostMatchesDomain(host, "b23.tv"):
		args = append(args, "--add-header", "Referer:https://www.bilibili.com/")
		args = append(args, "--add-header", "Origin:https://www.bilibili.com")
	case hostMatchesDomain(host, "nicovideo.jp"):
		args = append(args, "--add-header", "Referer:https://www.nicovideo.jp/")
	case hostMatchesDomain(host, "vimeo.com"):
		args = append(args, "--add-header", "Referer:https://vimeo.com/")
		args = append(args, "--add-header", "Origin:https://vimeo.com")
	case hostMatchesDomain(host, "vk.com"):
		args = append(args, "--add-header", "Referer:https://vk.com/")
		args = append(args, "--add-header", "Origin:https://vk.com")
	case hostMatchesDomain(host, "ok.ru"):
		args = append(args, "--add-header", "Referer:https://ok.ru/")
		args = append(args, "--add-header", "Origin:https://ok.ru")
	default:
		if IsAdultSlowSite(url) && origin != "" {
			args = append(args, "--add-header", "Referer:"+origin+"/")
			args = append(args, "--add-header", "Origin:"+origin)
		}
	}

	return args
}
