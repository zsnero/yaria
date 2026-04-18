package tui

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"math"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"yaria/internal/yaria/clean"
	"yaria/internal/yaria/config"
	"yaria/internal/yaria/daemon"
	"yaria/internal/yaria/deps"
	"yaria/internal/yaria/downloader"
	"yaria/internal/yaria/logger"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"golang.org/x/term"
)

type state int

const (
	mainMenuState state = iota
	urlState
	metadataLoadingState
	browserSelectionState
	formatState
	resolutionState
	downloadLocationState
	formatsLoadingState
	downloadingState
	downloadCompleteState
	settingsState
	themePickerState
	cleanScanState
	cleanListState
	cleanConfirmState
	cleanDoneState
	downloadsState
	depsCheckState
	depsUpdateState
)

// Theme definition
type theme struct {
	Name string
	Hue  float64 // HSV hue (0-360), -1 for rainbow
	Mono bool    // true = monochrome (white theme)
}

var themes = []theme{
	{Name: "Rainbow", Hue: -1},
	{Name: "Red", Hue: 0},
	{Name: "Orange", Hue: 30},
	{Name: "Yellow", Hue: 55},
	{Name: "Green", Hue: 120},
	{Name: "Cyan", Hue: 180},
	{Name: "Blue", Hue: 220},
	{Name: "Purple", Hue: 270},
	{Name: "Magenta", Hue: 300},
	{Name: "Pink", Hue: 330},
	{Name: "White", Hue: 0, Mono: true},
}

func themeNames() []string {
	names := make([]string, len(themes))
	for i, t := range themes {
		names[i] = t.Name
	}
	return names
}

func getTheme(name string) theme {
	for _, t := range themes {
		if strings.EqualFold(t.Name, name) {
			return t
		}
	}
	return themes[0] // Default to Rainbow
}

type Model struct {
	cfg               *config.Config
	userCfg           config.UserConfig
	log               logger.Logger
	dl                downloader.Downloader
	state             state
	url               string
	Title             string
	formats           []downloader.Format
	videoFormats      []downloader.Format
	cursor            int
	choices           []string
	choiceValues      []string
	Confirmed         bool
	rainbowOffset     int      // For animation
	currentQuote      string   // Current funny quote
	rabbitFrame       int      // Current rabbit animation frame
	locationChoices   []string // Download location options
	ThumbnailPath     string   // Path to downloaded thumbnail
	IsKittyTerminal   bool     // Whether terminal supports GPU images
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

	// Daemon downloads fields
	daemonDownloads     []daemon.DownloadInfo
	activeDownloadID    string        // ID of the download being tracked on the progress screen
	cameFromDownloads   bool          // true if we entered downloadingState from the downloads dashboard
	daemonRestarted     bool          // ensures daemon is restarted once per TUI session
	stopActivePoller    chan struct{} // signals the active download poller to stop
	activePollerCmd     tea.Cmd       // cmd to read next active download poll msg
	stopDownloadsPoller chan struct{} // signals the downloads dashboard poller to stop
	downloadsPollerCmd  tea.Cmd       // cmd to read next downloads list poll msg

	// Dependencies fields
	depsResults  []deps.Dep
	depsUpdating bool

	// Clean cache fields
	cleanEntries   []clean.Entry
	cleanTotalSize int64
	cleanPartials  []clean.Entry
	cleanPartialSz int64
	cleanMetas     []clean.Entry
	cleanMetaSz    int64
	cleanAction    string
	cleanRemoved   int
	cleanFreed     int64
	cleanErrs      []error
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
	// Detect terminal capabilities
	isKitty := false
	termEnv := os.Getenv("TERM")
	termProgram := os.Getenv("TERM_PROGRAM")

	// Check for kitty terminal
	if strings.Contains(termEnv, "kitty") || strings.Contains(termProgram, "kitty") {
		isKitty = true
	}

	// Load persistent user config
	userCfg := config.LoadUserConfig()

	m := &Model{
		cfg:             cfg,
		userCfg:         userCfg,
		log:             log,
		state:           mainMenuState,
		rainbowOffset:   0,
		currentQuote:    getRandomQuote(),
		rabbitFrame:     0,
		IsKittyTerminal: isKitty,
	}
	m.rebuildMainMenuChoices()
	return m
}

func (m *Model) SetDownloader(dl downloader.Downloader) {
	m.dl = dl
}

func (m *Model) Run(url, title string) error {
	m.url = url
	m.Title = title
	if url != "" {
		m.state = formatState // Skip to format selection if URL provided
		m.choices = []string{
			"Video (with audio)",
			"Audio only",
		}
		m.choiceValues = nil
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
	playlistInfo  string
	title         string
	thumbnailPath string
	err           error
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

type yaziLocationSelectedMsg struct {
	path string
}

type daemonListMsg struct {
	downloads []daemon.DownloadInfo
	err       error
}

type daemonAddedMsg struct {
	id  string
	err error
}

type depsCheckDoneMsg struct {
	results []deps.Dep
}

type depsProgressMsg struct {
	name   string
	status string
	msg    string
}

type depsUpdateDoneMsg struct {
	results []deps.Dep
}

type cleanScanDoneMsg struct {
	entries   []clean.Entry
	totalSize int64
	partials  []clean.Entry
	partialSz int64
	metas     []clean.Entry
	metaSz    int64
	err       error
}

type cleanDoneMsg struct {
	removed int
	freed   int64
	errs    []error
}

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
	"  /\\_/\\  \n ( \u2022.\u2022 ) \n  > ^ <  ",
	"  /\\_/\\  \n ( o.o ) \n  > ^ <  ",
	"  /\\_/\\  \n ( \u2022.\u2022 ) \n  > ^ <  ",
	"  /\\_/\\  \n ( o.o ) \n  > ^ <  ",
	"  /\\_/\\  \n ( \u2022.\u2022 ) \n  > ^ <  ",
	"  /\\_/\\  \n ( o.o ) \n  > ^ <  ",
}

// getRabbitFrame returns the current rabbit animation frame
func getRabbitFrame(frame int) string {
	return rabbitFrames[frame%len(rabbitFrames)]
}

// ── Navigation helpers ──────────────────────────────────────────────

func (m *Model) backToMainMenu() (tea.Model, tea.Cmd) {
	m.state = mainMenuState
	m.cursor = 0
	m.errorMsg = ""
	m.urlInput = ""
	m.url = ""
	m.URL = ""
	m.Title = ""
	m.downloadComplete = false
	m.downloadError = ""
	m.downloadPercent = 0
	m.downloadProgress = ""
	m.downloadSpeed = ""
	m.downloadETA = ""
	m.Confirmed = false
	m.stopPollers()
	m.rebuildMainMenuChoices()
	return m, nil
}

func (m *Model) stopPollers() {
	if m.stopActivePoller != nil {
		close(m.stopActivePoller)
		m.stopActivePoller = nil
		m.activePollerCmd = nil
	}
	if m.stopDownloadsPoller != nil {
		close(m.stopDownloadsPoller)
		m.stopDownloadsPoller = nil
		m.downloadsPollerCmd = nil
	}
}

func (m *Model) rebuildMainMenuChoices() {
	count := len(m.daemonDownloads)
	downloadsLabel := "Downloads"
	if count > 0 {
		downloadsLabel = fmt.Sprintf("Downloads (%d)", count)
	}
	m.choices = []string{"Download Video", downloadsLabel, "Settings"}
	m.choiceValues = []string{"download", "downloads", "settings"}
}

func (m *Model) backToDownloads() (tea.Model, tea.Cmd) {
	m.state = downloadsState
	m.cursor = 0
	m.activeDownloadID = ""
	m.cameFromDownloads = false
	return m, m.startDownloadsPoller()
}

func (m *Model) backFromDownloadScreen() (tea.Model, tea.Cmd) {
	if m.cameFromDownloads {
		return m.backToDownloads()
	}
	return m.backToMainMenu()
}

func (m *Model) backToSettings() (tea.Model, tea.Cmd) {
	m.state = settingsState
	m.cursor = 0
	m.choices = []string{"Theme", "Clear Cache", "Download/Update Dependencies", "Back"}
	m.choiceValues = []string{"theme", "clearcache", "deps", "back"}
	return m, nil
}

// ── Update dispatch ─────────────────────────────────────────────────

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

	// Handle daemon messages globally (they can arrive while in any state)
	switch msg := msg.(type) {
	case daemonAddedMsg:
		if msg.err != nil {
			m.backToMainMenu()
			m.errorMsg = fmt.Sprintf("Failed to add download: %v", msg.err)
			return m, nil
		}
		// Show the detailed download progress screen
		m.activeDownloadID = msg.id
		m.state = downloadingState
		m.downloadPercent = 0
		m.downloadProgress = "Starting download..."
		m.downloadSpeed = ""
		m.downloadETA = ""
		m.downloadComplete = false
		m.downloadError = ""
		m.cameFromDownloads = false
		return m, m.startActiveDownloadPoller()
	case daemonListMsg:
		if msg.err == nil {
			m.daemonDownloads = msg.downloads
		}
		if m.state == downloadsState && m.downloadsPollerCmd != nil {
			return m, m.downloadsPollerCmd
		}
		return m, nil
	default:
		_ = msg
	}

	switch m.state {
	case mainMenuState:
		return m.updateMainMenu(msg)
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
	case downloadLocationState:
		return m.updateDownloadLocation(msg)
	case formatsLoadingState:
		return m.updateFormatsLoading(msg)
	case downloadingState:
		return m.updateDownloading(msg)
	case downloadCompleteState:
		return m.updateDownloadComplete(msg)
	case settingsState:
		return m.updateSettings(msg)
	case themePickerState:
		return m.updateThemePicker(msg)
	case cleanScanState:
		return m.updateCleanScan(msg)
	case cleanListState:
		return m.updateCleanList(msg)
	case cleanConfirmState:
		return m.updateCleanConfirm(msg)
	case cleanDoneState:
		return m.updateCleanDone(msg)
	case downloadsState:
		return m.updateDownloads(msg)
	case depsCheckState:
		return m.updateDepsCheck(msg)
	case depsUpdateState:
		return m.updateDepsUpdate(msg)
	}
	return m, nil
}

