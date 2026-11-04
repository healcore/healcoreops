package core

import (
	"testing"
	"time"

	"github.com/healcore/healcoreops/internal/config"
)

func TestScheduleDue(t *testing.T) {
	sun19 := time.Date(2026, 11, 8, 19, 5, 0, 0, time.Local) // воскресенье
	s := config.Schedule{Enabled: true, Day: 0, Hour: 19, Cats: []string{"user_temp"}}
	if !scheduleDue(s, sun19) {
		t.Fatal("должно сработать в воскресенье в 19:05")
	}
	if scheduleDue(s, sun19.Add(-time.Hour)) {
		t.Fatal("до 19:00 рано")
	}
	if scheduleDue(s, sun19.AddDate(0, 0, 1)) {
		t.Fatal("в понедельник не нужно")
	}
	s.LastRun = "2026-11-08 19:01"
	if scheduleDue(s, sun19) {
		t.Fatal("сегодня уже выполнено")
	}
	s.Day, s.LastRun = 7, "2026-11-08 19:01"
	if !scheduleDue(s, sun19.AddDate(0, 0, 1)) {
		t.Fatal("каждый день — должно сработать назавтра")
	}
	s.Enabled = false
	if scheduleDue(s, sun19.AddDate(0, 0, 2)) {
		t.Fatal("выключено")
	}
}
