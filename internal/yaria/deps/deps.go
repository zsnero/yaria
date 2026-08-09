package deps

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

	"yaria/internal/yaria/downloader"

	"github.com/google/go-github/v62/github"
)

// Dep represents a single dependency with its status
type Dep struct {
	Name    string // display name
	Binary  string // binary name to look for
	Status  string // "installed", "missing", "updating", "updated", "failed"
	Path    string // where it was found or installed
	Message string // status/error message
}

// DepsDir returns the dependencies directory next to the executable
func DepsDir() string {
	exePath, err := os.Executable()
	if err != nil {
		exePath, _ = os.Getwd()
	}
	return filepath.Join(filepath.Dir(exePath), "dependencies")
}

// CheckAll checks the status of all dependencies
func CheckAll() []Dep {
	depsDir := DepsDir()
	return []Dep{
		checkYtDlp(depsDir),
		checkAria2c(depsDir),
		checkDeno(depsDir),
		checkYazi(depsDir),
		checkWebtorrent(depsDir),
	}
}

// UpdateAll downloads missing or updates existing dependencies.
// It calls progressFn after each dependency is processed.
func UpdateAll(progressFn func(name, status, msg string)) []Dep {
	depsDir := DepsDir()
	_ = os.MkdirAll(depsDir, 0755)

	results := []Dep{
		installYtDlp(depsDir, progressFn),
		installAria2c(depsDir, progressFn),
		installDeno(depsDir, progressFn),
		installYazi(depsDir, progressFn),
		installWebtorrent(depsDir, progressFn),
	}
	return results
}

// ── Check helpers ───────────────────────────────────────────────────

func binaryName(base string) string {
	if runtime.GOOS == "windows" {
		return base + ".exe"
	}
	return base
}

func findBinary(name, depsDir string) (string, bool) {
	// Check system PATH first
	if p, err := exec.LookPath(name); err == nil {
		return p, true
	}
	// Check deps dir
	p := filepath.Join(depsDir, name)
	if _, err := os.Stat(p); err == nil {
		return p, true
	}
	return "", false
}

func checkYtDlp(depsDir string) Dep {
	bin := binaryName("yt-dlp")
	if p, ok := findBinary(bin, depsDir); ok {
		return Dep{Name: "yt-dlp", Binary: bin, Status: "installed", Path: p}
	}
	return Dep{Name: "yt-dlp", Binary: bin, Status: "missing"}
}

func checkAria2c(depsDir string) Dep {
	bin := binaryName("aria2c")
	if p, ok := findBinary(bin, depsDir); ok {
		return Dep{Name: "aria2c", Binary: bin, Status: "installed", Path: p}
	}
	return Dep{Name: "aria2c", Binary: bin, Status: "missing"}
}

func checkDeno(depsDir string) Dep {
	bin := binaryName("deno")
	if p, ok := findBinary(bin, depsDir); ok {
		return Dep{Name: "deno", Binary: bin, Status: "installed", Path: p}
	}
	return Dep{Name: "deno", Binary: bin, Status: "missing"}
}

func checkYazi(depsDir string) Dep {
	bin := binaryName("yazi")
	if p, ok := findBinary(bin, depsDir); ok {
		return Dep{Name: "yazi", Binary: bin, Status: "installed", Path: p}
	}
	return Dep{Name: "yazi", Binary: bin, Status: "missing"}
}

func checkWebtorrent(depsDir string) Dep {
	wt := "webtorrent"
	if runtime.GOOS == "windows" {
		wt = "webtorrent.cmd"
	}
	if p, err := exec.LookPath("webtorrent"); err == nil {
		return Dep{Name: "webtorrent-cli", Binary: wt, Status: "installed", Path: p}
	}
	binPath := filepath.Join(depsDir, "bin", wt)
	if _, err := os.Stat(binPath); err == nil {
		return Dep{Name: "webtorrent-cli", Binary: wt, Status: "installed", Path: binPath}
	}
	return Dep{Name: "webtorrent-cli", Binary: wt, Status: "missing"}
}