// ── Main Menu ───────────────────────────────────────────────────────

func (m *Model) updateMainMenu(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
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
			val := m.choiceValues[m.cursor]
			switch val {
			case "download":
				m.state = urlState
				m.cursor = 0
				m.urlInput = ""
				return m, nil
			case "downloads":
				m.state = downloadsState
				m.cursor = 0
				return m, m.startDownloadsPoller()
			case "settings":
				return m.backToSettings()
			}
		}
	}
	return m, nil
}

// ── URL Input ───────────────────────────────────────────────────────

func (m *Model) updateURL(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyEnter:
			m.URL = strings.TrimSpace(m.urlInput)
			if m.URL == "" {
				m.errorMsg = "No URL provided"
				return m, nil
			}
			m.url = m.URL
			m.errorMsg = ""
			m.state = metadataLoadingState
			m.loadingStart = time.Now()
			m.loadingDots = "."
			return m, tea.Batch(
				m.fetchMetadata(),
				tea.Tick(time.Millisecond*500, func(t time.Time) tea.Msg {
					return tickMsg{}
				}),
			)
		case tea.KeyCtrlC:
			return m, tea.Quit
		case tea.KeyEsc:
			// Go back to main menu
			return m.backToMainMenu()
		case tea.KeyRunes:
			m.urlInput += string(msg.Runes)
			m.errorMsg = ""
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
		return metadataFetchedMsg{
			playlistInfo:  playlistInfo,
			title:         title,
			thumbnailPath: "",
			err:           err,
		}
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

// ── Metadata Loading ────────────────────────────────────────────────

func (m *Model) updateMetadataLoading(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case metadataFetchedMsg:
		if msg.err != nil {
			// Check if error is due to age restriction
			errStr := msg.err.Error()
			if strings.Contains(errStr, "Sign in to confirm") || strings.Contains(errStr, "Age-restricted") {
				if !m.needsBrowser && m.cfg.CookieBrowser == "" {
					m.needsBrowser = true
					return m, m.detectBrowsersAsync()
				}
				m.errorMsg = fmt.Sprintf("Failed to fetch metadata: %v", msg.err)
				return m, tea.Quit
			}
			m.errorMsg = fmt.Sprintf("Failed to fetch metadata: %v", msg.err)
			return m, tea.Quit
		}
		m.PlaylistInfo = msg.playlistInfo
		m.Title = msg.title
		m.ThumbnailPath = msg.thumbnailPath
		m.state = formatState
		m.cursor = 0
		m.choices = []string{
			"Video (with audio)",
			"Audio only",
		}
		m.choiceValues = nil
		return m, nil
	case browsersDetectedMsg:
		m.availableBrowsers = msg.browsers
		if len(m.availableBrowsers) == 0 {
			m.errorMsg = "Age-restricted video. No supported browsers found for authentication."
			return m, tea.Quit
		}
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
		case "esc":
			// Go back to URL input
			m.state = urlState
			m.cursor = 0
			return m, nil
		}
	}
	return m, nil
}

// ── Browser Selection ───────────────────────────────────────────────

func (m *Model) updateBrowserSelection(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "esc":
			// Go back to URL input
			m.state = urlState
			m.cursor = 0
			m.needsBrowser = false
			return m, nil
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

// ── Format Selection ────────────────────────────────────────────────

func (m *Model) updateFormat(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "esc":
			// Go back to URL input
			m.state = urlState
			m.cursor = 0
			return m, nil
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
				m.state = downloadLocationState
				m.cursor = 0
				m.locationChoices = []string{
					"Browse with Yazi",
					"Download in Current Directory",
				}
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

// ── Formats Loading ─────────────────────────────────────────────────

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
			m.state = downloadLocationState
			m.cursor = 0
			m.locationChoices = []string{
				"Browse with Yazi",
				"Download in Current Directory",
			}
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
		case "esc":
			// Go back to format selection
			m.state = formatState
			m.cursor = 0
			m.choices = []string{
				"Video (with audio)",
				"Audio only",
			}
			return m, nil
		}
	}
	return m, nil
}

// ── Resolution Selection ────────────────────────────────────────────

func (m *Model) updateResolution(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "esc":
			// Go back to format selection
			m.state = formatState
			m.cursor = 0
			m.choices = []string{
				"Video (with audio)",
				"Audio only",
			}
			return m, nil
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
			m.state = downloadLocationState
			m.cursor = 0
			m.locationChoices = []string{
				"Browse with Yazi",
				"Download in Current Directory",
			}
		}
	}
	return m, nil
}

// ── Download Location ───────────────────────────────────────────────

func (m *Model) updateDownloadLocation(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "esc":
			// Go back to resolution (if video) or format (if audio)
			if m.cfg.IsAudioOnly {
				m.state = formatState
				m.cursor = 0
				m.choices = []string{
					"Video (with audio)",
					"Audio only",
				}
			} else {
				m.state = resolutionState
				m.cursor = 0
				// Rebuild resolution choices
				m.choices = []string{"Default (best available)"}
				for _, f := range m.videoFormats {
					if f.FileSize != "" {
						m.choices = append(m.choices, fmt.Sprintf("%dp (%s, %s) - %s", f.Height, f.Ext, f.Protocol, f.FileSize))
					} else {
						m.choices = append(m.choices, fmt.Sprintf("%dp (%s, %s)", f.Height, f.Ext, f.Protocol))
					}
				}
			}
			return m, nil
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.locationChoices)-1 {
				m.cursor++
			}
		case "enter":
			if m.cursor == 0 {
				// Launch yazi file explorer
				return m, m.launchYaziFileExplorer()
			} else {
				// Use current directory
				cwd, _ := os.Getwd()
				m.cfg.DownloadLocation = cwd
				return m.startDownloadFromLocation()
			}
		}
	case yaziLocationSelectedMsg:
		// Ensure the path is a directory, not a file
		selected := msg.path
		if info, err := os.Stat(selected); err == nil && !info.IsDir() {
			selected = filepath.Dir(selected)
		}
		m.cfg.DownloadLocation = selected
		return m.startDownloadFromLocation()
	}
	return m, nil
}

