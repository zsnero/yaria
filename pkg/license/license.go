package license

import "yaria/internal/license"

type LicenseInfo = license.LicenseInfo
type ActivationRequest = license.ActivationRequest
type APIResponse = license.APIResponse
type DeviceFingerprint = license.DeviceFingerprint

func LoadCachedLicense() (*LicenseInfo, error)        { return license.LoadCachedLicense() }
func SaveLicense(info *LicenseInfo) error              { return license.SaveLicense(info) }
func RemoveLicense() error                             { return license.RemoveLicense() }
func ValidateOnline(key string) (*LicenseInfo, error)  { return license.ValidateOnline(key) }
func ActivateKey(key string) (*LicenseInfo, error)     { return license.ActivateKey(key) }
func StartTrial() (*LicenseInfo, error)                { return license.StartTrial() }
func CheckLicense() *LicenseInfo                       { return license.CheckLicense() }
func IsPro() bool                                      { return license.IsPro() }
func Deactivate() error                                { return license.Deactivate() }
func GenerateDeviceID() string                         { return license.GenerateDeviceID() }
func DeviceSummary() string                            { return license.DeviceSummary() }
func GetDeviceInfo() (string, string)                  { return license.GetDeviceInfo() }
func GetDeviceFingerprint() DeviceFingerprint          { return license.GetDeviceFingerprint() }
