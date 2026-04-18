package config

import "yaria/internal/yaria/config"

type Config = config.Config
type UserConfig = config.UserConfig

func New() *Config {
	return config.New()
}

func LoadUserConfig() UserConfig {
	return config.LoadUserConfig()
}

func SaveUserConfig(cfg UserConfig) error {
	return config.SaveUserConfig(cfg)
}
