package tui

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"math/rand"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"yaria/config"
	"yaria/downloader"
	"yaria/logger"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"golang.org/x/term"
)

type state int

const (
	urlState state = iota
	metadataLoadingState
	browserSelectionState
	formatState
	resolutionState
	confirmationState
	formatsLoadingState
	downloadingState
	downloadCompleteState
)

type Model struct {
	cfg               *config.Config
	log               logger.Logger
	dl                downloader.Downloader
	state             state
	url               string
	Title             string
	formats           []downloader.Format
	videoFormats      []downloader.Format
	cursor            int
	choices           []string
	Confirmed         bool
	rainbowOffset     int    // For rainbow animation
	currentQuote      string // Current funny quote
	rabbitFrame       int    // Current rabbit animation frame
	URL               string
	urlInput          string
	loadingStart      time.Time
	loadingDots       string
	errorMsg          string
	PlaylistInfo      string
	availableBrowsers []string
	needsBrowser      bool
	downloadProgress  string
	downloadPercent   float64
	downloadSpeed     string
	downloadETA       string
	downloadComplete  bool
	downloadError     string
	TempDir           string
	Args              []string
}

// Splits on either '\r' or '\n' so we capture carriage-return progress updates
func splitCRLF(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if i := bytes.IndexAny(data, "\r\n"); i >= 0 {
		// handle CRLF (\r\n)
		if data[i] == '\r' {
			if i+1 < len(data) && data[i+1] == '\n' {
				return i + 2, data[:i], nil
			}
			return i + 1, data[:i], nil
		}
		// LF
		return i + 1, data[:i], nil
	}
	if atEOF && len(data) > 0 {
		return len(data), data, nil
	}
	return 0, nil, nil
}

func New(cfg *config.Config, log logger.Logger) *Model {
	return &Model{
		cfg:           cfg,
		log:           log,
		state:         urlState,
		rainbowOffset: 0,
		currentQuote:  getRandomQuote(),
		rabbitFrame:   0,
		choices: []string{
			"Video (with audio)",
			"Audio only",
		},
	}
}

func (m *Model) SetDownloader(dl downloader.Downloader) {
	m.dl = dl
}

func (m *Model) Run(url, title string) error {
	m.url = url
	m.Title = title
	if url != "" {
		m.state = formatState // Skip URL input if provided
	}
	p := tea.NewProgram(m, tea.WithInputTTY())
	_, err := p.Run()
	return err
}

func (m *Model) RunDownloadOnly() error {
	// Start directly in downloading state
	m.state = downloadingState
	p := tea.NewProgram(m, tea.WithInputTTY())
	_, err := p.Run()
	return err
}

func (m *Model) Init() tea.Cmd {
	if m.state == formatState && m.url != "" {
		return m.startLoading
	}
	if m.state == downloadingState {
		return m.startDownload()
	}
	return nil
}

func (m *Model) startLoading() tea.Msg {
	return tickMsg{}
}

type tickMsg struct{}

type metadataFetchedMsg struct {
	playlistInfo string
	title        string
	err          error
}

type formatsFetchedMsg struct {
	formats []downloader.Format
	err     error
}

type browsersDetectedMsg struct {
	browsers []string
}

type downloadProgressMsg struct {
	progress string
	percent  float64
	speed    string
	eta      string
}

type downloadCompleteMsg struct {
	success bool
	err     error
}

type rainbowAnimMsg struct{}

