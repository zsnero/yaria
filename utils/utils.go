package utils

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// SanitizeFilename cleans a filename
func SanitizeFilename(name string) string {
	invalidChars := regexp.MustCompile(`[<>:"/\\|?*]`)
	name = invalidChars.ReplaceAllString(name, "_")

	name = strings.TrimFunc(name, func(r rune) bool {
		return unicode.IsSpace(r) || r == '.'
	})
	name = regexp.MustCompile(`\s+`).ReplaceAllString(name, "_")

	if name == "" {
		name = GenerateTempDirName("untitled")
	}
	return name
}

// GenerateTempDirName creates a timestamped directory name
func GenerateTempDirName(prefix string) string {
	return fmt.Sprintf("%s_%d", prefix, time.Now().Unix())
}

// CreateUniqueTempDir ensures a unique temporary directory
func CreateUniqueTempDir(baseDir string) (string, error) {
	tempDir := baseDir
	counter := 1
	for {
		if _, err := os.Stat(tempDir); errors.Is(err, os.ErrNotExist) {
			return tempDir, os.MkdirAll(tempDir, 0755)
		}
		tempDir = fmt.Sprintf("%s_%d", baseDir, counter)
		counter++
	}
}

// FileExists checks if a file exists
func FileExists(path string) bool {
	_, err := os.Stat(path)
	return !errors.Is(err, os.ErrNotExist)
}

// MoveFile moves a file with overwrite protection
func MoveFile(src, dest string) error {
	if FileExists(dest) {
		return errors.New("destination file already exists")
	}
	return os.Rename(src, dest)
}

// FindVideoFile locates the first video file in a directory
func FindVideoFile(dir string) (string, error) {
	var videoFile string
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && strings.Contains(info.Name(), ".") {
			videoFile = path
			return filepath.SkipDir // Stop after finding first file
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if videoFile == "" {
		return "", errors.New("no file found")
	}
	return videoFile, nil
}

// SplitN splits a string with a separator
func SplitN(s, sep string, n int) []string {
	return strings.SplitN(s, sep, n)
}

// ParseInt converts a string to an integer
func ParseInt(s string) (int, error) {
	return strconv.Atoi(s)
}