// startDownloadFromLocation sends the download to the daemon and navigates to downloads screen
func (m *Model) startDownloadFromLocation() (tea.Model, tea.Cmd) {
	m.Confirmed = true
	dir := m.cfg.DownloadLocation
	if dir == "" {
		cwd, _ := os.Getwd()
		dir = cwd
	}
	// Immediately show downloading state so user gets feedback
	m.state = downloadingState
	m.downloadPercent = 0
	m.downloadProgress = "Starting download daemon..."
	m.downloadSpeed = ""
	m.downloadETA = ""
	m.downloadComplete = false
	m.downloadError = ""
	return m, m.addToDaemon(m.url, m.Title, dir)
}

func (m *Model) addToDaemon(url, title, dir string) tea.Cmd {
	cfg := m.cfg
	needsRestart := !m.daemonRestarted
	m.daemonRestarted = true
	return func() tea.Msg {
		// Restart daemon once per TUI session to ensure latest binary is used
		if needsRestart {
			if err := daemon.RestartDaemon(); err != nil {
				return daemonAddedMsg{err: err}
			}
		} else {
			if err := daemon.EnsureRunning(); err != nil {
				return daemonAddedMsg{err: err}
			}
		}
		resp, err := daemon.Send(daemon.Request{
			Cmd:            daemon.CmdAdd,
			URL:            url,
			Title:          title,
			Dir:            dir,
			IsAudioOnly:    cfg.IsAudioOnly,
			AudioFormat:    cfg.AudioFormat,
			Resolution:     cfg.Resolution,
			CookieBrowser:  cfg.CookieBrowser,
			UseAria2c:      cfg.UseAria2c,
			Aria2cArgs:     cfg.Aria2cArgs,
			OutputTemplate: cfg.OutputTemplate,
		})
		if err != nil {
			return daemonAddedMsg{err: err}
		}
		if !resp.OK {
			return daemonAddedMsg{err: fmt.Errorf("%s", resp.Error)}
		}
		id := ""
		if len(resp.Torrents) > 0 {
			id = resp.Torrents[0].ID
		}
		return daemonAddedMsg{id: id}
	}
}

// ── Download Progress ───────────────────────────────────────────────

func (m *Model) startDownload() tea.Cmd {
	go m.runDownload()
	return waitForProgress
}

func (m *Model) runDownload() {
	m.sendProgress("Starting download...", 0, "", "")

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
		"--progress-template", "%(progress)s %(progress._total_bytes_str)s %(progress._downloaded_bytes_str)s %(progress._speed_str)s %(progress._eta_str)s",
	}

	problematicSites := []string{
		"pornhub.com", "xvideos.com", "xhamster.com", "youporn.com", "redtube.com",
		"spankbang.com", "eporner.com", "tube8.com", "tnaflix.com", "keezmovies.com",
		"twitter.com", "x.com", "instagram.com", "facebook.com", "tiktok.com",
		"vimeo.com", "dailymotion.com", "twitch.tv", "soundcloud.com",
		"reddit.com", "imgur.com", "giphy.com",
	}

	isProblematic := false
	for _, site := range problematicSites {
		if strings.Contains(m.url, site) {
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

	var outputPath string
	if m.cfg.DownloadLocation != "" {
		outputPath = m.cfg.DownloadLocation + "/%(title)s/%(title)s.%(ext)s"
	} else {
		outputPath = m.TempDir + "/" + m.cfg.OutputTemplate
	}
	cmdArgs = append(cmdArgs, "--output", outputPath)

	if m.cfg.CookieBrowser != "" {
		cmdArgs = append(cmdArgs, "--cookies-from-browser", m.cfg.CookieBrowser)
	}

	cmdArgs = append(cmdArgs, "--user-agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")

	if isProblematic {
		cmdArgs = append(cmdArgs, "--add-header", "Accept-Language:en-US,en;q=0.9")
		cmdArgs = append(cmdArgs, "--add-header", "Accept:*/*")
		cmdArgs = append(cmdArgs, "--add-header", "Connection:keep-alive")
		cmdArgs = append(cmdArgs, "--add-header", "Sec-Fetch-Dest:empty")
		cmdArgs = append(cmdArgs, "--add-header", "Sec-Fetch-Mode:cors")
		cmdArgs = append(cmdArgs, "--sleep-interval", "1")
		cmdArgs = append(cmdArgs, "--max-sleep-interval", "3")

		if strings.Contains(m.url, "pornhub.com") {
			cmdArgs = append(cmdArgs, "--add-header", "Referer:https://www.pornhub.com/")
			cmdArgs = append(cmdArgs, "--add-header", "Origin:https://www.pornhub.com")
			cmdArgs = append(cmdArgs, "--add-header", "Sec-Fetch-Site:same-origin")
			cmdArgs = append(cmdArgs, "--add-header", "Cookie:age_verified=1")
			cmdArgs = append(cmdArgs, "--add-header", "Cookie:accessAgeDisclaimerPH=1")
		} else if strings.Contains(m.url, "xvideos.com") {
			cmdArgs = append(cmdArgs, "--add-header", "Referer:https://www.xvideos.com/")
			cmdArgs = append(cmdArgs, "--add-header", "Origin:https://www.xvideos.com")
			cmdArgs = append(cmdArgs, "--add-header", "Sec-Fetch-Site:same-origin")
		} else if strings.Contains(m.url, "xhamster.com") {
			cmdArgs = append(cmdArgs, "--add-header", "Referer:https://xhamster.com/")
			cmdArgs = append(cmdArgs, "--add-header", "Origin:https://xhamster.com")
			cmdArgs = append(cmdArgs, "--add-header", "Sec-Fetch-Site:same-origin")
			cmdArgs = append(cmdArgs, "--add-header", "Cookie:age_verified=true")
		} else if strings.Contains(m.url, "twitter.com") || strings.Contains(m.url, "x.com") {
			cmdArgs = append(cmdArgs, "--add-header", "Referer:https://twitter.com/")
			cmdArgs = append(cmdArgs, "--add-header", "Origin:https://twitter.com")
			cmdArgs = append(cmdArgs, "--add-header", "Sec-Fetch-Site:same-origin")
			cmdArgs = append(cmdArgs, "--add-header", "Authorization:Bearer AAAAAAAAAAAAAAAAAAAAANRILgAAAAAAnNwIzUejRCOuH5E6I8xnZz4qs")
		} else if strings.Contains(m.url, "instagram.com") {
			cmdArgs = append(cmdArgs, "--add-header", "Referer:https://www.instagram.com/")
			cmdArgs = append(cmdArgs, "--add-header", "Origin:https://www.instagram.com")
			cmdArgs = append(cmdArgs, "--add-header", "Sec-Fetch-Site:same-origin")
		} else if strings.Contains(m.url, "tiktok.com") {
			cmdArgs = append(cmdArgs, "--add-header", "Referer:https://www.tiktok.com/")
			cmdArgs = append(cmdArgs, "--add-header", "Origin:https://www.tiktok.com")
			cmdArgs = append(cmdArgs, "--add-header", "Sec-Fetch-Site:same-origin")
		} else if strings.Contains(m.url, "vimeo.com") {
			cmdArgs = append(cmdArgs, "--add-header", "Referer:https://vimeo.com/")
			cmdArgs = append(cmdArgs, "--add-header", "Origin:https://vimeo.com")
			cmdArgs = append(cmdArgs, "--add-header", "Sec-Fetch-Site:same-origin")
		} else if strings.Contains(m.url, "reddit.com") {
			cmdArgs = append(cmdArgs, "--add-header", "Referer:https://www.reddit.com/")
			cmdArgs = append(cmdArgs, "--add-header", "Origin:https://www.reddit.com")
			cmdArgs = append(cmdArgs, "--add-header", "Sec-Fetch-Site:same-origin")
		} else if strings.Contains(m.url, "facebook.com") {
			cmdArgs = append(cmdArgs, "--add-header", "Referer:https://www.facebook.com/")
			cmdArgs = append(cmdArgs, "--add-header", "Origin:https://www.facebook.com")
			cmdArgs = append(cmdArgs, "--add-header", "Sec-Fetch-Site:same-origin")
		} else {
			cmdArgs = append(cmdArgs, "--add-header", "Sec-Fetch-Site:cross-origin")
		}
	}

	if m.cfg.IsAudioOnly {
		cmdArgs = append(cmdArgs, "--extract-audio", "--audio-format", m.cfg.AudioFormat)
	} else {
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
	cmd.Env = append(os.Environ(), "PYTHONUNBUFFERED=1")

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

	go m.parseOutput(stdout)
	go m.parseOutput(stderr)

	err = cmd.Wait()
	if err != nil {
		m.sendDownloadComplete(false, err)
	} else {
		m.sendDownloadComplete(true, nil)
	}
}

func (m *Model) parseOutput(reader io.Reader) {
	scanner := bufio.NewScanner(reader)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)
	scanner.Split(splitCRLF)

	customProgressRegex := regexp.MustCompile(`download:\[download\].*?\((\d+\.?\d*)%\)`)
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

		if line != "" {
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
				m.sendProgress(line, 0, "", "")
			}
		}
	}
}

