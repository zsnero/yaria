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

type MantorexConfig = appconfig.MantorexConfig

func GetMantorexConfig() MantorexConfig { return appconfig.GetMantorexConfig() }
