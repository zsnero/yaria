package menu

import (
	"fmt"
	"math"
	"os"
	"strings"
	"time"

	"yaria/internal/license"
	"yaria/internal/pro"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"golang.org/x/term"
)

const version = "1.0.0"

// Result represents what the user selected from the menu.
type Result struct {
	Choice string // "yaria", "mantorex", "activate", "deactivate", "quit"
}

type menuState int

const (
	mainMenuState menuState = iota
	activateKeyState
	activatingState
	activateResultState
)

type animTickMsg struct{}
type activateResultMsg struct {
	success bool
	message string
}

type model struct {
	state           menuState
	cursor          int
	choices         []string
	choiceValues    []string
	isPro           bool
	licenseInfo     *license.LicenseInfo
	rainbowOffset   int
	textInput       string
	activateMsg     string
	activateSuccess bool
	result          *Result
	currentQuote    string
}

var quotes = []string{
	"All your media in one place",
	"Download anything, stream everything",
	"Powered by Go and good vibes",
	"The ultimate media companion",
	"Two tools, one binary",
	"Free as in freedom",
	"Pro features, pro experience",
	"Your media, your rules",
	"Built for power users",
	"Fast, simple, powerful",
}

func newModel() *model {
	info := license.CheckLicense()
	isPro := info.Valid && info.Plan == "pro"

	m := &model{
		state:        mainMenuState,
		isPro:        isPro,
		licenseInfo:  info,
		currentQuote: quotes[time.Now().UnixNano()%int64(len(quotes))],
	}
	m.buildChoices()
	return m
}

func (m *model) buildChoices() {
	m.choices = []string{}
	m.choiceValues = []string{}

	m.choices = append(m.choices, "Yaria - Video/Audio Downloader")
	m.choiceValues = append(m.choiceValues, "yaria")

	if !pro.Available() {
		// Community build: Mantorex not compiled in
		m.choices = append(m.choices, "Mantorex - Torrent Search & Stream (Pro Build Only)")
		m.choiceValues = append(m.choiceValues, "mantorex_unavailable")
	} else if m.isPro {
		// Pro build + licensed
		m.choices = append(m.choices, "Mantorex - Torrent Search & Stream (Pro)")
		m.choiceValues = append(m.choiceValues, "mantorex")
	} else {
		// Pro build + no license
		m.choices = append(m.choices, "Mantorex - Torrent Search & Stream (Locked)")
		m.choiceValues = append(m.choiceValues, "mantorex_locked")
	}

	if pro.Available() && !m.isPro {
		m.choices = append(m.choices, "Activate Pro License")
		m.choiceValues = append(m.choiceValues, "activate")
	}

	m.choices = append(m.choices, "Quit")
	m.choiceValues = append(m.choiceValues, "quit")
}

func (m *model) Init() tea.Cmd {
	return func() tea.Msg { return animTickMsg{} }
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case animTickMsg:
		m.rainbowOffset = (m.rainbowOffset + 5) % 360
		return m, tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg { return animTickMsg{} })

	case activateResultMsg:
		m.activateMsg = msg.message
		m.activateSuccess = msg.success
		m.state = activateResultState
		if msg.success {
			m.isPro = true
			m.licenseInfo = license.CheckLicense()
			m.buildChoices()
		}
		return m, nil
	}

	switch m.state {
	case mainMenuState:
		return m.updateMainMenu(msg)
	case activateKeyState:
		return m.updateActivateKey(msg)
	case activatingState:
		return m, nil
	case activateResultState:
		return m.updateActivateResult(msg)
	}
	return m, nil
}

func (m *model) updateMainMenu(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			m.result = &Result{Choice: "quit"}
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
			case "yaria":
				m.result = &Result{Choice: "yaria"}
				return m, tea.Quit
			case "mantorex":
				m.result = &Result{Choice: "mantorex"}
				return m, tea.Quit
			case "mantorex_locked":
				m.state = activateKeyState
				m.textInput = ""
				m.cursor = 0
				return m, nil
			case "mantorex_unavailable":
				// Community build: do nothing, just stay on menu
				return m, nil
			case "activate":
				m.state = activateKeyState
				m.textInput = ""
				m.cursor = 0
				return m, nil
			case "quit":
				m.result = &Result{Choice: "quit"}
				return m, tea.Quit
			}
		}
	}
	return m, nil
}

