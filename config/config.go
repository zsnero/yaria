package config

import (
	"fmt"
	"io"
	"os"
	"time"
)

// Program configuration
type Config struct {
	MaxRetries     int
	RetryDelay     time.Duration
	Aria2cArgs     string
	OutputTemplate string
	UseAria2c      bool
	Stdout         io.Writer
	Stderr         io.Writer
	IsAudioOnly    bool
	AudioFormat    string
	Resolution     string
	CookieBrowser  string
}

// Config with default values
func New() *Config {
	return &Config{
		MaxRetries:     3,
		RetryDelay:     5 * time.Second,
		Aria2cArgs:     "--max-connection-per-server=16 --min-split-size=1M --split=16 --max-concurrent-downloads=16",
		OutputTemplate: "%(title)s.%(ext)s",
		UseAria2c:      true,
		Stdout:         os.Stdout,
		Stderr:         os.Stderr,
		IsAudioOnly:    false,
		AudioFormat:    "mp3",
		Resolution:     "",
		CookieBrowser:  "",
	}
}

// Logs and waits before retrying
func (c *Config) WaitBeforeRetry(attempt int) {
	fmt.Fprintf(c.Stdout, "Waiting %v before retrying...\n", c.RetryDelay)
	time.Sleep(c.RetryDelay)
}
