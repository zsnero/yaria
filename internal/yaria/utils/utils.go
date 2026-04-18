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

// Cleans a filename
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

// Creates a timestamped directory name
func GenerateTempDirName(prefix string) string {
	return fmt.Sprintf("%s_%d", prefix, time.Now().Unix())
}

// Ensures a unique temporary directory
func CreateUniqueTempDir(baseDir string) (string, error) {
	tempDir := baseDir
	counter := 1
	for {
		if _, err := os.Stat(tempDir); errors.Is(err, os.ErrNotExist) {
			return tempDir, os.MkdirAll(tempDir, 0o755)
		}
		tempDir = fmt.Sprintf("%s_%d", baseDir, counter)
		counter++
	}
}

// Checks if a file exists
func FileExists(path string) bool {
	_, err := os.Stat(path)
	return !errors.Is(err, os.ErrNotExist)
}

// Moves a file with overwrite protection
func MoveFile(src, dest string) error {
	if FileExists(dest) {
		return errors.New("destination file already exists")
	}
	return os.Rename(src, dest)
}

// Locates the first video file in a directory
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

// Splits a string with a separator
func SplitN(s, sep string, n int) []string {
	return strings.SplitN(s, sep, n)
}

// Converts a string to an integer
func ParseInt(s string) (int, error) {
	return strconv.Atoi(s)
}

// Converts a string to int, returning 0 on error
func MustParseInt(s string) int {
	i, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return i
}

// Aria2InstallCmd returns the shell command to install aria2 on the current system.
func Aria2InstallCmd() string {
	distro := DetectDistro()
	switch distro {
	case "arch", "manjaro", "endeavouros":
		return "sudo pacman -S aria2"
	case "fedora":
		return "sudo dnf install aria2"
	case "centos", "rhel", "rocky", "alma":
		return "sudo yum install aria2"
	case "opensuse", "sles":
		return "sudo zypper install aria2"
	case "alpine":
		return "sudo apk add aria2"
	case "freebsd":
		return "sudo pkg install aria2"
	case "macos":
		return "brew install aria2"
	default:
		return "sudo apt install aria2  (or: sudo dnf/yum/pacman install aria2)"
	}
}

// DetectDistro reads /etc/os-release or /etc/lsb-release to identify the
// current Linux distribution. Returns a lowercase id string.
func DetectDistro() string {
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		// Try lsb-release as fallback
		data, err = os.ReadFile("/etc/lsb-release")
		if err == nil {
			return parseIDFromLsb(string(data))
		}
		// Check for macOS
		if _, err := os.Stat("/System/Library/CoreServices/SystemVersion.plist"); err == nil {
			return "macos"
		}
		return "unknown"
	}

	content := string(data)
	id := parseIDFromOsRelease(content)
	if id != "" {
		return id
	}

	// Fallback: check NAME field
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "NAME=") {
			name := strings.Trim(strings.TrimPrefix(line, "NAME="), "\"")
			name = strings.ToLower(name)
			switch {
			case strings.Contains(name, "ubuntu"):
				return "ubuntu"
			case strings.Contains(name, "debian"):
				return "debian"
			case strings.Contains(name, "fedora"):
				return "fedora"
			case strings.Contains(name, "arch"):
				return "arch"
			case strings.Contains(name, "centos"):
				return "centos"
			case strings.Contains(name, "opensuse"):
				return "opensuse"
			case strings.Contains(name, "alpine"):
				return "alpine"
			}
		}
	}
	return "unknown"
}

func parseIDFromOsRelease(content string) string {
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "ID=") {
			id := strings.Trim(strings.TrimPrefix(line, "ID="), "\"")
			return strings.ToLower(id)
		}
	}
	return ""
}

func parseIDFromLsb(content string) string {
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "DISTRIB_ID=") {
			id := strings.Trim(strings.TrimPrefix(line, "DISTRIB_ID="), "\"")
			return strings.ToLower(id)
		}
	}
	return ""
}
