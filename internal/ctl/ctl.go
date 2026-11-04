// Package ctl — платформенно-зависимое управление процессами и системой:
// приоритеты, привязка к ядрам, освобождение памяти, профили питания.
package ctl

import (
	"github.com/healcore/healcoreops/internal/i18n"

	"errors"
	"os"

	"github.com/shirou/gopsutil/v4/process"
)

// Priority — абстрактный уровень приоритета, одинаковый для всех ОС.
type Priority int

const (
	PrioIdle Priority = iota
	PrioBelowNormal
	PrioNormal
	PrioAboveNormal
	PrioHigh
)

// PriorityNames — подписи уровней приоритета для интерфейса.
var PriorityNames = []string{"Низкий", "Ниже среднего", "Обычный", "Выше среднего", "Высокий"}

func (p Priority) String() string {
	if p < 0 || int(p) >= len(PriorityNames) {
		return "—"
	}
	return i18n.T(PriorityNames[p])
}

// PriorityFromName возвращает уровень по подписи или -1.
func PriorityFromName(name string) Priority {
	for i, n := range PriorityNames {
		if n == name {
			return Priority(i)
		}
	}
	return -1
}

// PowerProfile — профиль энергопотребления.
type PowerProfile int

const (
	PowerPerformance PowerProfile = iota
	PowerBalanced
	PowerSaver
)

// PowerNames — подписи профилей.
var PowerNames = []string{"Производительность", "Сбалансированный", "Энергосбережение"}

// TrimResult — итог освобождения памяти.
type TrimResult struct {
	Processes int    // сколько процессов обработано
	Note      string // пояснение, что было сделано
}

// ErrUnsupported — операция недоступна на этой платформе.
var ErrUnsupported error = unsupported{}

type unsupported struct{}

func (unsupported) Error() string {
	return i18n.T("операция не поддерживается в этой ОС")
}

func friendly(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, os.ErrPermission) {
		return errors.New(i18n.T("недостаточно прав — запустите приложение от имени администратора"))
	}
	if errors.Is(err, os.ErrNotExist) || errors.Is(err, process.ErrorProcessNotRunning) {
		return errors.New(i18n.T("процесс уже завершён"))
	}
	return err
}

func proc(pid int32) (*process.Process, error) {
	p, err := process.NewProcess(pid)
	if err != nil {
		return nil, friendly(err)
	}
	return p, nil
}

// Kill принудительно завершает процесс.
func Kill(pid int32) error {
	if pid == int32(os.Getpid()) {
		return errors.New(i18n.T("нельзя завершить само приложение"))
	}
	p, err := proc(pid)
	if err != nil {
		return err
	}
	return friendly(p.Kill())
}

// Suspend приостанавливает процесс.
func Suspend(pid int32) error {
	if pid == int32(os.Getpid()) {
		return errors.New(i18n.T("нельзя приостановить само приложение"))
	}
	p, err := proc(pid)
	if err != nil {
		return err
	}
	return friendly(p.Suspend())
}

// Resume возобновляет приостановленный процесс.
func Resume(pid int32) error {
	p, err := proc(pid)
	if err != nil {
		return err
	}
	return friendly(p.Resume())
}

func coresMask(cores []int) (uint64, error) {
	var m uint64
	for _, c := range cores {
		if c >= 0 && c < 64 {
			m |= 1 << uint(c)
		}
	}
	if m == 0 {
		return 0, errors.New(i18n.T("не выбрано ни одного ядра"))
	}
	return m, nil
}
