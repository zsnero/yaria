package daemon

import (
	"crypto/sha1"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"yaria/internal/yaria/cookies"
	"yaria/internal/yaria/downloader"

	"github.com/creack/pty"
	"yaria/internal/yaria/procexec"
)

type managedDownload struct {
	entry         DownloadEntry
	paused        bool
	percent       float64
	speed         string
	eta           string
	state         string // "preparing", "downloading", "paused", "complete", "exists", "error"
	errMsg        string
	statusMsg     string // current activity: "Extracting URL...", "Solving JS challenges...", etc.
	cmd           *exec.Cmd
	cancel        func() // kills the yt-dlp process
	alreadyExists bool   // yt-dlp reported file already downloaded
	addedAt       time.Time
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
	for i, entry := range store.GetDownloads() {
		md := &managedDownload{
			entry:   entry,
			paused:  entry.Paused,
			state:   "preparing",
			addedAt: time.Now().Add(time.Duration(i) * time.Millisecond), // preserve restore order
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
		entry:   entry,
		state:   "preparing",
		addedAt: time.Now(),
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

// List returns info about all downloads, sorted newest first.
func (m *Manager) List() []DownloadInfo {
	m.mu.Lock()
	defer m.mu.Unlock()

	type indexedInfo struct {
		info    DownloadInfo
		addedAt time.Time
	}
	items := make([]indexedInfo, 0, len(m.downloads))
	for _, md := range m.downloads {
		items = append(items, indexedInfo{
			info: DownloadInfo{
				ID:        md.entry.ID,
				URL:       md.entry.URL,
				Title:     md.entry.Title,
				Dir:       md.entry.Dir,
				State:     md.state,
				Percent:   md.percent,
				Speed:     md.speed,
				ETA:       md.eta,
				Error:     md.errMsg,
				StatusMsg: md.statusMsg,
			},
			addedAt: md.addedAt,
		})
	}

	// Sort newest first for stable UI ordering (map iteration is random).
	sort.Slice(items, func(i, j int) bool {
		return items[i].addedAt.After(items[j].addedAt)
	})

	out := make([]DownloadInfo, len(items))
	for i, item := range items {
		out[i] = item.info
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
	}

	// Slow mode only for adult hosts; social/normal keep full speed
	if downloader.IsAdultSlowSite(entry.URL) {
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
			"--sleep-interval", "1",
			"--max-sleep-interval", "3",
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

	// Cookie handling: kooky first, yt-dlp fallback
	cookieBrowser := entry.CookieBrowser
	if cookieBrowser == "" {
		cookieBrowser = downloader.DetectBrowser()
	}
	cookieArgs := cookies.GetYTDLPCookieArgs(entry.URL, cookieBrowser)
	cmdArgs = append(cmdArgs, cookieArgs...)

	if downloader.NeedsSiteHeaders(entry.URL) {
		cmdArgs = append(cmdArgs, "--add-header", "Accept-Language:en-US,en;q=0.9")
		cmdArgs = append(cmdArgs, downloader.GetSiteHeaders(entry.URL)...)
	}

	cmdArgs = append(cmdArgs, entry.URL)

	if entry.UseAria2c {
		aria2Cmd := "aria2c"
		if runtime.GOOS == "windows" {
			aria2Cmd = "aria2c.exe"
		}
		args := entry.Aria2cArgs
		if args == "" {
			args = "--max-connection-per-server=16 --min-split-size=1M --split=32 --max-concurrent-downloads=16 --file-allocation=none --summary-interval=1"
		}
		cmdArgs = append(cmdArgs, "--downloader", aria2Cmd, "--downloader-args", "aria2c:"+args)
	}

	cmd := exec.Command(ytDlpCmd, cmdArgs...)
	procexec.HideConsole(cmd)
	cmd.Env = append(os.Environ(), "PYTHONUNBUFFERED=1", "TERM=dumb")

	// Use a PTY (pseudo-terminal) so aria2c and yt-dlp think they're
	// writing to a real terminal. This disables their pipe buffering
	// and gives us real-time progress output.
	ptmx, err := pty.Start(cmd)
	if err != nil {
		// Fallback to pipes if PTY fails (e.g., on Windows)
		cmd2 := exec.Command(ytDlpCmd, cmdArgs...)
		procexec.HideConsole(cmd2)
		cmd2.Env = cmd.Env
		stdout, err2 := cmd2.StdoutPipe()
		if err2 != nil {
			m.setError(md, err2.Error())
			return
		}
		stderr, err2 := cmd2.StderrPipe()
		if err2 != nil {
			m.setError(md, err2.Error())
			return
		}
		if err2 := cmd2.Start(); err2 != nil {
			m.setError(md, err2.Error())
			return
		}
		cmd = cmd2
		m.mu.Lock()
		md.cmd = cmd
		md.cancel = func() { _ = cmd.Process.Kill() }
		m.mu.Unlock()
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); m.parseProgress(md, stdout) }()
		go func() { defer wg.Done(); m.parseProgress(md, stderr) }()
		wg.Wait()
	} else {
		m.mu.Lock()
		md.cmd = cmd
		md.cancel = func() { _ = cmd.Process.Kill() }
		m.mu.Unlock()

		// PTY merges stdout+stderr into a single stream
		m.parseProgress(md, ptmx)
		ptmx.Close()
	}

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
	ytdlpProgressRegex := regexp.MustCompile(`\[download\]\s+(\d+\.?\d*)%`)
	// aria2c format: [#hexid SIZE/TOTAL(PERCENT%) CN:N DL:SPEED ETA:TIME]
	aria2cProgressRegex := regexp.MustCompile(`\((\d+)%\)`)
	aria2cFullRegex := regexp.MustCompile(`\[#[0-9a-f]+\s+.*?\((\d+)%\).*?DL:([0-9.]+\S+)(?:.*?ETA:(\S+))?`)
	speedRegex := regexp.MustCompile(`(?:DL:|at\s+)(\d+\.?\d*\S+/s)`)
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

	// Read byte-by-byte to avoid pipe buffering issues on Linux.
	// bufio.Scanner relies on the OS delivering data promptly, but Linux
	// pipe buffers (4-64KB) hold yt-dlp's \r-based progress until enough
	// bytes accumulate. Reading one byte at a time and splitting on \r or
	// \n gives real-time progress updates.
	var lineBuf []byte
	oneByte := make([]byte, 1)

	processLine := func(line string) {
		if line == "" {
			return
		}

		if strings.Contains(line, "has already been downloaded") ||
			strings.Contains(line, "has already been recorded in the archive") {
			m.mu.Lock()
			md.alreadyExists = true
			m.mu.Unlock()
			return
		}

		var percent float64
		var speed, eta string
		matched := false

		// Try aria2c full format first: [#hex SIZE/TOTAL(PCT%) CN:N DL:SPEED ETA:TIME]
		if am := aria2cFullRegex.FindStringSubmatch(line); len(am) >= 2 {
			percent, _ = strconv.ParseFloat(am[1], 64)
			if len(am) >= 3 && am[2] != "" {
				speed = am[2] + "/s"
			}
			if len(am) >= 4 && am[3] != "" {
				eta = am[3]
			}
			matched = true
		} else if matches := ytdlpProgressRegex.FindStringSubmatch(line); len(matches) >= 2 {
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
			// For non-aria2c lines, try to extract speed/eta separately
			if speed == "" {
				if sm := speedRegex.FindStringSubmatch(line); len(sm) >= 2 {
					speed = sm[1]
				}
			}
			if eta == "" {
				if em := etaRegex.FindStringSubmatch(line); len(em) >= 2 {
					eta = em[1]
				}
			}
			m.mu.Lock()
			md.percent = percent
			md.speed = speed
			md.eta = eta
			md.statusMsg = ""
			if md.state != "paused" {
				md.state = "downloading"
			}
			m.mu.Unlock()
			return
		}

		// Capture informational status messages from yt-dlp
		m.mu.Lock()
		if strings.Contains(line, "Extracting URL") {
			md.statusMsg = "Extracting video info..."
			md.state = "preparing"
		} else if strings.Contains(line, "Downloading webpage") {
			md.statusMsg = "Downloading webpage..."
			md.state = "preparing"
		} else if strings.Contains(line, "Solving JS challenges") || strings.Contains(line, "jsc:deno") {
			md.statusMsg = "Solving JS challenges..."
			md.state = "preparing"
		} else if strings.Contains(line, "Downloading player") {
			md.statusMsg = "Downloading player..."
			md.state = "preparing"
		} else if strings.Contains(line, "player API") || strings.Contains(line, "API JSON") {
			md.statusMsg = "Fetching player API..."
			md.state = "preparing"
		} else if strings.Contains(line, "m3u8 information") {
			md.statusMsg = "Fetching stream info..."
			md.state = "preparing"
		} else if strings.Contains(line, "Downloading") && strings.Contains(line, "format") {
			md.statusMsg = "Preparing download..."
			md.state = "preparing"
		} else if strings.Contains(line, "Destination:") {
			md.statusMsg = "Starting download..."
			md.state = "downloading"
		} else if strings.Contains(line, "Extracting cookies") || strings.Contains(line, "Extracted") {
			md.statusMsg = "Extracting cookies..."
			md.state = "preparing"
		} else if strings.Contains(line, "[Merger]") {
			md.statusMsg = "Merging video + audio..."
			md.state = "downloading"
			md.percent = 99
		} else if strings.Contains(line, "Deleting original") {
			md.statusMsg = "Cleaning up..."
		}
		m.mu.Unlock()
	}

	for {
		n, err := reader.Read(oneByte)
		if n == 1 {
			b := oneByte[0]
			if b == '\r' || b == '\n' {
				if len(lineBuf) > 0 {
					processLine(string(lineBuf))
					lineBuf = lineBuf[:0]
				}
			} else {
				lineBuf = append(lineBuf, b)
			}
		}
		if err != nil {
			// Process any remaining data
			if len(lineBuf) > 0 {
				processLine(string(lineBuf))
			}
			break
		}
	}
}
