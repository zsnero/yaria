package daemon

import (
	"bufio"
	"bytes"
	"crypto/sha1"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
)

type managedDownload struct {
	entry         DownloadEntry
	paused        bool
	percent       float64
	speed         string
	eta           string
	state         string // "preparing", "downloading", "paused", "complete", "exists", "error"
	errMsg        string
	cmd           *exec.Cmd
	cancel        func() // kills the yt-dlp process
	alreadyExists bool   // yt-dlp reported file already downloaded
}

// Manager manages all background yt-dlp downloads
type Manager struct {
	mu        sync.Mutex
	downloads map[string]*managedDownload
	store     *StateStore
}

// NewManager creates a manager and restores saved downloads that were in-progress
func NewManager(store *StateStore) *Manager {
	m := &Manager{
		downloads: make(map[string]*managedDownload),
		store:     store,
	}
	// Restore only paused or in-progress downloads (completed ones are already removed from state)
	for _, entry := range store.GetDownloads() {
		md := &managedDownload{
			entry:  entry,
			paused: entry.Paused,
			state:  "preparing",
		}
		m.downloads[entry.ID] = md
		if entry.Paused {
			md.state = "paused"
		} else {
			go m.runDownload(md)
		}
	}
	return m
}

func makeID(url, dir string) string {
	h := sha1.Sum([]byte(url + "\x00" + dir))
	return fmt.Sprintf("%x", h[:8])
}

// Add adds a new download to the manager
func (m *Manager) Add(req Request) (string, error) {
	id := makeID(req.URL, req.Dir)

	m.mu.Lock()
	if existing, ok := m.downloads[id]; ok {
		// If the previous download finished or failed, allow re-downloading
		if existing.state == "complete" || existing.state == "exists" || existing.state == "error" {
			if existing.cancel != nil {
				existing.cancel()
			}
			delete(m.downloads, id)
		} else {
			// Still active (downloading/paused/preparing) - return existing
			m.mu.Unlock()
			return id, nil
		}
	}

	entry := DownloadEntry{
		ID:             id,
		URL:            req.URL,
		Title:          req.Title,
		Dir:            req.Dir,
		Paused:         false,
		IsAudioOnly:    req.IsAudioOnly,
		AudioFormat:    req.AudioFormat,
		Resolution:     req.Resolution,
		CookieBrowser:  req.CookieBrowser,
		UseAria2c:      req.UseAria2c,
		Aria2cArgs:     req.Aria2cArgs,
		OutputTemplate: req.OutputTemplate,
	}
	md := &managedDownload{
		entry: entry,
		state: "preparing",
	}
	m.downloads[id] = md
	m.mu.Unlock()

	m.store.AddDownload(entry)
	_ = m.store.Save()

	go m.runDownload(md)
	return id, nil
}

// Remove stops and removes a download
func (m *Manager) Remove(id string) error {
	m.mu.Lock()
	md, ok := m.downloads[id]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("download %s not found", id)
	}
	if md.cancel != nil {
		md.cancel()
	}
	delete(m.downloads, id)
	m.mu.Unlock()

	m.store.RemoveDownload(id)
	_ = m.store.Save()
	return nil
}

// Pause pauses a download
func (m *Manager) Pause(id string) error {
	m.mu.Lock()
	md, ok := m.downloads[id]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("download %s not found", id)
	}
	md.paused = true
	md.state = "paused"
	if md.cancel != nil {
		md.cancel()
		md.cancel = nil
	}
	m.mu.Unlock()

	m.store.SetPaused(id, true)
	_ = m.store.Save()
	return nil
}

// Resume resumes a paused download
func (m *Manager) Resume(id string) error {
	m.mu.Lock()
	md, ok := m.downloads[id]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("download %s not found", id)
	}
	if md.state == "complete" {
		m.mu.Unlock()
		return nil
	}
	md.paused = false
	md.state = "preparing"
	m.mu.Unlock()

	m.store.SetPaused(id, false)
	_ = m.store.Save()

	go m.runDownload(md)
	return nil
}

// List returns info about all downloads
func (m *Manager) List() []DownloadInfo {
	m.mu.Lock()
	defer m.mu.Unlock()

	var out []DownloadInfo
	for _, md := range m.downloads {
		out = append(out, DownloadInfo{
			ID:      md.entry.ID,
			URL:     md.entry.URL,
			Title:   md.entry.Title,
			Dir:     md.entry.Dir,
			State:   md.state,
			Percent: md.percent,
			Speed:   md.speed,
			ETA:     md.eta,
			Error:   md.errMsg,
		})
	}
	return out
}

