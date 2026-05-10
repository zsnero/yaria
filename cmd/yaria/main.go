package main

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"yaria/internal/appconfig"
	"yaria/internal/license"
	"yaria/internal/menu"
	"yaria/internal/pro"
	yariaLib "yaria/internal/yaria"

	"github.com/charmbracelet/lipgloss"
)

const version = "1.2.1"

// Site patterns for auto-detection
var (
	youtubePatterns    = []string{"youtube.com", "youtu.be", "youtube-nocookie.com"}
	instagramPatterns  = []string{"instagram.com", "ddinstagram.com"}
	twitterPatterns    = []string{"twitter.com", "x.com", "fixupx.com"}
	tiktokPatterns     = []string{"tiktok.com"}
	facebookPatterns   = []string{"facebook.com", "fb.watch"}
	redditPatterns     = []string{"reddit.com", "old.reddit.com"}
	threadsPatterns    = []string{"threads.net"}
	pinterestPatterns  = []string{"pinterest.com"}
	snapchatPatterns   = []string{"snapchat.com"}
	spotifyPatterns    = []string{"spotify.com", "open.spotify.com"}
	soundcloudPatterns = []string{"soundcloud.com"}
	linkedinPatterns   = []string{"linkedin.com"}
)

func isURL(url string) bool {
	return strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://")
}

func isMagnet(url string) bool {
	return strings.HasPrefix(url, "magnet:")
}

func detectSite(url string) string {
	urlLower := strings.ToLower(url)
	for _, p := range youtubePatterns {
		if strings.Contains(urlLower, p) {
			return "youtube"
		}
	}
	for _, p := range instagramPatterns {
		if strings.Contains(urlLower, p) {
			return "instagram"
		}
	}
	for _, p := range twitterPatterns {
		if strings.Contains(urlLower, p) {
			return "twitter"
		}
	}
	for _, p := range tiktokPatterns {
		if strings.Contains(urlLower, p) {
			return "tiktok"
		}
	}
	for _, p := range facebookPatterns {
		if strings.Contains(urlLower, p) {
			return "facebook"
		}
	}
	for _, p := range redditPatterns {
		if strings.Contains(urlLower, p) {
			return "reddit"
		}
	}
	for _, p := range threadsPatterns {
		if strings.Contains(urlLower, p) {
			return "threads"
		}
	}
	for _, p := range pinterestPatterns {
		if strings.Contains(urlLower, p) {
			return "pinterest"
		}
	}
	for _, p := range snapchatPatterns {
		if strings.Contains(urlLower, p) {
			return "snapchat"
		}
	}
	for _, p := range spotifyPatterns {
		if strings.Contains(urlLower, p) {
			return "spotify"
		}
	}
	for _, p := range soundcloudPatterns {
		if strings.Contains(urlLower, p) {
			return "soundcloud"
		}
	}
	for _, p := range linkedinPatterns {
		if strings.Contains(urlLower, p) {
			return "linkedin"
		}
	}
	return ""
}

// detectVideoURL checks if URL is a video/social media URL that yaria can download
func detectVideoURL(url string) bool {
	return detectSite(url) != ""
}

func printUsage() {
	h := lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	d := lipgloss.NewStyle().Faint(true)
	proSt := lipgloss.NewStyle().Foreground(lipgloss.Color("214"))

	fmt.Println(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12")).Render("Yaria") +
		d.Render(" v"+version))
	fmt.Println()
	fmt.Println(h.Render("Usage:"))
	fmt.Println("  yaria                                Launch interactive menu")
	fmt.Println("  yaria download [args...]              Download video/audio")
	fmt.Println("  yaria mantorex [args...]             Torrent search & stream " + proSt.Render("(Pro)"))
	fmt.Println("  yaria activate <key>                 Activate a Pro license key")
	fmt.Println("  yaria deactivate                     Remove stored license")
	fmt.Println("  yaria status                         Show license status")
	fmt.Println("  yaria --help                         Show this help message")
	fmt.Println()
	fmt.Println(h.Render("Quick Download:"))
	fmt.Println("  yaria <URL>                          Download video with best quality")
	fmt.Println("    Supported: YouTube, Instagram, Twitter, TikTok, Facebook,")
	fmt.Println("                Reddit, Threads, Pinterest, Snapchat, Spotify,")
	fmt.Println("                SoundCloud, LinkedIn")
	fmt.Println()
	fmt.Println(h.Render("Quick Stream/Download:"))
	fmt.Println("  yaria <magnet-link>                 Stream torrent (default)")
	fmt.Println("  yaria -s <magnet-link>               Stream torrent (same as default)")
	fmt.Println("  yaria -d <magnet-link>               Download torrent to current directory")
	fmt.Println()
	fmt.Println(h.Render("Mantorex (Pro):"))
	fmt.Println("  yaria mantorex                       Interactive torrent search TUI")
	fmt.Println("  yaria mantorex web                   Open WebUI in browser")
	fmt.Println("  yaria mantorex <magnet-link>         Stream or download a magnet link")
	fmt.Println("  yaria mantorex daemon                Start background daemon")
	fmt.Println("  yaria mantorex --help                Show Mantorex help")
	fmt.Println()
	fmt.Println(h.Render("License:"))
	fmt.Println("  Purchase a Pro key at " + lipgloss.NewStyle().Bold(true).Render("yaria.live"))
}

