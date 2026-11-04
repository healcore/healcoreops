// Package core — состояние приложения: мониторинг, правила, автооптимизация, журнал.
package core

import (
	"fmt"
	"runtime"
	"runtime/debug"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v4/mem"

	"github.com/healcore/healcoreops/internal/config"
	"github.com/healcore/healcoreops/internal/ctl"
	"github.com/healcore/healcoreops/internal/health"
	"github.com/healcore/healcoreops/internal/i18n"
	"github.com/healcore/healcoreops/internal/opt"
	"github.com/healcore/healcoreops/internal/rules"
	"github.com/healcore/healcoreops/internal/sys"
)

// Version — версия приложения.
const Version = "1.8.2"

// Уровни записей журнала.
const (
	LvlInfo = iota
	LvlOK
	LvlWarn
	LvlErr
)

// LogEntry — запись журнала.
type LogEntry struct {
	ID   int64  `json:"id"`
	Time string `json:"time"`
	Lvl  int    `json:"lvl"`
	Msg  string `json:"msg"`
}

// Core — общее состояние, разделяемое HTTP-обработчиками и фоновым циклом.
type Core struct {
	mu        sync.Mutex
	cfg       *config.Config
	snap      *sys.Snapshot
	lastTrim  time.Time
	tempScan  *opt.TempScan
	tempBytes int64

	col        *sys.Collector
	eng        *rules.Engine
	intervalCh chan time.Duration

	logMu  sync.Mutex
	logs   []LogEntry
	nextID int64

	// OnTheme вызывается при смене темы (окно Windows меняет цвет заголовка).
	OnTheme func(dark bool)
	// OnQuit закрывает приложение (после перезапуска с правами администратора).
	OnQuit func()
	// OnNotify показывает системное уведомление (Windows — всплывающее окно из трея).
	OnNotify func(title, text string)
	// OnOpacity меняет прозрачность окна (40–100 %).
	OnOpacity func(pct int)
	// OnAcrylic включает/выключает размытие фона окна.
	OnAcrylic func(on bool)

	notify notifyState
	gmu    sync.Mutex // защищает game
	game   gameState
	update *UpdateInfo

	// Source — кто вносит изменения: ui (окно), cli (командная строка), schedule, policy.
	Source string
	// Quiet — не писать в журнал Windows и не показывать уведомления (режим командной строки).
	Quiet bool
}

// New загружает настройки и готовит сборщик метрик.
func New() *Core {
	cfg := config.Load()
	i18n.Set(cfg.Lang)
	c := &Core{
		cfg:        cfg,
		col:        sys.NewCollector(),
		eng:        rules.NewEngine(),
		intervalCh: make(chan time.Duration, 1),
	}
	c.eng.SetRules(cfg.Rules)
	return c
}

// Start запускает фоновый мониторинг.
func (c *Core) Start() {
	c.Log(LvlInfo, i18n.T("HealCoreOps %s запущен. Настройки: %s"), Version, config.Path())
	if !ctl.IsElevated() {
		c.Log(LvlWarn, "%s", i18n.T("Приложение запущено без прав администратора — часть действий будет недоступна"))
	}
	go c.loop()
	go c.autoUpdateCheck()
	go c.scheduleLoop()
	go c.applyEnforcedProfile()
	if health.Supported() {
		health.Start()
	}
}

// Prime делает снимок системы без фонового цикла (командная строка).
func (c *Core) Prime() {
	c.col.Collect()
	time.Sleep(time.Second) // загрузка ЦП считается по двум замерам
	s := c.col.Collect()
	c.mu.Lock()
	c.snap = s
	c.mu.Unlock()
}

// Config возвращает копию настроек.
func (c *Core) Config() config.Config {
	c.mu.Lock()
	defer c.mu.Unlock()
	cp := *c.cfg
	cp.Rules = append([]rules.Rule(nil), c.cfg.Rules...)
	return cp
}

// UpdateConfig изменяет настройки и сохраняет их на диск.
func (c *Core) UpdateConfig(fn func(*config.Config)) {
	c.mu.Lock()
	oldInterval, oldTheme := c.cfg.IntervalSec, c.cfg.Theme
	fn(c.cfg)
	cp := *c.cfg
	c.mu.Unlock()
	i18n.Set(cp.Lang)
	if cp.IntervalSec != oldInterval {
		select {
		case c.intervalCh <- time.Duration(cp.IntervalSec) * time.Second:
		default:
		}
	}
	if cp.Theme != oldTheme && c.OnTheme != nil {
		c.OnTheme(cp.Theme == "dark")
	}
	if err := cp.Save(); err != nil {
		c.Log(LvlErr, i18n.T("Не удалось сохранить настройки: %v"), err)
	}
}

// Snapshot возвращает последний снимок системы.
func (c *Core) Snapshot() *sys.Snapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.snap
}

func (c *Core) loop() {
	c.tick()
	t := time.NewTicker(time.Duration(c.Config().IntervalSec) * time.Second)
	defer t.Stop()
	for {
		select {
		case <-t.C:
		case d := <-c.intervalCh:
			t.Reset(d)
			continue
		}
		c.tick()
	}
}