func (m *model) updateActivateKey(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			m.result = &Result{Choice: "quit"}
			return m, tea.Quit
		case "esc":
			m.state = mainMenuState
			m.cursor = 0
			return m, nil
		case "enter":
			key := strings.TrimSpace(m.textInput)
			if key == "" {
				return m, nil
			}
			m.state = activatingState
			return m, m.activateKey(key)
		case "backspace":
			if len(m.textInput) > 0 {
				m.textInput = m.textInput[:len(m.textInput)-1]
			}
		default:
			// Capture typed characters
			if len(msg.Runes) > 0 {
				m.textInput += string(msg.Runes)
			}
		}
	}
	return m, nil
}

func (m *model) activateKey(key string) tea.Cmd {
	return func() tea.Msg {
		info, err := license.ActivateKey(key)
		if err != nil {
			return activateResultMsg{
				success: false,
				message: fmt.Sprintf("Activation failed: %v", err),
			}
		}
		if !info.Valid {
			return activateResultMsg{
				success: false,
				message: "Invalid license key",
			}
		}
		return activateResultMsg{
			success: true,
			message: fmt.Sprintf("License activated! Plan: %s\nBound to: %s", info.Plan, info.DeviceName),
		}
	}
}

func (m *model) updateActivateResult(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			m.result = &Result{Choice: "quit"}
			return m, tea.Quit
		case "enter", "esc":
			m.state = mainMenuState
			m.cursor = 0
			return m, nil
		}
	}
	return m, nil
}

