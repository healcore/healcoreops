//go:build !windows

// Package winps запускает скрипты PowerShell (только Windows).
package winps

import (
	"errors"
	"strings"
	"time"
)

var errNo = errors.New("PowerShell is available on Windows only")

// Run недоступен вне Windows.
func Run(string, time.Duration) ([]byte, error) { return nil, errNo }

// JSON недоступен вне Windows.
func JSON(string, time.Duration, any) error { return errNo }

// Quote экранирует строку для PowerShell.
func Quote(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

// Stream недоступен вне Windows.
func Stream(string, time.Duration, func(string)) error { return errNo }
