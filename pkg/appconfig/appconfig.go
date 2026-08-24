// Package appconfig re-exports yaria/internal/appconfig for external consumers.
package appconfig

import "yaria/internal/appconfig"

func Init()                            { appconfig.Init() }
func Save() error                      { return appconfig.Save() }
func ConfigDir() string                { return appconfig.ConfigDir() }
func ConfigFile() string               { return appconfig.ConfigFile() }
func YariaTheme() string               { return appconfig.YariaTheme() }
func SetYariaTheme(t string) error     { return appconfig.SetYariaTheme(t) }
func MantorexDataDir() string          { return appconfig.MantorexDataDir() }
func MantorexResultsLimit() int        { return appconfig.MantorexResultsLimit() }
func MantorexTorrentPort() int         { return appconfig.MantorexTorrentPort() }
func MantorexHostPort() int            { return appconfig.MantorexHostPort() }
func MantorexTheme() string            { return appconfig.MantorexTheme() }
func SetMantorexTheme(t string) error  { return appconfig.SetMantorexTheme(t) }
func MantorexDebug() bool              { return appconfig.MantorexDebug() }
func TMDBApiKey() string                 { return appconfig.TMDBApiKey() }
func UserTMDBApiKey() string             { return appconfig.UserTMDBApiKey() }
func UsingBuiltinTMDB() bool             { return appconfig.UsingBuiltinTMDB() }
func SetBuiltinTMDBKey(key string)       { appconfig.SetBuiltinTMDBKey(key) }
func SetTMDBApiKey(key string) error     { return appconfig.SetTMDBApiKey(key) }

// --- Proxy settings ---

func ProxyType() string              { return appconfig.ProxyType() }
func SetProxyType(t string) error    { return appconfig.SetProxyType(t) }
func ProxyAddr() string              { return appconfig.ProxyAddr() }
func SetProxyAddr(addr string) error { return appconfig.SetProxyAddr(addr) }

// --- Speed limit ---

func SpeedLimit() int64                { return appconfig.SpeedLimit() }
func SetSpeedLimit(limit int64) error  { return appconfig.SetSpeedLimit(limit) }

// --- Browser extension bridge ---

func BrowserExtensionEnabled() bool                 { return appconfig.BrowserExtensionEnabled() }
func SetBrowserExtensionEnabled(b bool) error       { return appconfig.SetBrowserExtensionEnabled(b) }
func BrowserExtensionPort() int                     { return appconfig.BrowserExtensionPort() }
func SetBrowserExtensionPort(p int) error           { return appconfig.SetBrowserExtensionPort(p) }
func BrowserExtensionToken() string                 { return appconfig.BrowserExtensionToken() }
func SetBrowserExtensionToken(t string) error       { return appconfig.SetBrowserExtensionToken(t) }
func BrowserExtensionDownloadDir() string           { return appconfig.BrowserExtensionDownloadDir() }
func SetBrowserExtensionDownloadDir(d string) error { return appconfig.SetBrowserExtensionDownloadDir(d) }

// --- Media Server ---

func MediaServerEnabled() bool              { return appconfig.MediaServerEnabled() }
func SetMediaServerEnabled(b bool) error    { return appconfig.SetMediaServerEnabled(b) }
func MediaServerPort() int                  { return appconfig.MediaServerPort() }
func SetMediaServerPort(p int) error        { return appconfig.SetMediaServerPort(p) }
func MediaServerPin() string                { return appconfig.MediaServerPin() }
func SetMediaServerPin(p string) error      { return appconfig.SetMediaServerPin(p) }

// --- Media Library ---

func MediaMovieDirs() []string              { return appconfig.MediaMovieDirs() }
func SetMediaMovieDirs(dirs []string) error { return appconfig.SetMediaMovieDirs(dirs) }
func MediaTVDirs() []string                 { return appconfig.MediaTVDirs() }
func SetMediaTVDirs(dirs []string) error    { return appconfig.SetMediaTVDirs(dirs) }
func MediaVideoDirs() []string              { return appconfig.MediaVideoDirs() }
func SetMediaVideoDirs(dirs []string) error { return appconfig.SetMediaVideoDirs(dirs) }

// --- Desktop UI ---

type UISettings = appconfig.UISettings
type PlayerSettings = appconfig.PlayerSettings

func GetUISettings() UISettings              { return appconfig.GetUISettings() }
func SetUISettings(s UISettings) error       { return appconfig.SetUISettings(s) }
func GetPlayerSettings() PlayerSettings      { return appconfig.GetPlayerSettings() }
func SetPlayerSettings(s PlayerSettings) error {
	return appconfig.SetPlayerSettings(s)
}
func BlurIsSet() bool                        { return appconfig.BlurIsSet() }
func UIConfigured() bool                     { return appconfig.UIConfigured() }

type MantorexConfig = appconfig.MantorexConfig

func GetMantorexConfig() MantorexConfig { return appconfig.GetMantorexConfig() }

// --- Jackett / Torznab ---

func JackettEnabled() bool              { return appconfig.JackettEnabled() }
func SetJackettEnabled(b bool) error    { return appconfig.SetJackettEnabled(b) }
func JackettURL() string                { return appconfig.JackettURL() }
func SetJackettURL(url string) error    { return appconfig.SetJackettURL(url) }
func JackettAPIKey() string             { return appconfig.JackettAPIKey() }
func SetJackettAPIKey(key string) error { return appconfig.SetJackettAPIKey(key) }

// --- Provider management ---

func EnabledProviders() []string              { return appconfig.EnabledProviders() }
func SetEnabledProviders(names []string) error { return appconfig.SetEnabledProviders(names) }
