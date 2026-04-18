package downloader

import (
	"yaria/internal/yaria/config"
	"yaria/internal/yaria/downloader"
)

type Format = downloader.Format
type YTDLPDownloader = downloader.YTDLPDownloader
type Downloader = downloader.Downloader

func New(cfg *config.Config) (*YTDLPDownloader, error) {
	return downloader.New(cfg)
}
