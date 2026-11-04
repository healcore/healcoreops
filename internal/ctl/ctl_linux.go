//go:build linux

package ctl

import (
	"github.com/healcore/healcoreops/internal/i18n"

	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

// SetPriority меняет nice-значение процесса.
func SetPriority(pid int32, p Priority) error {
	if p < 0 || int(p) >= len(niceOf) {
		return errors.New(i18n.T("неизвестный приоритет"))
	}
	// В Linux nice задаётся для каждого потока отдельно — применяем ко всем.
	return forEachThread(pid, func(tid int) error {
		return unix.Setpriority(unix.PRIO_PROCESS, tid, niceOf[p])
	})
}

// forEachThread вызывает fn для каждого потока процесса.
func forEachThread(pid int32, fn func(tid int) error) error {
	tids := []int{int(pid)}
	if ents, err := os.ReadDir(fmt.Sprintf("/proc/%d/task", pid)); err == nil && len(ents) > 0 {
		tids = tids[:0]
		for _, e := range ents {
			var t int
			if _, err := fmt.Sscan(e.Name(), &t); err == nil {
				tids = append(tids, t)
			}
		}
	}
	var first error
	ok := 0
	for _, t := range tids {
		if err := fn(t); err != nil {
			if first == nil && !errors.Is(err, unix.ESRCH) {
				first = err
			}
			continue
		}
		ok++
	}
	if ok == 0 && first != nil {
		return friendly(first)
	}
	if ok == 0 {
		return errors.New(i18n.T("процесс уже завершён"))
	}
	return nil
}

// GetPriority читает nice-значение процесса.
func GetPriority(pid int32) (Priority, error) {
	v, err := unix.Getpriority(unix.PRIO_PROCESS, int(pid))
	if err != nil {
		return -1, friendly(err)
	}
	// Системный вызов в Linux возвращает 20 - nice.
	return fromNice(20 - v), nil
}

// AffinitySupported сообщает, можно ли привязывать процессы к ядрам.
func AffinitySupported() bool { return true }

// SetAffinity привязывает процесс к указанным ядрам.
func SetAffinity(pid int32, cores []int) error {
	if _, err := coresMask(cores); err != nil {
		return err
	}
	var set unix.CPUSet
	set.Zero()
	for _, c := range cores {
		set.Set(c)
	}
	return forEachThread(pid, func(tid int) error {
		return unix.SchedSetaffinity(tid, &set)
	})
}

// GetAffinity возвращает ядра, разрешённые процессу.
func GetAffinity(pid int32) ([]int, error) {
	var set unix.CPUSet
	if err := unix.SchedGetaffinity(int(pid), &set); err != nil {
		return nil, friendly(err)
	}
	var out []int
	for i := 0; i < 1024; i++ {
		if set.IsSet(i) {
			out = append(out, i)
		}
	}
	return out, nil
}

// TrimMemory сбрасывает страничный кэш ядра и уплотняет память (нужен root).
func TrimMemory() (TrimResult, error) {
	if !IsElevated() {
		return TrimResult{}, errors.New(i18n.T("для сброса кэша ядра нужны права root (запустите через sudo)"))
	}
	unix.Sync()
	if err := os.WriteFile("/proc/sys/vm/drop_caches", []byte("1"), 0o200); err != nil {
		return TrimResult{}, friendly(err)
	}
	_ = os.WriteFile("/proc/sys/vm/compact_memory", []byte("1"), 0o200)
	return TrimResult{Note: i18n.T("сброшен страничный кэш, память уплотнена")}, nil
}

var ppdNames = []string{"performance", "balanced", "power-saver"}

// SetPowerProfile переключает профиль через power-profiles-daemon или регулятор частоты ЦП.
func SetPowerProfile(p PowerProfile) error {
	if path, err := exec.LookPath("powerprofilesctl"); err == nil {
		out, err := exec.Command(path, "set", ppdNames[p]).CombinedOutput()
		if err == nil {
			return nil
		}
		if msg := strings.TrimSpace(string(out)); msg != "" {
			return fmt.Errorf("powerprofilesctl: %s", lastLine(msg))
		}
		return err
	}
	return setGovernor(p)
}

func setGovernor(p PowerProfile) error {
	files, _ := filepath.Glob("/sys/devices/system/cpu/cpu[0-9]*/cpufreq/scaling_governor")
	if len(files) == 0 {
		return errors.New(i18n.T("управление частотой ЦП недоступно"))
	}
	avail, _ := os.ReadFile("/sys/devices/system/cpu/cpu0/cpufreq/scaling_available_governors")
	has := func(g string) bool { return strings.Contains(" "+string(avail)+" ", " "+g+" ") }
	gov := "performance"
	switch p {
	case PowerBalanced:
		gov = "powersave"
		for _, g := range []string{"schedutil", "ondemand", "conservative"} {
			if has(g) {
				gov = g
				break
			}
		}
	case PowerSaver:
		gov = "powersave"
	}
	for _, f := range files {
		if err := os.WriteFile(f, []byte(gov), 0o200); err != nil {
			return friendly(err)
		}
	}
	return nil
}

// CurrentPowerProfile определяет текущий профиль.
func CurrentPowerProfile() (PowerProfile, error) {
	if path, err := exec.LookPath("powerprofilesctl"); err == nil {
		if out, err := exec.Command(path, "get").Output(); err == nil {
			s := strings.TrimSpace(string(out))
			for i, n := range ppdNames {
				if s == n {
					return PowerProfile(i), nil
				}
			}
		}
	}
	b, err := os.ReadFile("/sys/devices/system/cpu/cpu0/cpufreq/scaling_governor")
	if err != nil {
		return PowerBalanced, errors.New(i18n.T("профиль не определён"))
	}
	switch strings.TrimSpace(string(b)) {
	case "performance":
		return PowerPerformance, nil
	case "powersave":
		return PowerSaver, nil
	}
	return PowerBalanced, nil
}

func lastLine(s string) string {
	lines := strings.Split(s, "\n")
	return lines[len(lines)-1]
}