var progressChan = make(chan tea.Msg, 1000)

func (m *Model) sendProgress(progress string, percent float64, speed, eta string) {
	progressChan <- downloadProgressMsg{
		progress: progress,
		percent:  percent,
		speed:    speed,
		eta:      eta,
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

type activeDownloadPollMsg struct {
	info *daemon.DownloadInfo
	err  error
}

func (m *Model) startActiveDownloadPoller() tea.Cmd {
	// Stop any existing poller
	if m.stopActivePoller != nil {
		close(m.stopActivePoller)
	}
	stop := make(chan struct{})
	m.stopActivePoller = stop
	ch := make(chan activeDownloadPollMsg, 10)
	id := m.activeDownloadID

	go func() {
		for {
			select {
			case <-stop:
				return
			default:
			}
			time.Sleep(500 * time.Millisecond)
			select {
			case <-stop:
				return
			default:
			}

			if !daemon.IsRunning() {
				select {
				case ch <- activeDownloadPollMsg{err: fmt.Errorf("daemon not running")}:
				case <-stop:
				}
				return
			}
			resp, err := daemon.Send(daemon.Request{Cmd: daemon.CmdList})
			if err != nil {
				select {
				case ch <- activeDownloadPollMsg{err: err}:
				case <-stop:
				}
				return
			}
			found := false
			for _, dl := range resp.Torrents {
				if dl.ID == id {
					info := dl
					select {
					case ch <- activeDownloadPollMsg{info: &info}:
					case <-stop:
						return
					}
					found = true
					if dl.State == "complete" || dl.State == "exists" || dl.State == "error" {
						return
					}
					break
				}
			}
			if !found {
				select {
				case ch <- activeDownloadPollMsg{info: &daemon.DownloadInfo{State: "complete", Percent: 100}}:
				case <-stop:
				}
				return
			}
		}
	}()

	waitCmd := func() tea.Msg { return <-ch }
	m.activePollerCmd = waitCmd
	return waitCmd
}

func (m *Model) updateDownloading(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case activeDownloadPollMsg:
		if msg.err != nil {
			m.downloadError = msg.err.Error()
			m.state = downloadCompleteState
			return m, nil
		}
		info := msg.info
		m.downloadPercent = info.Percent
		m.downloadSpeed = info.Speed
		m.downloadETA = info.ETA

		switch info.State {
		case "downloading", "preparing":
			status := "Downloading..."
			if info.State == "preparing" {
				status = "Preparing download..."
			}
			m.downloadProgress = status
			return m, m.activePollerCmd
		case "complete":
			m.downloadComplete = true
			m.downloadPercent = 100
			m.state = downloadCompleteState
			return m, nil
		case "exists":
			m.downloadComplete = true
			m.downloadPercent = 100
			m.downloadError = "already_exists"
			m.state = downloadCompleteState
			return m, nil
		case "error":
			m.downloadError = info.Error
			if m.downloadError == "" {
				m.downloadError = "Download failed"
			}
			m.state = downloadCompleteState
			return m, nil
		case "paused":
			m.downloadProgress = "Download paused"
			return m, m.activePollerCmd
		default:
			return m, m.activePollerCmd
		}
	case downloadProgressMsg:
		// Legacy: still handle direct yt-dlp progress for RunDownloadOnly mode
		m.downloadProgress = msg.progress
		m.downloadPercent = msg.percent
		m.downloadSpeed = msg.speed
		m.downloadETA = msg.eta
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
		case "esc":
			return m.backFromDownloadScreen()
		}
	}
	// If we have an active daemon download, wait for next poll; otherwise wait for progressChan
	if m.activeDownloadID != "" && m.activePollerCmd != nil {
		return m, m.activePollerCmd
	}
	return m, waitForProgress
}

func (m *Model) updateDownloadComplete(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "enter", "esc":
			return m.backFromDownloadScreen()
		}
	}
	return m, nil
}

// ── Downloads Dashboard ─────────────────────────────────────────────

func (m *Model) fetchDownloads() tea.Cmd {
	return func() tea.Msg {
		if !daemon.IsRunning() {
			return daemonListMsg{downloads: nil}
		}
		resp, err := daemon.Send(daemon.Request{Cmd: daemon.CmdList})
		if err != nil {
			return daemonListMsg{err: err}
		}
		return daemonListMsg{downloads: resp.Torrents}
	}
}

func (m *Model) startDownloadsPoller() tea.Cmd {
	// Stop any existing poller
	if m.stopDownloadsPoller != nil {
		close(m.stopDownloadsPoller)
	}
	stop := make(chan struct{})
	m.stopDownloadsPoller = stop
	ch := make(chan daemonListMsg, 10)

	go func() {
		for {
			select {
			case <-stop:
				return
			default:
			}
			time.Sleep(time.Second)
			select {
			case <-stop:
				return
			default:
			}

			if !daemon.IsRunning() {
				select {
				case ch <- daemonListMsg{downloads: nil}:
				case <-stop:
					return
				}
				continue
			}
			resp, err := daemon.Send(daemon.Request{Cmd: daemon.CmdList})
			if err != nil {
				select {
				case ch <- daemonListMsg{err: err}:
				case <-stop:
					return
				}
				continue
			}
			select {
			case ch <- daemonListMsg{downloads: resp.Torrents}:
			case <-stop:
				return
			}
		}
	}()

	waitCmd := func() tea.Msg { return <-ch }
	m.downloadsPollerCmd = waitCmd
	return waitCmd
}

func (m *Model) updateDownloads(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "esc":
			return m.backToMainMenu()
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.daemonDownloads)-1 {
				m.cursor++
			}
		case "enter":
			// Open detailed progress screen for selected download
			if m.cursor < len(m.daemonDownloads) {
				dl := m.daemonDownloads[m.cursor]
				m.activeDownloadID = dl.ID
				m.Title = dl.Title
				m.downloadPercent = dl.Percent
				m.downloadSpeed = dl.Speed
				m.downloadETA = dl.ETA
				m.downloadComplete = false
				m.downloadError = ""
				m.downloadProgress = ""
				m.cameFromDownloads = true
				m.state = downloadingState
				return m, m.startActiveDownloadPoller()
			}
		case "p":
			// Pause/resume toggle
			if m.cursor < len(m.daemonDownloads) {
				dl := m.daemonDownloads[m.cursor]
				if dl.State == "paused" {
					go daemon.Send(daemon.Request{Cmd: daemon.CmdResume, ID: dl.ID})
				} else if dl.State == "downloading" || dl.State == "preparing" {
					go daemon.Send(daemon.Request{Cmd: daemon.CmdPause, ID: dl.ID})
				}
			}
		case "d", "x":
			// Delete
			if m.cursor < len(m.daemonDownloads) {
				dl := m.daemonDownloads[m.cursor]
				go daemon.Send(daemon.Request{Cmd: daemon.CmdRemove, ID: dl.ID})
				if m.cursor > 0 && m.cursor >= len(m.daemonDownloads)-1 {
					m.cursor--
				}
			}
		}
	}
	return m, nil
}

// ── Settings ────────────────────────────────────────────────────────

