package clean

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Entry represents a file or directory that can be cleaned
type Entry struct {
	Path      string
	Name      string
	Size      int64
	IsDir     bool
	IsPartial bool
	IsMeta    bool
}

// SizeHuman returns a human-readable size string
func (e Entry) SizeHuman() string {
	return FormatBytes(e.Size)
}

// FormatBytes formats bytes into a human-readable string
func FormatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(b)/float64(div), "KMGTPE"[exp])
}

// ScanDir scans a directory for cleanable entries (partial files, yt-dlp cache, temp files)
func ScanDir(dir string) ([]Entry, int64, error) {
	var entries []Entry
	var totalSize int64

	items, err := os.ReadDir(dir)
	if err != nil {
		return nil, 0, err
	}

	for _, item := range items {
		name := item.Name()

		isMeta := isMetaFile(name)
		isPartial := isPartialFile(name)

		path := filepath.Join(dir, name)
		info, err := item.Info()
		if err != nil {
			continue
		}

		var size int64
		if item.IsDir() {
			size = dirSize(path)
		} else {
			size = info.Size()
		}

		entry := Entry{
			Path:      path,
			Name:      name,
			Size:      size,
			IsDir:     item.IsDir(),
			IsPartial: isPartial,
			IsMeta:    isMeta,
		}
		entries = append(entries, entry)
		totalSize += size
	}

	return entries, totalSize, nil
}

// ScanPartials finds all partial download files recursively
func ScanPartials(dir string) ([]Entry, int64) {
	var partials []Entry
	var totalSize int64

	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			return nil
		}
		if isPartialFile(info.Name()) {
			entry := Entry{
				Path:      path,
				Name:      relPath(dir, path),
				Size:      info.Size(),
				IsPartial: true,
			}
			partials = append(partials, entry)
			totalSize += info.Size()
		}
		return nil
	})

	return partials, totalSize
}

// ScanMeta finds all metadata/cache files recursively
func ScanMeta(dir string) ([]Entry, int64) {
	var metas []Entry
	var totalSize int64

	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			return nil
		}
		if isMetaFile(info.Name()) {
			entry := Entry{
				Path:   path,
				Name:   relPath(dir, path),
				Size:   info.Size(),
				IsMeta: true,
			}
			metas = append(metas, entry)
			totalSize += info.Size()
		}
		return nil
	})

	return metas, totalSize
}

// ScanYtdlpCache finds and measures yt-dlp cache directories
func ScanYtdlpCache() ([]Entry, int64) {
	var entries []Entry
	var totalSize int64

	// yt-dlp cache is typically at ~/.cache/yt-dlp
	home, err := os.UserHomeDir()
	if err != nil {
		return entries, totalSize
	}

	cacheDirs := []string{
		filepath.Join(home, ".cache", "yt-dlp"),
		filepath.Join(home, ".yt-dlp"),
	}

	for _, dir := range cacheDirs {
		info, err := os.Stat(dir)
		if err != nil {
			continue
		}
		size := dirSize(dir)
		if size > 0 {
			entry := Entry{
				Path:   dir,
				Name:   info.Name(),
				Size:   size,
				IsDir:  true,
				IsMeta: true,
			}
			entries = append(entries, entry)
			totalSize += size
		}
	}

	return entries, totalSize
}

// RemoveEntries removes the given entries and returns stats
func RemoveEntries(entries []Entry) (int, int64, []error) {
	var removed int
	var freedSize int64
	var errs []error

	for _, entry := range entries {
		var err error
		if entry.IsDir {
			err = os.RemoveAll(entry.Path)
		} else {
			err = os.Remove(entry.Path)
		}
		if err != nil {
			errs = append(errs, err)
		} else {
			removed++
			freedSize += entry.Size
		}
	}
	return removed, freedSize, errs
}

// RemoveAll removes everything in a directory
func RemoveAll(dir string) (int64, error) {
	entries, totalSize, err := ScanDir(dir)
	if err != nil {
		return 0, err
	}
	for _, entry := range entries {
		if entry.IsDir {
			_ = os.RemoveAll(entry.Path)
		} else {
			_ = os.Remove(entry.Path)
		}
	}
	return totalSize, nil
}

func isPartialFile(name string) bool {
	return strings.HasSuffix(name, ".part") ||
		strings.HasSuffix(name, ".ytdl") ||
		strings.HasSuffix(name, ".part-Frag0")
}

func isMetaFile(name string) bool {
	return strings.HasSuffix(name, ".json") && strings.Contains(name, "yt-dlp") ||
		strings.HasSuffix(name, ".sqlite") ||
		strings.HasSuffix(name, ".sqlite-journal") ||
		name == ".cache" ||
		name == "youtube-sigfuncs"
}

func dirSize(path string) int64 {
	var size int64
	_ = filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() {
			size += info.Size()
		}
		return nil
	})
	return size
}

func relPath(base, path string) string {
	rel, err := filepath.Rel(base, path)
	if err != nil {
		return path
	}
	return rel
}
