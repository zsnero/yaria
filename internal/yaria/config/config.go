package config

import (
	"fmt"
	"io"
	"os"
	"time"

	"yaria/internal/appconfig"
)

// Program configuration (runtime)
type Config struct {
	MaxRetries       int
	RetryDelay       time.Duration
	Aria2cArgs       string
	OutputTemplate   string
	UseAria2c        bool
	Stdout           io.Writer
	Stderr           io.Writer
	IsAudioOnly      bool
	AudioFormat      string
	Resolution       string
	CookieBrowser    string
	ContainerFormat  string // "mp4", "mkv", "webm" — used for --merge-output-format
	DownloadLocation string
}

// Persistent user preferences (theme etc.)
type UserConfig struct {
	Theme string `json:"theme"`
}

// LoadUserConfig loads persistent user preferences from app.toml
func LoadUserConfig() UserConfig {
	cfg := UserConfig{
		Theme: appconfig.YariaTheme(),
	}
	if cfg.Theme == "" {
		cfg.Theme = "Rainbow"
	}
	return cfg
}

// SaveUserConfig persists user preferences to app.toml
func SaveUserConfig(cfg UserConfig) error {
	return appconfig.SetYariaTheme(cfg.Theme)
}

// Config with default values
func New() *Config {
	return &Config{
		MaxRetries:       3,
		RetryDelay:       5 * time.Second,
		Aria2cArgs:       "--max-connection-per-server=16 --min-split-size=1M --split=32 --max-concurrent-downloads=32 --file-allocation=none --optimize-concurrent-downloads=true --disk-cache=128M --max-tries=5 --retry-wait=1 --timeout=20 --connect-timeout=10 --lowest-speed-limit=10K --continue=true --allow-overwrite=true --allow-piece-length-change=true --enable-http-pipelining=true --enable-http-keep-alive=true --enable-mmap=true --enable-color=false --summary-interval=1 --console-log-level=notice --auto-file-renaming=false --stream-piece-selector=geom",
		OutputTemplate:   "%(title)s.%(ext)s",
		UseAria2c:        true,
		Stdout:           os.Stdout,
		Stderr:           os.Stderr,
		IsAudioOnly:      false,
		AudioFormat:      "mp3",
		Resolution:       "",
		CookieBrowser:    "",
		DownloadLocation: "",
	}
}

// Logs and waits before retrying
func (c *Config) WaitBeforeRetry(attempt int) {
	w := c.Stdout
	if w == nil {
		w = io.Discard
	}
	fmt.Fprintf(w, "Waiting %v before retrying...\n", c.RetryDelay)
	time.Sleep(c.RetryDelay)
}
