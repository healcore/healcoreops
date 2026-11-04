//go:build windows

package ctl

import (
	"github.com/healcore/healcoreops/internal/i18n"
	"os"
	"path/filepath"

	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	kernel32        = windows.NewLazySystemDLL("kernel32.dll")
	procSetAffinity = kernel32.NewProc("SetProcessAffinityMask")
	procGetAffinity = kernel32.NewProc("GetProcessAffinityMask")
	procEmptyWS     = kernel32.NewProc("K32EmptyWorkingSet")
)

var priorityClasses = []uint32{
	windows.IDLE_PRIORITY_CLASS,
	windows.BELOW_NORMAL_PRIORITY_CLASS,
	windows.NORMAL_PRIORITY_CLASS,
	windows.ABOVE_NORMAL_PRIORITY_CLASS,
	windows.HIGH_PRIORITY_CLASS,
}

func open(pid int32, access uint32) (windows.Handle, error) {
	h, err := windows.OpenProcess(access, false, uint32(pid))
	return h, friendly(err)
}

// SetPriority задаёт класс приоритета процесса.
func SetPriority(pid int32, p Priority) error {
	if p < 0 || int(p) >= len(priorityClasses) {
		return errors.New(i18n.T("неизвестный приоритет"))
	}
	h, err := open(pid, windows.PROCESS_SET_INFORMATION|windows.PROCESS_QUERY_LIMITED_INFORMATION)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(h)
	return friendly(windows.SetPriorityClass(h, priorityClasses[p]))
}

// GetPriority возвращает текущий класс приоритета процесса.
func GetPriority(pid int32) (Priority, error) {
	h, err := open(pid, windows.PROCESS_QUERY_LIMITED_INFORMATION)
	if err != nil {
		return -1, err
	}
	defer windows.CloseHandle(h)
	c, err := windows.GetPriorityClass(h)
	if err != nil {
		return -1, friendly(err)
	}
	switch c {
	case windows.IDLE_PRIORITY_CLASS:
		return PrioIdle, nil
	case windows.BELOW_NORMAL_PRIORITY_CLASS:
		return PrioBelowNormal, nil
	case windows.ABOVE_NORMAL_PRIORITY_CLASS:
		return PrioAboveNormal, nil
	case windows.HIGH_PRIORITY_CLASS, windows.REALTIME_PRIORITY_CLASS:
		return PrioHigh, nil
	default:
		return PrioNormal, nil
	}
}

// AffinitySupported сообщает, можно ли привязывать процессы к ядрам.
func AffinitySupported() bool { return true }

// SetAffinity привязывает процесс к указанным логическим ядрам.
func SetAffinity(pid int32, cores []int) error {
	mask, err := coresMask(cores)
	if err != nil {
		return err
	}
	h, err := open(pid, windows.PROCESS_SET_INFORMATION|windows.PROCESS_QUERY_LIMITED_INFORMATION)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(h)
	r, _, e := procSetAffinity.Call(uintptr(h), uintptr(mask))
	if r == 0 {
		return friendly(e)
	}
	return nil
}

// GetAffinity возвращает список ядер, на которых может работать процесс.
func GetAffinity(pid int32) ([]int, error) {
	h, err := open(pid, windows.PROCESS_QUERY_LIMITED_INFORMATION)
	if err != nil {
		return nil, err
	}
	defer windows.CloseHandle(h)
	var pm, sm uintptr
	r, _, e := procGetAffinity.Call(uintptr(h), uintptr(unsafe.Pointer(&pm)), uintptr(unsafe.Pointer(&sm)))
	if r == 0 {
		return nil, friendly(e)
	}
	var out []int
	for i := 0; i < 64; i++ {
		if pm&(1<<uint(i)) != 0 {
			out = append(out, i)
		}
	}
	return out, nil
}

// IsElevated — запущено ли приложение с правами администратора.
func IsElevated() bool {
	return windows.GetCurrentProcessToken().IsElevated()
}

// TrimMemory сжимает рабочие наборы всех доступных процессов:
// неиспользуемые страницы уходят из ОЗУ, освобождая память для активных задач.
func TrimMemory() (TrimResult, error) {
	pids := make([]uint32, 4096)
	var n uint32
	if err := windows.EnumProcesses(pids, &n); err != nil {
		return TrimResult{}, friendly(err)
	}
	count := 0
	for _, pid := range pids[:n/4] {
		if pid == 0 {
			continue
		}
		h, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
		if err != nil {
			continue
		}
		if r, _, _ := procEmptyWS.Call(uintptr(h)); r != 0 {
			count++
		}
		windows.CloseHandle(h)
	}
	return TrimResult{Processes: count, Note: i18n.T("сжаты рабочие наборы процессов")}, nil
}

var powerGUID = map[PowerProfile]string{
	PowerPerformance: "8c5e7fda-e8bf-4a96-9a85-a6e23a8c635c",
	PowerBalanced:    "381b4222-f694-41f0-9685-ff5bb260df2e",
	PowerSaver:       "a1841308-3542-4fab-9e3d-6e4d3b4e9e3e",
}

var guidRe = regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`)

func powercfg(args ...string) (string, error) {
	cmd := exec.Command("powercfg", args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// SetPowerProfile переключает схему электропитания Windows.
func SetPowerProfile(p PowerProfile) error {
	guid := powerGUID[p]
	if _, err := powercfg("/setactive", guid); err == nil {
		return nil
	}
	// Схема может быть скрыта (Modern Standby) — пробуем восстановить её из шаблона.
	out, err := powercfg("-duplicatescheme", guid)
	if err != nil {
		return fmt.Errorf(i18n.T("схема «%s» недоступна в этой системе"), PowerNames[p])
	}
	newGUID := guidRe.FindString(out)
	if newGUID == "" {
		return fmt.Errorf(i18n.T("не удалось создать схему «%s»"), PowerNames[p])
	}
	if _, err := powercfg("/setactive", newGUID); err != nil {
		return fmt.Errorf(i18n.T("не удалось включить схему «%s»"), PowerNames[p])
	}
	return nil
}

// CurrentPowerProfile определяет активную схему электропитания.
func CurrentPowerProfile() (PowerProfile, error) {
	out, err := powercfg("/getactivescheme")
	if err != nil {
		return PowerBalanced, err
	}
	low := strings.ToLower(out)
	for p, g := range powerGUID {
		if strings.Contains(low, g) {
			return p, nil
		}
	}
	return PowerBalanced, errors.New(i18n.T("используется пользовательская схема"))
}

// RestartElevated перезапускает приложение с запросом прав администратора (UAC).
func RestartElevated() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	verb, _ := windows.UTF16PtrFromString("runas")
	file, _ := windows.UTF16PtrFromString(exe)
	// --relaunch: новая копия дождётся выхода текущей, а не передаст ей фокус.
	args, _ := windows.UTF16PtrFromString("--relaunch")
	dir, _ := windows.UTF16PtrFromString(filepath.Dir(exe))
	if err := windows.ShellExecute(0, verb, file, args, dir, windows.SW_SHOWNORMAL); err != nil {
		return errors.New(i18n.T("запуск с правами администратора отменён"))
	}
	return nil
}

// OpenURL открывает ссылку в браузере по умолчанию.
func OpenURL(u string) error {
	verb, _ := windows.UTF16PtrFromString("open")
	p, _ := windows.UTF16PtrFromString(u)
	return windows.ShellExecute(0, verb, p, nil, nil, windows.SW_SHOWNORMAL)
}
