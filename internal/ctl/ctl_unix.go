//go:build !windows

package ctl

import (
	"os"
	"os/exec"
	"runtime"
)

// OpenURL открывает ссылку в браузере по умолчанию.
func OpenURL(u string) error {
	bin := "xdg-open"
	if runtime.GOOS == "darwin" {
		bin = "open"
	}
	return exec.Command(bin, u).Start()
}

// Значения nice для каждого уровня приоритета (меньше — выше приоритет).
var niceOf = []int{19, 10, 0, -5, -10}

func fromNice(n int) Priority {
	switch {
	case n >= 15:
		return PrioIdle
	case n >= 5:
		return PrioBelowNormal
	case n > -3:
		return PrioNormal
	case n > -8:
		return PrioAboveNormal
	default:
		return PrioHigh
	}
}

// IsElevated — запущено ли приложение от root.
func IsElevated() bool { return os.Geteuid() == 0 }

// RestartElevated недоступен: в Linux/macOS запустите приложение через sudo.
func RestartElevated() error { return ErrUnsupported }
