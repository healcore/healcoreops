//go:build !windows

package autostart

import "github.com/healcore/healcoreops/internal/ctl"

// Supported — управление автозагрузкой доступно только в Windows.
func Supported() bool { return false }

// List недоступен вне Windows.
func List() ([]Item, error) { return nil, ctl.ErrUnsupported }

// SetEnabled недоступен вне Windows.
func SetEnabled(string, bool) error { return ctl.ErrUnsupported }

// SetSelfStartup недоступен вне Windows.
func SetSelfStartup(bool) error { return ctl.ErrUnsupported }

// IsSelfStartup вне Windows всегда false.
func IsSelfStartup() bool { return false }
