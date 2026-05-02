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
func TMDBApiKey() string               { return appconfig.TMDBApiKey() }
func SetTMDBApiKey(key string) error   { return appconfig.SetTMDBApiKey(key) }

// --- Proxy settings ---

func ProxyType() string              { return appconfig.ProxyType() }
func SetProxyType(t string) error    { return appconfig.SetProxyType(t) }
func ProxyAddr() string              { return appconfig.ProxyAddr() }
func SetProxyAddr(addr string) error { return appconfig.SetProxyAddr(addr) }

// --- Speed limit ---

func SpeedLimit() int64                { return appconfig.SpeedLimit() }
func SetSpeedLimit(limit int64) error  { return appconfig.SetSpeedLimit(limit) }

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

type MantorexConfig = appconfig.MantorexConfig

func GetMantorexConfig() MantorexConfig { return appconfig.GetMantorexConfig() }
