//go:build !pro

package pro

import (
	"fmt"
	"os"

	"yaria/internal/license"

	"github.com/charmbracelet/lipgloss"
)

// Available reports whether the real Mantorex module is compiled in.
func Available() bool {
	return false
}

// RunCLI is called for `yaria mantorex [args...]`.
// In the open-source build this just prints a message.
func RunCLI(args []string) {
	fmt.Println()
	errSt := lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true)
	fmt.Println(errSt.Render("  Mantorex is not included in this build."))
	fmt.Println()
	dimSt := lipgloss.NewStyle().Faint(true)
	fmt.Println(dimSt.Render("  Mantorex is available in the official Yaria Pro binary."))
	fmt.Println(dimSt.Render("  Download it from yaria.app"))
	fmt.Println()
	os.Exit(1)
}

// RunInteractive is called from the TUI menu when user selects Mantorex.
// In the open-source build this prints a message and returns.
func RunInteractive() {
	fmt.Println()
	errSt := lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true)
	fmt.Println(errSt.Render("  Mantorex is not included in this build."))
	fmt.Println()
	dimSt := lipgloss.NewStyle().Faint(true)
	_, deviceSummary := license.GetDeviceInfo()
	fmt.Println(dimSt.Render("  Mantorex is available in the official Yaria Pro binary."))
	fmt.Println(dimSt.Render("  Download it from yaria.app"))
	fmt.Println(dimSt.Render("  This device: " + deviceSummary))
	fmt.Println()
	fmt.Println(dimSt.Render("  Press Enter to continue..."))
	fmt.Scanln()
}
