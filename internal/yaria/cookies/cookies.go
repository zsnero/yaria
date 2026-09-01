// Package cookies provides browser cookie extraction for yt-dlp.
// Uses kooky (pure Go) as primary method, falls back to yt-dlp's
// --cookies-from-browser if kooky fails.
package cookies

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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
// Order:
//  1. kooky export to ~/.yaria/cookies.txt (works while browser is open)
//  2. yt-dlp --cookies-from-browser export (Firefox/LibreWolf preferred;
//     Brave/Chrome often fail on Linux with "cannot decrypt v11 cookies")
//  3. direct --cookies-from-browser as last resort (skipped on Windows to avoid hangs)
//
// Note: yt-dlp often says "Chrome cookie database" for any Chromium browser
// (including Edge/Brave), so that message does not mean Chrome is installed.
func GetYTDLPCookieArgs(url string, fallbackBrowser string) []string {
	var cookiesFile string

	if strings.Contains(url, "youtube.com") || strings.Contains(url, "youtu.be") ||
		strings.Contains(url, "google.com") {
		cookiesFile = ExtractYouTubeCookies()
	} else {
		cookiesFile = ExtractSiteCookies(url)
	}

	if cookiesFile != "" && cookieFileLooksAuthed(cookiesFile, url) {
		return []string{"--cookies", cookiesFile}
	}

	// Prefer Firefox-family browsers: Brave/Chrome cookie decryption often
	// fails on Linux ("cannot decrypt v11 cookies: no key found").
	browsers := cookieBrowserCandidates(fallbackBrowser)
	for _, b := range browsers {
		if b == "" {
			continue
		}
		if exported := exportCookiesViaYTDLP(b); exported != "" {
			return []string{"--cookies", exported}
		}
	}

	// Last resort: let yt-dlp read the browser DB itself (can hang on Windows).
	if runtime.GOOS != "windows" {
		for _, b := range browsers {
			if b == "" {
				continue
			}
			// Skip pure chromium names that commonly fail decrypt — Firefox forks first.
			low := strings.ToLower(b)
			if strings.HasPrefix(low, "brave") || strings.HasPrefix(low, "chrome") ||
				strings.HasPrefix(low, "chromium") || strings.HasPrefix(low, "edge") {
				continue
			}
			return []string{"--cookies-from-browser", b}
		}
	}
	return nil
}

// cookieFileLooksAuthed returns true if the netscape cookie file is large enough
// and, for YouTube, contains a login marker (LOGIN_INFO / SID / SAPISID).
func cookieFileLooksAuthed(path, url string) bool {
	info, err := os.Stat(path)
	if err != nil || info.Size() < 50 {
		return false
	}
	if !(strings.Contains(url, "youtube.com") || strings.Contains(url, "youtu.be") ||
		strings.Contains(url, "google.com")) {
		return true
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	s := string(data)
	// Any of these usually means a real Google/YouTube session.
	return strings.Contains(s, "\tLOGIN_INFO\t") ||
		strings.Contains(s, "\tSAPISID\t") ||
		strings.Contains(s, "\t__Secure-1PSID\t") ||
		strings.Contains(s, "\tSID\t")
}

func cookieBrowserCandidates(fallbackBrowser string) []string {
	var out []string
	seen := map[string]bool{}
	add := func(b string) {
		b = strings.TrimSpace(b)
		if b == "" || seen[b] {
			return
		}
		seen[b] = true
		out = append(out, b)
	}
	add(fallbackBrowser)
	// Always try LibreWolf/Firefox profile paths even if DetectBrowser picked Brave.
	home, _ := os.UserHomeDir()
	for _, root := range []string{
		filepath.Join(home, ".librewolf"),
		filepath.Join(home, ".var", "app", "io.gitlab.librewolf-community", ".librewolf"),
		filepath.Join(home, "Library", "Application Support", "LibreWolf"),
		filepath.Join(home, ".mozilla", "firefox"),
		filepath.Join(home, ".var", "app", "org.mozilla.firefox", ".mozilla", "firefox"),
	} {
		if p := newestFirefoxProfile(root); p != "" {
			add("firefox:" + p)
		}
	}
	add("firefox")
	add("brave")
	add("chrome")
	add("chromium")
	return out
}

func newestFirefoxProfile(root string) string {
	entries, err := os.ReadDir(root)
	if err != nil {
		return ""
	}
	var best string
	var bestTime time.Time
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		cookiesDB := filepath.Join(root, name, "cookies.sqlite")
		st, err := os.Stat(cookiesDB)
		if err != nil {
			continue
		}
		if best == "" || st.ModTime().After(bestTime) {
			best = filepath.Join(root, name)
			bestTime = st.ModTime()
		}
	}
	return best
}

// exportCookiesViaYTDLP writes a netscape cookies file using yt-dlp's browser reader.
// Returns path on success. Times out quickly so the UI never hangs.
func exportCookiesViaYTDLP(browser string) string {
	yt := "yt-dlp"
	if runtime.GOOS == "windows" {
		yt = "yt-dlp.exe"
	}
	if _, err := exec.LookPath(yt); err != nil {
		return ""
	}
	outPath := getCookiesFilePath()
	// Export without needing a real URL — yt-dlp still loads browser cookies.
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, yt,
		"--cookies-from-browser", browser,
		"--cookies", outPath,
		"--skip-download",
		"--no-warnings",
		"https://www.youtube.com",
	)
	out, err := cmd.CombinedOutput()
	msg := strings.ToLower(string(out))
	if err != nil {
		// Decrypt failures (Brave/Chrome v11) or locked DB — try next browser
		if strings.Contains(msg, "decrypt") || strings.Contains(msg, "could not copy") ||
			strings.Contains(msg, "cookie") && strings.Contains(msg, "database") {
			return ""
		}
		// Some yt-dlp versions still write cookies before failing the dummy URL
	}
	if cookieFileLooksAuthed(outPath, "https://www.youtube.com") {
		cacheMu.Lock()
		cachedFile = outPath
		cachedAt = time.Now()
		cacheMu.Unlock()
		_ = os.Chmod(outPath, 0600)
		return outPath
	}
	return ""
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