func (m *Model) updateSettings(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "esc":
			return m.backToMainMenu()
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.choices)-1 {
				m.cursor++
			}
		case "enter":
			val := m.choiceValues[m.cursor]
			switch val {
			case "theme":
				m.state = themePickerState
				m.cursor = 0
				m.choices = themeNames()
				m.choiceValues = themeNames()
				return m, nil
			case "clearcache":
				m.state = cleanScanState
				return m, m.scanClean()
			case "deps":
				m.state = depsCheckState
				m.depsResults = nil
				m.depsUpdating = false
				return m, m.checkDeps()
			case "back":
				return m.backToMainMenu()
			}
		}
	}
	return m, nil
}

// ── Dependencies ────────────────────────────────────────────────────

var depsProgressChan = make(chan depsProgressMsg, 100)

func (m *Model) checkDeps() tea.Cmd {
	return func() tea.Msg {
		results := deps.CheckAll()
		return depsCheckDoneMsg{results: results}
	}
}

func (m *Model) updateDepsCheck(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case depsCheckDoneMsg:
		m.depsResults = msg.results
		return m, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "esc":
			return m.backToSettings()
		case "enter", "u":
			// Start updating
			m.depsUpdating = true
			m.state = depsUpdateState
			return m, tea.Batch(m.startDepsUpdate(), waitForDepsProgress)
		}
	}
	return m, nil
}

func (m *Model) startDepsUpdate() tea.Cmd {
	return func() tea.Msg {
		results := deps.UpdateAll(func(name, status, msg string) {
			depsProgressChan <- depsProgressMsg{name: name, status: status, msg: msg}
		})
		return depsUpdateDoneMsg{results: results}
	}
}

func waitForDepsProgress() tea.Msg {
	return <-depsProgressChan
}

func (m *Model) updateDepsUpdate(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case depsProgressMsg:
		// Update the matching dep in results
		found := false
		for i, d := range m.depsResults {
			if d.Name == msg.name {
				m.depsResults[i].Status = msg.status
				m.depsResults[i].Message = msg.msg
				found = true
				break
			}
		}
		if !found {
			m.depsResults = append(m.depsResults, deps.Dep{
				Name: msg.name, Status: msg.status, Message: msg.msg,
			})
		}
		return m, waitForDepsProgress
	case depsUpdateDoneMsg:
		m.depsResults = msg.results
		m.depsUpdating = false
		return m, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "esc":
			if !m.depsUpdating {
				return m.backToSettings()
			}
		case "enter":
			if !m.depsUpdating {
				return m.backToSettings()
			}
		}
	}
	if m.depsUpdating {
		return m, waitForDepsProgress
	}
	return m, nil
}

// ── Theme Picker ────────────────────────────────────────────────────

func (m *Model) updateThemePicker(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "esc":
			return m.backToSettings()
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(themes)-1 {
				m.cursor++
			}
		case "enter":
			selected := themes[m.cursor].Name
			m.userCfg.Theme = selected
			_ = config.SaveUserConfig(m.userCfg)
			return m.backToSettings()
		}
	}
	return m, nil
}

// ── Clean Cache ─────────────────────────────────────────────────────

func (m *Model) scanClean() tea.Cmd {
	return func() tea.Msg {
		// Scan yt-dlp cache directories
		ytdlpEntries, ytdlpSize := clean.ScanYtdlpCache()

		// Scan current directory for partial files
		cwd, _ := os.Getwd()
		partials, partialSize := clean.ScanPartials(cwd)

		// Combine yt-dlp cache as "metas"
		return cleanScanDoneMsg{
			entries:   ytdlpEntries,
			totalSize: ytdlpSize + partialSize,
			partials:  partials,
			partialSz: partialSize,
			metas:     ytdlpEntries,
			metaSz:    ytdlpSize,
		}
	}
}

func (m *Model) updateCleanScan(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case cleanScanDoneMsg:
		if msg.err != nil {
			m.errorMsg = fmt.Sprintf("Scan failed: %v", msg.err)
			return m.backToSettings()
		}
		m.cleanEntries = msg.entries
		m.cleanTotalSize = msg.totalSize
		m.cleanPartials = msg.partials
		m.cleanPartialSz = msg.partialSz
		m.cleanMetas = msg.metas
		m.cleanMetaSz = msg.metaSz

		// Build choices based on what was found
		m.choices = nil
		m.choiceValues = nil

		if len(m.cleanPartials) > 0 {
			m.choices = append(m.choices, fmt.Sprintf("Partial downloads (%d files, %s)", len(m.cleanPartials), clean.FormatBytes(m.cleanPartialSz)))
			m.choiceValues = append(m.choiceValues, "partials")
		}
		if len(m.cleanMetas) > 0 {
			m.choices = append(m.choices, fmt.Sprintf("yt-dlp cache (%d items, %s)", len(m.cleanMetas), clean.FormatBytes(m.cleanMetaSz)))
			m.choiceValues = append(m.choiceValues, "meta")
		}
		if len(m.cleanPartials) > 0 && len(m.cleanMetas) > 0 {
			m.choices = append(m.choices, fmt.Sprintf("Both (%s total)", clean.FormatBytes(m.cleanTotalSize)))
			m.choiceValues = append(m.choiceValues, "both")
		}

		if len(m.choices) == 0 {
			// Nothing to clean
			m.cleanRemoved = 0
			m.cleanFreed = 0
			m.cleanErrs = nil
			m.state = cleanDoneState
			return m, nil
		}

		m.choices = append(m.choices, "Cancel")
		m.choiceValues = append(m.choiceValues, "cancel")

		m.state = cleanListState
		m.cursor = 0
		return m, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "esc":
			return m.backToSettings()
		}
	}
	return m, nil
}

func (m *Model) updateCleanList(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "esc":
			return m.backToSettings()
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.choices)-1 {
				m.cursor++
			}
		case "enter":
			val := m.choiceValues[m.cursor]
			if val == "cancel" {
				return m.backToSettings()
			}
			m.cleanAction = val
			// Build confirmation
			m.state = cleanConfirmState
			m.cursor = 0
			var sizeStr string
			switch val {
			case "partials":
				sizeStr = clean.FormatBytes(m.cleanPartialSz)
			case "meta":
				sizeStr = clean.FormatBytes(m.cleanMetaSz)
			case "both":
				sizeStr = clean.FormatBytes(m.cleanTotalSize)
			}
			m.choices = []string{
				fmt.Sprintf("Yes, remove (%s)", sizeStr),
				"No, cancel",
			}
			m.choiceValues = []string{"yes", "no"}
			return m, nil
		}
	}
	return m, nil
}

func (m *Model) updateCleanConfirm(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "esc":
			return m.backToSettings()
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.choices)-1 {
				m.cursor++
			}
		case "enter":
			val := m.choiceValues[m.cursor]
			if val == "no" {
				return m.backToSettings()
			}
			// Execute cleanup
			return m, m.executeClean()
		}
	}
	return m, nil
}

func (m *Model) executeClean() tea.Cmd {
	action := m.cleanAction
	partials := m.cleanPartials
	metas := m.cleanMetas

	return func() tea.Msg {
		var removed int
		var freed int64
		var errs []error

		switch action {
		case "partials":
			removed, freed, errs = clean.RemoveEntries(partials)
		case "meta":
			removed, freed, errs = clean.RemoveEntries(metas)
		case "both":
			combined := append(partials, metas...)
			removed, freed, errs = clean.RemoveEntries(combined)
		}

		return cleanDoneMsg{removed: removed, freed: freed, errs: errs}
	}
}

func (m *Model) updateCleanDone(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case cleanDoneMsg:
		m.cleanRemoved = msg.removed
		m.cleanFreed = msg.freed
		m.cleanErrs = msg.errs
		return m, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "esc", "enter":
			return m.backToSettings()
		}
	}
	return m, nil
}

// ── Terminal helpers ────────────────────────────────────────────────

func getTerminalSize() (int, int) {
	w, h, _ := term.GetSize(int(os.Stdout.Fd()))
	if w == 0 {
		return 80, 24
	}
	return w, h
}

