//go:build !windows && !linux

package ctl

import (
	"github.com/healcore/healcoreops/internal/i18n"

	"errors"
	"os/exec"
	"runtime"
	"strings"

	"golang.org/x/sys/unix"
)

// SetPriority меняет nice-значение процесса.
func SetPriority(pid int32, p Priority) error {
	if p < 0 || int(p) >= len(niceOf) {
		return errors.New(i18n.T("неизвестный приоритет"))
	}
	return friendly(unix.Setpriority(unix.PRIO_PROCESS, int(pid), niceOf[p]))
}

// GetPriority читает nice-значение процесса.
func GetPriority(pid int32) (Priority, error) {
	v, err := unix.Getpriority(unix.PRIO_PROCESS, int(pid))
	if err != nil {
		return -1, friendly(err)
	}
	return fromNice(v), nil
}

// AffinitySupported — в macOS/BSD привязка к ядрам из userspace недоступна.
func AffinitySupported() bool { return false }

// SetAffinity не поддерживается на этой платформе.
func SetAffinity(int32, []int) error { return ErrUnsupported }

// GetAffinity не поддерживается на этой платформе.
func GetAffinity(int32) ([]int, error) { return nil, ErrUnsupported }

// TrimMemory вызывает системную утилиту purge (macOS, нужен root).
func TrimMemory() (TrimResult, error) {
	if runtime.GOOS != "darwin" {
		return TrimResult{}, ErrUnsupported
	}
	if out, err := exec.Command("purge").CombinedOutput(); err != nil {
		if s := strings.TrimSpace(string(out)); s != "" {
			return TrimResult{}, errors.New(s)
		}
		return TrimResult{}, errors.New(i18n.T("для очистки памяти нужны права root (sudo)"))
	}
	return TrimResult{Note: i18n.T("очищен дисковый кэш (purge)")}, nil
}

// SetPowerProfile включает/выключает режим энергосбережения macOS.
func SetPowerProfile(p PowerProfile) error {
	if runtime.GOOS != "darwin" {
		return ErrUnsupported
	}
	v := "0"
	if p == PowerSaver {
		v = "1"
	}
	if out, err := exec.Command("pmset", "-a", "lowpowermode", v).CombinedOutput(); err != nil {
		if s := strings.TrimSpace(string(out)); s != "" {
			return errors.New(s)
		}
		return errors.New(i18n.T("нужны права root (sudo)"))
	}
	return nil
}

// CurrentPowerProfile читает состояние режима энергосбережения.
func CurrentPowerProfile() (PowerProfile, error) {
	out, err := exec.Command("pmset", "-g").Output()
	if err != nil {
		return PowerBalanced, err
	}
	for _, l := range strings.Split(string(out), "\n") {
		f := strings.Fields(l)
		if len(f) == 2 && f[0] == "lowpowermode" && f[1] == "1" {
			return PowerSaver, nil
		}
	}
	return PowerBalanced, nil
}