// Collection of funny quotes inspired by Minecraft splash texts
var quotes = []string{
	"More pixels than reality!",
	"Download at the speed of light!",
	"Powered by rainbows and memes",
	"100% organic video downloader",
	"Made with actual rainbows",
	"Faster than a speeding bullet!",
	"Downloads videos, makes coffee",
	"Internet's favorite downloader",
	"Warning: May cause addiction",
	"Now with 200% more rainbows",
	"Downloads cat videos exclusively",
	"Powered by unicorn tears",
	"Internet speed: Over 9000!",
	"Downloads in 4K, dreams in 8K",
	"More colors than a rainbow",
	"Faster than your internet",
	"Downloads everything, even your will to live",
	"Now with extra glitter",
	"Internet's best kept secret",
	"Downloads faster than light",
	"Rainbows included by default",
	"Warning: Contains awesome",
	"More powerful than a locomotive",
	"Downloads videos, saves souls",
	"Internet's chosen one",
	"Powered by pure magic",
	"Faster than a cheetah on steroids",
	"Downloads everything, regrets nothing",
	"Now with 50% more memes",
	"Internet's favorite time waster",
	"Downloads at warp speed",
	"More powerful than your computer",
	"Warning: May break the internet",
	"Downloads videos, fixes life",
	"Now with 100% more awesome",
	"Internet's secret weapon",
	"Powered by dragon fire",
	"Faster than your WiFi bill",
	"Downloads everything, especially your free time",
	"Now with extra sparkles",
	"Internet's most wanted",
	"Downloads at the speed of memes",
	"More powerful than a superhero",
	"Warning: Contains unlimited entertainment",
	"Downloads videos, makes you happy",
	"Now with 200% more glitter",
	"Internet's fastest downloader",
	"Powered by pure awesomeness",
	"Faster than your attention span",
	"Downloads everything, even your homework",
	"Now with 100% more rainbows",
	"Internet's best friend",
	"Downloads at light speed",
	"More powerful than your mom's WiFi",
	"Warning: May cause extreme happiness",
	"Downloads videos, saves the day",
	"Now with extra magic",
	"Internet's chosen downloader",
	"Powered by love and rainbows",
	"Faster than your ex's text back",
	"Downloads everything, especially cat videos",
	"Now with 50% more sparkles",
	"Internet's most loved",
	"Downloads at meme speed",
	"More powerful than a tank",
	"Warning: Contains unlimited fun",
	"Downloads videos, makes life better",
	"Now with 100% more love",
	"Internet's hero",
	"Downloads at quantum speed",
	"More powerful than your will to study",
	"Warning: May break your productivity",
	"Downloads videos, fixes boredom",
	"Now with extra unicorns",
	"Internet's legend",
	"Downloads at rainbow speed",
	"More powerful than your dad's jokes",
	"Warning: Contains epic downloads",
	"Downloads videos, makes dreams come true",
	"Now with 200% more magic",
	"Internet's champion",
	"Powered by pure rainbows",
	"Faster than your last relationship",
	"Downloads everything, even your sanity",
	"Now with 100% more unicorns",
	"Internet's favorite child",
	"Downloads at god speed",
	"More powerful than your credit card",
	"Warning: May cause addiction to downloading",
	"Downloads videos, saves the world",
	"Now with extra dragons",
	"Internet's savior",
	"Downloads at lightning speed",
	"More powerful than your WiFi password",
	"Warning: Contains extreme awesomeness",
	"Downloads videos, makes legends",
	"Now with 50% more dragons",
	"Internet's myth",
	"Downloads at rainbow warrior speed",
	"More powerful than your phone battery",
	"Warning: May break the space-time continuum",
	"Downloads videos, creates universes",
	"Now with 100% more dragons",
	"Internet's deity",
	"Downloads at infinite speed",
	"More powerful than your imagination",
	"Warning: Contains unlimited power",
	"Downloads videos, becomes legendary",
	"Now with extra phoenix tears",
	"Internet's creator",
	"Downloads at impossible speed",
	"More powerful than your dreams",
	"Warning: May bend reality",
	"Downloads videos, transcends dimensions",
	"Now with 200% more phoenix tears",
	"Internet's god",
	"Downloads at transcendental speed",
	"More powerful than existence itself",
	"Warning: Contains the meaning of life",
	"Downloads videos, achieves enlightenment",
	"Now with 100% more enlightenment",
	"Internet's everything",
	"Downloads at the speed of thought",
	"More powerful than the universe",
	"Warning: May create new realities",
	"Downloads videos, becomes one with the code",
	"Now with extra cosmic energy",
	"Internet's final form",
	"Downloads at the speed of creation",
	"More powerful than time itself",
	"Warning: Contains the source code of the universe",
	"Downloads videos, becomes the download",
	"Now with 100% cosmic power",
}

// getRandomQuote returns a random funny quote
func getRandomQuote() string {
	rand.Seed(time.Now().UnixNano())
	return quotes[rand.Intn(len(quotes))]
}

// Rabbit running animation frames
var rabbitFrames = []string{
	"  /\\_/\\  \n ( o.o ) \n  > ^ <  ",
	"  /\\_/\\  \n ( •.• ) \n  > ^ <  ",
	"  /\\_/\\  \n ( o.o ) \n  > ^ <  ",
	"  /\\_/\\  \n ( •.• ) \n  > ^ <  ",
	"  /\\_/\\  \n ( o.o ) \n  > ^ <  ",
	"  /\\_/\\  \n ( •.• ) \n  > ^ <  ",
	"  /\\_/\\  \n ( o.o ) \n  > ^ <  ",
}

