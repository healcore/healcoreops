package core

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v4/host"

	"github.com/healcore/healcoreops/internal/autostart"
	"github.com/healcore/healcoreops/internal/config"
	"github.com/healcore/healcoreops/internal/ctl"
	"github.com/healcore/healcoreops/internal/diag"
	"github.com/healcore/healcoreops/internal/diskspace"
	"github.com/healcore/healcoreops/internal/drivers"
	"github.com/healcore/healcoreops/internal/history"
	"github.com/healcore/healcoreops/internal/i18n"
	"github.com/healcore/healcoreops/internal/opt"
	"github.com/healcore/healcoreops/internal/sys"
)

// CheckItem — результат одной проверки «Проверки ПК».
type CheckItem struct {
	ID     string `json:"id"`
	Level  int    `json:"level"` // 0 хорошо, 1 внимание, 2 проблема
	Title  string `json:"title"`
	Detail string `json:"detail"`
	Page   string `json:"page,omitempty"`   // раздел, где исправить
	Action string `json:"action,omitempty"` // быстрое действие: trim | quickclean
}

// Checkup — итог проверки.
type Checkup struct {
	Items   []CheckItem `json:"items"`
	Bad     int         `json:"bad"`
	Warn    int         `json:"warn"`
	Elapsed int64       `json:"elapsed"`
}

// quickCats — категории, которые безопасно чистить без вопросов.
func quickCats() []string {
	c := []string{"user_temp", "wer", "thumbs"}
	if ctl.IsElevated() {
		c = append(c, "win_temp")
	}
	return c
}