func (m *model) View() string {
	termW, termH := getTerminalSize()

	maxW := termW - 6
	if maxW < 40 {
		maxW = 40
	}
	if maxW > 80 {
		maxW = 80
	}

	rc1 := lipgloss.Color(rainbowColor(m.rainbowOffset))
	rc2 := lipgloss.Color(rainbowColor(m.rainbowOffset + 60))
	rc3 := lipgloss.Color(rainbowColor(m.rainbowOffset + 120))

	headerSt := lipgloss.NewStyle().Bold(true).Foreground(rc1).PaddingBottom(1).Align(lipgloss.Center).Width(maxW)
	choiceSt := lipgloss.NewStyle().PaddingLeft(2).Width(maxW)
	selectedSt := lipgloss.NewStyle().PaddingLeft(2).Foreground(rc2).Bold(true).Width(maxW)
	panelSt := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(rc3).
		Padding(1, 2).
		Width(maxW + 6)
	footerSt := lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Align(lipgloss.Center).Width(maxW)
	appNameSt := lipgloss.NewStyle().Bold(true).Foreground(rc1).Align(lipgloss.Center).Width(maxW).MarginTop(1).MarginBottom(0)
	quoteSt := lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Align(lipgloss.Center).Width(maxW).Italic(true).MarginBottom(1)
	dimSt := lipgloss.NewStyle().Faint(true).Width(maxW)
	inputSt := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(rc1).
		Padding(0, 1).
		MarginTop(1).
		Width(maxW - 4)
	successSt := lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Bold(true).Width(maxW).Align(lipgloss.Center)
	errorSt := lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true).Width(maxW).Align(lipgloss.Center)
	lockedSt := lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Italic(true)

	var top strings.Builder
	if m.isPro {
		proTagSt := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214")).Align(lipgloss.Center).Width(maxW)
		top.WriteString(appNameSt.Render("Yaria"))
		top.WriteString("\n")
		top.WriteString(proTagSt.Render("PRO"))
		top.WriteString("\n")
	} else {
		top.WriteString(appNameSt.Render("Yaria"))
		top.WriteString("\n")
		top.WriteString(quoteSt.Render("v" + version))
		top.WriteString("\n")
	}
	top.WriteString(quoteSt.Render(m.currentQuote))
	top.WriteString("\n")

	var panel strings.Builder

	switch m.state {
	case mainMenuState:
		panel.WriteString(headerSt.Render("Choose a tool"))
		panel.WriteString("\n")

		for i, choice := range m.choices {
			display := choice
			// Dim locked/unavailable mantorex
			if m.choiceValues[i] == "mantorex_locked" || m.choiceValues[i] == "mantorex_unavailable" {
				display = lockedSt.Render(choice)
			}

			if m.cursor == i {
				panel.WriteString(selectedSt.Render("> " + choice))
			} else {
				panel.WriteString(choiceSt.Render("  " + display))
			}
			panel.WriteString("\n")
		}

		if !pro.Available() {
			panel.WriteString("\n")
			panel.WriteString(dimSt.Align(lipgloss.Center).Render("Mantorex requires the official Pro build from yaria.app"))
		} else if !m.isPro {
			panel.WriteString("\n")
			panel.WriteString(dimSt.Align(lipgloss.Center).Render("Purchase a key at yaria.app to unlock Mantorex"))
			panel.WriteString("\n")
			panel.WriteString(dimSt.Align(lipgloss.Center).Render("Each key is valid for one device only"))
		}

		// Show device info
		_, deviceSummary := license.GetDeviceInfo()
		panel.WriteString("\n")
		panel.WriteString(dimSt.Align(lipgloss.Center).Render("Device: " + deviceSummary))

	case activateKeyState:
		panel.WriteString(headerSt.Render("Enter License Key"))
		panel.WriteString("\n")
		panel.WriteString(dimSt.Align(lipgloss.Center).Render("Paste your license key from yaria.app"))
		panel.WriteString("\n")
		_, deviceSummary := license.GetDeviceInfo()
		panel.WriteString(dimSt.Align(lipgloss.Center).Render("This key will be bound to: " + deviceSummary))
		panel.WriteString("\n")

		displayInput := m.textInput
		maxInputW := maxW - 10
		if len(displayInput) > maxInputW {
			displayInput = displayInput[:maxInputW-3] + "..."
		}
		panel.WriteString(inputSt.Render(displayInput + "|"))
		panel.WriteString("\n\n")
		panel.WriteString(dimSt.Align(lipgloss.Center).Render("Enter to activate  |  Esc to go back"))

	case activatingState:
		dots := strings.Repeat(".", (m.rainbowOffset/30)%4+1)
		panel.WriteString(headerSt.Render("Validating license" + dots))
		panel.WriteString("\n\n")
		loadSt := lipgloss.NewStyle().Foreground(rc2).Align(lipgloss.Center).Width(maxW)
		panel.WriteString(loadSt.Render("Contacting license server"))

	case activateResultState:
		if m.activateSuccess {
			panel.WriteString(headerSt.Render("License Activated"))
			panel.WriteString("\n")
			panel.WriteString(successSt.Render(m.activateMsg))
		} else {
			panel.WriteString(headerSt.Render("Activation Failed"))
			panel.WriteString("\n")
			panel.WriteString(errorSt.Render(m.activateMsg))
		}
		panel.WriteString("\n\n")
		panel.WriteString(dimSt.Align(lipgloss.Center).Render("Press Enter to continue"))
	}

	mainPanel := panelSt.Render(panel.String())
	footer := footerSt.Render("Press Ctrl+C to quit")
	combined := lipgloss.JoinVertical(lipgloss.Center, top.String(), mainPanel, footer)
	ui := lipgloss.Place(termW, termH, lipgloss.Center, lipgloss.Center, combined)
	return ui
}

// RunMenu displays the main menu and returns the user's choice.
func RunMenu() *Result {
	m := newModel()
	p := tea.NewProgram(m, tea.WithAltScreen())
	finalModel, err := p.Run()
	if err != nil {
		fmt.Println("Error:", err)
		os.Exit(1)
	}
	if fm, ok := finalModel.(*model); ok && fm.result != nil {
		return fm.result
	}
	return &Result{Choice: "quit"}
}

func getTerminalSize() (int, int) {
	w, h, _ := term.GetSize(int(os.Stdout.Fd()))
	if w == 0 {
		return 80, 24
	}
	return w, h
}

func rainbowColor(offset int) string {
	hue := float64(offset%360) / 360.0
	r, g, b := hsvToRGB(hue, 0.8, 1.0)
	return fmt.Sprintf("#%02x%02x%02x", int(r*255), int(g*255), int(b*255))
}

func hsvToRGB(h, s, v float64) (float64, float64, float64) {
	i := math.Floor(h * 6)
	f := h*6 - i
	p := v * (1 - s)
	q := v * (1 - f*s)
	t := v * (1 - (1-f)*s)

	switch int(i) % 6 {
	case 0:
		return v, t, p
	case 1:
		return q, v, p
	case 2:
		return p, v, t
	case 3:
		return p, q, v
	case 4:
		return t, p, v
	case 5:
		return v, p, q
	}
	return v, t, p
}
