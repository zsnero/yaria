package config

import (
	"fmt"
	"io"
	"os"
	"time"
)

// Program configuration
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
	DownloadLocation string
}

// Config with default values
func New() *Config {
	return &Config{
		MaxRetries:       3,
		RetryDelay:       5 * time.Second,
		Aria2cArgs:       "--max-connection-per-server=16 --min-split-size=1M --split=32 --max-concurrent-downloads=16 --file-allocation=none --optimize-concurrent-downloads=true --disk-cache=64M --max-tries=5 --retry-wait=2 --timeout=30 --connect-timeout=30 --lowest-speed-limit=10K --continue=true --allow-overwrite=true --allow-piece-length-change=true --enable-rpc=false --enable-http-pipelining=true --enable-http-keep-alive=true --enable-mmap=true --enable-color=false --summary-interval=0 --log-level=error --console-log-level=error",
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
	fmt.Fprintf(c.Stdout, "Waiting %v before retrying...\n", c.RetryDelay)
	time.Sleep(c.RetryDelay)
}
