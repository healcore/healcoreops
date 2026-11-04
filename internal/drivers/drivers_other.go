//go:build !windows

package drivers

import "github.com/healcore/healcoreops/internal/ctl"

var errBusy = ctl.ErrUnsupported

// Supported — работа с драйверами доступна только в Windows.
func Supported() bool { return false }

// List недоступен вне Windows.
func List() ([]Device, error) { return nil, ctl.ErrUnsupported }

// Check недоступен вне Windows.
func Check(bool) error { return ctl.ErrUnsupported }

// Update недоступен вне Windows.
func Update([]string, bool, bool) error { return ctl.ErrUnsupported }