// RunCheckup проверяет компьютер целиком: нагрузку, мусор, безопасность,
// сбои, драйверы, накопители, автозагрузку и ожидающую перезагрузку.
func (c *Core) RunCheckup() Checkup {
	t0 := time.Now()
	P := i18n.Pick
	var (
		mu    sync.Mutex
		items []CheckItem
		wg    sync.WaitGroup
	)
	add := func(it CheckItem) { mu.Lock(); items = append(items, it); mu.Unlock() }
	run := func(f func()) { wg.Add(1); go func() { defer wg.Done(); f() }() }

	// Нагрузка и рекомендации индекса состояния
	if s := c.Snapshot(); s != nil {
		_, recs := c.Analysis(s)
		for _, r := range recs {
			lvl := 1
			if r.Level == opt.Crit {
				lvl = 2
			}
			add(CheckItem{ID: "rec", Level: lvl, Title: r.Title, Detail: r.Detail, Page: "optimize"})
		}
	}
	run(func() {
		if r := diag.PendingReboot(); r.Pending {
			add(CheckItem{ID: "reboot", Level: 1, Title: P("Windows ждёт перезагрузки", "Windows is waiting for a reboot"),
				Detail: P("Причина: ", "Reason: ") + strings.Join(r.Reasons, "; ")})
		}
	})
	run(func() {
		cats := diskspace.Categories()
		var total int64
		var parts []string
		quick := map[string]bool{}
		for _, q := range quickCats() {
			quick[q] = true
		}
		var quickBytes int64
		for _, ct := range cats {
			if !ct.Clean || (ct.Admin && !ctl.IsElevated()) || ct.Size < 1<<20 {
				continue
			}
			total += ct.Size
			if quick[ct.ID] {
				quickBytes += ct.Size
			}
			if len(parts) < 3 {
				parts = append(parts, fmt.Sprintf("%s — %s", ct.Title, sys.Bytes(uint64(ct.Size))))
			}
		}
		if total >= 1<<30 {
			it := CheckItem{ID: "junk", Level: 1, Title: fmt.Sprintf(P("Можно освободить %s", "%s can be freed"), sys.Bytes(uint64(total))),
				Detail: strings.Join(parts, "; "), Page: "disk"}
			if quickBytes >= 100<<20 {
				it.Action = "quickclean"
			}
			add(it)
		}
	})
	if diag.Supported() {
		run(func() {
			for _, s := range diag.Security() {
				if s.Level >= 1 {
					d := s.State
					if s.Detail != "" {
						d += ". " + s.Detail
					}
					add(CheckItem{ID: "sec_" + s.ID, Level: s.Level, Title: s.Title, Detail: d, Page: pageOr(s.Page, "report")})
				}
			}
		})
		run(func() {
			r, err := diag.Crashes(14)
			if err != nil {
				return
			}
			if n := len(r.BSOD); n > 0 {
				b := r.BSOD[0].Bug
				add(CheckItem{ID: "bsod", Level: 2, Title: fmt.Sprintf(P("Синих экранов за 2 недели: %d", "Blue screens in 2 weeks: %d"), n),
					Detail: b.Name + " (" + b.Hex + "). " + b.Hint, Page: "crashes"})
			}
			if n := len(r.Unexpected); n >= 2 {
				add(CheckItem{ID: "unexpected", Level: 1, Title: fmt.Sprintf(P("Внезапных выключений: %d", "Unexpected shutdowns: %d"), n),
					Detail: P("Компьютер выключался без завершения работы — питание, перегрев или зависание.", "The PC shut down without a proper shutdown — power, overheating or a hang."), Page: "crashes"})
			}
			for _, a := range r.Apps {
				if a.Count >= 3 {
					add(CheckItem{ID: "appcrash", Level: 1, Title: fmt.Sprintf(P("%s падает — сбоев: %d", "%s keeps crashing — %d crashes"), a.App, a.Count),
						Detail: P("Обновите или переустановите программу. Модуль: ", "Update or reinstall the app. Module: ") + a.Module, Page: "crashes"})
					break
				}
			}
		})
	}
	if drivers.Supported() {
		run(func() {
			devs, err := drivers.List()
			if err != nil {
				return
			}
			var need []string
			for _, d := range devs {
				if d.NeedsDriver {
					need = append(need, d.Name)
				}
			}
			if len(need) > 0 {
				add(CheckItem{ID: "drivers", Level: 2, Title: P("Устройства без драйвера", "Devices without a driver"), Detail: strings.Join(need, ", "), Page: "drivers"})
			}
		})
	}
	if autostart.Supported() {
		run(func() {
			list, err := autostart.List()
			if err != nil {
				return
			}
			n := 0
			for _, a := range list {
				if a.Enabled {
					n++
				}
			}
			if n > 10 {
				add(CheckItem{ID: "autostart", Level: 1, Title: fmt.Sprintf(P("В автозагрузке %d программ", "%d programs start with Windows"), n),
					Detail: P("Отключите лишние — Windows будет загружаться быстрее.", "Disable the extras — Windows will boot faster."), Page: "autostart"})
			}
		})
	}
	h := freshHealth()
	for _, d := range h.Disks {
		hl := strings.ToLower(d.Health)
		if d.Health != "" && hl != "healthy" && hl != "ok" && hl != "исправен" {
			add(CheckItem{ID: "disk_health", Level: 2, Title: P("Накопитель неисправен: ", "Drive is unhealthy: ") + d.Name,
				Detail: P("Сделайте резервную копию важных файлов как можно скорее.", "Back up important files as soon as possible."), Page: "overview"})
		} else if d.WearPct >= 80 {
			add(CheckItem{ID: "disk_wear", Level: 1, Title: fmt.Sprintf(P("SSD изношен на %d%%: %s", "SSD is %d%% worn: %s"), d.WearPct, d.Name),
				Detail: P("Ресурс подходит к концу — запланируйте замену и храните копии файлов.", "Nearing end of life — plan a replacement and keep backups."), Page: "overview"})
		}
	}
	if h.Battery.Present && h.Battery.WearPct >= 40 {
		add(CheckItem{ID: "battery", Level: 1, Title: fmt.Sprintf(P("Батарея изношена на %.0f%%", "Battery is %.0f%% worn"), h.Battery.WearPct),
			Detail: P("Ноутбук работает от батареи заметно меньше, чем новый.", "The laptop runs noticeably shorter on battery than when new.")})
	}
	wg.Wait()

	res := Checkup{Items: items, Elapsed: time.Since(t0).Milliseconds()}
	for _, it := range items {
		switch it.Level {
		case 2:
			res.Bad++
		case 1:
			res.Warn++
		}
	}
	sort.SliceStable(res.Items, func(i, j int) bool { return res.Items[i].Level > res.Items[j].Level })
	if len(res.Items) == 0 {
		res.Items = []CheckItem{{ID: "ok", Level: 0, Title: P("Всё в порядке", "Everything looks good"),
			Detail: P("Проблем не найдено: нагрузка в норме, защита включена, сбоев и мусора нет.", "No problems found: normal load, protection on, no crashes or junk.")}}
	}
	return res
}

