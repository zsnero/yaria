package tui

import (
	"fmt"
	"strings"
	"time"
	"yaria/config"
	"yaria/downloader"
	"yaria/logger"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// state represents the TUI's current screen
type state int

const (
	urlState state = iota
	formatState
	resolutionState
	confirmationState
	loadingState
)

// Model represents the TUI state
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

// Create new TUI model
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

// SetDownloader sets the downloader instance
func (m *Model) SetDownloader(dl downloader.Downloader) {
	m.dl = dl
}

// Run starts the Bubble Tea program
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

// Bubble Tea methods

// Init initializes the model
func (m *Model) Init() tea.Cmd {
	if m.state == formatState && m.url != "" {
		return m.startLoading
	}
	return nil
}

// startLoading starts the loading animation
func (m *Model) startLoading() tea.Msg {
	return tickMsg{}
}

type tickMsg struct{}

// Update handles user input and state transitions
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
			// Fetch title
			var err error
			_, m.title, err = m.dl.GetMetadata([]string{m.URL})
			if err != nil {
				m.log.Error("❌ Error: Failed to fetch video title: %v", err)
				return m, tea.Quit
			}
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
		// Loading animation
		m.loadingDots = strings.Repeat(".", (int(time.Since(m.loadingStart)/time.Millisecond/500)%3)+1)
		// Fetch formats
		formats, err := m.dl.GetFormats(m.url)
		if err != nil {
			m.log.Error("❌ Failed to fetch formats: %v", err)
			return m, tea.Quit
		}
		m.formats = formats
		m.videoFormats = make([]downloader.Format, 0)
		for _, f := range formats {
			if !f.IsAudio {
				m.videoFormats = append(m.videoFormats, f)
			}
		}
		if len(m.videoFormats) == 0 {
			m.log.Warn("⚠️ No specific video formats available, using default (best available)")
			m.cfg.Resolution = "" // Fallback to default
			m.state = confirmationState
			m.cursor = 0
		} else {
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
			if m.cursor < len(m.videoFormats) {
				m.cursor++
			}
		case "enter":
			if m.cursor == 0 {
				m.cfg.Resolution = "" // Default resolution
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

// View renders the TUI
func (m *Model) View() string {
	var s strings.Builder

	// Styles
	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("205")).
		PaddingBottom(1)
	choiceStyle := lipgloss.NewStyle().
		PaddingLeft(2)
	selectedStyle := lipgloss.NewStyle().
		PaddingLeft(2).
		Foreground(lipgloss.Color("212")).
		Bold(true)
	inputStyle := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		Padding(0, 1).
		MarginTop(1)

	switch m.state {
	case urlState:
		s.WriteString(headerStyle.Render("Enter video URL:"))
		s.WriteString("\n")
		s.WriteString(inputStyle.Render(m.urlInput + "|"))
		s.WriteString("\n")
	case formatState:
		s.WriteString(headerStyle.Render("Select download format:"))
		s.WriteString("\n")
		for i, choice := range m.choices {
			if m.cursor == i {
				s.WriteString(selectedStyle.Render(fmt.Sprintf("> %s", choice)))
			} else {
				s.WriteString(choiceStyle.Render(fmt.Sprintf("  %s", choice)))
			}
			s.WriteString("\n")
		}
	case loadingState:
		s.WriteString(headerStyle.Render("Fetching formats" + m.loadingDots))
		s.WriteString("\n")
	case resolutionState:
		s.WriteString(headerStyle.Render("Select resolution:"))
		s.WriteString("\n")
		// Default resolution
		if m.cursor == 0 {
			s.WriteString(selectedStyle.Render("> Default (best available)"))
		} else {
			s.WriteString(choiceStyle.Render("  Default (best available)"))
		}
		s.WriteString("\n")
		// Specific resolutions
		for i, format := range m.videoFormats {
			if m.cursor == i+1 {
				s.WriteString(selectedStyle.Render(fmt.Sprintf("> %dp (%s, %s)", format.Height, format.Ext, format.Protocol)))
			} else {
				s.WriteString(choiceStyle.Render(fmt.Sprintf("  %dp (%s, %s)", format.Height, format.Ext, format.Protocol)))
			}
			s.WriteString("\n")
		}
		s.WriteString("\nNote: Some formats may be restricted by YouTube. If download fails, try Default or run `yt-dlp --list-formats <URL>`.\n")
	case confirmationState:
		s.WriteString(headerStyle.Render(fmt.Sprintf("Download '%s'? (y/n)", m.title)))
		s.WriteString("\n")
	}

	s.WriteString("\nPress q to quit.\n")
	return s.String()
}
