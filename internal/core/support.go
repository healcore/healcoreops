package core

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/healcore/healcoreops/internal/drivers"
	"github.com/healcore/healcoreops/internal/i18n"
	"github.com/healcore/healcoreops/internal/maintenance"
	"github.com/healcore/healcoreops/internal/policy"
)

// Пакет для поддержки: всё, что нужно, чтобы разобраться в проблеме, одним zip —
// отчёт о компьютере, инвентаризация, журналы программы, история изменений,
// настройки и последние фоновые операции. Личных файлов в нём нет, а путь к папке
// пользователя и имя пользователя заменяются на %USERPROFILE% и <user>.

const supportReadme = `Пакет для поддержки HealCoreOps / HealCoreOps support bundle
=============================================================

Создан / Created: %s
Версия / Version: %s (%s/%s)

Что внутри / Contents:
  report.json      отчёт о компьютере (железо, защита, сбои, загрузка) / computer report
  inventory.json   инвентаризация (программы, обновления, порты, здоровье дисков) / inventory
  log-*.txt        журналы программы за последние месяцы / app logs
  log-session.txt  журнал текущего запуска / current session log
  history.json     изменения, сделанные программой / changes made by the app
  alerts.json      активные оповещения / active alerts
  jobs.json        последние фоновые операции (обслуживание, драйверы) / last background jobs
  config.json      настройки программы / app settings
  policy.json      политики администратора / admin policies

Личных файлов, паролей и истории браузера здесь нет. Путь к папке пользователя
заменён на %%USERPROFILE%%, имя пользователя — на <user>. Имя компьютера оставлено:
оно нужно, чтобы отличать компьютеры. Перед отправкой содержимое можно просмотреть.

No personal files, passwords or browser history are included. The user profile path
is replaced with %%USERPROFILE%%, the user name with <user>. The computer name is kept.
You can review the contents before sending.
`

// redactor заменяет путь к профилю и имя пользователя (в обычном и JSON-виде).
func redactor() func([]byte) []byte {
	type pair struct{ from, to string }
	var pairs []pair
	add := func(from, to string) {
		if len(from) < 3 {
			return
		}
		pairs = append(pairs, pair{from, to})
		if esc := strings.ReplaceAll(from, `\`, `\\`); esc != from {
			pairs = append(pairs, pair{esc, strings.ReplaceAll(to, `\`, `\\`)})
		}
	}
	for _, env := range []string{"USERPROFILE", "HOME"} {
		if v := os.Getenv(env); v != "" && v != "/" && v != "/root" {
			add(v, "%USERPROFILE%")
		}
	}
	if u, err := user.Current(); err == nil {
		name := u.Username
		if i := strings.LastIndexAny(name, `\/`); i >= 0 {
			add(name, `<domain>\<user>`)
			name = name[i+1:]
		}
		if len(name) >= 3 && !strings.EqualFold(name, "root") && !strings.EqualFold(name, "admin") && !strings.EqualFold(name, "user") {
			for _, w := range []string{`\`, `/`} {
				add(`Users`+w+name, `Users`+w+`<user>`)
				add(`users`+w+name, `users`+w+`<user>`)
			}
			add(`"`+name+`"`, `"<user>"`)
		}
	}
	// длинные строки — первыми, чтобы «C:\Users\ivan» заменялось раньше «ivan»
	sort.SliceStable(pairs, func(i, j int) bool { return len(pairs[i].from) > len(pairs[j].from) })
	return func(b []byte) []byte {
		for _, p := range pairs {
			b = bytes.ReplaceAll(b, []byte(p.from), []byte(p.to))
		}
		return b
	}
}

// tailFile — последние max байт файла (с начала строки).
func tailFile(p string, max int64) []byte {
	f, err := os.Open(p)
	if err != nil {
		return nil
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil
	}
	if st.Size() > max {
		_, _ = f.Seek(st.Size()-max, io.SeekStart)
	}
	b, _ := io.ReadAll(f)
	if st.Size() > max {
		if i := bytes.IndexByte(b, '\n'); i >= 0 {
			b = b[i+1:]
		}
	}
	return b
}

// SupportBundle собирает пакет для поддержки в zip по пути path (пусто — в папку отчётов).
func (c *Core) SupportBundle(path string) (string, error) {
	if path == "" {
		path = filepath.Join(ReportsDir(), fmt.Sprintf("healcoreops-support-%s-%s.zip", safeName(c.col.Host().Hostname), time.Now().Format("20060102-1504")))
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	red := redactor()
	files := map[string][]byte{}
	js := func(v any) []byte { b, _ := json.MarshalIndent(v, "", "  "); return b }

	files["README.txt"] = []byte(fmt.Sprintf(supportReadme, time.Now().Format("2006-01-02 15:04:05"), Version, runtime.GOOS, runtime.GOARCH))
	files["report.json"] = js(c.BuildReport())
	files["inventory.json"] = js(c.BuildInventory())
	files["history.json"] = js(c.History().List())
	files["alerts.json"] = js(nz(c.Alerts()))
	files["jobs.json"] = js(map[string]any{"maintenance": maintenance.Status(), "drivers": drivers.Status()})
	files["config.json"] = js(c.Config())
	files["policy.json"] = js(policy.Get())

	var sess strings.Builder
	for _, e := range c.LogSince(0) {
		lvl := [...]string{"INFO", "OK  ", "WARN", "ERR "}[min(max(e.Lvl, 0), 3)]
		fmt.Fprintf(&sess, "%s %s %s\r\n", e.Time, lvl, e.Msg)
	}
	files["log-session.txt"] = []byte(sess.String())
	logs, _ := filepath.Glob(filepath.Join(LogDir(), "healcoreops-*.log"))
	sort.Strings(logs)
	if len(logs) > 2 {
		logs = logs[len(logs)-2:] // текущий и прошлый месяц
	}
	for _, p := range logs {
		files["log-"+strings.TrimPrefix(filepath.Base(p), "healcoreops-")+".txt"] = tailFile(p, 2<<20)
	}

	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return "", err
	}
	zw := zip.NewWriter(f)
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: n, Method: zip.Deflate, Modified: time.Now()})
		if err != nil {
			break
		}
		b := files[n]
		if n != "README.txt" {
			b = red(b)
		}
		_, _ = w.Write(b)
	}
	err = zw.Close()
	if e := f.Close(); err == nil {
		err = e
	}
	if err != nil {
		os.Remove(tmp)
		return "", err
	}
	if err := os.Rename(tmp, path); err != nil {
		return "", err
	}
	c.Log(LvlInfo, i18n.T("Пакет для поддержки сохранён: %s"), path)
	return path, nil
}
