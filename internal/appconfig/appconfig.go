// Package appconfig provides a unified configuration system for Yaria.
// All settings are stored in a single file: ~/.config/yaria/app.yaml
// Uses Viper for loading, defaults, and persistence.
package appconfig

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"sync"

	"github.com/spf13/viper"
)

var (
	once     sync.Once
	v        *viper.Viper
	confDir  string
	confFile string
)

// App.yaml structure:
//
// yaria:
//   theme: Rainbow
//
// mantorex:
//   data_dir: ~/Downloads/Mantorex
//   results_limit: 100
//   torrent_port: 9999
//   host_port: 3434
//   theme: Cyan
//   debug: false
//
// api_keys:
//   tmdb: "your-tmdb-api-key"

// Init initializes Viper and loads the config file.
// Safe to call multiple times; only runs once.
func Init() {
	once.Do(func() {
		u, _ := user.Current()
		confDir = filepath.Join(u.HomeDir, ".config", "yaria")
		confFile = filepath.Join(confDir, "app.yaml")
		_ = os.MkdirAll(confDir, 0755)

		v = viper.New()
		v.SetConfigName("app")
		v.SetConfigType("yaml")
		v.AddConfigPath(confDir)

		// Yaria defaults
		v.SetDefault("yaria.theme", "Rainbow")

		// Mantorex defaults
		v.SetDefault("mantorex.data_dir", "~/Downloads/Mantorex")
		v.SetDefault("mantorex.results_limit", 100)
		v.SetDefault("mantorex.torrent_port", 9999)
		v.SetDefault("mantorex.host_port", 3434)
		v.SetDefault("mantorex.theme", "")
		v.SetDefault("mantorex.debug", false)

		// API keys
		v.SetDefault("api_keys.tmdb", "")

		// Read config file (create if missing)
		if err := v.ReadInConfig(); err != nil {
			if _, ok := err.(viper.ConfigFileNotFoundError); ok {
				_ = v.SafeWriteConfigAs(confFile)
			}
		}
	})
}

// ConfigDir returns the config directory path.
func ConfigDir() string {
	Init()
	return confDir
}

// ConfigFile returns the config file path.
func ConfigFile() string {
	Init()
	return confFile
}

// Save writes the current config to disk.
func Save() error {
	Init()
	return v.WriteConfigAs(confFile)
}

// --- Yaria settings ---

// YariaTheme returns the Yaria TUI theme.
func YariaTheme() string {
	Init()
	return v.GetString("yaria.theme")
}

// SetYariaTheme sets and saves the Yaria TUI theme.
func SetYariaTheme(theme string) error {
	Init()
	v.Set("yaria.theme", theme)
	return Save()
}

// --- Mantorex settings ---

// MantorexDataDir returns the Mantorex download directory.
func MantorexDataDir() string {
	Init()
	return v.GetString("mantorex.data_dir")
}

// MantorexResultsLimit returns the max search results.
func MantorexResultsLimit() int {
	Init()
	return v.GetInt("mantorex.results_limit")
}

// MantorexTorrentPort returns the BitTorrent listen port.
func MantorexTorrentPort() int {
	Init()
	return v.GetInt("mantorex.torrent_port")
}

// MantorexHostPort returns the HTTP streaming/WebUI port.
func MantorexHostPort() int {
	Init()
	return v.GetInt("mantorex.host_port")
}

// MantorexTheme returns the Mantorex TUI theme.
func MantorexTheme() string {
	Init()
	return v.GetString("mantorex.theme")
}

// SetMantorexTheme sets and saves the Mantorex TUI theme.
func SetMantorexTheme(theme string) error {
	Init()
	v.Set("mantorex.theme", theme)
	return Save()
}

// MantorexDebug returns the debug flag.
func MantorexDebug() bool {
	Init()
	return v.GetBool("mantorex.debug")
}

// --- API Keys ---

// TMDBApiKey returns the TMDB API key.
func TMDBApiKey() string {
	Init()
	return v.GetString("api_keys.tmdb")
}

// SetTMDBApiKey sets and saves the TMDB API key.
func SetTMDBApiKey(key string) error {
	Init()
	v.Set("api_keys.tmdb", key)
	return Save()
}

// --- Mantorex compat layer (for existing code) ---

// MantorexConfig holds all Mantorex settings in one struct
// for backward compatibility with existing code that expects it.
type MantorexConfig struct {
	DataDir      string
	ResultsLimit int
	TorrentPort  int
	HostPort     int
	Debug        bool
	Theme        string
	TMDBApiKey   string
}

// GetMantorexConfig returns all Mantorex config as a struct.
func GetMantorexConfig() MantorexConfig {
	Init()
	return MantorexConfig{
		DataDir:      v.GetString("mantorex.data_dir"),
		ResultsLimit: v.GetInt("mantorex.results_limit"),
		TorrentPort:  v.GetInt("mantorex.torrent_port"),
		HostPort:     v.GetInt("mantorex.host_port"),
		Debug:        v.GetBool("mantorex.debug"),
		Theme:        v.GetString("mantorex.theme"),
		TMDBApiKey:   v.GetString("api_keys.tmdb"),
	}
}

// String returns a summary for display.
func (c MantorexConfig) String() string {
	return fmt.Sprintf(
		"DataDir: %v | ResultsLimit: %d | TorrentPort: %d | HostPort: %d | Debug: %v",
		c.DataDir, c.ResultsLimit, c.TorrentPort, c.HostPort, c.Debug,
	)
}
