package license

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/user"
	"path/filepath"
	"time"
)

const (
	// ValidationEndpoint is the URL to validate license keys against.
	// Yaria website API endpoint.
	ValidationEndpoint = "https://yaria.live/api/validate"

	// ActivationEndpoint is the URL to activate (bind) a key to a device.
	ActivationEndpoint = "https://yaria.live/api/activate"

	// CacheDuration is how long a validated license is trusted offline.
	CacheDuration = 7 * 24 * time.Hour // 7 days
)

// LicenseInfo represents the stored license data.
type LicenseInfo struct {
	Key         string    `json:"key"`
	Valid       bool      `json:"valid"`
	Plan        string    `json:"plan"` // "free" or "pro"
	Email       string    `json:"email,omitempty"`
	DeviceID    string    `json:"device_id"`             // bound device fingerprint
	DeviceName  string    `json:"device_name,omitempty"` // human-readable device summary
	ValidatedAt time.Time `json:"validated_at"`
	ExpiresAt   time.Time `json:"expires_at,omitempty"`
}

// ActivationRequest is sent to the server when activating a key.
type ActivationRequest struct {
	Key        string `json:"key"`
	DeviceID   string `json:"device_id"`
	DeviceName string `json:"device_name"`
	OS         string `json:"os"`
	Hostname   string `json:"hostname"`
	Username   string `json:"username"`
}

// APIResponse is the expected response from the license API.
type APIResponse struct {
	Valid     bool   `json:"valid"`
	Plan      string `json:"plan"`
	Email     string `json:"email,omitempty"`
	DeviceID  string `json:"device_id,omitempty"`
	ExpiresAt string `json:"expires_at,omitempty"`
	Error     string `json:"error,omitempty"`
}

func configDir() string {
	u, err := user.Current()
	if err != nil {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, ".yaria")
	}
	return filepath.Join(u.HomeDir, ".yaria")
}

func licensePath() string {
	return filepath.Join(configDir(), "license.json")
}

// LoadCachedLicense reads the locally cached license info.
func LoadCachedLicense() (*LicenseInfo, error) {
	data, err := os.ReadFile(licensePath())
	if err != nil {
		return nil, err
	}
	var info LicenseInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return nil, err
	}
	return &info, nil
}

// SaveLicense writes the license info to disk.
func SaveLicense(info *LicenseInfo) error {
	dir := configDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(licensePath(), data, 0o600)
}

// RemoveLicense deletes the cached license file.
func RemoveLicense() error {
	return os.Remove(licensePath())
}

// ValidateOnline checks a license key + device against the remote API.
// The server verifies the key is bound to this device.
func ValidateOnline(key string) (*LicenseInfo, error) {
	deviceID := GenerateDeviceID()
	url := fmt.Sprintf("%s?key=%s&device_id=%s", ValidationEndpoint, key, deviceID)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to reach license server: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("license server returned status %d: %s", resp.StatusCode, string(body))
	}

	var apiResp APIResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, fmt.Errorf("invalid response from license server: %w", err)
	}

	if apiResp.Error != "" {
		return nil, fmt.Errorf("%s", apiResp.Error)
	}

	info := &LicenseInfo{
		Key:         key,
		Valid:       apiResp.Valid,
		Plan:        apiResp.Plan,
		Email:       apiResp.Email,
		DeviceID:    deviceID,
		DeviceName:  DeviceSummary(),
		ValidatedAt: time.Now(),
	}

	if apiResp.ExpiresAt != "" {
		if t, err := time.Parse(time.RFC3339, apiResp.ExpiresAt); err == nil {
			info.ExpiresAt = t
		}
	}

	return info, nil
}

// ActivateKey binds a license key to this device, validates it, and caches
// the result. The server will reject the key if it's already bound to a
// different device.
func ActivateKey(key string) (*LicenseInfo, error) {
	deviceID := GenerateDeviceID()
	fp := GetDeviceFingerprint()

	reqBody := ActivationRequest{
		Key:        key,
		DeviceID:   deviceID,
		DeviceName: DeviceSummary(),
		OS:         fp.OS,
		Hostname:   fp.Hostname,
		Username:   fp.Username,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to encode request: %w", err)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Post(ActivationEndpoint, "application/json", bytes.NewReader(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to reach license server: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("license server returned status %d: %s", resp.StatusCode, string(body))
	}

	var apiResp APIResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, fmt.Errorf("invalid response from license server: %w", err)
	}

	if apiResp.Error != "" {
		return nil, fmt.Errorf("%s", apiResp.Error)
	}

	if !apiResp.Valid {
		return nil, fmt.Errorf("invalid license key")
	}

	info := &LicenseInfo{
		Key:         key,
		Valid:       apiResp.Valid,
		Plan:        apiResp.Plan,
		Email:       apiResp.Email,
		DeviceID:    deviceID,
		DeviceName:  DeviceSummary(),
		ValidatedAt: time.Now(),
	}

	if apiResp.ExpiresAt != "" {
		if t, err := time.Parse(time.RFC3339, apiResp.ExpiresAt); err == nil {
			info.ExpiresAt = t
		}
	}

	if err := SaveLicense(info); err != nil {
		return info, fmt.Errorf("license valid but failed to save: %w", err)
	}

	return info, nil
}

// CheckLicense returns the current license status.
// It checks the local cache first, verifies the device ID matches,
// and if the cache is expired, tries to re-validate online.
func CheckLicense() *LicenseInfo {
	cached, err := LoadCachedLicense()
	if err != nil {
		return &LicenseInfo{Valid: false, Plan: "free"}
	}

	if !cached.Valid {
		return &LicenseInfo{Valid: false, Plan: "free"}
	}

	// Client-side device binding check: verify the cached license
	// was issued for THIS device. Prevents copying license.json
	// to another machine.
	currentDeviceID := GenerateDeviceID()
	if cached.DeviceID != "" && cached.DeviceID != currentDeviceID {
		return &LicenseInfo{Valid: false, Plan: "free"}
	}

	// Check if subscription has expired
	if !cached.ExpiresAt.IsZero() && time.Now().After(cached.ExpiresAt) {
		return &LicenseInfo{Valid: false, Plan: "free"}
	}

	// Check if we need to re-validate (cache expired)
	if time.Since(cached.ValidatedAt) > CacheDuration {
		// Try to re-validate online (server also checks device_id)
		refreshed, err := ValidateOnline(cached.Key)
		if err != nil {
			// Offline: trust the cache for a grace period (double the cache duration)
			if time.Since(cached.ValidatedAt) > CacheDuration*2 {
				return &LicenseInfo{Valid: false, Plan: "free"}
			}
			return cached
		}
		_ = SaveLicense(refreshed)
		return refreshed
	}

	return cached
}

// IsPro returns true if the user has a valid pro license for this device.
func IsPro() bool {
	// Dev bypass: only enable for this specific device
	if GenerateDeviceID() == "0b3794b39ee44bff" {
		return true
	}
	info := CheckLicense()
	return info.Valid && info.Plan == "pro"
}

// Deactivate removes the stored license.
func Deactivate() error {
	return RemoveLicense()
}

// GetDeviceInfo returns the current device ID and summary for display.
func GetDeviceInfo() (id string, summary string) {
	return GenerateDeviceID(), DeviceSummary()
}
