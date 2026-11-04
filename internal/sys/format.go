package sys

import (
	"fmt"

	"github.com/healcore/healcoreops/internal/i18n"
)

var (
	unitsRU = []string{"Б", "КБ", "МБ", "ГБ", "ТБ", "ПБ"}
	unitsEN = []string{"B", "KB", "MB", "GB", "TB", "PB"}
)

// Bytes форматирует размер в человекочитаемом виде.
func Bytes(b uint64) string {
	units := unitsRU
	if i18n.Get() == "en" {
		units = unitsEN
	}
	v := float64(b)
	i := 0
	for v >= 1024 && i < len(units)-1 {
		v /= 1024
		i++
	}
	switch {
	case i == 0:
		return fmt.Sprintf("%d %s", b, units[0])
	case v >= 100:
		return fmt.Sprintf("%.0f %s", v, units[i])
	case v >= 10:
		return fmt.Sprintf("%.1f %s", v, units[i])
	default:
		return fmt.Sprintf("%.2f %s", v, units[i])
	}
}

// Rate форматирует скорость передачи.
func Rate(bps float64) string {
	if bps < 0 {
		bps = 0
	}
	return Bytes(uint64(bps)) + i18n.Pick("/с", "/s")
}

// Uptime форматирует время работы системы.
func Uptime(sec uint64) string {
	d := sec / 86400
	h := sec % 86400 / 3600
	m := sec % 3600 / 60
	if d > 0 {
		return fmt.Sprintf("%d д %d ч %d мин", d, h, m)
	}
	if h > 0 {
		return fmt.Sprintf("%d ч %d мин", h, m)
	}
	return fmt.Sprintf("%d мин", m)
}