// getRabbitFrame returns the current rabbit animation frame
func getRabbitFrame(frame int) string {
	return rabbitFrames[frame%len(rabbitFrames)]
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Handle rainbow animation
	switch msg.(type) {
	case tea.WindowSizeMsg:
		return m, func() tea.Msg { return rainbowAnimMsg{} }
	case rainbowAnimMsg:
		m.rainbowOffset = (m.rainbowOffset + 5) % 360
		m.rabbitFrame = (m.rabbitFrame + 1) % len(rabbitFrames)
		return m, tea.Tick(time.Millisecond*100, func(t time.Time) tea.Msg { return rainbowAnimMsg{} })
	}

	// Start animation on first update if not already started
	if m.rainbowOffset == 0 {
		return m, func() tea.Msg { return rainbowAnimMsg{} }
	}

	switch m.state {
	case urlState:
		return m.updateURL(msg)
	case metadataLoadingState:
		return m.updateMetadataLoading(msg)
	case browserSelectionState:
		return m.updateBrowserSelection(msg)
	case formatState:
		return m.updateFormat(msg)
	case resolutionState:
		return m.updateResolution(msg)
	case confirmationState:
		return m.updateConfirmation(msg)
	case formatsLoadingState:
		return m.updateFormatsLoading(msg)
	case downloadingState:
		return m.updateDownloading(msg)
	case downloadCompleteState:
		return m.updateDownloadComplete(msg)
	}
	return m, nil
}

func (m *Model) updateURL(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyEnter:
			m.URL = strings.TrimSpace(m.urlInput)
			if m.URL == "" {
				m.errorMsg = "No URL provided"
				return m, tea.Quit
			}
			m.url = m.URL
			m.state = metadataLoadingState
			m.loadingStart = time.Now()
			m.loadingDots = "."
			return m, tea.Batch(
				m.fetchMetadata(),
				tea.Tick(time.Millisecond*500, func(t time.Time) tea.Msg {
					return tickMsg{}
				}),
			)
		case tea.KeyCtrlC, tea.KeyEsc:
			return m, tea.Quit
		case tea.KeyRunes:
			m.urlInput += string(msg.Runes)
		case tea.KeyBackspace:
			if len(m.urlInput) > 0 {
				m.urlInput = m.urlInput[:len(m.urlInput)-1]
			}
		}
	}
	return m, nil
}

func (m *Model) fetchMetadata() tea.Cmd {
	return func() tea.Msg {
		playlistInfo, title, err := m.dl.GetMetadata([]string{m.url})
		return metadataFetchedMsg{playlistInfo: playlistInfo, title: title, err: err}
	}
}

// Checks which supported browsers are available
func detectBrowsers() []string {
	var browsers []string
	supportedBrowsers := []string{"firefox", "chrome", "chromium", "brave", "edge", "opera", "safari"}

	for _, browser := range supportedBrowsers {
		var cmd *exec.Cmd
		if runtime.GOOS == "windows" {
			cmd = exec.Command("where", browser)
		} else {
			cmd = exec.Command("which", browser)
		}
		if err := cmd.Run(); err == nil {
			browsers = append(browsers, browser)
		}
	}
	return browsers
}

func (m *Model) detectBrowsersAsync() tea.Cmd {
	return func() tea.Msg {
		return browsersDetectedMsg{browsers: detectBrowsers()}
	}
}

func (m *Model) updateMetadataLoading(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case metadataFetchedMsg:
		if msg.err != nil {
			// Check if error is due to age restriction
			errStr := msg.err.Error()
			if strings.Contains(errStr, "Sign in to confirm") || strings.Contains(errStr, "Age-restricted") {
				// If we haven't tried with browser cookies yet, start browser detection
				if !m.needsBrowser && m.cfg.CookieBrowser == "" {
					m.needsBrowser = true
					// Start async browser detection
					return m, m.detectBrowsersAsync()
				}
				// Already tried with browser, show error
				m.errorMsg = fmt.Sprintf("Failed to fetch metadata: %v", msg.err)
				return m, tea.Quit
			}
			m.errorMsg = fmt.Sprintf("Failed to fetch metadata: %v", msg.err)
			return m, tea.Quit
		}
		m.PlaylistInfo = msg.playlistInfo
		m.Title = msg.title
		m.state = formatState
		m.cursor = 0
		return m, nil
	case browsersDetectedMsg:
		m.availableBrowsers = msg.browsers
		if len(m.availableBrowsers) == 0 {
			m.errorMsg = "Age-restricted video. No supported browsers found for authentication."
			return m, tea.Quit
		}
		// Show browser selection screen so user can choose which browser they're logged into
		m.state = browserSelectionState
		m.cursor = 0
		m.choices = m.availableBrowsers
		return m, nil
	case tickMsg:
		m.loadingDots = strings.Repeat(".", (int(time.Since(m.loadingStart)/time.Millisecond/500)%3)+1)
		return m, tea.Tick(time.Millisecond*500, func(t time.Time) tea.Msg {
			return tickMsg{}
		})
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m *Model) updateBrowserSelection(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.choices)-1 {
				m.cursor++
			}
		case "enter":
			m.cfg.CookieBrowser = m.availableBrowsers[m.cursor]
			m.needsBrowser = true
			// Retry metadata fetch with selected browser
			m.state = metadataLoadingState
			m.loadingStart = time.Now()
			m.loadingDots = "."
			return m, tea.Batch(
				m.fetchMetadata(),
				tea.Tick(time.Millisecond*500, func(t time.Time) tea.Msg {
					return tickMsg{}
				}),
			)
		}
	}
	return m, nil
}