// themeColor generates a hex color string based on theme and animation offset
func themeColor(themeName string, offset int) string {
	t := getTheme(themeName)
	if t.Mono {
		// White theme: pulsing brightness
		v := 0.7 + 0.3*math.Sin(float64(offset)*math.Pi/180)
		c := int(v * 255)
		return fmt.Sprintf("#%02x%02x%02x", c, c, c)
	}
	if t.Hue < 0 {
		// Rainbow: cycle through full hue spectrum
		hue := float64(offset%360) / 360.0
		r, g, b := hsvToRGB(hue, 0.8, 1.0)
		return fmt.Sprintf("#%02x%02x%02x", int(r*255), int(g*255), int(b*255))
	}
	// Static hue: pulse saturation and brightness
	s := 0.6 + 0.4*math.Sin(float64(offset)*math.Pi/180)
	v := 0.7 + 0.3*math.Sin(float64(offset)*math.Pi/180*1.3)
	r, g, b := hsvToRGB(t.Hue/360.0, s, v)
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

// ── View ────────────────────────────────────────────────────────────

func (m *Model) View() string {
	termW, termH := getTerminalSize()

	maxContentWidth := termW - 20
	if maxContentWidth < 40 {
		maxContentWidth = 40
	}
	if maxContentWidth > 80 {
		maxContentWidth = 80
	}

	// Get current theme name
	currentTheme := m.userCfg.Theme
	if currentTheme == "" {
		currentTheme = "Rainbow"
	}

	// Generate theme colors
	rc1 := lipgloss.Color(themeColor(currentTheme, m.rainbowOffset))
	rc2 := lipgloss.Color(themeColor(currentTheme, m.rainbowOffset+60))
	rc3 := lipgloss.Color(themeColor(currentTheme, m.rainbowOffset+120))

	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(rc1).PaddingBottom(1).Align(lipgloss.Center).Width(maxContentWidth)
	choiceStyle := lipgloss.NewStyle().PaddingLeft(2).Width(maxContentWidth)
	selectedStyle := lipgloss.NewStyle().PaddingLeft(2).Foreground(rc2).Bold(true).Width(maxContentWidth)
	inputStyle := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(rc1).
		BorderBackground(lipgloss.Color("")).
		Padding(0, 1).
		MarginTop(1).
		Width(maxContentWidth - 4)
	panelStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(rc3).
		BorderBackground(lipgloss.Color("")).
		Padding(1, 2).
		Width(maxContentWidth + 6)
	footerStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Align(lipgloss.Center).Width(maxContentWidth)

	appNameStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(rc1).
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

	dimStyle := lipgloss.NewStyle().Faint(true).Width(maxContentWidth)

	// Build the complete view
	var content strings.Builder

	content.WriteString(appNameStyle.Render("Yaria"))
	content.WriteString("\n")
	content.WriteString(quoteStyle.Render(m.currentQuote))
	content.WriteString("\n")

	var mainContent strings.Builder
	switch m.state {
	case mainMenuState:
		mainContent.WriteString(headerStyle.Render("Main Menu"))
		mainContent.WriteString("\n")
		for i, choice := range m.choices {
			if m.cursor == i {
				mainContent.WriteString(selectedStyle.Render(fmt.Sprintf("> %s", choice)))
			} else {
				mainContent.WriteString(choiceStyle.Render(fmt.Sprintf("  %s", choice)))
			}
			mainContent.WriteString("\n")
		}

	case urlState:
		mainContent.WriteString(headerStyle.Render("Enter video URL"))
		mainContent.WriteString("\n")
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
			displayChoice := choice
			if len(displayChoice) > maxContentWidth-5 {
				displayChoice = displayChoice[:maxContentWidth-8] + "..."
			}
			if m.cursor == i {
				mainContent.WriteString(selectedStyle.Render(fmt.Sprintf("> %s", displayChoice)))
			} else {
				mainContent.WriteString(choiceStyle.Render(fmt.Sprintf("  %s", displayChoice)))
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
		rabbitStyle := lipgloss.NewStyle().
			Foreground(rc1).
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
		rabbitStyle := lipgloss.NewStyle().
			Foreground(rc1).
			Align(lipgloss.Center).
			Width(maxContentWidth).
			MarginTop(1)
		mainContent.WriteString(rabbitStyle.Render(getRabbitFrame(m.rabbitFrame)))

	case resolutionState:
		mainContent.WriteString(headerStyle.Render("Select resolution"))
		mainContent.WriteString("\n")
		for i, choice := range m.choices {
			displayChoice := choice
			if len(displayChoice) > maxContentWidth-5 {
				displayChoice = displayChoice[:maxContentWidth-8] + "..."
			}
			if m.cursor == i {
				mainContent.WriteString(selectedStyle.Render(fmt.Sprintf("> %s", displayChoice)))
			} else {
				mainContent.WriteString(choiceStyle.Render(fmt.Sprintf("  %s", displayChoice)))
			}
			mainContent.WriteString("\n")
		}
		noteStyle := lipgloss.NewStyle().Faint(true).Width(maxContentWidth)
		mainContent.WriteString("\n" + noteStyle.Render(
			"Note: Some formats may be restricted by YouTube.\nIf download fails, try Default or run `yt-dlp --list-formats <URL>`."))

	case downloadLocationState:
		mainContent.WriteString(headerStyle.Render("Choose Download Location"))
		mainContent.WriteString("\n")
		for i, choice := range m.locationChoices {
			if m.cursor == i {
				mainContent.WriteString(selectedStyle.Render(fmt.Sprintf("> %s", choice)))
			} else {
				mainContent.WriteString(choiceStyle.Render(fmt.Sprintf("  %s", choice)))
			}
			mainContent.WriteString("\n")
		}
		mainContent.WriteString("\n")
		infoStyle := lipgloss.NewStyle().Faint(true).Width(maxContentWidth).Align(lipgloss.Center)
		mainContent.WriteString(infoStyle.Render("Files will be downloaded to the selected location"))

	case downloadingState:
		mainContent.WriteString(headerStyle.Render("Downloading"))
		mainContent.WriteString("\n")

		// Show title if available
		if m.Title != "" {
			displayTitle := m.Title
			maxTitleW := maxContentWidth - 4
			if len(displayTitle) > maxTitleW {
				displayTitle = displayTitle[:maxTitleW-3] + "..."
			}
			titleStyle := lipgloss.NewStyle().Width(maxContentWidth).Align(lipgloss.Center).Faint(true)
			mainContent.WriteString(titleStyle.Render(displayTitle))
		}
		mainContent.WriteString("\n")

		progressMsg := m.downloadProgress
		if progressMsg == "" {
			progressMsg = "Preparing download..."
		}
		progressStyle := lipgloss.NewStyle().Width(maxContentWidth).Align(lipgloss.Center)
		mainContent.WriteString(progressStyle.Render(progressMsg))
		mainContent.WriteString("\n\n")

		barWidth := maxContentWidth - 10
		if barWidth < 10 {
			barWidth = 10
		}
		filledWidth := int(float64(barWidth) * m.downloadPercent / 100.0)
		emptyWidth := barWidth - filledWidth
		progressBar := strings.Repeat("\u2588", filledWidth) + strings.Repeat("\u2591", emptyWidth)
		progressBarStyle := lipgloss.NewStyle().Width(maxContentWidth).Align(lipgloss.Center).Foreground(rc2)
		mainContent.WriteString(progressBarStyle.Render(progressBar))
		mainContent.WriteString("\n")
		mainContent.WriteString(progressBarStyle.Render(fmt.Sprintf("%.1f%%", m.downloadPercent)))
		mainContent.WriteString("\n")

		if m.downloadSpeed != "" || m.downloadETA != "" {
			infoStyle := lipgloss.NewStyle().Width(maxContentWidth).Align(lipgloss.Center).Faint(true)
			mainContent.WriteString("\n")
			parts := []string{}
			if m.downloadSpeed != "" {
				parts = append(parts, "Speed: "+m.downloadSpeed)
			}
			if m.downloadETA != "" {
				parts = append(parts, "ETA: "+m.downloadETA)
			}
			mainContent.WriteString(infoStyle.Render(strings.Join(parts, " | ")))
		}

	case downloadCompleteState:
		if m.downloadError == "already_exists" {
			warnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("220")).Bold(true).Width(maxContentWidth).Align(lipgloss.Center)
			mainContent.WriteString(headerStyle.Render("Already Downloaded"))
			mainContent.WriteString("\n\n")
			warnStyle2 := lipgloss.NewStyle().Foreground(lipgloss.Color("220")).Width(maxContentWidth).Align(lipgloss.Center)
			mainContent.WriteString(warnStyle.Render("This file already exists in the download directory"))
			if m.Title != "" {
				mainContent.WriteString("\n\n")
				mainContent.WriteString(warnStyle2.Render(m.Title))
			}
			mainContent.WriteString("\n\n")
			infoStyle := lipgloss.NewStyle().Faint(true).Width(maxContentWidth).Align(lipgloss.Center)
			mainContent.WriteString(infoStyle.Render("Press Enter or Esc to continue"))
		} else if m.downloadComplete {
			successStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Bold(true).Width(maxContentWidth).Align(lipgloss.Center)
			mainContent.WriteString(headerStyle.Render("Download Complete!"))
			mainContent.WriteString("\n\n")
			mainContent.WriteString(successStyle.Render("Download finished successfully"))
			mainContent.WriteString("\n\n")
			infoStyle := lipgloss.NewStyle().Faint(true).Width(maxContentWidth).Align(lipgloss.Center)
			mainContent.WriteString(infoStyle.Render("Press Enter or Esc to continue"))
		} else if m.downloadError != "" {
			errorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true).Width(maxContentWidth).Align(lipgloss.Center)
			mainContent.WriteString(headerStyle.Render("Download Failed"))
			mainContent.WriteString("\n\n")
			mainContent.WriteString(errorStyle.Render(m.downloadError))
			mainContent.WriteString("\n\n")
			infoStyle := lipgloss.NewStyle().Faint(true).Width(maxContentWidth).Align(lipgloss.Center)
			mainContent.WriteString(infoStyle.Render("Press Enter or Esc to continue"))
		}

	case downloadsState:
		mainContent.WriteString(headerStyle.Render("Downloads"))
		mainContent.WriteString("\n")
		if len(m.daemonDownloads) == 0 {
			dimCenter := lipgloss.NewStyle().Faint(true).Width(maxContentWidth).Align(lipgloss.Center)
			mainContent.WriteString(dimCenter.Render("No downloads"))
		} else {
			for i, dl := range m.daemonDownloads {
				// State icon
				var icon string
				var iconColor lipgloss.Color
				switch dl.State {
				case "downloading":
					icon = "v"
					iconColor = lipgloss.Color("42") // green
				case "paused":
					icon = "||"
					iconColor = lipgloss.Color("220") // yellow
				case "complete":
					icon = "*"
					iconColor = lipgloss.Color("39") // cyan
				case "exists":
					icon = "="
					iconColor = lipgloss.Color("220") // yellow
				case "error":
					icon = "x"
					iconColor = lipgloss.Color("196") // red
				default:
					icon = "..."
					iconColor = lipgloss.Color("213") // magenta
				}
				iconStr := lipgloss.NewStyle().Foreground(iconColor).Render(icon)

				// Title (truncated)
				title := dl.Title
				if title == "" {
					title = dl.URL
				}
				maxTitle := maxContentWidth - 20
				if maxTitle < 10 {
					maxTitle = 10
				}
				if len(title) > maxTitle {
					title = title[:maxTitle-3] + "..."
				}

				// Progress bar (small inline)
				barW := 20
				filled := int(float64(barW) * dl.Percent / 100.0)
				empty := barW - filled
				bar := strings.Repeat("\u2588", filled) + strings.Repeat("\u2591", empty)

				// Stats line
				stats := fmt.Sprintf("%.0f%%", dl.Percent)
				if dl.Speed != "" {
					stats += " " + dl.Speed
				}
				if dl.ETA != "" {
					stats += " ETA:" + dl.ETA
				}
				if dl.State == "error" && dl.Error != "" {
					errTrunc := dl.Error
					if len(errTrunc) > 40 {
						errTrunc = errTrunc[:37] + "..."
					}
					stats = errTrunc
				}

				line := fmt.Sprintf("%s %s\n    %s %s", iconStr, title, bar, stats)

				if m.cursor == i {
					mainContent.WriteString(selectedStyle.Render("> " + line))
				} else {
					mainContent.WriteString(choiceStyle.Render("  " + line))
				}
				mainContent.WriteString("\n")
			}
		}
		mainContent.WriteString("\n")
		dimFooter := lipgloss.NewStyle().Faint(true).Width(maxContentWidth).Align(lipgloss.Center)
		mainContent.WriteString(dimFooter.Render("enter: details  p: pause/resume  d: remove  esc: back"))

	case settingsState:
		mainContent.WriteString(headerStyle.Render("Settings"))
		mainContent.WriteString("\n")
		currentThemeName := m.userCfg.Theme
		if currentThemeName == "" {
			currentThemeName = "Rainbow"
		}
		mainContent.WriteString(dimStyle.Align(lipgloss.Center).Render("Current theme: " + currentThemeName))
		mainContent.WriteString("\n\n")
		for i, choice := range m.choices {
			if m.cursor == i {
				mainContent.WriteString(selectedStyle.Render(fmt.Sprintf("> %s", choice)))
			} else {
				mainContent.WriteString(choiceStyle.Render(fmt.Sprintf("  %s", choice)))
			}
			mainContent.WriteString("\n")
		}

	case themePickerState:
		mainContent.WriteString(headerStyle.Render("Choose Theme"))
		mainContent.WriteString("\n")
		currentThemeName := m.userCfg.Theme
		if currentThemeName == "" {
			currentThemeName = "Rainbow"
		}
		for i, t := range themes {
			preview := themeColor(t.Name, m.rainbowOffset)
			dot := lipgloss.NewStyle().Foreground(lipgloss.Color(preview)).Render("*")
			label := t.Name
			if strings.EqualFold(label, currentThemeName) {
				label += " (current)"
			}
			if m.cursor == i {
				mainContent.WriteString(selectedStyle.Render(fmt.Sprintf("> %s %s", dot, label)))
			} else {
				mainContent.WriteString(choiceStyle.Render(fmt.Sprintf("  %s %s", dot, label)))
			}
			mainContent.WriteString("\n")
		}

	case cleanScanState:
		mainContent.WriteString(headerStyle.Render("Scanning for cache data..."))
		mainContent.WriteString("\n")
		rabbitStyle := lipgloss.NewStyle().
			Foreground(rc1).
			Align(lipgloss.Center).
			Width(maxContentWidth).
			MarginTop(1)
		mainContent.WriteString(rabbitStyle.Render(getRabbitFrame(m.rabbitFrame)))

	case cleanListState:
		mainContent.WriteString(headerStyle.Render("Clear Cache"))
		mainContent.WriteString("\n")
		mainContent.WriteString(dimStyle.Align(lipgloss.Center).Render(fmt.Sprintf("Total cleanable: %s", clean.FormatBytes(m.cleanTotalSize))))
		mainContent.WriteString("\n\n")
		for i, choice := range m.choices {
			if m.cursor == i {
				mainContent.WriteString(selectedStyle.Render(fmt.Sprintf("> %s", choice)))
			} else {
				mainContent.WriteString(choiceStyle.Render(fmt.Sprintf("  %s", choice)))
			}
			mainContent.WriteString("\n")
		}

	case cleanConfirmState:
		mainContent.WriteString(headerStyle.Render("Confirm Cleanup"))
		mainContent.WriteString("\n")
		mainContent.WriteString(dimStyle.Align(lipgloss.Center).Render("This action cannot be undone."))
		mainContent.WriteString("\n\n")
		for i, choice := range m.choices {
			if m.cursor == i {
				mainContent.WriteString(selectedStyle.Render(fmt.Sprintf("> %s", choice)))
			} else {
				mainContent.WriteString(choiceStyle.Render(fmt.Sprintf("  %s", choice)))
			}
			mainContent.WriteString("\n")
		}

	case cleanDoneState:
		if m.cleanRemoved == 0 && m.cleanFreed == 0 {
			mainContent.WriteString(headerStyle.Render("Nothing to Clean"))
			mainContent.WriteString("\n\n")
			successStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Bold(true).Width(maxContentWidth).Align(lipgloss.Center)
			mainContent.WriteString(successStyle.Render("No cache or partial files found"))
		} else {
			mainContent.WriteString(headerStyle.Render("Cleanup Complete"))
			mainContent.WriteString("\n\n")
			successStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Bold(true).Width(maxContentWidth).Align(lipgloss.Center)
			mainContent.WriteString(successStyle.Render(fmt.Sprintf("Removed %d items, freed %s", m.cleanRemoved, clean.FormatBytes(m.cleanFreed))))
			if len(m.cleanErrs) > 0 {
				errorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Width(maxContentWidth).Align(lipgloss.Center)
				mainContent.WriteString("\n\n")
				mainContent.WriteString(errorStyle.Render(fmt.Sprintf("%d errors occurred during cleanup", len(m.cleanErrs))))
			}
		}
		mainContent.WriteString("\n\n")
		infoStyle := lipgloss.NewStyle().Faint(true).Width(maxContentWidth).Align(lipgloss.Center)
		mainContent.WriteString(infoStyle.Render("Press Enter or Esc to continue"))

	case depsCheckState, depsUpdateState:
		if m.depsUpdating {
			mainContent.WriteString(headerStyle.Render("Updating Dependencies..."))
		} else {
			mainContent.WriteString(headerStyle.Render("Dependencies"))
		}
		mainContent.WriteString("\n")

		if len(m.depsResults) == 0 {
			dimCenter := lipgloss.NewStyle().Faint(true).Width(maxContentWidth).Align(lipgloss.Center)
			mainContent.WriteString(dimCenter.Render("Checking dependencies..."))
			mainContent.WriteString("\n")
			rabbitStyle := lipgloss.NewStyle().
				Foreground(rc1).
				Align(lipgloss.Center).
				Width(maxContentWidth).
				MarginTop(1)
			mainContent.WriteString(rabbitStyle.Render(getRabbitFrame(m.rabbitFrame)))
		} else {
			for _, d := range m.depsResults {
				var icon string
				var iconColor lipgloss.Color
				switch d.Status {
				case "installed":
					icon = "*"
					iconColor = lipgloss.Color("42") // green
				case "updated":
					icon = "*"
					iconColor = lipgloss.Color("39") // cyan
				case "missing":
					icon = "x"
					iconColor = lipgloss.Color("196") // red
				case "updating":
					icon = "..."
					iconColor = lipgloss.Color("213") // magenta
				case "failed":
					icon = "!"
					iconColor = lipgloss.Color("196") // red
				default:
					icon = "?"
					iconColor = lipgloss.Color("240")
				}
				iconStr := lipgloss.NewStyle().Foreground(iconColor).Render(icon)

				statusText := d.Status
				if d.Message != "" && d.Status != "installed" && d.Status != "updated" {
					statusText = d.Message
					if len(statusText) > maxContentWidth-30 {
						statusText = statusText[:maxContentWidth-33] + "..."
					}
				}
				if d.Status == "installed" && d.Path != "" {
					short := d.Path
					if len(short) > 35 {
						short = "..." + short[len(short)-32:]
					}
					statusText = short
				}

				line := fmt.Sprintf("%s  %-18s %s", iconStr, d.Name, statusText)
				mainContent.WriteString(choiceStyle.Render(line))
				mainContent.WriteString("\n")
			}

			mainContent.WriteString("\n")
			if m.depsUpdating {
				dimCenter := lipgloss.NewStyle().Faint(true).Width(maxContentWidth).Align(lipgloss.Center)
				mainContent.WriteString(dimCenter.Render("Please wait..."))
			} else if m.state == depsCheckState {
				dimCenter := lipgloss.NewStyle().Faint(true).Width(maxContentWidth).Align(lipgloss.Center)
				mainContent.WriteString(dimCenter.Render("Press Enter to download/update all | Esc to go back"))
			} else {
				dimCenter := lipgloss.NewStyle().Faint(true).Width(maxContentWidth).Align(lipgloss.Center)
				mainContent.WriteString(dimCenter.Render("Press Enter or Esc to go back"))
			}
		}
	}

	// Display error message if present
	if m.errorMsg != "" {
		errorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true).Width(maxContentWidth).Align(lipgloss.Center)
		mainContent.WriteString("\n\n")
		mainContent.WriteString(errorStyle.Render(m.errorMsg))
	}

	// Build footer with navigation hint
	var footerText string
	switch m.state {
	case mainMenuState:
		footerText = "Press Esc or Ctrl+C to quit"
	case downloadingState:
		footerText = "Esc: back (download continues) | Ctrl+C: quit"
	case downloadCompleteState:
		footerText = "Press Enter or Esc to continue"
	case downloadsState:
		footerText = "enter: details  p: pause/resume  d: remove  esc: back"
	default:
		footerText = "Press Esc to go back | Ctrl+C to quit"
	}

	mainPanel := panelStyle.Render(mainContent.String())
	footer := footerStyle.Render(footerText)
	combined := lipgloss.JoinVertical(lipgloss.Center, content.String(), mainPanel, footer)
	ui := lipgloss.Place(termW, termH, lipgloss.Center, lipgloss.Center, combined)
	return ui
}

