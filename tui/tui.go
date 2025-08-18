package tui

import (
	"fmt"
	"os"
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
	formatState
	resolutionState
	confirmationState
	loadingState
)

type Model struct {
	cfg          *config.Config
	log          logger.Logger
	dl           downloader.Downloader
	state        state
	url          string
	title        string
	formats      []downloader.Format
	videoFormats []downloader.Format
	cursor       int
	choices      []string
	Confirmed    bool
	URL          string
	urlInput     string
	loadingStart time.Time
	loadingDots  string
}

func New(cfg *config.Config, log logger.Logger) *Model {
	return &Model{
		cfg:   cfg,
		log:   log,
		state: urlState,
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
	m.title = title
	if url != "" {
		m.state = formatState // Skip URL input if provided
	}
	p := tea.NewProgram(m, tea.WithInputTTY())
	_, err := p.Run()
	return err
}

func (m *Model) Init() tea.Cmd {
	if m.state == formatState && m.url != "" {
		return m.startLoading
	}
	return nil
}

func (m *Model) startLoading() tea.Msg {
	return tickMsg{}
}

type tickMsg struct{}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m.state {
	case urlState:
		return m.updateURL(msg)
	case formatState:
		return m.updateFormat(msg)
	case resolutionState:
		return m.updateResolution(msg)
	case confirmationState:
		return m.updateConfirmation(msg)
	case loadingState:
		return m.updateLoading(msg)
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
				m.log.Error("❌ Error: No URL provided")
				return m, tea.Quit
			}
			m.url = m.URL
			_, m.title, _ = m.dl.GetMetadata([]string{m.URL})
			m.state = formatState
			m.cursor = 0
			return m, m.startLoading
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
				m.state = loadingState
				m.loadingStart = time.Now()
				m.loadingDots = "."
				return m, tea.Tick(time.Millisecond*500, func(t time.Time) tea.Msg {
					return tickMsg{}
				})
			} else {
				m.cfg.IsAudioOnly = true
				m.state = confirmationState
				m.cursor = 0
			}
		}
	case tickMsg:
		m.state = loadingState
		m.loadingStart = time.Now()
		m.loadingDots = "."
		return m, tea.Tick(time.Millisecond*500, func(t time.Time) tea.Msg {
			return tickMsg{}
		})
	}
	return m, nil
}

func (m *Model) updateLoading(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg.(type) {
	case tickMsg:
		m.loadingDots = strings.Repeat(".", (int(time.Since(m.loadingStart)/time.Millisecond/500)%3)+1)
		formats, err := m.dl.GetFormats(m.url)
		if err != nil {
			m.log.Error("❌ Failed to fetch formats: %v", err)
			return m, tea.Quit
		}
		m.formats = formats
		m.videoFormats = []downloader.Format{}
		for _, f := range formats {
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
				m.choices = append(m.choices, fmt.Sprintf("%dp (%s, %s)", f.Height, f.Ext, f.Protocol))
			}
			m.state = resolutionState
			m.cursor = 0
		}
		return m, nil
	}
	return m, tea.Tick(time.Millisecond*500, func(t time.Time) tea.Msg {
		return tickMsg{}
	})
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
			} else {
				m.cfg.Resolution = m.videoFormats[m.cursor-1].ID
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
			return m, tea.Quit
		case "n":
			return m, tea.Quit
		}
	}
	return m, nil
}

func getTerminalSize() (width, height int) {
	if w, err := strconv.Atoi(os.Getenv("COLUMNS")); err == nil && w > 0 {
		width = w
	}
	if h, err := strconv.Atoi(os.Getenv("LINES")); err == nil && h > 0 {
		height = h
	}
	if w, h2, err := term.GetSize(int(os.Stdout.Fd())); err == nil {
		width, height = w, h2
	}
	if width == 0 {
		width = 80
	}
	if height == 0 {
		height = 24
	}
	return
}

func (m *Model) View() string {
	termW, termH := getTerminalSize()
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205")).PaddingBottom(1).Align(lipgloss.Center)
	choiceStyle := lipgloss.NewStyle().PaddingLeft(2)
	selectedStyle := lipgloss.NewStyle().PaddingLeft(2).Foreground(lipgloss.Color("212")).Bold(true)
	inputStyle := lipgloss.NewStyle().Border(lipgloss.NormalBorder()).Padding(0, 1).MarginTop(1).Align(lipgloss.Center)
	panelStyle := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(1, 2).Align(lipgloss.Center)
	footerStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Align(lipgloss.Center)

	var mainContent strings.Builder
	switch m.state {
	case urlState:
		mainContent.WriteString(headerStyle.Render("Enter video URL"))
		mainContent.WriteString("\n")
		mainContent.WriteString(inputStyle.Render(m.urlInput + "|"))
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
	case loadingState:
		mainContent.WriteString(headerStyle.Render("Fetching formats" + m.loadingDots))
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
		mainContent.WriteString(headerStyle.Render(fmt.Sprintf("Download '%s'? (y/n)", m.title)))
	}

	mainPanel := panelStyle.Render(mainContent.String())
	ui := lipgloss.Place(termW, termH, lipgloss.Center, lipgloss.Center, mainPanel)
	_ = footerStyle.Render("Press q to quit")
	return ui
}