func (c *Core) tick() {
	s := c.col.Collect()
	c.mu.Lock()
	c.snap = s
	cfg := *c.cfg
	needTrim := cfg.AutoTrim && s.MemPct >= float64(cfg.TrimThreshold) && time.Since(c.lastTrim) > 3*time.Minute
	if needTrim {
		c.lastTrim = time.Now()
	}
	c.mu.Unlock()

	if cfg.AutoRules {
		c.logEvents(c.eng.Apply(s.Procs, false))
	}
	if needTrim {
		c.Log(LvlInfo, i18n.T("Память занята на %.0f%% (порог %d%%) — автоосвобождение"), s.MemPct, cfg.TrimThreshold)
		go c.TrimMemory()
	}
	c.gameTick(s, cfg)
	c.notifyTick(s, cfg)
}

func (c *Core) logEvents(evs []rules.Event) {
	for _, ev := range evs {
		if ev.OK {
			c.Log(LvlOK, "%s", ev.Msg)
		} else {
			c.Log(LvlErr, "%s", ev.Msg)
		}
	}
}

// Log добавляет запись в журнал.
func (c *Core) Log(lvl int, format string, args ...any) {
	c.logMu.Lock()
	defer c.logMu.Unlock()
	c.nextID++
	now := time.Now()
	msg := fmt.Sprintf(format, args...)
	c.logs = append(c.logs, LogEntry{ID: c.nextID, Time: now.Format("15:04:05"), Lvl: lvl, Msg: msg})
	flog.write(now, lvl, msg)
	if len(c.logs) > 1000 {
		c.logs = c.logs[len(c.logs)-1000:]
	}
}

// LogSince возвращает записи с ID больше since.
func (c *Core) LogSince(since int64) []LogEntry {
	c.logMu.Lock()
	defer c.logMu.Unlock()
	var out []LogEntry
	for _, e := range c.logs {
		if e.ID > since {
			out = append(out, e)
		}
	}
	return out
}

// ClearLog очищает журнал.
func (c *Core) ClearLog() {
	c.logMu.Lock()
	c.logs = nil
	c.logMu.Unlock()
}

// TrimMemory освобождает оперативную память и пишет результат в журнал.
func (c *Core) TrimMemory() (string, error) {
	c.mu.Lock()
	c.lastTrim = time.Now()
	c.mu.Unlock()
	before, _ := mem.VirtualMemory()
	res, err := ctl.TrimMemory()
	runtime.GC()
	debug.FreeOSMemory()
	if err != nil {
		c.Log(LvlErr, i18n.T("Освобождение памяти: %v"), err)
		return "", err
	}
	time.Sleep(800 * time.Millisecond)
	after, _ := mem.VirtualMemory()
	freed := sys.Bytes(0)
	if before != nil && after != nil && after.Available > before.Available {
		freed = sys.Bytes(after.Available - before.Available)
	}
	msg := fmt.Sprintf(i18n.T("Память освобождена: %s, высвобождено %s"), res.Note, freed)
	if res.Processes > 0 {
		msg += fmt.Sprintf(i18n.T(" (процессов: %d)"), res.Processes)
	}
	c.Log(LvlOK, "%s", msg)
	return msg, nil
}

// SetRules сохраняет и активирует правила.
func (c *Core) SetRules(rs []rules.Rule) {
	c.UpdateConfig(func(cfg *config.Config) { cfg.Rules = rs })
	c.eng.SetRules(rs)
}

// ApplyRules применяет правила ко всем текущим процессам.
func (c *Core) ApplyRules() int {
	s := c.Snapshot()
	if s == nil {
		return 0
	}
	evs := c.eng.Apply(s.Procs, true)
	if len(evs) == 0 {
		c.Log(LvlInfo, "%s", i18n.T("Правила применены: подходящих процессов не найдено"))
	}
	c.logEvents(evs)
	return len(evs)
}

// Analysis — индекс состояния и рекомендации для текущего снимка.
func (c *Core) Analysis(s *sys.Snapshot) (int, []opt.Recommendation) {
	c.mu.Lock()
	tb := c.tempBytes
	c.mu.Unlock()
	return opt.Analyze(s, tb)
}

// ScanTemp ищет устаревшие временные файлы.
func (c *Core) ScanTemp() opt.TempScan {
	days := c.Config().TempAgeDays
	res := opt.ScanTemp(time.Duration(days) * 24 * time.Hour)
	c.mu.Lock()
	c.tempScan, c.tempBytes = &res, res.Bytes
	c.mu.Unlock()
	c.Log(LvlInfo, i18n.T("Временные файлы: найдено %d, %s"), len(res.Files), sys.Bytes(uint64(res.Bytes)))
	return res
}

// CleanTemp удаляет найденные ранее временные файлы.
func (c *Core) CleanTemp() (int, int64, int) {
	c.mu.Lock()
	ts := c.tempScan
	c.tempScan, c.tempBytes = nil, 0
	c.mu.Unlock()
	if ts == nil {
		return 0, 0, 0
	}
	n, freed, failed := ts.Clean()
	c.Log(LvlOK, i18n.T("Очистка: удалено %d файлов, освобождено %s, пропущено %d"), n, sys.Bytes(uint64(freed)), failed)
	return n, freed, failed
}