// ── Thumbnail rendering ─────────────────────────────────────────────

func (m *Model) renderThumbnail(width int) string {
	if m.ThumbnailPath == "" {
		return ""
	}
	if m.IsKittyTerminal {
		return m.renderKittyImage(width)
	}
	return m.renderASCIIArt(width)
}

func (m *Model) renderKittyImage(width int) string {
	if _, err := os.Stat(m.ThumbnailPath); err != nil {
		return ""
	}
	imageData, err := os.ReadFile(m.ThumbnailPath)
	if err != nil {
		return ""
	}
	encoded := base64.StdEncoding.EncodeToString(imageData)
	chunkSize := 4096
	var result strings.Builder

	if len(encoded) <= chunkSize {
		result.WriteString(fmt.Sprintf("\033_Gf=100,a=T,t=d;%s\033\\", encoded))
		return result.String()
	}

	for i := 0; i < len(encoded); i += chunkSize {
		end := i + chunkSize
		if end > len(encoded) {
			end = len(encoded)
		}
		chunk := encoded[i:end]
		if i == 0 {
			result.WriteString(fmt.Sprintf("\033_Gf=100,a=T,t=d,m=1;%s\033\\", chunk))
		} else if end >= len(encoded) {
			result.WriteString(fmt.Sprintf("\033_Gm=0;%s\033\\", chunk))
		} else {
			result.WriteString(fmt.Sprintf("\033_Gm=1;%s\033\\", chunk))
		}
	}
	return result.String()
}