// ── Install/Update helpers ──────────────────────────────────────────

func installYtDlp(depsDir string, progress func(string, string, string)) Dep {
	name := "yt-dlp"
	bin := binaryName("yt-dlp")
	dest := filepath.Join(depsDir, bin)

	progress(name, "updating", "Fetching latest release...")

	client := github.NewClient(nil)
	release, _, err := client.Repositories.GetLatestRelease(context.Background(), "yt-dlp", "yt-dlp")
	if err != nil {
		return depFailed(name, bin, progress, fmt.Sprintf("Failed to fetch release: %v", err))
	}

	var downloadURL string
	for _, asset := range release.Assets {
		if asset.GetName() == bin {
			downloadURL = asset.GetBrowserDownloadURL()
			break
		}
	}
	if downloadURL == "" {
		return depFailed(name, bin, progress, "No suitable binary found for this platform")
	}

	progress(name, "updating", "Downloading...")
	if err := downloadFile(downloadURL, dest); err != nil {
		return depFailed(name, bin, progress, fmt.Sprintf("Download failed: %v", err))
	}
	chmodExec(dest)
	progress(name, "updated", "OK")
	return Dep{Name: name, Binary: bin, Status: "updated", Path: dest, Message: "OK"}
}

func installAria2c(depsDir string, progress func(string, string, string)) Dep {
	name := "aria2c"
	bin := binaryName("aria2c")
	dest := filepath.Join(depsDir, bin)

	progress(name, "updating", "Downloading portable aria2...")
	logw := &progressWriter{fn: progress, name: name}
	path, err := downloader.EnsureAria2(depsDir, logw)
	if err != nil {
		return depFailed(name, bin, progress, err.Error())
	}
	if path != "" {
		dest = path
	}
	progress(name, "updated", "OK")
	return Dep{Name: name, Binary: bin, Status: "updated", Path: dest, Message: "OK"}
}

// progressWriter adapts deps progress callbacks to io.Writer for EnsureAria2.
type progressWriter struct {
	fn   func(name, status, msg string)
	name string
}

func (w *progressWriter) Write(p []byte) (int, error) {
	msg := strings.TrimSpace(string(p))
	if msg != "" && w.fn != nil {
		w.fn(w.name, "updating", msg)
	}
	return len(p), nil
}

func installDeno(depsDir string, progress func(string, string, string)) Dep {
	name := "deno"
	bin := binaryName("deno")
	dest := filepath.Join(depsDir, bin)
	zipPath := filepath.Join(depsDir, "deno.zip")

	progress(name, "updating", "Determining download URL...")

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
		return depFailed(name, bin, progress, "Unsupported platform")
	}

	progress(name, "updating", "Downloading...")
	if err := downloadFile(denoURL, zipPath); err != nil {
		return depFailed(name, bin, progress, fmt.Sprintf("Download failed: %v", err))
	}
	defer os.Remove(zipPath)

	progress(name, "updating", "Extracting...")
	if err := extractBinaryFromZip(zipPath, dest, "deno"); err != nil {
		return depFailed(name, bin, progress, fmt.Sprintf("Extract failed: %v", err))
	}
	chmodExec(dest)
	progress(name, "updated", "OK")
	return Dep{Name: name, Binary: bin, Status: "updated", Path: dest, Message: "OK"}
}

