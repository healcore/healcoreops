package core

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/healcore/healcoreops/internal/autostart"
	"github.com/healcore/healcoreops/internal/config"
	"github.com/healcore/healcoreops/internal/ctl"
	"github.com/healcore/healcoreops/internal/diag"
	"github.com/healcore/healcoreops/internal/diskspace"
	"github.com/healcore/healcoreops/internal/drivers"
	"github.com/healcore/healcoreops/internal/health"
	"github.com/healcore/healcoreops/internal/i18n"
	"github.com/healcore/healcoreops/internal/maintenance"
	"github.com/healcore/healcoreops/internal/opt"
	"github.com/healcore/healcoreops/internal/policy"
	"github.com/healcore/healcoreops/internal/programs"
	"github.com/healcore/healcoreops/internal/reghealer"
	"github.com/healcore/healcoreops/internal/rules"
	"github.com/healcore/healcoreops/internal/sys"
	"github.com/healcore/healcoreops/internal/sysadmin"
	"github.com/healcore/healcoreops/internal/tweaks"
)

// HealCoreURL — ссылка на проект; открывается по кнопке внизу окна.
const HealCoreURL = "https://github.com/healcore"

type procDTO struct {
	PID    int32   `json:"pid"`
	Name   string  `json:"name"`
	User   string  `json:"user"`
	CPU    float64 `json:"cpu"`
	RSS    uint64  `json:"rss"`
	Thr    int32   `json:"thr"`
	Prio   int     `json:"prio"`
	Create int64   `json:"create"`
}

type diskDTO struct {
	Mount string  `json:"mount"`
	Total uint64  `json:"total"`
	Used  uint64  `json:"used"`
	Pct   float64 `json:"pct"`
}

type snapDTO struct {
	Time        int64                `json:"time"`
	CPU         float64              `json:"cpu"`
	Cores       []float64            `json:"cores"`
	MemTotal    uint64               `json:"memTotal"`
	MemUsed     uint64               `json:"memUsed"`
	MemAvail    uint64               `json:"memAvail"`
	MemPct      float64              `json:"memPct"`
	SwapTotal   uint64               `json:"swapTotal"`
	SwapUsed    uint64               `json:"swapUsed"`
	SwapPct     float64              `json:"swapPct"`
	NetRx       float64              `json:"netRx"`
	NetTx       float64              `json:"netTx"`
	DiskR       float64              `json:"diskR"`
	DiskW       float64              `json:"diskW"`
	Disks       []diskDTO            `json:"disks"`
	Uptime      uint64               `json:"uptime"`
	Load        []float64            `json:"load"`
	Threads     int64                `json:"threads"`
	Procs       []procDTO            `json:"procs"`
	Score       int                  `json:"score"`
	Recs        []opt.Recommendation `json:"recs"`
	RulesActive int                  `json:"rulesActive"`
}

func (c *Core) snapDTO(s *sys.Snapshot) snapDTO {
	d := snapDTO{
		Time: s.Time.UnixMilli(), CPU: s.CPU, Cores: s.Cores,
		MemTotal: s.MemTotal, MemUsed: s.MemUsed, MemAvail: s.MemAvail, MemPct: s.MemPct,
		SwapTotal: s.SwapTotal, SwapUsed: s.SwapUsed, SwapPct: s.SwapPct,
		NetRx: s.NetRx, NetTx: s.NetTx, DiskR: s.DiskR, DiskW: s.DiskW,
		Uptime: s.Uptime, Threads: s.Threads, RulesActive: c.eng.Active(),
		Disks: []diskDTO{}, Procs: make([]procDTO, 0, len(s.Procs)),
	}
	if s.HasLoad {
		d.Load = s.Load[:]
	}
	for _, x := range s.Disks {
		d.Disks = append(d.Disks, diskDTO{x.Mount, x.Total, x.Used, x.Pct})
	}
	for _, p := range s.Procs {
		prio := -1
		if p.PrioOK {
			prio = int(p.Priority)
		}
		d.Procs = append(d.Procs, procDTO{p.PID, p.Name, p.User, p.CPU, p.RSS, p.Threads, prio, p.Create})
	}
	d.Score, d.Recs = c.Analysis(s)
	if d.Recs == nil {
		d.Recs = []opt.Recommendation{}
	}
	return d
}

