package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/healcore/healcoreops/internal/autostart"
	"github.com/healcore/healcoreops/internal/ctl"
	"github.com/healcore/healcoreops/internal/diskspace"
	"github.com/healcore/healcoreops/internal/drivers"
	"github.com/healcore/healcoreops/internal/health"
	"github.com/healcore/healcoreops/internal/i18n"
	"github.com/healcore/healcoreops/internal/maintenance"
	"github.com/healcore/healcoreops/internal/netconn"
	"github.com/healcore/healcoreops/internal/programs"
	"github.com/healcore/healcoreops/internal/sysadmin"
	"github.com/healcore/healcoreops/internal/sysnap"
	"github.com/healcore/healcoreops/internal/winupdate"
)

func (c *Core) snapStore() sysnap.Store {
	return sysnap.Store{Dir: filepath.Join(DataDir(), "snapshots")}
}

// captureSnapshot собирает снимок системы (10–40 с).
func (c *Core) captureSnapshot(name string) sysnap.Snapshot {
	src := map[string]sysnap.Source{
		sysnap.AreaPorts: func() (map[string]string, error) {
			ps, err := netconn.Listening(c.procNamePath)
			m := map[string]string{}
			for _, p := range ps {
				// динамические порты (49152+) программы открывают заново при каждом запуске — это шум
				if p.Name == "" || p.Port >= 49152 {
					continue
				}
				m[fmt.Sprintf("%s %d — %s", strings.ToUpper(p.Proto), p.Port, p.Name)] = p.Exposure
			}
			return m, err
		},
	}
	if programs.Supported() {
		src[sysnap.AreaPrograms] = func() (map[string]string, error) {
			ps, err := programs.List()
			m := map[string]string{}
			for _, p := range ps {
				k := p.Name
				if p.Arch == "x86" {
					k += " (x86)"
				}
				m[k] = p.Version
			}
			return m, err
		}
	}
	if autostart.Supported() {
		src[sysnap.AreaAutostart] = func() (map[string]string, error) {
			as, err := autostart.List()
			m := map[string]string{}
			for _, a := range as {
				// значения — без перевода: снимки должны сравниваться при любом языке интерфейса
				st := "on"
				if !a.Enabled {
					st = "off"
				}
				m[a.Name+" — "+a.Location] = st + " · " + a.Command
			}
			return m, err
		}
	}
	if sysadmin.Supported() {
		src[sysnap.AreaServices] = func() (map[string]string, error) {
			ss, err := sysadmin.Services()
			m := map[string]string{}
			for _, s := range ss {
				k := s.Display
				if k == "" || k == s.Name {
					k = s.Name
				} else {
					k += " (" + s.Name + ")"
				}
				m[k] = s.Start + " · " + s.Path
			}
			return m, err
		}
		src[sysnap.AreaTasks] = func() (map[string]string, error) {
			ts, err := sysadmin.Tasks()
			m := map[string]string{}
			for _, t := range ts {
				st := "on"
				if !t.Enabled {
					st = "off"
				}
				m[t.Path] = st + " · " + t.Action
			}
			return m, err
		}
	}
	if drivers.Supported() {
		src[sysnap.AreaDrivers] = func() (map[string]string, error) {
			ds, err := drivers.List()
			m := map[string]string{}
			for _, d := range ds {
				k := d.Name
				if _, dup := m[k]; dup {
					k += " · " + d.ID
				}
				m[k] = d.Version
			}
			return m, err
		}
	}
	return sysnap.Capture(name, c.col.Host().Hostname, src)
}

// pauseAt — срок паузы для подписи в истории.
func pauseAt(days int) string {
	return time.Now().Add(time.Duration(days) * 24 * time.Hour).Format("02.01.2006")
}

var snapMu sync.Mutex // одновременно — один снимок (сбор тяжёлый)

