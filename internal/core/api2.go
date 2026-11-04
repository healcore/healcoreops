package core

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/healcore/healcoreops/internal/ctl"
	"github.com/healcore/healcoreops/internal/diag"
	"github.com/healcore/healcoreops/internal/diskspace"
	"github.com/healcore/healcoreops/internal/history"
	"github.com/healcore/healcoreops/internal/i18n"
	"github.com/healcore/healcoreops/internal/maintenance"
	"github.com/healcore/healcoreops/internal/policy"
	"github.com/healcore/healcoreops/internal/programs"
	"github.com/healcore/healcoreops/internal/sys"
	"github.com/healcore/healcoreops/internal/sysadmin"
)

func nz[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

// routes2 — разделы версии 1.8: история, диск, программы, службы, задачи,
// диагностика, сеть, отчёт, проверка ПК, политики и расписание.
func (c *Core) routes2(m *http.ServeMux) {
	// ---- История изменений ----
	m.HandleFunc("GET /api/history", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, nz(c.History().List())) })
	m.HandleFunc("POST /api/history/undo", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			ID int64 `json:"id"`
		}
		if err := readJSON(r, &in); err != nil {
			writeErr(w, err)
			return
		}
		if err := c.Undo(in.ID); err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, true)
	})
	m.HandleFunc("POST /api/history/clear", func(w http.ResponseWriter, r *http.Request) { c.History().Clear(); writeJSON(w, true) })
	m.HandleFunc("POST /api/logs/open", func(w http.ResponseWriter, r *http.Request) {
		_ = os.MkdirAll(LogDir(), 0o755)
		if err := diskspace.Reveal(LogDir()); err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, LogDir())
	})

	// ---- Место на диске ----
	m.HandleFunc("GET /api/disk/drives", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, nz(diskspace.Drives())) })
	m.HandleFunc("GET /api/disk/categories", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, nz(diskspace.Categories())) })
	m.HandleFunc("GET /api/disk/scan", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, diskspace.Status()) })
	m.HandleFunc("POST /api/disk/scan", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Root string `json:"root"`
		}
		if err := readJSON(r, &in); err != nil {
			writeErr(w, err)
			return
		}
		if err := diskspace.Start(in.Root); err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, true)
	})
	m.HandleFunc("POST /api/disk/cancel", func(w http.ResponseWriter, r *http.Request) { diskspace.Cancel(); writeJSON(w, true) })
	m.HandleFunc("POST /api/disk/clean", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			ID    string `json:"id"`
			Title string `json:"title"`
		}
		if err := readJSON(r, &in); err != nil {
			writeErr(w, err)
			return
		}
		res, err := diskspace.Clean(in.ID)
		if err != nil {
			writeErr(w, err)
			return
		}
		msg := fmt.Sprintf(i18n.T("Очищено «%s»: %s, файлов %d"), in.Title, sys.Bytes(uint64(res.Freed)), res.Removed)
		c.Log(LvlOK, "%s", msg)
		c.Record("disk", msg, in.ID, nil, i18n.Pick("Кэш и временные файлы удалены безвозвратно — программы создадут их заново.", "Cache and temp files were deleted permanently — apps recreate them."))
		writeJSON(w, res)
	})
	m.HandleFunc("POST /api/disk/recycle", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Path string `json:"path"`
		}
		if err := readJSON(r, &in); err != nil {
			writeErr(w, err)
			return
		}
		if err := diskspace.Recycle(in.Path); err != nil {
			if errors.Is(err, diskspace.ErrProtected) {
				err = errors.New(i18n.Pick("это системная папка или файл — удалять нельзя", "this is a system folder or file — it can't be deleted"))
			}
			writeErr(w, err)
			return
		}
		c.Log(LvlOK, i18n.T("В Корзину: %s"), in.Path)
		var u *history.Undo
		if diskspace.Supported() && ctlWindows() {
			u = undoArgs("recycle", "path", in.Path)
		}
		c.Record("disk", i18n.Pick("Перемещено в Корзину: ", "Moved to Recycle Bin: ")+filepath.Base(in.Path), in.Path, u, i18n.Pick("Восстановите из Корзины.", "Restore it from the Recycle Bin."))
		writeJSON(w, true)
	})
	m.HandleFunc("POST /api/disk/reveal", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Path string `json:"path"`
		}
		if err := readJSON(r, &in); err != nil {
			writeErr(w, err)
			return
		}
		if in.Path == "cleanmgr" {
			if err := diskspace.OpenCleanmgr(); err != nil {
				writeErr(w, err)
				return
			}
			writeJSON(w, true)
			return
		}
		if _, err := os.Stat(in.Path); err != nil {
			writeErr(w, err)
			return
		}
		if err := diskspace.Reveal(in.Path); err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, true)
	})

	// ---- Программы ----
	m.HandleFunc("GET /api/programs", func(w http.ResponseWriter, r *http.Request) {
		p, err := programs.List()
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, map[string]any{"programs": nz(p), "winget": programs.WingetAvailable()})
	})
	m.HandleFunc("POST /api/programs/uninstall", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			IDs   []string `json:"ids"`
			Quiet bool     `json:"quiet"`
		}
		if err := readJSON(r, &in); err != nil {
			writeErr(w, err)
			return
		}
		if err := programs.Uninstall(in.IDs, in.Quiet); err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, true)
	})
	m.HandleFunc("GET /api/programs/leftovers", func(w http.ResponseWriter, r *http.Request) {
		l, err := programs.Leftovers(r.URL.Query().Get("id"))
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, l)
	})
	m.HandleFunc("POST /api/programs/leftovers/remove", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			IDs  []string `json:"ids"`
			Name string   `json:"name"`
		}
		if err := readJSON(r, &in); err != nil {
			writeErr(w, err)
			return
		}
		n, backup, failed := programs.RemoveLeftovers(in.IDs)
		var u *history.Undo
		if backup != "" {
			u = undoArgs("reg_import", "dir", backup)
		}
		if n > 0 {
			c.Record("programs", fmt.Sprintf(i18n.Pick("Удалены остатки «%s»: %d", "Removed leftovers of “%s”: %d"), in.Name, n), backup, u,
				i18n.Pick("Папки — в Корзине, ключи реестра — в резервной копии.", "Folders are in the Recycle Bin, registry keys are backed up."))
		}
		writeJSON(w, map[string]any{"removed": n, "backup": backup, "failed": failed})
	})
	m.HandleFunc("GET /api/programs/upgrades", func(w http.ResponseWriter, r *http.Request) {
		u, err := programs.Upgrades()
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, u)
	})
	m.HandleFunc("POST /api/programs/upgrade", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			IDs []string `json:"ids"`
		}
		if err := readJSON(r, &in); err != nil {
			writeErr(w, err)
			return
		}
		if err := programs.UpgradeApps(in.IDs); err != nil {
			writeErr(w, err)
			return
		}
		what := strings.Join(in.IDs, ", ")
		if what == "" {
			what = i18n.Pick("все", "all")
		}
		c.Record("programs", i18n.Pick("Обновление программ (winget): ", "Updating programs (winget): ")+what, "", nil, "")
		writeJSON(w, true)
	})
	// общий журнал фоновых операций (удаление программ, winget, трассировка)
	m.HandleFunc("GET /api/job", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, maintenance.Status()) })

	// ---- Службы ----
	m.HandleFunc("GET /api/services", func(w http.ResponseWriter, r *http.Request) {
		s, err := sysadmin.Services()
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, s)
	})
	m.HandleFunc("POST /api/services/action", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Name, Display, Action, Prev string
		}
		if err := readJSON(r, &in); err != nil {
			writeErr(w, err)
			return
		}
		if err := sysadmin.ServiceAction(in.Name, in.Action); err != nil {
			c.Log(LvlErr, i18n.T("Служба %s: %v"), in.Name, err)
			writeErr(w, err)
			return
		}
		verb := map[string]string{"start": i18n.Pick("запущена", "started"), "stop": i18n.Pick("остановлена", "stopped"), "restart": i18n.Pick("перезапущена", "restarted")}[in.Action]
		var u *history.Undo
		if in.Action != "restart" && in.Prev != "" {
			u = undoArgs("service_state", "name", in.Name, "state", in.Prev)
		}
		c.Log(LvlOK, i18n.T("Служба %s %s"), in.Display, verb)
		c.Record("services", fmt.Sprintf(i18n.Pick("Служба «%s» %s", "Service “%s” %s"), in.Display, verb), in.Name, u, "")
		writeJSON(w, true)
	})
	m.HandleFunc("POST /api/services/start-type", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Name, Display, Start string
		}
		if err := readJSON(r, &in); err != nil {
			writeErr(w, err)
			return
		}
		prev, _ := sysadmin.ServiceStart(in.Name)
		if err := sysadmin.SetServiceStart(in.Name, in.Start); err != nil {
			c.Log(LvlErr, i18n.T("Служба %s: %v"), in.Name, err)
			writeErr(w, err)
			return
		}
		var u *history.Undo
		if sysadmin.ValidStart(prev) && prev != in.Start {
			u = undoArgs("service_start", "name", in.Name, "start", prev)
		}
		c.Log(LvlOK, i18n.T("Служба %s: тип запуска %s → %s"), in.Display, prev, in.Start)
		c.Record("services", fmt.Sprintf(i18n.Pick("Служба «%s»: запуск %s → %s", "Service “%s”: startup %s → %s"), in.Display, prev, in.Start), in.Name, u, "")
		writeJSON(w, true)
	})

	// ---- Планировщик ----
	m.HandleFunc("GET /api/tasks", func(w http.ResponseWriter, r *http.Request) {
		t, err := sysadmin.Tasks()
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, t)
	})
	m.HandleFunc("POST /api/tasks/enable", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Path string `json:"path"`
			On   bool   `json:"on"`
		}
		if err := readJSON(r, &in); err != nil {
			writeErr(w, err)
			return
		}
		if err := sysadmin.SetTaskEnabled(in.Path, in.On); err != nil {
			writeErr(w, err)
			return
		}
		verb := i18n.Pick("Задача включена: ", "Task enabled: ")
		if !in.On {
			verb = i18n.Pick("Задача отключена: ", "Task disabled: ")
		}
		c.Log(LvlOK, "%s%s", verb, in.Path)
		c.Record("tasks", verb+in.Path, "", undoArgs("task", "path", in.Path, "on", b2s(!in.On)), "")
		writeJSON(w, true)
	})
	m.HandleFunc("POST /api/tasks/run", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Path string `json:"path"`
		}
		if err := readJSON(r, &in); err != nil {
			writeErr(w, err)
			return
		}
		if err := sysadmin.RunTask(in.Path); err != nil {
			writeErr(w, err)
			return
		}
		c.Log(LvlOK, i18n.T("Задача запущена: %s"), in.Path)
		writeJSON(w, true)
	})

	// ---- Диагностика ----
	m.HandleFunc("GET /api/crashes", func(w http.ResponseWriter, r *http.Request) {
		days, _ := strconv.Atoi(r.URL.Query().Get("days"))
		rep, err := diag.Crashes(days)
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, rep)
	})
	m.HandleFunc("GET /api/boot", func(w http.ResponseWriter, r *http.Request) {
		b, err := diag.BootInfo(LastBoot())
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, b)
	})
	m.HandleFunc("GET /api/reboot", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, diag.PendingReboot()) })
	m.HandleFunc("GET /api/security", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, nz(diag.Security())) })

	// ---- Сеть ----
	m.HandleFunc("GET /api/net/adapters", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, nz(diag.Adapters())) })
	m.HandleFunc("POST /api/net/check", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, diag.CheckInternet()) })
	m.HandleFunc("POST /api/net/ping", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Host string `json:"host"`
			Port int    `json:"port"`
		}
		if err := readJSON(r, &in); err != nil {
			writeErr(w, err)
			return
		}
		if in.Port == 0 {
			in.Port = 443
		}
		writeJSON(w, diag.TCPPing(strings.TrimSpace(in.Host), in.Port, 4))
	})
	m.HandleFunc("POST /api/net/port", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Host string `json:"host"`
			Port int    `json:"port"`
		}
		if err := readJSON(r, &in); err != nil {
			writeErr(w, err)
			return
		}
		open, ms, err := diag.PortCheck(strings.TrimSpace(in.Host), in.Port)
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, map[string]any{"open": open, "ms": ms})
	})
	m.HandleFunc("POST /api/net/trace", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Host string `json:"host"`
		}
		if err := readJSON(r, &in); err != nil {
			writeErr(w, err)
			return
		}
		if err := diag.Traceroute(strings.TrimSpace(in.Host)); err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, true)
	})

	// ---- Отчёт и проверка ----
	m.HandleFunc("POST /api/report", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Open bool `json:"open"`
		}
		_ = readJSON(r, &in)
		rep := c.BuildReport()
		path, err := c.SaveReport(rep, "")
		if err != nil {
			writeErr(w, err)
			return
		}
		c.Log(LvlOK, i18n.T("Отчёт сохранён: %s"), path)
		if in.Open {
			_ = ctl.OpenURL(path)
		}
		writeJSON(w, map[string]string{"path": path})
	})
	m.HandleFunc("POST /api/report/reveal", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Path string `json:"path"`
		}
		_ = readJSON(r, &in)
		if in.Path == "" || !strings.HasPrefix(filepath.Clean(in.Path), filepath.Clean(ReportsDir())) {
			in.Path = ReportsDir()
			_ = os.MkdirAll(in.Path, 0o755)
		}
		if err := diskspace.Reveal(in.Path); err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, true)
	})
	m.HandleFunc("POST /api/checkup", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, c.RunCheckup()) })
	m.HandleFunc("POST /api/quickclean", func(w http.ResponseWriter, r *http.Request) {
		if policy.Get().Blocks(policy.RuleDisk) {
			writeErr(w, errPolicy())
			return
		}
		res, err := c.QuickClean()
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, res)
	})

	// ---- Политики ----
	m.HandleFunc("GET /api/policy", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, policy.Get()) })
	m.HandleFunc("POST /api/policy/template", func(w http.ResponseWriter, r *http.Request) {
		dir := ReportsDir()
		_ = os.MkdirAll(dir, 0o755)
		p := filepath.Join(dir, "HealCoreOps-policy-template.reg")
		// .reg читается regedit в UTF-16 LE с BOM
		if err := os.WriteFile(p, utf16le(strings.ReplaceAll(policy.Template(), "\n", "\r\n")), 0o644); err != nil {
			writeErr(w, err)
			return
		}
		_ = diskspace.Reveal(p)
		writeJSON(w, map[string]string{"path": p})
	})

	// ---- Расписание ----
	m.HandleFunc("POST /api/schedule/run", func(w http.ResponseWriter, r *http.Request) {
		res := c.RunSchedule()
		writeJSON(w, res)
	})
}

func errPolicy() error {
	return errors.New(i18n.Pick("действие запрещено политикой администратора", "this action is blocked by an administrator policy"))
}

func ctlWindows() bool { return programs.Supported() }

func utf16le(s string) []byte {
	out := []byte{0xFF, 0xFE}
	for _, r := range s {
		if r > 0xFFFF {
			r -= 0x10000
			hi, lo := 0xD800+(r>>10), 0xDC00+(r&0x3FF)
			out = append(out, byte(hi), byte(hi>>8), byte(lo), byte(lo>>8))
			continue
		}
		out = append(out, byte(r), byte(r>>8))
	}
	return out
}
