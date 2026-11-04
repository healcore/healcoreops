//go:build !windows

package core

import "github.com/healcore/healcoreops/internal/history"

// writeSystemEvent вне Windows ничего не делает (журнал — в файле).
func writeSystemEvent(history.Entry) {}