func (m *Model) renderASCIIArt(width int) string {
	return ""
}

// ── Yazi file explorer ──────────────────────────────────────────────

func (m *Model) launchYaziFileExplorer() tea.Cmd {
	tmpFile := "/tmp/yaria-selected-path"
	if runtime.GOOS == "windows" {
		tmpFile = os.TempDir() + "\\yaria-selected-path"
	}

	exePath, _ := os.Executable()
	depsDir := filepath.Join(filepath.Dir(exePath), "dependencies")
	yaziBinary := filepath.Join(depsDir, "yazi")
	if runtime.GOOS == "windows" {
		yaziBinary += ".exe"
	}

	yaziPath := yaziBinary
	if _, err := os.Stat(yaziPath); err != nil {
		if _, err := exec.LookPath("yazi"); err != nil {
			return func() tea.Msg {
				return yaziLocationSelectedMsg{path: m.TempDir}
			}
		}
		yaziPath = "yazi"
	}

	c := exec.Command(yaziPath, "--chooser-file", tmpFile)
	return tea.ExecProcess(c, func(err error) tea.Msg {
		if err == nil {
			if path, readErr := os.ReadFile(tmpFile); readErr == nil {
				selectedPath := strings.TrimSpace(string(path))
				if selectedPath != "" {
					os.Remove(tmpFile)
					return yaziLocationSelectedMsg{path: selectedPath}
				}
			}
		}
		os.Remove(tmpFile)
		return yaziLocationSelectedMsg{path: m.TempDir}
	})
}