func (m *Model) updateFormat(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.choices)-1 {
				m.cursor++
			}
		case "enter":
			if m.cursor == 0 {
				m.cfg.IsAudioOnly = false
				m.state = formatsLoadingState
				m.loadingStart = time.Now()
				m.loadingDots = "."
				return m, tea.Batch(
					m.fetchFormats(),
					tea.Tick(time.Millisecond*500, func(t time.Time) tea.Msg {
						return tickMsg{}
					}),
				)
			} else {
				m.cfg.IsAudioOnly = true
				m.state = confirmationState
				m.cursor = 0
			}
		}
	}
	return m, nil
}

func (m *Model) fetchFormats() tea.Cmd {
	return func() tea.Msg {
		formats, err := m.dl.GetFormats(m.url)
		return formatsFetchedMsg{formats: formats, err: err}
	}
}

func (m *Model) updateFormatsLoading(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case formatsFetchedMsg:
		if msg.err != nil {
			m.errorMsg = fmt.Sprintf("Failed to fetch formats: %v", msg.err)
			return m, tea.Quit
		}
		m.formats = msg.formats
		m.videoFormats = []downloader.Format{}
		for _, f := range msg.formats {
			if !f.IsAudio {
				m.videoFormats = append(m.videoFormats, f)
			}
		}
		if len(m.videoFormats) == 0 {
			m.cfg.Resolution = ""
			m.state = confirmationState
			m.cursor = 0
		} else {
			m.choices = []string{"Default (best available)"}
			for _, f := range m.videoFormats {
				if f.FileSize != "" {
					m.choices = append(m.choices, fmt.Sprintf("%dp (%s, %s) - %s", f.Height, f.Ext, f.Protocol, f.FileSize))
				} else {
					m.choices = append(m.choices, fmt.Sprintf("%dp (%s, %s)", f.Height, f.Ext, f.Protocol))
				}
			}
			m.state = resolutionState
			m.cursor = 0
		}
		return m, nil
	case tickMsg:
		m.loadingDots = strings.Repeat(".", (int(time.Since(m.loadingStart)/time.Millisecond/500)%3)+1)
		return m, tea.Tick(time.Millisecond*500, func(t time.Time) tea.Msg {
			return tickMsg{}
		})
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m *Model) updateResolution(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.choices)-1 {
				m.cursor++
			}
		case "enter":
			if m.cursor == 0 {
				m.cfg.Resolution = ""
			} else if m.cursor-1 < len(m.videoFormats) {
				m.cfg.Resolution = m.videoFormats[m.cursor-1].ID
			} else {
				m.cfg.Resolution = ""
			}
			m.state = confirmationState
			m.cursor = 0
		}
	}
	return m, nil
}

func (m *Model) updateConfirmation(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "y":
			m.Confirmed = true
			// Prepare download parameters if not already set
			if m.TempDir == "" || len(m.Args) == 0 {
				// Generate temp directory name from title
				finalName := m.Title
				if finalName == "" {
					finalName = "Video_" + fmt.Sprintf("%d", time.Now().Unix())
				}
				// We'll set TempDir and Args, then start download
				m.Args = []string{m.url}
				// For now, just quit and let main.go handle it
				return m, tea.Quit
			}
			// TUI mode - handle download in TUI
			m.state = downloadingState
			return m, m.startDownload()
		case "n":
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m *Model) startDownload() tea.Cmd {
	// Start the actual download in a goroutine
	go m.runDownload()
	// Return a command that waits for progress updates
	return waitForProgress
}