func installYazi(depsDir string, progress func(string, string, string)) Dep {
	name := "yazi"
	bin := binaryName("yazi")
	dest := filepath.Join(depsDir, bin)
	zipPath := filepath.Join(depsDir, "yazi.zip")

	progress(name, "updating", "Determining download URL...")

	// Build yazi download URL based on platform
	var yaziURL string
	goos := runtime.GOOS
	goarch := runtime.GOARCH

	switch {
	case goos == "linux" && goarch == "amd64":
		yaziURL = "https://github.com/sxyazi/yazi/releases/latest/download/yazi-x86_64-unknown-linux-gnu.zip"
	case goos == "linux" && goarch == "arm64":
		yaziURL = "https://github.com/sxyazi/yazi/releases/latest/download/yazi-aarch64-unknown-linux-gnu.zip"
	case goos == "darwin" && goarch == "amd64":
		yaziURL = "https://github.com/sxyazi/yazi/releases/latest/download/yazi-x86_64-apple-darwin.zip"
	case goos == "darwin" && goarch == "arm64":
		yaziURL = "https://github.com/sxyazi/yazi/releases/latest/download/yazi-aarch64-apple-darwin.zip"
	case goos == "windows" && goarch == "amd64":
		yaziURL = "https://github.com/sxyazi/yazi/releases/latest/download/yazi-x86_64-pc-windows-msvc.zip"
	case goos == "windows" && goarch == "arm64":
		yaziURL = "https://github.com/sxyazi/yazi/releases/latest/download/yazi-aarch64-pc-windows-msvc.zip"
	default:
		return depFailed(name, bin, progress, "Unsupported platform")
	}

	progress(name, "updating", "Downloading...")
	if err := downloadFile(yaziURL, zipPath); err != nil {
		return depFailed(name, bin, progress, fmt.Sprintf("Download failed: %v", err))
	}
	defer os.Remove(zipPath)

	progress(name, "updating", "Extracting...")
	if err := extractBinaryFromZip(zipPath, dest, "yazi"); err != nil {
		return depFailed(name, bin, progress, fmt.Sprintf("Extract failed: %v", err))
	}
	chmodExec(dest)
	progress(name, "updated", "OK")
	return Dep{Name: name, Binary: bin, Status: "updated", Path: dest, Message: "OK"}
}

func installWebtorrent(depsDir string, progress func(string, string, string)) Dep {
	name := "webtorrent-cli"
	wt := "webtorrent"
	if runtime.GOOS == "windows" {
		wt = "webtorrent.cmd"
	}

	progress(name, "updating", "Checking npm...")

	npmPath, err := exec.LookPath("npm")
	if err != nil {
		return depFailed(name, wt, progress, "npm not found - install Node.js to enable torrent streaming")
	}
	_ = npmPath

	progress(name, "updating", "Installing via npm...")
	cmd := exec.Command("npm", "install", "-g", "--prefix", depsDir, "webtorrent-cli")
	output, err := cmd.CombinedOutput()
	if err != nil {
		msg := fmt.Sprintf("npm install failed: %v", err)
		if len(output) > 0 {
			lines := strings.Split(string(output), "\n")
			if len(lines) > 0 {
				msg += " - " + lines[len(lines)-1]
			}
		}
		return depFailed(name, wt, progress, msg)
	}

	progress(name, "updated", "OK")
	return Dep{Name: name, Binary: wt, Status: "updated", Path: filepath.Join(depsDir, "bin", wt), Message: "OK"}
}

// ── Utility functions ───────────────────────────────────────────────

func depFailed(name, bin string, progress func(string, string, string), msg string) Dep {
	progress(name, "failed", msg)
	return Dep{Name: name, Binary: bin, Status: "failed", Message: msg}
}

func downloadFile(url, dest string) error {
	resp, err := http.Get(url)
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
	_, err = io.Copy(out, resp.Body)
	out.Close()
	return err
}

func chmodExec(path string) {
	if runtime.GOOS != "windows" {
		_ = os.Chmod(path, 0755)
	}
}

func extractBinaryFromZip(zipPath, dest, binaryBaseName string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer r.Close()

	binName := binaryBaseName
	if runtime.GOOS == "windows" {
		binName += ".exe"
	}

	for _, f := range r.File {
		name := filepath.Base(f.Name)
		if name == binName {
			rc, err := f.Open()
			if err != nil {
				return err
			}
			defer rc.Close()
			out, err := os.Create(dest)
			if err != nil {
				return err
			}
			_, err = io.Copy(out, rc)
			out.Close()
			return err
		}
	}
	return errors.New(binaryBaseName + " binary not found in zip archive")
}
