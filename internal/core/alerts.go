package core

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/healcore/healcoreops/internal/config"
	"github.com/healcore/healcoreops/internal/diag"
	"github.com/healcore/healcoreops/internal/health"
	"github.com/healcore/healcoreops/internal/i18n"
	"github.com/healcore/healcoreops/internal/sys"
	"github.com/healcore/healcoreops/internal/sysadmin"
)

// Alert — активное оповещение.
type Alert struct {
	Key   string `json:"key"`
	Level int    `json:"level"` // 1 внимание, 2 плохо
	Title string `json:"title"`
	Text  string `json:"text"`
	Page  string `json:"page,omitempty"` // раздел, где разобраться
	Since int64  `json:"since"`          // unix-мс
}

type alertInput struct {
	disks      []sys.Disk
	health     health.Snapshot
	memHighFor time.Duration
	memPct     float64
	reboot     diag.Reboot
	services   map[string]string // имя → состояние (для наблюдаемых)
	svcNames   map[string]string // имя → отображаемое имя
}

// computeAlerts — какие пороги нарушены сейчас.
func computeAlerts(in alertInput, a config.Alerts) []Alert {
	var out []Alert
	add := func(key string, lvl int, page, title, text string) {
		out = append(out, Alert{Key: key, Level: lvl, Title: title, Text: text, Page: page})
	}
	if a.DiskFreePct > 0 {
		for _, d := range in.disks {
			if d.Total < 2<<30 { // флешки и служебные разделы не в счёт
				continue
			}
			free := 100 - d.Pct
			if free < float64(a.DiskFreePct) {
				lvl := 1
				if free < float64(a.DiskFreePct)/2 {
					lvl = 2
				}
				add("disk_free:"+d.Mount, lvl, "disk", fmt.Sprintf(i18n.T("Мало места на %s"), d.Mount),
					fmt.Sprintf(i18n.T("Свободно %.0f%% (%s). Освободите место в разделе «Место на диске»."), free, sys.Bytes(d.Total-d.Used)))
			}
		}
	}
	if a.DiskHealth || a.DiskTemp > 0 {
		for _, d := range in.health.Disks {
			if a.DiskHealth && (d.Reason == "health" || d.Reason == "errors" || d.Reason == "wear") && d.Level > 0 {
				var why string
				switch d.Reason {
				case "errors":
					why = fmt.Sprintf(i18n.T("неисправленные ошибки чтения/записи: %d"), d.ReadErr+d.WriteErr)
				case "wear":
					why = fmt.Sprintf(i18n.T("износ %d%%"), d.WearPct)
				default:
					why = fmt.Sprintf(i18n.T("состояние: %s"), d.Health)
				}
				add("disk_health:"+d.Name, d.Level, "hwhealth", fmt.Sprintf(i18n.T("Накопитель %s: проблемы"), d.Name),
					why+". "+i18n.T("Сделайте резервную копию важных файлов."))
			}
			if a.DiskTemp > 0 && d.TempC >= a.DiskTemp {
				add("disk_temp:"+d.Name, 1, "hwhealth", fmt.Sprintf(i18n.T("Накопитель %s перегревается"), d.Name),
					fmt.Sprintf(i18n.T("Температура %d °C (порог %d °C)."), d.TempC, a.DiskTemp))
			}
		}
	}
	if a.GPUTemp > 0 {
		for _, g := range in.health.GPUs {
			if g.TempC >= a.GPUTemp {
				add("gpu_temp:"+g.Name, 2, "hwhealth", fmt.Sprintf(i18n.T("Видеокарта перегревается: %d °C"), g.TempC),
					fmt.Sprintf(i18n.T("%s — порог %d °C. Проверьте охлаждение и пыль в корпусе."), g.Name, a.GPUTemp))
			}
		}
	}
	if a.Battery && in.health.Battery.Present && in.health.Battery.Level >= 2 {
		add("battery", 1, "hwhealth", i18n.T("Батарея сильно изношена"),
			fmt.Sprintf(i18n.T("Износ %.0f%% — ноутбук работает от батареи заметно меньше нового."), in.health.Battery.WearPct))
	}
	if a.MemPct > 0 && in.memPct >= float64(a.MemPct) && in.memHighFor >= 10*time.Minute {
		add("mem", 1, "optimize", i18n.T("Память почти заполнена"),
			fmt.Sprintf(i18n.T("Занято %.0f%% дольше %d мин."), in.memPct, int(in.memHighFor.Minutes())))
	}
	if a.Reboot && in.reboot.Pending {
		add("reboot", 1, "overview", i18n.T("Windows ждёт перезагрузки"), strings.Join(in.reboot.Reasons, " · "))
	}
	for _, name := range a.Services {
		st, ok := in.services[strings.ToLower(name)]
		disp := in.svcNames[strings.ToLower(name)]
		if disp == "" {
			disp = name
		}
		switch {
		case !ok:
			add("svc:"+strings.ToLower(name), 2, "services", fmt.Sprintf(i18n.T("Служба не найдена: %s"), disp), i18n.T("Служба из списка наблюдения отсутствует в системе."))
		case st != "running":
			add("svc:"+strings.ToLower(name), 2, "services", fmt.Sprintf(i18n.T("Служба остановлена: %s"), disp), fmt.Sprintf(i18n.T("Состояние: %s."), st))
		}
	}
	return out
}