// routes3 — разделы версии 1.9: здоровье железа, история нагрузки, подключения
// и порты, дубликаты, снимки системы, Центр обновлений, инвентаризация, оповещения.
func (c *Core) routes3(m *http.ServeMux) {
	// ---- Здоровье железа и оповещения ----
	m.HandleFunc("GET /api/alerts", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, c.Alerts()) })
	m.HandleFunc("POST /api/alerts/check", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, nz(c.CheckAlerts())) })
	m.HandleFunc("POST /api/health/refresh", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, health.CollectNow()) })

	// ---- История нагрузки ----
	m.HandleFunc("GET /api/loadhist", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		to := time.Now()
		from := to.Add(-24 * time.Hour)
		if h, err := strconv.Atoi(q.Get("hours")); err == nil && h > 0 && h <= 24*31 {
			from = to.Add(-time.Duration(h) * time.Hour)
		}
		if f, err := strconv.ParseInt(q.Get("from"), 10, 64); err == nil && f > 0 {
			from = time.UnixMilli(f)
		}
		if t, err := strconv.ParseInt(q.Get("to"), 10, 64); err == nil && t > 0 {
			to = time.UnixMilli(t)
		}
		res, on := c.LoadHistory(from, to, 360)
		writeJSON(w, map[string]any{"enabled": on, "days": c.Config().LoadHistoryDays, "range": res})
	})

	// ---- Подключения и порты ----
	m.HandleFunc("GET /api/net/conns", func(w http.ResponseWriter, r *http.Request) {
		l, err := netconn.ByProcess(c.procNamePath)
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, nz(l))
	})
	m.HandleFunc("GET /api/net/ports", func(w http.ResponseWriter, r *http.Request) {
		l, err := netconn.Listening(c.procNamePath)
		if err != nil {
			writeErr(w, err)
			return
		}
		type portDTO struct {
			netconn.Port
			Key   string `json:"key"`
			Risky bool   `json:"risky"`
		}
		out := make([]portDTO, 0, len(l))
		for _, p := range l {
			out = append(out, portDTO{p, netconn.PortKey(p), netconn.Risky(p)})
		}
		writeJSON(w, out)
	})
	m.HandleFunc("POST /api/net/firewall", func(w http.ResponseWriter, r *http.Request) {
		l, err := netconn.Listening(c.procNamePath)
		if err != nil {
			writeErr(w, err)
			return
		}
		v, err := netconn.Firewall(l)
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, nz(v))
	})

	// ---- Дубликаты ----
	m.HandleFunc("GET /api/disk/dupes", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, diskspace.DupStatus()) })
	m.HandleFunc("POST /api/disk/dupes", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Root  string `json:"root"`
			MinMB int64  `json:"minMB"`
		}
		if err := readJSON(r, &in); err != nil {
			writeErr(w, err)
			return
		}
		if in.MinMB <= 0 {
			in.MinMB = 1
		}
		if err := diskspace.DupStart(in.Root, in.MinMB<<20); err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, true)
	})
	m.HandleFunc("POST /api/disk/dupes/cancel", func(w http.ResponseWriter, r *http.Request) { diskspace.DupCancel(); writeJSON(w, true) })

	// ---- Снимки системы ----
	m.HandleFunc("GET /api/snapshots", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, c.snapStore().List()) })
	m.HandleFunc("POST /api/snapshots", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Name string `json:"name"`
		}
		if err := readJSON(r, &in); err != nil {
			writeErr(w, err)
			return
		}
		if !snapMu.TryLock() {
			writeErr(w, errors.New(i18n.Pick("снимок уже создаётся", "a snapshot is already being taken")))
			return
		}
		defer snapMu.Unlock()
		s := c.captureSnapshot(in.Name)
		if err := c.snapStore().Save(&s); err != nil {
			writeErr(w, err)
			return
		}
		c.Log(LvlInfo, i18n.T("Создан снимок системы «%s»"), s.Name)
		writeJSON(w, sysnap.MetaOf(s))
	})
	m.HandleFunc("POST /api/snapshots/diff", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			A string `json:"a"`
			B string `json:"b"` // пусто или "now" — сравнить с текущим состоянием
		}
		if err := readJSON(r, &in); err != nil {
			writeErr(w, err)
			return
		}
		st := c.snapStore()
		a, err := st.Get(in.A)
		if err != nil {
			writeErr(w, err)
			return
		}
		var b sysnap.Snapshot
		if in.B == "" || in.B == "now" {
			if !snapMu.TryLock() {
				writeErr(w, errors.New(i18n.Pick("снимок уже создаётся", "a snapshot is already being taken")))
				return
			}
			b = c.captureSnapshot(i18n.Pick("Сейчас", "Now"))
			snapMu.Unlock()
			b.ID = "now"
		} else if b, err = st.Get(in.B); err != nil {
			writeErr(w, err)
			return
		}
		if a.Time > b.Time {
			a, b = b, a
		}
		writeJSON(w, sysnap.Compare(a, b))
	})
	m.HandleFunc("POST /api/snapshots/delete", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			ID string `json:"id"`
		}
		if err := readJSON(r, &in); err != nil {
			writeErr(w, err)
			return
		}
		if err := c.snapStore().Delete(in.ID); err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, true)
	})
	m.HandleFunc("POST /api/snapshots/rename", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		}
		if err := readJSON(r, &in); err != nil {
			writeErr(w, err)
			return
		}
		if err := c.snapStore().Rename(in.ID, in.Name); err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, true)
	})

	// ---- Центр обновлений Windows ----
	m.HandleFunc("GET /api/wu", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"supported": winupdate.Supported(), "pause": winupdate.Pause(), "search": winupdate.Search()})
	})
	m.HandleFunc("POST /api/wu/search", func(w http.ResponseWriter, r *http.Request) {
		if err := winupdate.StartSearch(); err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, true)
	})
	m.HandleFunc("GET /api/wu/history", func(w http.ResponseWriter, r *http.Request) {
		h, err := winupdate.History(120)
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, nz(h))
	})
	m.HandleFunc("POST /api/wu/install", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			IDs    []string `json:"ids"`
			Titles []string `json:"titles"`
		}
		if err := readJSON(r, &in); err != nil {
			writeErr(w, err)
			return
		}
		if len(in.IDs) == 0 {
			writeErr(w, errors.New(i18n.Pick("не выбрано ни одного обновления", "no updates selected")))
			return
		}
		title := fmt.Sprintf(i18n.Pick("Установка обновлений Windows (%d)", "Installing Windows updates (%d)"), len(in.IDs))
		err := maintenance.StartJob("wu-install", title, func(emit func(string)) error {
			reboot, err := winupdate.Install(in.IDs, emit)
			detail := strings.Join(in.Titles, "\n")
			note := ""
			if reboot {
				note = i18n.Pick("Нужна перезагрузка.", "Restart required.")
			}
			if err == nil {
				c.Record("updates", title, detail, nil, note)
			} else {
				c.Log(LvlErr, "%s: %v", title, err)
			}
			if c.OnNotify != nil {
				msg := i18n.Pick("Обновления установлены.", "Updates installed.")
				if err != nil {
					msg = i18n.Pick("Установка завершилась с ошибками — подробности в журнале.", "Installation finished with errors — see the log.")
				} else if reboot {
					msg += " " + note
				}
				c.OnNotify("HealCoreOps", msg)
			}
			return err
		})
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, true)
	})
	m.HandleFunc("POST /api/wu/pause", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Days int `json:"days"`
		}
		if err := readJSON(r, &in); err != nil {
			writeErr(w, err)
			return
		}
		prev := winupdate.ReadPause()
		if err := winupdate.SetPause(in.Days); err != nil {
			writeErr(w, err)
			return
		}
		pb, _ := json.Marshal(prev)
		title := i18n.Pick("Обновления Windows возобновлены", "Windows updates resumed")
		if in.Days > 0 {
			title = fmt.Sprintf(i18n.Pick("Обновления Windows приостановлены до %s", "Windows updates paused until %s"), pauseAt(min(in.Days, 35)))
		}
		c.Record("updates", title, "", undoArgs("wu_pause", "prev", string(pb)), "")
		writeJSON(w, winupdate.Pause())
	})

	m.HandleFunc("POST /api/open-settings", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Page string `json:"page"`
		}
		if err := readJSON(r, &in); err != nil {
			writeErr(w, err)
			return
		}
		// только известные страницы Параметров Windows
		pages := map[string]bool{"windowsupdate": true, "windowsupdate-history": true, "windowsupdate-options": true, "storagesense": true}
		if !pages[in.Page] {
			writeErr(w, errors.New("unknown settings page"))
			return
		}
		if err := ctl.OpenURL("ms-settings:" + in.Page); err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, true)
	})

	// ---- Инвентаризация ----
	m.HandleFunc("POST /api/inventory", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Format string `json:"format"` // json | csv
		}
		if err := readJSON(r, &in); err != nil {
			writeErr(w, err)
			return
		}
		inv := c.BuildInventory()
		ext := ".json"
		if in.Format == "csv" {
			ext = ".csv"
		}
		p, err := SaveInventory(inv, filepath.Join(ReportsDir(), "inventory-"+safeName(inv.Host)+ext))
		if err != nil {
			writeErr(w, err)
			return
		}
		_ = diskspace.Reveal(p)
		writeJSON(w, p)
	})
}
