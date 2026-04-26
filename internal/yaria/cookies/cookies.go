// Package cookies provides browser cookie extraction for yt-dlp.
// Uses kooky (pure Go) as primary method, falls back to yt-dlp's
// --cookies-from-browser if kooky fails.
package cookies

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/browserutils/kooky"
	_ "github.com/browserutils/kooky/browser/all" // register all browser finders
)

var (
	cacheMu       sync.Mutex
	cachedFile    string
	cachedAt      time.Time
	cacheDuration = 5 * time.Minute // re-extract cookies every 5 minutes
)

// ExtractCookiesFile extracts cookies from all available browsers for the
// given domains and writes them to a Netscape-format cookies.txt file.
// Returns the path to the cookies file, or empty string if extraction fails.
// Results are cached for 5 minutes to avoid repeated slow extractions.
func ExtractCookiesFile(domains []string) string {
	cacheMu.Lock()
	defer cacheMu.Unlock()

	// Return cached file if still fresh
	if cachedFile != "" && time.Since(cachedAt) < cacheDuration {
		if _, err := os.Stat(cachedFile); err == nil {
			return cachedFile
		}
	}

	cookiesPath := getCookiesFilePath()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Write to file
	f, err := os.Create(cookiesPath)
	if err != nil {
		return ""
	}
	fmt.Fprint(f, "# HTTP Cookie File\n\n")

	// Extract cookies for each domain and write them
	written := 0
	writeCookie := func(cookie *kooky.Cookie) {
		httpCookie := &cookie.Cookie
		// Skip cookies with zero/invalid expiry -- yt-dlp rejects them
		expires := httpCookie.Expires.Unix()
		if expires <= 0 {
			// Session cookies: set expiry far in the future
			expires = time.Now().Add(24 * time.Hour).Unix()
		}
		var domainStr string
		if httpCookie.HttpOnly {
			domainStr = "#HttpOnly_"
		}
		domainStr += httpCookie.Domain
		hasDot := "FALSE"
		if strings.HasPrefix(httpCookie.Domain, ".") {
			hasDot = "TRUE"
		}
		secure := "FALSE"
		if httpCookie.Secure {
			secure = "TRUE"
		}
		fmt.Fprintf(f, "%s\t%s\t%s\t%s\t%d\t%s\t%s\n",
			domainStr, hasDot, httpCookie.Path, secure,
			expires, httpCookie.Name, httpCookie.Value)
		written++
	}

	for _, domain := range domains {
		seq := kooky.TraverseCookies(ctx, kooky.Valid, kooky.DomainHasSuffix(domain))
		for cookie := range seq.OnlyCookies() {
			writeCookie(cookie)
		}
	}

	// If no domains specified, extract all cookies
	if len(domains) == 0 {
		seq := kooky.TraverseCookies(ctx, kooky.Valid)
		for cookie := range seq.OnlyCookies() {
			writeCookie(cookie)
		}
	}

	f.Close()

	// Check if we got any cookies
	info, err := os.Stat(cookiesPath)
	if err != nil || info.Size() < 30 { // less than header = no cookies
		os.Remove(cookiesPath)
		return ""
	}

	cachedFile = cookiesPath
	cachedAt = time.Now()
	return cookiesPath
}

// ExtractYouTubeCookies extracts cookies for YouTube/Google domains.
func ExtractYouTubeCookies() string {
	return ExtractCookiesFile([]string{
		".youtube.com",
		".google.com",
		".googlevideo.com",
	})
}

// ExtractSiteCookies extracts cookies for a specific site URL.
func ExtractSiteCookies(siteURL string) string {
	domain := extractDomain(siteURL)
	if domain == "" {
		return ""
	}
	return ExtractCookiesFile([]string{domain})
}

// ClearCache invalidates the cached cookies file.
func ClearCache() {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	if cachedFile != "" {
		os.Remove(cachedFile)
		cachedFile = ""
	}
}

// getCookiesFilePath returns the path for the cookies cache file.
func getCookiesFilePath() string {
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".yaria")
	os.MkdirAll(dir, 0700)
	return filepath.Join(dir, "cookies.txt")
}

// extractDomain extracts the domain from a URL for cookie filtering.
func extractDomain(url string) string {
	url = strings.TrimPrefix(url, "https://")
	url = strings.TrimPrefix(url, "http://")
	url = strings.TrimPrefix(url, "www.")
	if idx := strings.Index(url, "/"); idx > 0 {
		url = url[:idx]
	}
	if idx := strings.Index(url, ":"); idx > 0 {
		url = url[:idx]
	}
	// Return with leading dot for suffix matching
	if !strings.HasPrefix(url, ".") {
		url = "." + url
	}
	return url
}



// GetYTDLPCookieArgs returns yt-dlp arguments for cookie authentication.
// Tries kooky first (pure Go, works with locked browsers), falls back to
// --cookies-from-browser (yt-dlp's Python-based extraction).
func GetYTDLPCookieArgs(url string, fallbackBrowser string) []string {
	// Try kooky first
	var cookiesFile string

	if strings.Contains(url, "youtube.com") || strings.Contains(url, "youtu.be") ||
		strings.Contains(url, "google.com") {
		cookiesFile = ExtractYouTubeCookies()
	} else {
		cookiesFile = ExtractSiteCookies(url)
	}

	if cookiesFile != "" {
		info, err := os.Stat(cookiesFile)
		if err == nil && info.Size() > 50 {
			return []string{"--cookies", cookiesFile}
		}
	}

	// Fallback: use yt-dlp's --cookies-from-browser
	if fallbackBrowser != "" {
		return []string{"--cookies-from-browser", fallbackBrowser}
	}

	return nil
}



func init() {
	// Ensure cookies file permissions are restrictive
	cookiesPath := getCookiesFilePath()
	if _, err := os.Stat(cookiesPath); err == nil {
		os.Chmod(cookiesPath, 0600)
	}
}

// AvailableBrowsers returns the names of browsers that have cookie stores found on this system.
func AvailableBrowsers() []string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stores := kooky.FindAllCookieStores(ctx)
	seen := make(map[string]bool)
	var browsers []string
	for _, store := range stores {
		name := store.Browser()
		if !seen[name] {
			seen[name] = true
			browsers = append(browsers, name)
		}
	}
	return browsers
}

// HasCookiesFor checks if any browser has cookies for the given domain.
func HasCookiesFor(domain string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	seq := kooky.TraverseCookies(ctx, kooky.Valid, kooky.DomainHasSuffix(domain))
	for range seq.OnlyCookies() {
		cancel()
		return true
	}
	return false
}

// BrowserCookieCount returns how many cookies each browser has for a domain.
func BrowserCookieCount(domain string) map[string]int {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result := make(map[string]int)
	seq := kooky.TraverseCookies(ctx, kooky.Valid, kooky.DomainHasSuffix(domain))
	for cookie := range seq.OnlyCookies() {
		result[cookie.Domain]++
	}
	return result
}
