//go:build !windows

package tweaks

import "github.com/healcore/healcoreops/internal/ctl"

// Supported — твики доступны только в Windows.
func Supported() bool { return false }

// ListApps недоступен вне Windows.
func ListApps() ([]App, error) { return nil, ctl.ErrUnsupported }

// RemoveApps недоступен вне Windows.
func RemoveApps([]string) []Result { return nil }

// ListSettings недоступен вне Windows.
func ListSettings() []Setting { return nil }

// SetSetting недоступен вне Windows.
func SetSetting(string, bool) error { return ctl.ErrUnsupported }

// CreateRestorePoint недоступен вне Windows.
func CreateRestorePoint() error { return ctl.ErrUnsupported }

// RestartExplorer недоступен вне Windows.
func RestartExplorer() error { return ctl.ErrUnsupported }