// Close kills all running downloads
func (m *Manager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, md := range m.downloads {
		if md.cancel != nil {
			md.cancel()
		}
	}
}

// splitCRLF splits on \r or \n for real-time yt-dlp progress
func splitCRLF(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if i := bytes.IndexAny(data, "\r\n"); i >= 0 {
		if data[i] == '\r' {
			if i+1 < len(data) && data[i+1] == '\n' {
				return i + 2, data[:i], nil
			}
			return i + 1, data[:i], nil
		}
		return i + 1, data[:i], nil
	}
	if atEOF && len(data) > 0 {
		return len(data), data, nil
	}
	return 0, nil, nil
}

func (m *Manager) runDownload(md *managedDownload) {
	m.mu.Lock()
	if md.paused {
		md.state = "paused"
		m.mu.Unlock()
		return
	}
	md.state = "downloading"
	md.percent = 0
	m.mu.Unlock()

	ytDlpCmd := "yt-dlp"
	if runtime.GOOS == "windows" {
		ytDlpCmd = "yt-dlp.exe"
	}

	entry := md.entry

	cmdArgs := []string{
		"--no-overwrites",
		"--geo-bypass",
		"--no-check-certificate",
		"--concurrent-fragments", "32",
		"--buffer-size", "64K",
		"--http-chunk-size", "10M",
		"--newline",
		"--progress",
		"--no-color",
		"--extractor-retries", "2",
		"--fragment-retries", "3",
		"--progress-template", "%(progress)s %(progress._total_bytes_str)s %(progress._downloaded_bytes_str)s %(progress._speed_str)s %(progress._eta_str)s",
	}

	// Problematic sites get reduced concurrency
	problematicSites := []string{
		"pornhub.com", "xvideos.com", "xhamster.com", "youporn.com", "redtube.com",
		"spankbang.com", "eporner.com", "tube8.com", "tnaflix.com", "keezmovies.com",
		"twitter.com", "x.com", "instagram.com", "facebook.com", "tiktok.com",
		"vimeo.com", "dailymotion.com", "twitch.tv", "soundcloud.com",
		"reddit.com", "imgur.com", "giphy.com",
	}
	isProblematic := false
	for _, site := range problematicSites {
		if strings.Contains(entry.URL, site) {
			isProblematic = true
			break
		}
	}
	if isProblematic {
		cmdArgs = []string{
			"--no-overwrites",
			"--geo-bypass",
			"--no-check-certificate",
			"--concurrent-fragments", "8",
			"--buffer-size", "32K",
			"--http-chunk-size", "5M",
			"--newline",
			"--progress",
			"--no-color",
			"--extractor-retries", "5",
			"--fragment-retries", "10",
			"--retries", "10",
			"--retry-sleep", "5",
			"--progress-template", "%(progress)s %(progress._total_bytes_str)s %(progress._downloaded_bytes_str)s %(progress._speed_str)s %(progress._eta_str)s",
		}
	}

	// Ensure the base download directory exists
	os.MkdirAll(entry.Dir, 0755)

	// Output path - always download into a subfolder named after the video title
	// Use filepath.Join for the base dir, then append the yt-dlp template
	outputPath := filepath.Join(entry.Dir, "%(title)s", "%(title)s.%(ext)s")
	cmdArgs = append(cmdArgs, "--output", outputPath)

	if entry.CookieBrowser != "" {
		cmdArgs = append(cmdArgs, "--cookies-from-browser", entry.CookieBrowser)
	}

	cmdArgs = append(cmdArgs, "--user-agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")

	if entry.IsAudioOnly {
		af := entry.AudioFormat
		if af == "" {
			af = "mp3"
		}
		cmdArgs = append(cmdArgs, "--extract-audio", "--audio-format", af)
	} else {
		cmdArgs = append(cmdArgs, "--merge-output-format", "mp4", "--remux-video", "mp4")
		if entry.Resolution != "" {
			cmdArgs = append(cmdArgs, "--format", entry.Resolution+"+bestaudio/best")
		} else {
			cmdArgs = append(cmdArgs, "--format", "bestvideo+bestaudio/best")
		}
	}

	cmdArgs = append(cmdArgs, entry.URL)

	if entry.UseAria2c {
		aria2Cmd := "aria2c"
		if runtime.GOOS == "windows" {
			aria2Cmd = "aria2c.exe"
		}
		args := entry.Aria2cArgs
		if args == "" {
			args = "--max-connection-per-server=16 --min-split-size=1M --split=32 --max-concurrent-downloads=16 --file-allocation=none"
		}
		cmdArgs = append(cmdArgs, "--downloader", aria2Cmd, "--downloader-args", "aria2c:"+args)
	}

	cmd := exec.Command(ytDlpCmd, cmdArgs...)
	cmd.Env = append(os.Environ(), "PYTHONUNBUFFERED=1")

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		m.setError(md, err.Error())
		return
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		m.setError(md, err.Error())
		return
	}

	if err := cmd.Start(); err != nil {
		m.setError(md, err.Error())
		return
	}

	m.mu.Lock()
	md.cmd = cmd
	md.cancel = func() {
		_ = cmd.Process.Kill()
	}
	m.mu.Unlock()

	// Parse progress from both stdout and stderr
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); m.parseProgress(md, stdout) }()
	go func() { defer wg.Done(); m.parseProgress(md, stderr) }()
	wg.Wait()

	err = cmd.Wait()

	m.mu.Lock()
	md.cmd = nil
	md.cancel = nil
	if md.paused {
		// Was killed because of pause, not an error
		md.state = "paused"
		m.mu.Unlock()
		return
	}
	if err != nil {
		md.state = "error"
		md.errMsg = err.Error()
	} else if md.alreadyExists {
		md.state = "exists"
		md.percent = 100
	} else {
		md.state = "complete"
		md.percent = 100
	}
	m.mu.Unlock()

	// Remove finished downloads from persistent state so they don't
	// re-run on daemon restart. They stay in memory for the TUI to query.
	m.store.RemoveDownload(md.entry.ID)
	_ = m.store.Save()
}

