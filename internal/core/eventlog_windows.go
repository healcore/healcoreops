//go:build windows

package core

import (
	"fmt"
	"sync"

	"golang.org/x/sys/windows/svc/eventlog"

	"github.com/healcore/healcoreops/internal/ctl"
	"github.com/healcore/healcoreops/internal/history"
)

const eventSource = "HealCoreOps"

var (
	evOnce sync.Once
	evLog  *eventlog.Log
)

// writeSystemEvent пишет изменение в журнал Windows «Приложение» (источник
// HealCoreOps, код 1000) — для аудита администратором и сбора SIEM.
func writeSystemEvent(e history.Entry) {
	evOnce.Do(func() {
		if ctl.IsElevated() {
			// Регистрация источника с сообщениями EventCreate — текст читается без «описание не найдено».
			_ = eventlog.InstallAsEventCreate(eventSource, eventlog.Info|eventlog.Warning|eventlog.Error)
		}
		evLog, _ = eventlog.Open(eventSource)
	})
	if evLog == nil {
		return
	}
	msg := fmt.Sprintf("%s\r\n%s\r\nArea: %s; Source: %s; User: %s; Admin: %v", e.Title, e.Detail, e.Area, e.Source, e.User, e.Admin)
	_ = evLog.Info(1000, msg)
}
