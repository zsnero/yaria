package license

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/user"
	"runtime"
	"strings"
)

// Contains the raw components used to generate a device ID.
type DeviceFingerprint struct {
	OS        string // runtime.GOOS (linux, darwin, windows)
	Hostname  string // machine hostname
	Username  string // OS username
	MachineID string // OS-specific machine identifier
}

// Collects all device identifiers.
func GetDeviceFingerprint() DeviceFingerprint {
	fp := DeviceFingerprint{
		OS: runtime.GOOS,
	}

	if hostname, err := os.Hostname(); err == nil {
		fp.Hostname = hostname
	}

	if u, err := user.Current(); err == nil {
		fp.Username = u.Username
	}

	fp.MachineID = getMachineID()

	return fp
}

// Produces a deterministic SHA-256 hash from all device
// components. The same machine + OS + user will always produce the same ID.
// Returns a 16-character hex string (first 8 bytes of the hash).
func GenerateDeviceID() string {
	fp := GetDeviceFingerprint()
	raw := fmt.Sprintf("yaria:%s:%s:%s:%s", fp.OS, fp.Hostname, fp.Username, fp.MachineID)
	hash := sha256.Sum256([]byte(raw))
	return fmt.Sprintf("%x", hash[:8])
}

// Returns a human-readable summary of the device (for display).
func DeviceSummary() string {
	fp := GetDeviceFingerprint()
	osName := fp.OS
	switch osName {
	case "darwin":
		osName = "macOS"
	case "linux":
		osName = "Linux"
	case "windows":
		osName = "Windows"
	}
	return fmt.Sprintf("%s (%s@%s)", osName, fp.Username, fp.Hostname)
}

// Retrieves the OS-specific machine identifier.
func getMachineID() string {
	switch runtime.GOOS {
	case "linux":
		return getLinuxMachineID()
	case "darwin":
		return getDarwinMachineID()
	case "windows":
		return getWindowsMachineID()
	default:
		return "unknown"
	}
}

// Reads /etc/machine-id or /var/lib/dbus/machine-id.
func getLinuxMachineID() string {
	paths := []string{"/etc/machine-id", "/var/lib/dbus/machine-id"}
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err == nil {
			id := strings.TrimSpace(string(data))
			if id != "" {
				return id
			}
		}
	}
	return "unknown"
}

// Uses IOPlatformUUID via ioreg.
func getDarwinMachineID() string {
	// Read from ioreg output cached in a known location, or use a fallback.
	// We avoid exec here for portability; use the hardware UUID file if available.
	// On macOS, /Library/Preferences/SystemConfiguration/com.apple.smb.server.plist
	// contains a UUID, but the most reliable is ioreg. We'll use os/exec as a last resort.
	//
	// Fallback: use hostname + username hash if ioreg isn't available at compile time.
	// In practice, this runs fine on macOS.
	data, err := execCommand("ioreg", "-rd1", "-c", "IOPlatformExpertDevice")
	if err != nil {
		return "unknown"
	}
	// Parse IOPlatformUUID from output
	for _, line := range strings.Split(string(data), "\n") {
		if strings.Contains(line, "IOPlatformUUID") {
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				uuid := strings.TrimSpace(parts[1])
				uuid = strings.Trim(uuid, "\" ")
				if uuid != "" {
					return uuid
				}
			}
		}
	}
	return "unknown"
}

// Reads the MachineGuid from the Windows registry.
func getWindowsMachineID() string {
	data, err := execCommand("reg", "query",
		`HKEY_LOCAL_MACHINE\SOFTWARE\Microsoft\Cryptography`,
		"/v", "MachineGuid")
	if err != nil {
		return "unknown"
	}
	// Parse REG_SZ value from output
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.Contains(line, "MachineGuid") {
			parts := strings.Fields(line)
			if len(parts) >= 3 {
				return parts[len(parts)-1]
			}
		}
	}
	return "unknown"
}