func pageOr(p, def string) string {
	if p != "" {
		return p
	}
	return def
}

// QuickClean удаляет безопасный мусор: временные файлы, отчёты об ошибках, кэш эскизов.
func (c *Core) QuickClean() (diskspace.CleanResult, error) {
	var total diskspace.CleanResult
	for _, cat := range quickCats() {
		r, err := diskspace.Clean(cat)
		if err != nil {
			continue
		}
		total.Removed += r.Removed
		total.Freed += r.Freed
		total.Failed += r.Failed
	}
	msg := fmt.Sprintf(i18n.T("Быстрая очистка: удалено %d файлов, освобождено %s"), total.Removed, sys.Bytes(uint64(total.Freed)))
	c.Log(LvlOK, "%s", msg)
	c.Record("disk", msg, strings.Join(quickCats(), ", "), nil, i18n.Pick("Удалён только мусор (временные файлы, отчёты, эскизы) — восстанавливать нечего.", "Only junk was removed (temp files, reports, thumbnails) — nothing to restore."))
	return total, nil
}

// SetPower включает профиль питания и записывает изменение с возможностью отката.
func (c *Core) SetPower(p ctl.PowerProfile) error {
	prev, perr := ctl.CurrentPowerProfile()
	if err := ctl.SetPowerProfile(p); err != nil {
		c.Log(LvlErr, i18n.T("Профиль питания: %v"), err)
		return err
	}
	name := i18n.T(ctl.PowerNames[p])
	c.Log(LvlOK, i18n.T("Включён профиль питания: %s"), name)
	var u *history.Undo
	if perr == nil && prev != p {
		u = undoArgs("power", "profile", fmt.Sprint(int(prev)))
	}
	c.Record("power", i18n.Pick("Профиль питания: ", "Power profile: ")+name, "", u, "")
	return nil
}

// ToggleGameMode включает/выключает игровой режим и возвращает новое состояние.
func (c *Core) ToggleGameMode() bool {
	var on bool
	c.UpdateConfig(func(cfg *config.Config) { cfg.GameMode = !cfg.GameMode; on = cfg.GameMode })
	return on
}

// ---------- Обслуживание по расписанию ----------

func scheduleDue(s config.Schedule, now time.Time) bool {
	if !s.Enabled || len(s.Cats) == 0 {
		return false
	}
	if s.Day != 7 && int(now.Weekday()) != s.Day {
		return false
	}
	if now.Hour() < s.Hour {
		return false
	}
	return !strings.HasPrefix(s.LastRun, now.Format("2006-01-02"))
}

func (c *Core) scheduleLoop() {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		if scheduleDue(c.Config().Schedule, time.Now()) {
			c.RunSchedule()
		}
		<-t.C
	}
}

// RunSchedule выполняет запланированную очистку.
func (c *Core) RunSchedule() diskspace.CleanResult {
	cfg := c.Config()
	var total diskspace.CleanResult
	var done []string
	for _, cat := range cfg.Schedule.Cats {
		r, err := diskspace.Clean(cat)
		if err != nil {
			continue
		}
		total.Removed += r.Removed
		total.Freed += r.Freed
		total.Failed += r.Failed
		done = append(done, cat)
	}
	c.UpdateConfig(func(cf *config.Config) { cf.Schedule.LastRun = time.Now().Format("2006-01-02 15:04") })
	msg := fmt.Sprintf(i18n.T("Очистка по расписанию: освобождено %s (%d файлов)"), sys.Bytes(uint64(total.Freed)), total.Removed)
	c.Log(LvlOK, "%s", msg)
	prev := c.Source
	c.Source = "schedule"
	c.Record("disk", msg, strings.Join(done, ", "), nil, "")
	c.Source = prev
	if cfg.Notify && c.OnNotify != nil && total.Freed > 0 {
		c.OnNotify("HealCoreOps", msg)
	}
	return total
}

// LastBoot — время последней загрузки системы.
func LastBoot() time.Time {
	bt, _ := host.BootTime()
	return time.Unix(int64(bt), 0)
}
