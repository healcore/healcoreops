//go:build !windows

package maintenance

import "github.com/healcore/healcoreops/internal/ctl"

var errBusy = ctl.ErrUnsupported

// Supported — обслуживание доступно только в Windows.
func Supported() bool { return false }

// Actions недоступно вне Windows.
func Actions() []Action { return nil }

// Run недоступно вне Windows.
func Run(string) error { return ctl.ErrUnsupported }