// Handler возвращает HTTP-обработчик: статические файлы интерфейса и API.
// Запросы к API принимаются только с токеном и только с локального адреса.
func (c *Core) Handler(static fs.FS, token string) http.Handler {
	api := http.NewServeMux()
	c.routes(api)

	files := http.FileServer(http.FS(static))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.Host)
		if err != nil || (host != "127.0.0.1" && host != "localhost") {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			if subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Token")), []byte(token)) != 1 {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			if policy.Get().Blocks(policy.RuleFor(r.Method, r.URL.Path)) {
				writeErr(w, errPolicy())
				return
			}
			api.ServeHTTP(w, r)
			return
		}
		files.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, err error) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusUnprocessableEntity)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}

func readJSON(r *http.Request, v any) error {
	return json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20)).Decode(v)
}

type pidReq struct {
	PID   int32 `json:"pid"`
	Prio  int   `json:"prio"`
	Cores []int `json:"cores"`
}

func (c *Core) procName(pid int32) string {
	if s := c.Snapshot(); s != nil {
		for _, p := range s.Procs {
			if p.PID == pid {
				return p.Name
			}
		}
	}
	return "?"
}

func (c *Core) routes(m *http.ServeMux) {
	m.HandleFunc("GET /api/host", func(w http.ResponseWriter, r *http.Request) {
		h := c.col.Host()
		writeJSON(w, map[string]any{
			"version": Version, "hostname": h.Hostname, "os": h.OS, "cpu": h.CPUModel,
			"logical": h.Logical, "physical": h.Physical, "memTotal": h.MemTotal,
			"elevated": ctl.IsElevated(), "goos": runtime.GOOS, "affinity": ctl.AffinitySupported(),
			"tweaks": tweaks.Supported(), "canElevate": runtime.GOOS == "windows" && !ctl.IsElevated(),
			"autostart": autostart.Supported(), "maintenance": maintenance.Supported(), "health": health.Supported(),
			"drivers": drivers.Supported(), "reghealer": reghealer.Supported(),
			"programs": programs.Supported(), "sysadmin": sysadmin.Supported(), "diag": diag.Supported(),
			"disk": diskspace.Supported(), "policy": policy.Get(), "logDir": LogDir(), "reportsDir": ReportsDir(),
			"configPath": config.Path(), "self": c.col.Self(),
		})
	})
	c.routes2(m)
	c.routes3(m)

	// ---- Автозагрузка ----
	m.HandleFunc("GET /api/autostart", func(w http.ResponseWriter, r *http.Request) {
		items, err := autostart.List()
		if err != nil {
			writeErr(w, err)
			return
		}
		if items == nil {
			items = []autostart.Item{}
		}
		writeJSON(w, items)
	})
	m.HandleFunc("POST /api/autostart", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			ID   string `json:"id"`
			On   bool   `json:"on"`
			Name string `json:"name"`
		}
		if err := readJSON(r, &in); err != nil {
			writeErr(w, err)
			return
		}
		if err := autostart.SetEnabled(in.ID, in.On); err != nil {
			c.Log(LvlErr, i18n.T("Автозагрузка: %v"), err)
			writeErr(w, err)
			return
		}
		state := i18n.T("включена")
		if !in.On {
			state = i18n.T("отключена")
		}
		c.Log(LvlOK, i18n.T("Автозагрузка %s"), state)
		title := in.Name
		if title == "" {
			title = in.ID
		}
		c.Record("autostart", fmt.Sprintf(i18n.Pick("Автозагрузка «%s» %s", "Startup “%s” %s"), title, state), "", undoArgs("autostart", "id", in.ID, "on", b2s(!in.On)), "")
		writeJSON(w, true)
	})

	// ---- Обслуживание ----
	m.HandleFunc("GET /api/maintenance/actions", func(w http.ResponseWriter, r *http.Request) {
		a := maintenance.Actions()
		if a == nil {
			a = []maintenance.Action{}
		}
		writeJSON(w, a)
	})
	m.HandleFunc("GET /api/maintenance/job", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, maintenance.Status())
	})
	m.HandleFunc("POST /api/maintenance/run", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			ID string `json:"id"`
		}
		if err := readJSON(r, &in); err != nil {
			writeErr(w, err)
			return
		}
		c.Log(LvlInfo, i18n.T("Обслуживание: запуск «%s»"), in.ID)
		if err := maintenance.Run(in.ID); err != nil {
			writeErr(w, err)
			return
		}
		title := in.ID
		for _, a := range maintenance.Actions() {
			if a.ID == in.ID {
				title = a.Title
			}
		}
		note := ""
		if strings.HasPrefix(in.ID, "dns_") {
			note = i18n.Pick("Вернуть DNS: «DNS: вернуть автоматический» в «Обслуживании».", "To revert: “DNS: back to automatic” in Maintenance.")
		}
		c.Record("maintenance", i18n.Pick("Обслуживание: ", "Maintenance: ")+title, in.ID, nil, note)
		writeJSON(w, true)
	})

	m.HandleFunc("POST /api/open-url", func(w http.ResponseWriter, r *http.Request) {
		if err := ctl.OpenURL(HealCoreURL); err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, true)
	})

	// ---- Здоровье железа ----
	m.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, health.Get()) })

	// ---- Драйверы ----
	m.HandleFunc("GET /api/drivers", func(w http.ResponseWriter, r *http.Request) {
		devs, err := drivers.List()
		if err != nil {
			writeErr(w, err)
			return
		}
		if devs == nil {
			devs = []drivers.Device{}
		}
		writeJSON(w, devs)
	})
	m.HandleFunc("GET /api/drivers/updates", func(w http.ResponseWriter, r *http.Request) {
		av, checked := drivers.Avails()
		writeJSON(w, map[string]any{"job": drivers.Status(), "avail": av, "checked": checked})
	})
	m.HandleFunc("POST /api/drivers/check", func(w http.ResponseWriter, r *http.Request) {
		if err := drivers.Check(!c.Config().DriverNoWU); err != nil {
			writeErr(w, err)
			return
		}
		c.Log(LvlInfo, "%s", i18n.Pick("Проверка обновлений драйверов", "Checking for driver updates"))
		writeJSON(w, true)
	})
	m.HandleFunc("POST /api/drivers/update", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			IDs   []string `json:"ids"`
			All   bool     `json:"all"`
			Names []string `json:"names"`
		}
		if err := readJSON(r, &in); err != nil {
			writeErr(w, err)
			return
		}
		if len(in.IDs) == 0 && !in.All {
			writeErr(w, errors.New(i18n.T("не выбрано ни одного обновления")))
			return
		}
		if err := drivers.Update(in.IDs, in.All, !c.Config().DriverNoWU); err != nil {
			writeErr(w, err)
			return
		}
		what := strings.Join(in.Names, ", ")
		if in.All {
			what = i18n.Pick("все устройства с обновлениями", "all devices with updates")
		}
		c.Log(LvlInfo, i18n.T("Установка драйвера: %s"), what)
		c.Record("drivers", i18n.Pick("Обновление драйверов: ", "Driver update: ")+what, "", nil,
			i18n.Pick("Откат драйвера: Диспетчер устройств → Свойства → «Откатить».", "To roll back: Device Manager → Properties → Roll Back Driver."))
		writeJSON(w, true)
	})
	m.HandleFunc("POST /api/drivers/site", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			URL string `json:"url"`
		}
		if err := readJSON(r, &in); err != nil {
			writeErr(w, err)
			return
		}
		if err := drivers.OpenVendor(in.URL); err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, true)
	})
	m.HandleFunc("GET /api/sysinfo", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, drivers.GetSysInfo()) })

	// ---- Registry healer ----
	m.HandleFunc("GET /api/reg/scan", func(w http.ResponseWriter, r *http.Request) {
		issues, err := reghealer.Scan()
		if err != nil {
			writeErr(w, err)
			return
		}
		if issues == nil {
			issues = []reghealer.Issue{}
		}
		writeJSON(w, issues)
	})
	m.HandleFunc("POST /api/reg/fix", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			IDs []string `json:"ids"`
		}
		if err := readJSON(r, &in); err != nil {
			writeErr(w, err)
			return
		}
		rep, err := reghealer.Fix(in.IDs)
		if err != nil {
			writeErr(w, err)
			return
		}
		c.Log(LvlOK, i18n.T("Реестр: удалено %d записей, резервная копия: %s"), rep.Removed, rep.Backup)
		if rep.Removed > 0 {
			c.Record("registry", fmt.Sprintf(i18n.Pick("Реестр: удалено висячих записей %d", "Registry: removed %d orphaned entries"), rep.Removed), rep.Backup, undoArgs("reg_import", "dir", rep.Backup), "")
		}
		writeJSON(w, rep)
	})

	// ---- Игровой режим ----
	m.HandleFunc("GET /api/gamestatus", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, c.GameStatus()) })

	// ---- Обновления ----
	m.HandleFunc("GET /api/update", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		info, err := c.CheckUpdate(ctx)
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, info)
	})
	m.HandleFunc("POST /api/update/open", func(w http.ResponseWriter, r *http.Request) {
		page := ReleasesPage
		if u := c.PendingUpdate(); u != nil && u.Page != "" {
			page = u.Page
		}
		if err := ctl.OpenURL(page); err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, true)
	})
	m.HandleFunc("POST /api/update/skip", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Version string `json:"version"`
		}
		_ = readJSON(r, &in)
		c.UpdateConfig(func(cfg *config.Config) { cfg.SkipVersion = in.Version })
		c.mu.Lock()
		c.update = nil
		c.mu.Unlock()
		writeJSON(w, true)
	})

	// ---- Профиль (экспорт/импорт настроек) ----
	m.HandleFunc("GET /api/profile/export", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="HealCoreOps-profile.json"`)
		_ = json.NewEncoder(w).Encode(c.ExportProfile())
	})
	m.HandleFunc("POST /api/profile/import", func(w http.ResponseWriter, r *http.Request) {
		var p Profile
		if err := readJSON(r, &p); err != nil {
			writeErr(w, errors.New(i18n.T("это не файл профиля HealCoreOps")))
			return
		}
		rep, err := c.ImportProfile(p)
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, rep)
	})

	m.HandleFunc("GET /api/snapshot", func(w http.ResponseWriter, r *http.Request) {
		s := c.Snapshot()
		for i := 0; s == nil && i < 50; i++ {
			time.Sleep(100 * time.Millisecond)
			s = c.Snapshot()
		}
		if s == nil {
			writeErr(w, errors.New("no data yet"))
			return
		}
		writeJSON(w, c.snapDTO(s))
	})

	m.HandleFunc("GET /api/config", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, c.Config()) })
	m.HandleFunc("POST /api/config", func(w http.ResponseWriter, r *http.Request) {
		var in map[string]json.RawMessage
		if err := readJSON(r, &in); err != nil {
			writeErr(w, err)
			return
		}
		if policy.Get().LockSettings {
			for k := range in { // при блокировке можно менять только внешний вид
				if k != "lang" && k != "theme" {
					delete(in, k)
				}
			}
		}
		c.UpdateConfig(func(cfg *config.Config) {
			for k, v := range in {
				switch k {
				case "lang":
					_ = json.Unmarshal(v, &cfg.Lang)
				case "theme":
					_ = json.Unmarshal(v, &cfg.Theme)
				case "interval_sec":
					_ = json.Unmarshal(v, &cfg.IntervalSec)
				case "auto_rules":
					_ = json.Unmarshal(v, &cfg.AutoRules)
				case "auto_trim":
					_ = json.Unmarshal(v, &cfg.AutoTrim)
				case "trim_threshold":
					_ = json.Unmarshal(v, &cfg.TrimThreshold)
				case "temp_age_days":
					_ = json.Unmarshal(v, &cfg.TempAgeDays)
				case "hog_threshold":
					_ = json.Unmarshal(v, &cfg.HogThreshold)
				case "notify":
					_ = json.Unmarshal(v, &cfg.Notify)
				case "game_mode":
					_ = json.Unmarshal(v, &cfg.GameMode)
				case "games":
					_ = json.Unmarshal(v, &cfg.Games)
				case "auto_update_check":
					_ = json.Unmarshal(v, &cfg.AutoUpdateCheck)
				case "close_to_tray":
					_ = json.Unmarshal(v, &cfg.CloseToTray)
				case "run_at_startup":
					_ = json.Unmarshal(v, &cfg.RunAtStartup)
				case "alerts":
					_ = json.Unmarshal(v, &cfg.Alerts)
				case "load_history_days":
					_ = json.Unmarshal(v, &cfg.LoadHistoryDays)
				case "driver_no_wu":
					_ = json.Unmarshal(v, &cfg.DriverNoWU)
				case "schedule":
					last := cfg.Schedule.LastRun
					_ = json.Unmarshal(v, &cfg.Schedule)
					cfg.Schedule.LastRun = last
				}
			}
			cfg.Normalize()
		})
		cfg := c.Config()
		if _, ok := in["run_at_startup"]; ok {
			if err := autostart.SetSelfStartup(cfg.RunAtStartup); err != nil {
				c.Log(LvlErr, i18n.T("Автозапуск: %v"), err)
			}
		}
		writeJSON(w, cfg)
	})

	m.HandleFunc("GET /api/log", func(w http.ResponseWriter, r *http.Request) {
		since, _ := strconv.ParseInt(r.URL.Query().Get("since"), 10, 64)
		out := c.LogSince(since)
		if out == nil {
			out = []LogEntry{}
		}
		writeJSON(w, out)
	})
	m.HandleFunc("POST /api/log/clear", func(w http.ResponseWriter, r *http.Request) { c.ClearLog(); writeJSON(w, true) })

	// ---- Процессы ----
	procAction := func(path, what string, fn func(int32) error) {
		m.HandleFunc("POST "+path, func(w http.ResponseWriter, r *http.Request) {
			var in pidReq
			if err := readJSON(r, &in); err != nil {
				writeErr(w, err)
				return
			}
			name := c.procName(in.PID)
			if err := fn(in.PID); err != nil {
				c.Log(LvlErr, "%s %s (PID %d): %v", i18n.T(what), name, in.PID, err)
				writeErr(w, err)
				return
			}
			c.Log(LvlOK, i18n.T("%s %s (PID %d) — выполнено"), i18n.T(what), name, in.PID)
			writeJSON(w, true)
		})
	}
	procAction("/api/proc/kill", "Завершение", ctl.Kill)
	procAction("/api/proc/suspend", "Приостановка", ctl.Suspend)
	procAction("/api/proc/resume", "Возобновление", ctl.Resume)

	m.HandleFunc("POST /api/proc/priority", func(w http.ResponseWriter, r *http.Request) {
		var in pidReq
		if err := readJSON(r, &in); err != nil {
			writeErr(w, err)
			return
		}
		name := c.procName(in.PID)
		if err := ctl.SetPriority(in.PID, ctl.Priority(in.Prio)); err != nil {
			c.Log(LvlErr, i18n.T("Приоритет %s (PID %d): %v"), name, in.PID, err)
			writeErr(w, err)
			return
		}
		c.Log(LvlOK, i18n.T("Приоритет %s (PID %d) → %s"), name, in.PID, ctl.Priority(in.Prio))
		writeJSON(w, true)
	})
	m.HandleFunc("GET /api/proc/affinity", func(w http.ResponseWriter, r *http.Request) {
		pid, _ := strconv.Atoi(r.URL.Query().Get("pid"))
		cores, err := ctl.GetAffinity(int32(pid))
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, cores)
	})
	m.HandleFunc("POST /api/proc/affinity", func(w http.ResponseWriter, r *http.Request) {
		var in pidReq
		if err := readJSON(r, &in); err != nil {
			writeErr(w, err)
			return
		}
		name := c.procName(in.PID)
		if err := ctl.SetAffinity(in.PID, in.Cores); err != nil {
			c.Log(LvlErr, i18n.T("Ядра для %s (PID %d): %v"), name, in.PID, err)
			writeErr(w, err)
			return
		}
		c.Log(LvlOK, i18n.T("%s (PID %d) привязан к ядрам %s"), name, in.PID, rules.FormatCores(in.Cores))
		writeJSON(w, true)
	})

	// ---- Правила ----
	m.HandleFunc("GET /api/rules", func(w http.ResponseWriter, r *http.Request) {
		rs := c.Config().Rules
		if rs == nil {
			rs = []rules.Rule{}
		}
		writeJSON(w, rs)
	})
	m.HandleFunc("POST /api/rules", func(w http.ResponseWriter, r *http.Request) {
		var rs []rules.Rule
		if err := readJSON(r, &rs); err != nil {
			writeErr(w, err)
			return
		}
		for i := range rs {
			if rs[i].ID == "" {
				rs[i].ID = rules.NewID()
			}
		}
		c.SetRules(rs)
		writeJSON(w, rs)
	})
	m.HandleFunc("POST /api/rules/apply", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, c.ApplyRules()) })
	m.HandleFunc("POST /api/rules/parse-cores", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Text string `json:"text"`
		}
		_ = readJSON(r, &in)
		cores, err := rules.ParseCores(in.Text, runtime.NumCPU())
		if err != nil {
			writeErr(w, err)
			return
		}
		if cores == nil {
			cores = []int{}
		}
		writeJSON(w, cores)
	})

	// ---- Оптимизация ----
	m.HandleFunc("POST /api/opt/trim", func(w http.ResponseWriter, r *http.Request) {
		msg, err := c.TrimMemory()
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, msg)
	})
	m.HandleFunc("GET /api/opt/hogs", func(w http.ResponseWriter, r *http.Request) {
		s := c.Snapshot()
		out := []procDTO{}
		if s != nil {
			for _, p := range opt.FindHogs(s.Procs, float64(c.Config().HogThreshold), c.col.Self()) {
				out = append(out, procDTO{p.PID, p.Name, p.User, p.CPU, p.RSS, p.Threads, int(p.Priority), p.Create})
			}
		}
		writeJSON(w, out)
	})
	m.HandleFunc("POST /api/opt/lower", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			PIDs []int32 `json:"pids"`
		}
		if err := readJSON(r, &in); err != nil {
			writeErr(w, err)
			return
		}
		done := 0
		for _, pid := range in.PIDs {
			name := c.procName(pid)
			if err := ctl.SetPriority(pid, ctl.PrioBelowNormal); err != nil {
				c.Log(LvlErr, "%s (PID %d): %v", name, pid, err)
				continue
			}
			done++
			c.Log(LvlOK, i18n.T("%s (PID %d) → приоритет «Ниже среднего»"), name, pid)
		}
		writeJSON(w, done)
	})
	m.HandleFunc("POST /api/opt/tempscan", func(w http.ResponseWriter, r *http.Request) {
		res := c.ScanTemp()
		writeJSON(w, map[string]any{"files": len(res.Files), "bytes": res.Bytes, "roots": res.Roots, "days": c.Config().TempAgeDays})
	})
	m.HandleFunc("POST /api/opt/tempclean", func(w http.ResponseWriter, r *http.Request) {
		n, freed, failed := c.CleanTemp()
		if n > 0 {
			c.Record("disk", fmt.Sprintf(i18n.Pick("Удалены временные файлы: %d (%s)", "Temp files deleted: %d (%s)"), n, sys.Bytes(uint64(freed))), "", nil, "")
		}
		writeJSON(w, map[string]any{"removed": n, "freed": freed, "failed": failed})
	})
	m.HandleFunc("GET /api/power", func(w http.ResponseWriter, r *http.Request) {
		p, err := ctl.CurrentPowerProfile()
		res := map[string]any{"profile": int(p)}
		if err != nil {
			res["profile"] = -1
			res["error"] = err.Error()
		}
		writeJSON(w, res)
	})
	m.HandleFunc("POST /api/power", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Profile int `json:"profile"`
		}
		if err := readJSON(r, &in); err != nil || in.Profile < 0 || in.Profile > 2 {
			writeErr(w, errors.New("bad profile"))
			return
		}
		if err := c.SetPower(ctl.PowerProfile(in.Profile)); err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, true)
	})

	// ---- Твики ----
	m.HandleFunc("GET /api/tweaks/apps", func(w http.ResponseWriter, r *http.Request) {
		apps, err := tweaks.ListApps()
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, apps)
	})
	m.HandleFunc("POST /api/tweaks/apps/remove", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			IDs []string `json:"ids"`
		}
		if err := readJSON(r, &in); err != nil {
			writeErr(w, err)
			return
		}
		c.Log(LvlInfo, i18n.T("Удаление встроенных приложений: %d шт…"), len(in.IDs))
		res := tweaks.RemoveApps(in.IDs)
		ok := 0
		for _, x := range res {
			if x.OK {
				ok++
				c.Log(LvlOK, i18n.T("Удалено: %s"), x.ID)
				c.Record("apps", i18n.Pick("Удалено встроенное приложение: ", "Removed built-in app: ")+x.ID, "", nil,
					i18n.Pick("Вернуть можно из Microsoft Store.", "Reinstall it from the Microsoft Store."))
			} else {
				c.Log(LvlErr, "%s: %s", x.ID, x.Msg)
			}
		}
		c.Log(LvlInfo, i18n.T("Удаление завершено: успешно %d из %d"), ok, len(res))
		writeJSON(w, res)
	})
	m.HandleFunc("GET /api/tweaks/settings", func(w http.ResponseWriter, r *http.Request) {
		s := tweaks.ListSettings()
		if s == nil {
			s = []tweaks.Setting{}
		}
		writeJSON(w, s)
	})
	m.HandleFunc("POST /api/tweaks/settings", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			ID string `json:"id"`
			On bool   `json:"on"`
		}
		if err := readJSON(r, &in); err != nil {
			writeErr(w, err)
			return
		}
		if in.ID == "defender" && in.On && policy.Get().DisableDefender {
			writeErr(w, errPolicy())
			return
		}
		if err := tweaks.SetSetting(in.ID, in.On); err != nil {
			c.Log(LvlErr, i18n.T("Твик %s: %v"), in.ID, err)
			writeErr(w, err)
			return
		}
		title := in.ID
		for _, st := range tweaks.ListSettings() {
			if st.ID == in.ID {
				title = st.Title
			}
		}
		c.RecordTweak(in.ID, title, in.On)
		state := i18n.T("применён")
		if !in.On {
			state = i18n.T("отменён")
		}
		c.Log(LvlOK, i18n.T("Твик %s %s"), in.ID, state)
		writeJSON(w, true)
	})
	m.HandleFunc("POST /api/tweaks/restore-point", func(w http.ResponseWriter, r *http.Request) {
		c.Log(LvlInfo, "%s", i18n.T("Создание точки восстановления…"))
		if err := tweaks.CreateRestorePoint(); err != nil {
			c.Log(LvlErr, i18n.T("Точка восстановления: %v"), err)
			writeErr(w, err)
			return
		}
		c.Log(LvlOK, "%s", i18n.T("Точка восстановления создана"))
		writeJSON(w, true)
	})
	m.HandleFunc("POST /api/tweaks/restart-explorer", func(w http.ResponseWriter, r *http.Request) {
		if err := tweaks.RestartExplorer(); err != nil {
			writeErr(w, err)
			return
		}
		c.Log(LvlOK, "%s", i18n.T("Проводник перезапущен"))
		writeJSON(w, true)
	})

	m.HandleFunc("POST /api/elevate", func(w http.ResponseWriter, r *http.Request) {
		if err := ctl.RestartElevated(); err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, true)
		if c.OnQuit != nil {
			go func() { time.Sleep(300 * time.Millisecond); c.OnQuit() }()
		}
	})
}