func getDefaultPlayer() string {
	// Check for vlc first (preferred for streaming)
	if _, err := exec.LookPath("vlc"); err == nil {
		return "vlc"
	}
	// Fall back to mpv
	if _, err := exec.LookPath("mpv"); err == nil {
		return "mpv"
	}
	return ""
}

func streamWithPlayer(url string) error {
	// Try webtorrent-cli first with streaming
	webtorrentCmd := "webtorrent"
	if runtime.GOOS == "windows" {
		webtorrentCmd = "webtorrent.cmd"
	}

	// Check if webtorrent is available
	if _, err := exec.LookPath(webtorrentCmd); err == nil {
		// Use --vlc flag to auto-stream with default player
		cmd := exec.Command(webtorrentCmd, url, "--vlc")
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}

	// Fall back to vlc/mpv for direct streaming
	player := getDefaultPlayer()
	if player == "" {
		fmt.Println("Error: No streaming option found.")
		fmt.Println("Install webtorrent-cli: npm install -g webtorrent-cli")
		fmt.Println("Or install vlc/mpv for direct streaming")
		os.Exit(1)
	}

	var cmd *exec.Cmd
	switch player {
	case "vlc":
		cmd = exec.Command("vlc", "--network-caching=3000", "--file-caching=3000", url)
	case "mpv":
		cmd = exec.Command("mpv",
			"--cache=yes",
			"--cache-secs=120",
			"--network-timeout=120",
			url)
	}

	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func downloadTorrent(url string) error {
	webtorrentCmd := "webtorrent"
	if runtime.GOOS == "windows" {
		webtorrentCmd = "webtorrent.cmd"
	}

	// Check if webtorrent is available
	if _, err := exec.LookPath(webtorrentCmd); err == nil {
		// Download to current directory
		cmd := exec.Command(webtorrentCmd, url, "--download", ".")
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}

	fmt.Println("Error: webtorrent-cli not found")
	fmt.Println("Install: npm install -g webtorrent-cli")
	os.Exit(1)
	return nil
}

func main() {
	// Initialize unified config on every run
	appconfig.Init()

	args := os.Args[1:]

	// No args: show interactive menu
	if len(args) == 0 {
		runInteractiveMenu()
		return
	}

	switch args[0] {
	case "--help", "-h", "help":
		printUsage()

	case "download", "dl":
		yariaLib.Run(args[1:])

	case "mantorex":
		if !pro.Available() {
			pro.RunCLI(args[1:])
			return
		}
		if !license.IsPro() {
			fmt.Println()
			errSt := lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true)
			fmt.Println(errSt.Render("  Mantorex requires a Pro license."))
			fmt.Println()
			_, deviceSummary := license.GetDeviceInfo()
			dimSt := lipgloss.NewStyle().Faint(true)
			fmt.Println(dimSt.Render("  Activate with: yaria activate <key>"))
			fmt.Println(dimSt.Render("  Purchase a key at yaria.live"))
			fmt.Println(dimSt.Render("  Each key is valid for one device only"))
			fmt.Println(dimSt.Render("  This device: " + deviceSummary))
			fmt.Println()
			os.Exit(1)
		}
		pro.RunCLI(args[1:])

	case "activate":
		if len(args) < 2 {
			fmt.Println("Usage: yaria activate <license-key>")
			os.Exit(1)
		}
		key := strings.TrimSpace(args[1])
		fmt.Println("Validating license key...")
		info, err := license.ActivateKey(key)
		if err != nil {
			errSt := lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true)
			fmt.Println(errSt.Render("Activation failed: " + err.Error()))
			os.Exit(1)
		}
		successSt := lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Bold(true)
		fmt.Println(successSt.Render("License activated!"))
		fmt.Printf("  Plan:   %s\n", info.Plan)
		if info.Email != "" {
			fmt.Printf("  Email:  %s\n", info.Email)
		}
		fmt.Printf("  Device: %s\n", info.DeviceName)

	case "deactivate":
		if err := license.Deactivate(); err != nil {
			fmt.Println("No license to deactivate")
		} else {
			fmt.Println("License deactivated")
		}

	case "status":
		info := license.CheckLicense()
		deviceID, deviceSummary := license.GetDeviceInfo()
		h := lipgloss.NewStyle().Bold(true)
		dimSt := lipgloss.NewStyle().Faint(true)

		if info.Valid && info.Plan == "pro" {
			proLabel := lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Bold(true)
			fmt.Println(h.Render("License: "), proLabel.Render("Pro"))
			if info.Email != "" {
				fmt.Println(h.Render("Email:   "), info.Email)
			}
			fmt.Println(h.Render("Status:  "), lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Render("Active"))
			if !info.ExpiresAt.IsZero() {
				fmt.Println(h.Render("Expires: "), info.ExpiresAt.Format("2006-01-02"))
			}
			fmt.Println(h.Render("Device:  "), deviceSummary)
			fmt.Println(h.Render("ID:      "), dimSt.Render(deviceID))
		} else {
			fmt.Println(h.Render("License: "), "Free")
			fmt.Println(h.Render("Status:  "), dimSt.Render("No Pro license"))
			fmt.Println(h.Render("Device:  "), deviceSummary)
			fmt.Println(h.Render("ID:      "), dimSt.Render(deviceID))
			fmt.Println()
			fmt.Println("  Purchase a key at yaria.live to unlock Mantorex")
			fmt.Println("  Each key is valid for one device only")
		}

		buildType := "Community"
		if pro.Available() {
			buildType = "Pro"
		}
		fmt.Println(h.Render("Build:   "), buildType)

	case "--version", "-v":
		buildType := "community"
		if pro.Available() {
			buildType = "pro"
		}
		fmt.Printf("Yaria v%s (%s)\n", version, buildType)

	case "daemon":
		// Yaria download daemon
		yariaLib.Run([]string{"daemon"})

	default:
		url := args[0]

		// Check for stream/download flags
		streamMode := false
		downloadMode := false

		for i, arg := range args {
			if arg == "-s" || arg == "--stream" {
				streamMode = true
				// Remove flag from args
				args = append(args[:i], args[i+1:]...)
				break
			}
			if arg == "-d" || arg == "--download" {
				downloadMode = true
				// Remove flag from args
				args = append(args[:i], args[i+1:]...)
				break
			}
		}

		url = args[0]

		// Check if it's a magnet link
		if isMagnet(url) {
			if streamMode {
				// Stream with webtorrent/vlc
				fmt.Println("Streaming torrent...")
				if err := streamWithPlayer(url); err != nil {
					fmt.Printf("Error: %v\n", err)
					os.Exit(1)
				}
				return
			}
			if downloadMode {
				// Download to current directory
				fmt.Println("Downloading torrent...")
				if err := downloadTorrent(url); err != nil {
					fmt.Printf("Error: %v\n", err)
					os.Exit(1)
				}
				return
			}
			// Default: stream
			fmt.Println("Streaming torrent...")
			if err := streamWithPlayer(url); err != nil {
				fmt.Printf("Error: %v\n", err)
				os.Exit(1)
			}
			return
		}

		// Check if it's a known video/social URL - auto-download
		if isURL(url) && detectVideoURL(url) {
			site := detectSite(url)
			fmt.Printf("Detected %s video, downloading with best quality...\n", site)
			// Add --format best to force best quality
			yariaLib.Run(append([]string{"--format", "bestvideo+bestaudio/best"}, args...))
			return
		}

		// Check if it looks like a URL (generic download)
		if isURL(url) {
			yariaLib.Run(args)
			return
		}

		fmt.Printf("Unknown command: %s\n\n", args[0])
		printUsage()
		os.Exit(1)
	}
}

func runInteractiveMenu() {
	for {
		result := menu.RunMenu()
		switch result.Choice {
		case "yaria":
			yariaLib.Run(nil)
			continue
		case "mantorex":
			if !pro.Available() {
				pro.RunInteractive()
				continue
			}
			pro.RunInteractive()
			continue
		case "quit":
			return
		default:
			return
		}
	}
}