func (m *Manager) setError(md *managedDownload, msg string) {
	m.mu.Lock()
	md.state = "error"
	md.errMsg = msg
	m.mu.Unlock()
}

func (m *Manager) parseProgress(md *managedDownload, reader interface{ Read([]byte) (int, error) }) {
	scanner := bufio.NewScanner(reader)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)
	scanner.Split(splitCRLF)

	ytdlpProgressRegex := regexp.MustCompile(`\[download\]\s+(\d+\.?\d*)%`)
	aria2cProgressRegex := regexp.MustCompile(`\((\d+)%\)`)
	speedRegex := regexp.MustCompile(`(?:DL:|at\s+)(\d+\.?\d*\w+/?s)`)
	etaRegex := regexp.MustCompile(`ETA[:\s]+(\S+)`)
	bytesProgressRegex := regexp.MustCompile(`([0-9.]+)\s*([kKmMgGtT]?i?B)/([0-9.]+)\s*([kKmMgGtT]?i?B)`)

	unitToMultiplier := func(unit string) float64 {
		switch strings.ToUpper(unit) {
		case "B":
			return 1
		case "KB":
			return 1e3
		case "KIB":
			return 1024
		case "MB":
			return 1e6
		case "MIB":
			return 1024 * 1024
		case "GB":
			return 1e9
		case "GIB":
			return 1024 * 1024 * 1024
		case "TB":
			return 1e12
		case "TIB":
			return 1024 * 1024 * 1024 * 1024
		}
		return 1
	}

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		// Detect "already been downloaded" or "already been recorded"
		if strings.Contains(line, "has already been downloaded") ||
			strings.Contains(line, "has already been recorded in the archive") {
			m.mu.Lock()
			md.alreadyExists = true
			m.mu.Unlock()
			continue
		}

		var percent float64
		var speed, eta string
		matched := false

		if matches := ytdlpProgressRegex.FindStringSubmatch(line); len(matches) >= 2 {
			percent, _ = strconv.ParseFloat(matches[1], 64)
			matched = true
		} else if matches := aria2cProgressRegex.FindStringSubmatch(line); len(matches) >= 2 {
			percent, _ = strconv.ParseFloat(matches[1], 64)
			matched = true
		} else if bm := bytesProgressRegex.FindStringSubmatch(line); len(bm) == 5 {
			cur, _ := strconv.ParseFloat(bm[1], 64)
			tot, _ := strconv.ParseFloat(bm[3], 64)
			mu := unitToMultiplier(bm[2])
			mt := unitToMultiplier(bm[4])
			if mt > 0 && tot > 0 {
				percent = (cur * mu) / (tot * mt) * 100.0
				matched = true
			}
		}

		if matched {
			if sm := speedRegex.FindStringSubmatch(line); len(sm) >= 2 {
				speed = sm[1]
			}
			if em := etaRegex.FindStringSubmatch(line); len(em) >= 2 {
				eta = em[1]
			}
			m.mu.Lock()
			md.percent = percent
			md.speed = speed
			md.eta = eta
			if md.state != "paused" {
				md.state = "downloading"
			}
			m.mu.Unlock()
		}
	}
}
