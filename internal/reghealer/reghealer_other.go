//go:build !windows

package reghealer

import "github.com/healcore/healcoreops/internal/ctl"

// Supported — проверка реестра доступна только в Windows.
func Supported() bool { return false }

// Scan недоступен вне Windows.
func Scan() ([]Issue, error) { return nil, ctl.ErrUnsupported }

// Fix недоступен вне Windows.
func Fix([]string) (FixReport, error) { return FixReport{}, ctl.ErrUnsupported }

// Restore вне Windows не поддерживается.
func Restore(string) error { return ctl.ErrUnsupported }