func (m *Model) runDownload() {
	// Send initial progress message
	m.sendProgress("Starting download...", 0, "", "")

	// Build yt-dlp command
	ytDlpCmd := "yt-dlp"
	if runtime.GOOS == "windows" {
		ytDlpCmd = "yt-dlp.exe"
	}

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
		"--output", m.TempDir + "/" + m.cfg.OutputTemplate,
	}

	if m.cfg.CookieBrowser != "" {
		cmdArgs = append(cmdArgs, "--cookies-from-browser", m.cfg.CookieBrowser)
	}

	// Add user-agent to avoid bot detection
	cmdArgs = append(cmdArgs, "--user-agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")

	if m.cfg.IsAudioOnly {
		cmdArgs = append(cmdArgs, "--extract-audio", "--audio-format", m.cfg.AudioFormat)
	} else {
		// Force mp4 container for video downloads
		cmdArgs = append(cmdArgs, "--merge-output-format", "mp4", "--remux-video", "mp4")
		if m.cfg.Resolution != "" {
			cmdArgs = append(cmdArgs, "--format", m.cfg.Resolution+"+bestaudio/best")
		} else {
			cmdArgs = append(cmdArgs, "--format", "bestvideo+bestaudio/best")
		}
	}

	cmdArgs = append(cmdArgs, m.Args...)

	if m.cfg.UseAria2c {
		aria2Cmd := "aria2c"
		if runtime.GOOS == "windows" {
			aria2Cmd = "aria2c.exe"
		}
		cmdArgs = append(cmdArgs, "--downloader", aria2Cmd, "--downloader-args", "aria2c:"+m.cfg.Aria2cArgs)
	}

	cmd := exec.Command(ytDlpCmd, cmdArgs...)

	// Force unbuffered output
	cmd.Env = append(os.Environ(), "PYTHONUNBUFFERED=1")

	// Create pipes for stdout and stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		m.sendDownloadComplete(false, err)
		return
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		m.sendDownloadComplete(false, err)
		return
	}

	if err := cmd.Start(); err != nil {
		m.sendDownloadComplete(false, err)
		return
	}

	// Parse output in goroutines (both stdout and stderr)
	go m.parseOutput(stdout)
	go m.parseOutput(stderr)

	// Wait for command to complete
	err = cmd.Wait()
	if err != nil {
		m.sendDownloadComplete(false, err)
	} else {
		m.sendDownloadComplete(true, nil)
	}
}

func (m *Model) parseOutput(reader io.Reader) {
	scanner := bufio.NewScanner(reader)
	// Increase buffer size for long lines
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)
	// Split on CR or LF so carriage-return updates are captured in real-time
	scanner.Split(splitCRLF)

	// Match our custom progress template: download:[download] 123456/987654 (12.5%) at 1.2MiB/s ETA 01:23
	customProgressRegex := regexp.MustCompile(`download:\[download\].*?\((\d+\.?\d*)%\)`)
	// Match yt-dlp progress: [download]  45.2% of 123.45MiB at 1.23MiB/s ETA 01:23
	ytdlpProgressRegex := regexp.MustCompile(`\[download\]\s+(\d+\.?\d*)%`)
	// Match aria2c progress: [#abc123 45.2MiB/123.45MiB(36%) CN:16 DL:1.2MiB ETA:1m23s]
	aria2cProgressRegex := regexp.MustCompile(`\((\d+)%\)`)
	// Match speed and ETA
	speedRegex := regexp.MustCompile(`(?:DL:|at\s+)(\d+\.?\d*\w+/?s)`)
	etaRegex := regexp.MustCompile(`ETA[:\s]+(\S+)`)
	// Match aria2c bytes progress: e.g., 1.0MiB/89MiB
	bytesProgressRegex := regexp.MustCompile(`([0-9.]+)\s*([kKmMgGtT]?i?B)/([0-9.]+)\s*([kKmMgGtT]?i?B)`)

	// helper to convert sizes to bytes for percentage calc
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

		// Process non-empty lines
		if line != "" {
			// Try standard yt-dlp progress format first: [download]  45.2% of 123.45MiB at 1.23MiB/s ETA 01:23
			if matches := ytdlpProgressRegex.FindStringSubmatch(line); len(matches) >= 2 {
				percent, _ := strconv.ParseFloat(matches[1], 64)
				speed := ""
				eta := ""
				if speedMatches := speedRegex.FindStringSubmatch(line); len(speedMatches) >= 2 {
					speed = speedMatches[1]
				}
				if etaMatches := etaRegex.FindStringSubmatch(line); len(etaMatches) >= 2 {
					eta = etaMatches[1]
				}
				m.sendProgress(line, percent, speed, eta)
			} else if matches := customProgressRegex.FindStringSubmatch(line); len(matches) >= 2 {
				percent, _ := strconv.ParseFloat(matches[1], 64)
				speed := ""
				eta := ""
				if speedMatches := speedRegex.FindStringSubmatch(line); len(speedMatches) >= 2 {
					speed = speedMatches[1]
				}
				if etaMatches := etaRegex.FindStringSubmatch(line); len(etaMatches) >= 2 {
					eta = etaMatches[1]
				}
				m.sendProgress(line, percent, speed, eta)
			} else if matches := aria2cProgressRegex.FindStringSubmatch(line); len(matches) >= 2 {
				// Parse aria2c progress format
				percent, _ := strconv.ParseFloat(matches[1], 64)
				speed := ""
				eta := ""
				if speedMatches := speedRegex.FindStringSubmatch(line); len(speedMatches) >= 2 {
					speed = speedMatches[1]
				}
				if etaMatches := etaRegex.FindStringSubmatch(line); len(etaMatches) >= 2 {
					eta = etaMatches[1]
				}
				m.sendProgress(line, percent, speed, eta)
			} else if bm := bytesProgressRegex.FindStringSubmatch(line); len(bm) == 5 {
				// Compute percent from bytes fraction when percent not present
				cur, _ := strconv.ParseFloat(bm[1], 64)
				tot, _ := strconv.ParseFloat(bm[3], 64)
				mu := unitToMultiplier(bm[2])
				mt := unitToMultiplier(bm[4])
				if mt > 0 && tot > 0 {
					percent := (cur * mu) / (tot * mt) * 100.0
					speed := ""
					if speedMatches := speedRegex.FindStringSubmatch(line); len(speedMatches) >= 2 {
						speed = speedMatches[1]
					}
					eta := ""
					if etaMatches := etaRegex.FindStringSubmatch(line); len(etaMatches) >= 2 {
						eta = etaMatches[1]
					}
					m.sendProgress(line, percent, speed, eta)
				}
			} else if strings.Contains(line, "[download]") || strings.Contains(line, "[info]") || strings.Contains(line, "Destination:") {
				// Send other download-related messages
				m.sendProgress(line, 0, "", "")
			}
		}
	}
}

