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

	// Keep short: locked browser DBs on Windows can block SQLite reads.
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
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
		if ctx.Err() != nil {
			break
		}
		seq := kooky.TraverseCookies(ctx, kooky.Valid, kooky.DomainHasSuffix(domain))
		for cookie := range seq.OnlyCookies() {
			if ctx.Err() != nil {
				break
			}
			writeCookie(cookie)
		}
	}

	// If no domains specified, extract all cookies
	if len(domains) == 0 && ctx.Err() == nil {
		seq := kooky.TraverseCookies(ctx, kooky.Valid)
		for cookie := range seq.OnlyCookies() {
			if ctx.Err() != nil {
				break
			}
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
// Filters out bloat: ST-* state cookies from .youtube.com (~80 cookies, ~78KB)
// and non-essential cookies from .google.com to avoid HTTP 413 errors.
func ExtractYouTubeCookies() string {
	return ExtractCookiesFileFiltered(
		[]string{".youtube.com", ".googlevideo.com"},
		// .google.com has hundreds of cookies from other Google services;
		// only extract the ones YouTube actually needs for authentication.
		".google.com",
		youtubeEssentialCookies,
		// ST-* are YouTube client-side state cookies (scroll position, UI state, etc.)
		// They are ~2KB each and there can be 50-100+ of them, causing HTTP 413 errors.
		[]string{"ST-"},
	)
}

// youtubeEssentialCookies is the set of .google.com cookie names that
// YouTube requires for authentication. All other .google.com cookies
// (from Gmail, Drive, Docs, etc.) are excluded to keep the cookie file
// small and avoid HTTP 413 "Request Entity Too Large" errors.
var youtubeEssentialCookies = map[string]bool{
	"SID": true, "HSID": true, "SSID": true,
	"APISID": true, "SAPISID": true,
	"__Secure-1PSID": true, "__Secure-3PSID": true,
	"__Secure-1PAPISID": true, "__Secure-3PAPISID": true,
	"__Secure-1PSIDTS": true, "__Secure-3PSIDTS": true,
	"__Secure-1PSIDCC": true, "__Secure-3PSIDCC": true,
	"NID": true, "SIDCC": true,
	"LOGIN_INFO": true, "PREF": true,
}

// ExtractCookiesFileFiltered extracts cookies for the given domains,
// plus cookies from filteredDomain that match the allowedNames whitelist.
// skipPrefixes are cookie name prefixes to exclude from all domains
// (e.g., "ST-" for YouTube state cookies).
// This prevents oversized cookie files that cause HTTP 413 errors.
func ExtractCookiesFileFiltered(domains []string, filteredDomain string, allowedNames map[string]bool, skipPrefixes []string) string {
	cacheMu.Lock()
	defer cacheMu.Unlock()

	// Return cached file if still fresh
	if cachedFile != "" && time.Since(cachedAt) < cacheDuration {
		if _, err := os.Stat(cachedFile); err == nil {
			return cachedFile
		}
	}

	cookiesPath := getCookiesFilePath()

	// Short timeout: locked Edge/Chrome DBs on Windows can block SQLite reads.
	// Never hold the cache lock for longer than this overall budget.
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	f, err := os.Create(cookiesPath)
	if err != nil {
		return ""
	}
	fmt.Fprint(f, "# HTTP Cookie File\n\n")

	written := 0
	shouldSkip := func(name string) bool {
		for _, prefix := range skipPrefixes {
			if strings.HasPrefix(name, prefix) {
				return true
			}
		}
		return false
	}
	writeCookie := func(cookie *kooky.Cookie) {
		httpCookie := &cookie.Cookie
		expires := httpCookie.Expires.Unix()
		if expires <= 0 {
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

	// Extract unfiltered domains (with skip-prefix filtering)
	for _, domain := range domains {
		if ctx.Err() != nil {
			break
		}
		seq := kooky.TraverseCookies(ctx, kooky.Valid, kooky.DomainHasSuffix(domain))
		for cookie := range seq.OnlyCookies() {
			if ctx.Err() != nil {
				break
			}
			if !shouldSkip(cookie.Name) {
				writeCookie(cookie)
			}
		}
	}

	// Extract filtered domain with name whitelist
	if filteredDomain != "" && len(allowedNames) > 0 && ctx.Err() == nil {
		seq := kooky.TraverseCookies(ctx, kooky.Valid, kooky.DomainHasSuffix(filteredDomain))
		for cookie := range seq.OnlyCookies() {
			if ctx.Err() != nil {
				break
			}
			if allowedNames[cookie.Name] {
				writeCookie(cookie)
			}
		}
	}

	f.Close()

	info, err := os.Stat(cookiesPath)
	if err != nil || info.Size() < 30 {
		os.Remove(cookiesPath)
		return ""
	}

	cachedFile = cookiesPath
	cachedAt = time.Now()
	return cookiesPath
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
// Tries kooky first (pure Go; can often read cookies while the browser is open),
// falls back to yt-dlp's --cookies-from-browser only when a browser was detected.
//
// On a fresh install (no YouTube login cookies), kooky returns nothing and the
// fallback may still fail if Edge/Chrome has the SQLite DB locked. Callers should
// retry without cookie args when yt-dlp reports a cookie-DB copy error.
//
// Note: yt-dlp often says "Chrome cookie database" for any Chromium browser
// (including Edge/Brave), so that message does not mean Chrome is installed.
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

	// Do NOT fall back to --cookies-from-browser here.
	// On Windows, yt-dlp's browser cookie copy often hangs or fails while Edge
	// is open, which freezes "Fetching..." in the GUI. Public videos work without
	// cookies; auth/age-gated content uses kooky when cookies are readable.
	// Callers that need --cookies-from-browser should pass it explicitly.
	_ = fallbackBrowser
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
