package core

import (
	"fmt"
	"runtime"
	"time"

	"github.com/healcore/healcoreops/internal/config"
	"github.com/healcore/healcoreops/internal/ctl"
	"github.com/healcore/healcoreops/internal/i18n"
	"github.com/healcore/healcoreops/internal/opt"
	"github.com/healcore/healcoreops/internal/rules"
	"github.com/healcore/healcoreops/internal/sys"
)

// ---------- Уведомления ----------

type notifyState struct {
	lastMem  time.Time
	hogSince map[string]time.Time // ключ pid:create → когда начал грузить ЦП
	hogDone  map[string]bool
}

const (
	hogNotifyAfter = 5 * time.Minute
	hogCorePct     = 80.0 // % одного ядра
	memNotifyPct   = 90.0
	memNotifyEvery = 15 * time.Minute
)

func (c *Core) notifyTick(s *sys.Snapshot, cfg config.Config) {
	if !cfg.Notify || c.OnNotify == nil {
		return
	}
	n := &c.notify
	if n.hogSince == nil {
		n.hogSince, n.hogDone = map[string]time.Time{}, map[string]bool{}
	}
	now := time.Now()

	if s.MemPct >= memNotifyPct && now.Sub(n.lastMem) > memNotifyEvery {
		n.lastMem = now
		c.OnNotify(i18n.T("Мало свободной памяти"),
			fmt.Sprintf(i18n.T("Память занята на %.0f%%. Откройте HealCoreOps → Оптимизация, чтобы освободить её."), s.MemPct))
	}

	ncpu := float64(runtime.NumCPU())
	seen := map[string]bool{}
	for _, p := range s.Procs {
		if p.PID == c.col.Self() || opt.IsProtected(p.Name) || p.CPU*ncpu < hogCorePct {
			continue
		}
		key := fmt.Sprintf("%d:%d", p.PID, p.Create)
		seen[key] = true
		since, ok := n.hogSince[key]
		if !ok {
			n.hogSince[key] = now
			continue
		}
		if !n.hogDone[key] && now.Sub(since) >= hogNotifyAfter {
			n.hogDone[key] = true
			c.OnNotify(i18n.T("Процесс нагружает процессор"),
				fmt.Sprintf(i18n.T("%s грузит ЦП уже %d мин (%.0f%%)."), p.Name, int(now.Sub(since).Minutes()), p.CPU))
		}
	}
	for k := range n.hogSince {
		if !seen[k] {
			delete(n.hogSince, k)
			delete(n.hogDone, k)
		}
	}
}

// ---------- Игровой режим ----------

type gameState struct {
	active      bool
	pid         int32
	name        string
	prevPower   ctl.PowerProfile
	prevPowerOK bool
	throttled   map[int32]ctl.Priority // процесс → прежний приоритет
}

// gameThrottlePct — фоновые процессы с нагрузкой выше этого (% одного ядра)
// во время игры получают приоритет «Ниже среднего».
const gameThrottlePct = 15.0

func findGame(procs []sys.Proc, games []string) (sys.Proc, bool) {
	for _, g := range games {
		r := rules.Rule{Pattern: g}
		for _, p := range procs {
			if r.Matches(p.Name) {
				return p, true
			}
		}
	}
	return sys.Proc{}, false
}

func (c *Core) gameTick(s *sys.Snapshot, cfg config.Config) {
	c.gmu.Lock()
	defer c.gmu.Unlock()
	g := &c.game
	game, running := findGame(s.Procs, cfg.Games)
	if !cfg.GameMode {
		running = false
	}

	switch {
	case running && !g.active:
		c.gameEnter(game, s)
	case running && g.active:
		if game.PID != g.pid { // перезапуск игры — переприменяем
			c.gameExit()
			c.gameEnter(game, s)
			return
		}
		c.throttleBackground(s, game.PID) // новые фоновые нагрузчики
	case !running && g.active:
		c.gameExit()
	}
}

func (c *Core) gameEnter(game sys.Proc, s *sys.Snapshot) {
	g := &c.game
	*g = gameState{active: true, pid: game.PID, name: game.Name, throttled: map[int32]ctl.Priority{}}

	if err := ctl.SetPriority(game.PID, ctl.PrioHigh); err != nil {
		c.Log(LvlWarn, i18n.T("Игровой режим: приоритет %s: %v"), game.Name, err)
	}
	if p, err := ctl.CurrentPowerProfile(); err == nil {
		g.prevPower, g.prevPowerOK = p, true
	}
	if !g.prevPowerOK || g.prevPower != ctl.PowerPerformance {
		if err := ctl.SetPowerProfile(ctl.PowerPerformance); err != nil {
			c.Log(LvlWarn, i18n.T("Игровой режим: профиль питания: %v"), err)
			g.prevPowerOK = false
		}
	} else {
		g.prevPowerOK = false // и так «Производительность» — восстанавливать нечего
	}
	c.throttleBackground(s, game.PID)
	go c.TrimMemory()

	msg := fmt.Sprintf(i18n.T("Игровой режим включён: %s. Приоритет — высокий, питание — «Производительность», фоновых процессов приторможено: %d"),
		game.Name, len(g.throttled))
	c.Log(LvlOK, "%s", msg)
	if c.OnNotify != nil && c.Config().Notify {
		c.OnNotify(i18n.T("Игровой режим"), msg)
	}
}

func (c *Core) throttleBackground(s *sys.Snapshot, gamePID int32) {
	g := &c.game
	for _, p := range opt.FindHogs(s.Procs, gameThrottlePct, c.col.Self()) {
		if p.PID == gamePID {
			continue
		}
		if _, done := g.throttled[p.PID]; done {
			continue
		}
		if err := ctl.SetPriority(p.PID, ctl.PrioBelowNormal); err == nil {
			g.throttled[p.PID] = p.Priority
		}
	}
}

func (c *Core) gameExit() {
	g := &c.game
	if !g.active {
		return
	}
	restored := 0
	for pid, prev := range g.throttled {
		if ctl.SetPriority(pid, prev) == nil {
			restored++
		}
	}
	if g.prevPowerOK {
		_ = ctl.SetPowerProfile(g.prevPower)
	}
	c.Log(LvlInfo, i18n.T("Игровой режим выключен (%s закрыта): восстановлено процессов %d"), g.name, restored)
	*g = gameState{}
}

// GameStatus — состояние игрового режима для интерфейса.
func (c *Core) GameStatus() map[string]any {
	c.gmu.Lock()
	defer c.gmu.Unlock()
	return map[string]any{"active": c.game.active, "name": c.game.name, "throttled": len(c.game.throttled)}
}