var progressChan = make(chan tea.Msg, 100)

func (m *Model) sendProgress(progress string, percent float64, speed, eta string) {
	select {
	case progressChan <- downloadProgressMsg{
		progress: progress,
		percent:  percent,
		speed:    speed,
		eta:      eta,
	}:
	default:
		// Channel full, skip this update
	}
}

func (m *Model) sendDownloadComplete(success bool, err error) {
	progressChan <- downloadCompleteMsg{
		success: success,
		err:     err,
	}
}

func waitForProgress() tea.Msg {
	return <-progressChan
}

func (m *Model) updateDownloading(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case downloadProgressMsg:
		m.downloadProgress = msg.progress
		m.downloadPercent = msg.percent
		m.downloadSpeed = msg.speed
		m.downloadETA = msg.eta
		// Continue waiting for more progress updates
		return m, waitForProgress
	case downloadCompleteMsg:
		if msg.success {
			m.downloadComplete = true
			m.state = downloadCompleteState
		} else {
			if msg.err != nil {
				m.downloadError = msg.err.Error()
			} else {
				m.downloadError = "Download failed"
			}
			m.state = downloadCompleteState
		}
		return m, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		}
	}
	return m, waitForProgress
}

func (m *Model) updateDownloadComplete(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "enter":
			return m, tea.Quit
		}
	}
	return m, nil
}

// getTerminalSize returns the terminal dimensions
func getTerminalSize() (int, int) {
	w, h, _ := term.GetSize(int(os.Stdout.Fd()))
	if w == 0 {
		return 80, 24
	}
	return w, h
}

// rainbowColor generates RGB colors for rainbow animation
func rainbowColor(offset int) string {
	// HSV to RGB conversion for rainbow effect
	hue := float64(offset%360) / 360.0

	// Convert HSV to RGB
	r, g, b := hsvToRGB(hue, 1.0, 1.0)

	// Convert to ANSI 256 color (using true color if supported)
	return fmt.Sprintf("#%02x%02x%02x", int(r*255), int(g*255), int(b*255))
}

// hsvToRGB converts HSV color space to RGB
func hsvToRGB(h, s, v float64) (r, g, b float64) {
	i := int(h * 6)
	f := h*6 - float64(i)
	p := v * (1 - s)
	q := v * (1 - f*s)
	t := v * (1 - (1-f)*s)

	switch i % 6 {
	case 0:
		r, g, b = v, t, p
	case 1:
		r, g, b = q, v, p
	case 2:
		r, g, b = p, v, t
	case 3:
		r, g, b = p, q, v
	case 4:
		r, g, b = t, p, v
	case 5:
		r, g, b = v, p, q
	}
	return r, g, b
}