// ---------- Цикл проверки ----------

type alertState struct {
	mu      sync.Mutex
	active  map[string]Alert
	memHigh time.Time
}

func (c *Core) memTrack(s *sys.Snapshot, cfg config.Config) {
	c.alerts.mu.Lock()
	defer c.alerts.mu.Unlock()
	if cfg.Alerts.MemPct > 0 && s.MemPct >= float64(cfg.Alerts.MemPct) {
		if c.alerts.memHigh.IsZero() {
			c.alerts.memHigh = time.Now()
		}
	} else {
		c.alerts.memHigh = time.Time{}
	}
}

// Alerts — активные оповещения.
func (c *Core) Alerts() []Alert {
	c.alerts.mu.Lock()
	defer c.alerts.mu.Unlock()
	out := make([]Alert, 0, len(c.alerts.active))
	for _, a := range c.alerts.active {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Level != out[j].Level {
			return out[i].Level > out[j].Level
		}
		return out[i].Since < out[j].Since
	})
	return out
}

// freshHealth — сведения о железе не старше 15 минут (иначе — собрать сейчас).
func freshHealth() health.Snapshot {
	h := health.Get()
	if !health.Supported() || (h.Ready && time.Since(time.UnixMilli(h.Updated)) < 15*time.Minute) {
		return h
	}
	return health.CollectNow()
}

// applyHealthBackground — фоновый опрос железа нужен, только если включены
// оповещения о дисках, видеокарте или батарее; тогда — раз в 5 минут.
func (c *Core) applyHealthBackground() {
	a := c.Config().Alerts
	if a.Enabled && (a.DiskHealth || a.DiskTemp > 0 || a.GPUTemp > 0 || a.Battery) {
		health.SetBackground(5 * time.Minute)
	} else {
		health.SetBackground(0)
	}
}

func (c *Core) alertLoop() {
	time.Sleep(90 * time.Second) // дать собраться сведениям о железе
	for {
		c.CheckAlerts()
		time.Sleep(2 * time.Minute)
	}
}

// CheckAlerts проверяет пороги сейчас и рассылает новые оповещения.
func (c *Core) CheckAlerts() []Alert {
	cfg := c.Config()
	if !cfg.Alerts.Enabled {
		c.alerts.mu.Lock()
		c.alerts.active = map[string]Alert{}
		c.alerts.mu.Unlock()
		return nil
	}
	in := alertInput{health: health.Get(), services: map[string]string{}, svcNames: map[string]string{}}
	if s := c.Snapshot(); s != nil {
		in.disks, in.memPct = s.Disks, s.MemPct
	}
	c.alerts.mu.Lock()
	if !c.alerts.memHigh.IsZero() {
		in.memHighFor = time.Since(c.alerts.memHigh)
	}
	c.alerts.mu.Unlock()
	if cfg.Alerts.Reboot && diag.Supported() {
		in.reboot = diag.PendingReboot()
	}
	if !sysadmin.Supported() {
		cfg.Alerts.Services = nil // проверить службы нельзя — не поднимаем ложных тревог
	} else if len(cfg.Alerts.Services) > 0 {
		if svcs, err := sysadmin.Services(); err == nil {
			for _, s := range svcs {
				in.services[strings.ToLower(s.Name)] = s.State
				in.svcNames[strings.ToLower(s.Name)] = s.Display
			}
		} else {
			cfg.Alerts.Services = nil // не удалось прочитать — не поднимаем ложных тревог
		}
	}
	now := time.Now().UnixMilli()
	cur := computeAlerts(in, cfg.Alerts)

	c.alerts.mu.Lock()
	if c.alerts.active == nil {
		c.alerts.active = map[string]Alert{}
	}
	var fresh []Alert
	seen := map[string]bool{}
	for _, a := range cur {
		seen[a.Key] = true
		if old, ok := c.alerts.active[a.Key]; ok {
			a.Since = old.Since
		} else {
			a.Since = now
			fresh = append(fresh, a)
		}
		c.alerts.active[a.Key] = a
	}
	var cleared []Alert
	for k, a := range c.alerts.active {
		if !seen[k] {
			cleared = append(cleared, a)
			delete(c.alerts.active, k)
		}
	}
	c.alerts.mu.Unlock()

	for _, a := range fresh {
		c.Log(LvlWarn, "%s: %s", a.Title, a.Text)
		writeAlertEvent(a.Title, a.Text, a.Level)
		if c.OnNotify != nil {
			c.OnNotify(a.Title, a.Text)
		}
	}
	for _, a := range cleared {
		c.Log(LvlInfo, i18n.T("Оповещение снято: %s"), a.Title)
	}
	return c.Alerts()
}
