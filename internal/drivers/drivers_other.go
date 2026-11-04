//go:build !windows

package drivers

import "github.com/healcore/healcoreops/internal/ctl"

var errBusy = ctl.ErrUnsupported

// Supported — работа с драйверами доступна только в Windows.
func Supported() bool { return false }

// List недоступен вне Windows.
func List() ([]Device, error) { return nil, ctl.ErrUnsupported }

// Scan недоступен вне Windows.
func Scan() error { return ctl.ErrUnsupported }

// Install недоступен вне Windows.
func Install([]string) error { return ctl.ErrUnsupported }

// InstallVendor недоступен вне Windows.
func InstallVendor(string, string) (string, error) { return "", ctl.ErrUnsupported }