func (m *Model) View() string {
	termW, termH := getTerminalSize()

	// Calculate max width for content (leave margin for borders and padding)
	maxContentWidth := termW - 20
	if maxContentWidth < 40 {
		maxContentWidth = 40
	}
	if maxContentWidth > 80 {
		maxContentWidth = 80
	}

	// Create rainbow border styles
	rainbowBorderColor := lipgloss.Color(rainbowColor(m.rainbowOffset))
	rainbowBorderColor2 := lipgloss.Color(rainbowColor(m.rainbowOffset + 60))
	rainbowBorderColor3 := lipgloss.Color(rainbowColor(m.rainbowOffset + 120))

	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(rainbowBorderColor).PaddingBottom(1).Align(lipgloss.Center).Width(maxContentWidth)
	choiceStyle := lipgloss.NewStyle().PaddingLeft(2).Width(maxContentWidth)
	selectedStyle := lipgloss.NewStyle().PaddingLeft(2).Foreground(rainbowBorderColor2).Bold(true).Width(maxContentWidth)
	inputStyle := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(rainbowBorderColor).
		BorderBackground(lipgloss.Color("")).
		Padding(0, 1).
		MarginTop(1).
		Width(maxContentWidth - 4)
	panelStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(rainbowBorderColor3).
		BorderBackground(lipgloss.Color("")).
		Padding(1, 2).
		Width(maxContentWidth + 6)
	footerStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Align(lipgloss.Center).Width(maxContentWidth)

	// Create header styles
	appNameStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(rainbowBorderColor).
		Align(lipgloss.Center).
		Width(maxContentWidth).
		MarginTop(1).
		MarginBottom(1)

	quoteStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("241")).
		Align(lipgloss.Center).
		Width(maxContentWidth).
		Italic(true).
		MarginBottom(2)

	// Build the complete view
	var content strings.Builder

	// Add header with app name and quote
	content.WriteString(appNameStyle.Render("🌈 Yaria 🌈"))
	content.WriteString("\n")
	content.WriteString(quoteStyle.Render(m.currentQuote))
	content.WriteString("\n")

	var mainContent strings.Builder
	switch m.state {
	case urlState:
		mainContent.WriteString(headerStyle.Render("Enter video URL"))
		mainContent.WriteString("\n")
		// Truncate URL input if too long for display
		displayInput := m.urlInput
		maxInputWidth := maxContentWidth - 10
		if len(displayInput) > maxInputWidth {
			displayInput = displayInput[:maxInputWidth-3] + "..."
		}
		mainContent.WriteString(inputStyle.Render(displayInput + "|"))
	case formatState:
		mainContent.WriteString(headerStyle.Render("Select download format"))
		mainContent.WriteString("\n")
		for i, choice := range m.choices {
			if m.cursor == i {
				mainContent.WriteString(selectedStyle.Render(fmt.Sprintf("> %s", choice)))
			} else {
				mainContent.WriteString(choiceStyle.Render(fmt.Sprintf("  %s", choice)))
			}
			mainContent.WriteString("\n")
		}
	case metadataLoadingState:
		loadingMsg := "Fetching video info"
		if m.cfg.CookieBrowser != "" {
			loadingMsg = fmt.Sprintf("Fetching video info (using %s cookies)", m.cfg.CookieBrowser)
		}
		mainContent.WriteString(headerStyle.Render(loadingMsg + m.loadingDots))
		mainContent.WriteString("\n")
		// Add rabbit animation
		rabbitStyle := lipgloss.NewStyle().
			Foreground(rainbowBorderColor).
			Align(lipgloss.Center).
			Width(maxContentWidth).
			MarginTop(1)
		mainContent.WriteString(rabbitStyle.Render(getRabbitFrame(m.rabbitFrame)))
	case browserSelectionState:
		mainContent.WriteString(headerStyle.Render("Age-restricted video - Select browser for authentication"))
		mainContent.WriteString("\n")
		for i, choice := range m.choices {
			if m.cursor == i {
				mainContent.WriteString(selectedStyle.Render(fmt.Sprintf("> %s", choice)))
			} else {
				mainContent.WriteString(choiceStyle.Render(fmt.Sprintf("  %s", choice)))
			}
			mainContent.WriteString("\n")
		}
	case formatsLoadingState:
		mainContent.WriteString(headerStyle.Render("Fetching formats" + m.loadingDots))
		mainContent.WriteString("\n")
		// Add rabbit animation
		rabbitStyle := lipgloss.NewStyle().
			Foreground(rainbowBorderColor).
			Align(lipgloss.Center).
			Width(maxContentWidth).
			MarginTop(1)
		mainContent.WriteString(rabbitStyle.Render(getRabbitFrame(m.rabbitFrame)))
	case resolutionState:
		mainContent.WriteString(headerStyle.Render("Select resolution"))
		mainContent.WriteString("\n")
		for i, choice := range m.choices {
			if m.cursor == i {
				mainContent.WriteString(selectedStyle.Render(fmt.Sprintf("> %s", choice)))
			} else {
				mainContent.WriteString(choiceStyle.Render(fmt.Sprintf("  %s", choice)))
			}
			mainContent.WriteString("\n")
		}
		mainContent.WriteString("\n" + lipgloss.NewStyle().Faint(true).Render(
			"Note: Some formats may be restricted by YouTube.\nIf download fails, try Default or run `yt-dlp --list-formats <URL>`."))
	case confirmationState:
		// Truncate title if too long
		displayTitle := m.Title
		maxTitleWidth := maxContentWidth - 20
		if len(displayTitle) > maxTitleWidth {
			displayTitle = displayTitle[:maxTitleWidth-3] + "..."
		}
		mainContent.WriteString(headerStyle.Render(fmt.Sprintf("Download '%s'? (y/n)", displayTitle)))
	case downloadingState:
		mainContent.WriteString(headerStyle.Render("Downloading"))
		mainContent.WriteString("\n\n")
		// Always show progress message
		progressMsg := m.downloadProgress
		if progressMsg == "" {
			progressMsg = "Preparing download..."
		}
		progressStyle := lipgloss.NewStyle().Width(maxContentWidth).Align(lipgloss.Center)
		mainContent.WriteString(progressStyle.Render(progressMsg))
		mainContent.WriteString("\n\n")

		// Always show progress bar (even at 0%)
		barWidth := maxContentWidth - 10
		if barWidth < 10 {
			barWidth = 10
		}
		filledWidth := int(float64(barWidth) * m.downloadPercent / 100.0)
		emptyWidth := barWidth - filledWidth
		progressBar := strings.Repeat("█", filledWidth) + strings.Repeat("░", emptyWidth)
		progressBarStyle := lipgloss.NewStyle().Width(maxContentWidth).Align(lipgloss.Center).Foreground(lipgloss.Color("212"))
		mainContent.WriteString(progressBarStyle.Render(progressBar))
		mainContent.WriteString("\n")
		mainContent.WriteString(progressBarStyle.Render(fmt.Sprintf("%.1f%%", m.downloadPercent)))
		mainContent.WriteString("\n")

		if m.downloadSpeed != "" && m.downloadETA != "" {
			infoStyle := lipgloss.NewStyle().Width(maxContentWidth).Align(lipgloss.Center).Faint(true)
			mainContent.WriteString("\n")
			mainContent.WriteString(infoStyle.Render(fmt.Sprintf("Speed: %s | ETA: %s", m.downloadSpeed, m.downloadETA)))
		}
	case downloadCompleteState:
		if m.downloadComplete {
			successStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Bold(true).Width(maxContentWidth).Align(lipgloss.Center)
			mainContent.WriteString(headerStyle.Render("Download Complete!"))
			mainContent.WriteString("\n\n")
			mainContent.WriteString(successStyle.Render("✓ Video downloaded successfully"))
			mainContent.WriteString("\n\n")
			infoStyle := lipgloss.NewStyle().Faint(true).Width(maxContentWidth).Align(lipgloss.Center)
			mainContent.WriteString(infoStyle.Render("Press Enter or Ctrl+C to exit"))
		} else if m.downloadError != "" {
			errorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true).Width(maxContentWidth).Align(lipgloss.Center)
			mainContent.WriteString(headerStyle.Render("Download Failed"))
			mainContent.WriteString("\n\n")
			mainContent.WriteString(errorStyle.Render("❌ " + m.downloadError))
			mainContent.WriteString("\n\n")
			infoStyle := lipgloss.NewStyle().Faint(true).Width(maxContentWidth).Align(lipgloss.Center)
			mainContent.WriteString(infoStyle.Render("Press Enter or Ctrl+C to exit"))
		}
	}

	// Display error message if present
	if m.errorMsg != "" {
		errorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true).Width(maxContentWidth).Align(lipgloss.Center)
		mainContent.WriteString("\n\n")
		mainContent.WriteString(errorStyle.Render("❌ " + m.errorMsg))
	}

	// Combine header, main content, and footer
	mainPanel := panelStyle.Render(mainContent.String())
	footer := footerStyle.Render("Press Ctrl+C to quit")
	combined := lipgloss.JoinVertical(lipgloss.Center, content.String(), mainPanel, footer)
	ui := lipgloss.Place(termW, termH, lipgloss.Center, lipgloss.Center, combined)
	return ui
}
