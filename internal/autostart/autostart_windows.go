//go:build windows

package autostart

import (
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"github.com/healcore/healcoreops/internal/i18n"
)

// Supported — управление автозагрузкой доступно в Windows.
func Supported() bool { return true }

// regSrc — источник автозагрузки в реестре и парный ему ключ для отключённых записей.
type regSrc struct {
	root       registry.Key
	path       string // ключ Run
	backup     string // наш ключ с отключёнными записями
	loc, scope string
	admin      bool
}

var regSrcs = []regSrc{
	{registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Run`, `Software\HealCoreOps\DisabledRun`, "HKCU \\ Run", "user", false},
	{registry.LOCAL_MACHINE, `Software\Microsoft\Windows\CurrentVersion\Run`, `Software\HealCoreOps\DisabledRun`, "HKLM \\ Run", "machine", true},
	{registry.LOCAL_MACHINE, `Software\Wow6432Node\Microsoft\Windows\CurrentVersion\Run`, `Software\HealCoreOps\DisabledRunWow`, "HKLM \\ Run (32-bit)", "machine", true},
}

func startupDirs() []struct {
	path, loc, scope string
	admin            bool
} {
	var out []struct {
		path, loc, scope string
		admin            bool
	}
	if ap := os.Getenv("APPDATA"); ap != "" {
		out = append(out, struct {
			path, loc, scope string
			admin            bool
		}{filepath.Join(ap, `Microsoft\Windows\Start Menu\Programs\Startup`), i18n.Pick("Папка «Автозагрузка»", "Startup folder"), "user", false})
	}
	if pd := os.Getenv("ProgramData"); pd != "" {
		out = append(out, struct {
			path, loc, scope string
			admin            bool
		}{filepath.Join(pd, `Microsoft\Windows\Start Menu\Programs\Startup`), i18n.Pick("Папка «Автозагрузка» (все пользователи)", "Startup folder (all users)"), "machine", true})
	}
	return out
}

func enc(parts ...string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strings.Join(parts, "\x00")))
}

func dec(id string) []string {
	b, err := base64.RawURLEncoding.DecodeString(id)
	if err != nil {
		return nil
	}
	return strings.Split(string(b), "\x00")
}

// List возвращает все записи автозагрузки.
func List() ([]Item, error) {
	var out []Item
	for i, s := range regSrcs {
		// включённые
		if k, err := registry.OpenKey(s.root, s.path, registry.QUERY_VALUE); err == nil {
			names, _ := k.ReadValueNames(-1)
			for _, n := range names {
				v, _, _ := k.GetStringValue(n)
				out = append(out, Item{
					ID: enc("reg", strconv.Itoa(i), n), Name: n, Command: v,
					Location: s.loc, Scope: s.scope, Enabled: true, Admin: s.admin,
				})
			}
			k.Close()
		}
		// отключённые (наш backup-ключ)
		if k, err := registry.OpenKey(s.root, s.backup, registry.QUERY_VALUE); err == nil {
			names, _ := k.ReadValueNames(-1)
			for _, n := range names {
				v, _, _ := k.GetStringValue(n)
				out = append(out, Item{
					ID: enc("reg", strconv.Itoa(i), n), Name: n, Command: v,
					Location: s.loc, Scope: s.scope, Enabled: false, Admin: s.admin,
				})
			}
			k.Close()
		}
	}
	for di, d := range startupDirs() {
		ents, err := os.ReadDir(d.path)
		if err != nil {
			continue
		}
		for _, e := range ents {
			if e.IsDir() {
				continue
			}
			name := e.Name()
			if strings.EqualFold(name, "desktop.ini") {
				continue
			}
			enabled := !strings.HasSuffix(strings.ToLower(name), ".disabled")
			display := strings.TrimSuffix(name, ".disabled")
			out = append(out, Item{
				ID: enc("file", strconv.Itoa(di), name), Name: display,
				Command: filepath.Join(d.path, name), Location: d.loc, Scope: d.scope,
				Enabled: enabled, Admin: d.admin,
			})
		}
	}
	return out, nil
}

func friendly(err error) error {
	if errors.Is(err, windows.ERROR_ACCESS_DENIED) || errors.Is(err, os.ErrPermission) {
		return errors.New(i18n.T("нужны права администратора — нажмите «Перезапустить от имени администратора»"))
	}
	return err
}

// SetEnabled включает или отключает запись автозагрузки (обратимо).
func SetEnabled(id string, on bool) error {
	p := dec(id)
	if len(p) != 3 {
		return errors.New(i18n.T("неизвестная запись автозагрузки"))
	}
	switch p[0] {
	case "reg":
		return setRegEnabled(p[1], p[2], on)
	case "file":
		return setFileEnabled(p[1], p[2], on)
	}
	return errors.New(i18n.T("неизвестная запись автозагрузки"))
}

func setRegEnabled(idxStr, name string, on bool) error {
	i, err := strconv.Atoi(idxStr)
	if err != nil || i < 0 || i >= len(regSrcs) {
		return errors.New(i18n.T("неизвестная запись автозагрузки"))
	}
	s := regSrcs[i]
	from, to := s.path, s.backup // отключаем: Run -> backup
	if on {
		from, to = s.backup, s.path // включаем: backup -> Run
	}
	src, err := registry.OpenKey(s.root, from, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return friendly(err)
	}
	defer src.Close()
	val, _, err := src.GetStringValue(name)
	if err != nil {
		return friendly(err)
	}
	dst, _, err := registry.CreateKey(s.root, to, registry.SET_VALUE)
	if err != nil {
		return friendly(err)
	}
	defer dst.Close()
	if err := dst.SetStringValue(name, val); err != nil {
		return friendly(err)
	}
	return friendly(src.DeleteValue(name))
}

func setFileEnabled(idxStr, name string, on bool) error {
	i, err := strconv.Atoi(idxStr)
	dirs := startupDirs()
	if err != nil || i < 0 || i >= len(dirs) {
		return errors.New(i18n.T("неизвестная запись автозагрузки"))
	}
	dir := dirs[i].path
	cur := filepath.Join(dir, name)
	var target string
	if on {
		target = filepath.Join(dir, strings.TrimSuffix(name, ".disabled"))
	} else {
		if strings.HasSuffix(strings.ToLower(name), ".disabled") {
			return nil
		}
		target = cur + ".disabled"
	}
	if cur == target {
		return nil
	}
	return friendly(os.Rename(cur, target))
}

// ---------- Автозапуск самого приложения ----------

const selfRunPath = `Software\Microsoft\Windows\CurrentVersion\Run`
const selfRunName = "HealCoreOps"

// SetSelfStartup добавляет (on=true) или убирает запись автозапуска приложения.
// Запись видна и в списке автозагрузки, её можно отключить там же.
func SetSelfStartup(on bool) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, selfRunPath, registry.SET_VALUE)
	if err != nil {
		return friendly(err)
	}
	defer k.Close()
	if !on {
		if err := k.DeleteValue(selfRunName); err != nil && !errors.Is(err, registry.ErrNotExist) {
			return friendly(err)
		}
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	return friendly(k.SetStringValue(selfRunName, `"`+exe+`" --minimized`))
}

// IsSelfStartup сообщает, включён ли автозапуск приложения.
func IsSelfStartup() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, selfRunPath, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	v, _, err := k.GetStringValue(selfRunName)
	return err == nil && v != ""
}
