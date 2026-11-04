//go:build windows

package reghealer

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/windows/registry"

	"github.com/healcore/healcoreops/internal/ctl"
	"github.com/healcore/healcoreops/internal/i18n"
)

// Supported — проверка реестра доступна в Windows.
func Supported() bool { return true }

type hive struct {
	key    registry.Key
	prefix string // форма для reg.exe: HKLM / HKCU
}

var (
	hklm = hive{registry.LOCAL_MACHINE, "HKLM"}
	hkcu = hive{registry.CURRENT_USER, "HKCU"}
)

// ---------- Проверка файлов ----------

// sysResolver — реальная файловая система Windows. «Нет доступа» и прочие
// ошибки считаются «файл есть»: висячей считаем запись только при точном «не найден».
var sysResolver = Resolver{
	Exists: func(p string) bool {
		_, err := os.Stat(p)
		return err == nil || !errors.Is(err, os.ErrNotExist)
	},
	Expand: func(s string) string {
		if v, err := registry.ExpandString(s); err == nil {
			return v
		}
		return s
	},
}

func missingTarget(cmd string) string { return sysResolver.Missing(cmd) }

// ---------- Сканирование ----------

func enc(parts ...string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strings.Join(parts, "\n")))
}

func dec(id string) []string {
	b, err := base64.RawURLEncoding.DecodeString(id)
	if err != nil {
		return nil
	}
	return strings.Split(string(b), "\n")
}

var runKeys = []struct {
	h    hive
	path string
}{
	{hkcu, `Software\Microsoft\Windows\CurrentVersion\Run`},
	{hkcu, `Software\Microsoft\Windows\CurrentVersion\RunOnce`},
	{hklm, `Software\Microsoft\Windows\CurrentVersion\Run`},
	{hklm, `Software\Microsoft\Windows\CurrentVersion\RunOnce`},
	{hklm, `Software\Wow6432Node\Microsoft\Windows\CurrentVersion\Run`},
	{hklm, `Software\Wow6432Node\Microsoft\Windows\CurrentVersion\RunOnce`},
}

var appPathKeys = []struct {
	h    hive
	path string
}{
	{hklm, `Software\Microsoft\Windows\CurrentVersion\App Paths`},
	{hklm, `Software\Wow6432Node\Microsoft\Windows\CurrentVersion\App Paths`},
	{hkcu, `Software\Microsoft\Windows\CurrentVersion\App Paths`},
}

var uninstallKeys = []struct {
	h    hive
	path string
}{
	{hklm, `Software\Microsoft\Windows\CurrentVersion\Uninstall`},
	{hklm, `Software\Wow6432Node\Microsoft\Windows\CurrentVersion\Uninstall`},
	{hkcu, `Software\Microsoft\Windows\CurrentVersion\Uninstall`},
}

// Scan ищет висячие записи реестра. Ничего не меняет.
func Scan() ([]Issue, error) {
	var out []Issue

	// 1) Автозапуск: значения Run/RunOnce с несуществующим файлом.
	for _, rk := range runKeys {
		k, err := registry.OpenKey(rk.h.key, rk.path, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		names, _ := k.ReadValueNames(-1)
		for _, n := range names {
			v, _, err := k.GetStringValue(n)
			if err != nil {
				continue
			}
			if miss := missingTarget(v); miss != "" {
				loc := rk.h.prefix + `\` + rk.path
				out = append(out, Issue{
					ID: enc("val", loc, n), Category: "startup",
					Title:    i18n.Pick("Автозапуск: ", "Startup: ") + n,
					Detail:   i18n.Pick("файл не найден: ", "file not found: ") + miss,
					Location: loc + `\` + n,
				})
			}
		}
		k.Close()
	}

	// 2) App Paths: подключи, чей файл по умолчанию отсутствует.
	for _, ak := range appPathKeys {
		root, err := registry.OpenKey(ak.h.key, ak.path, registry.ENUMERATE_SUB_KEYS)
		if err != nil {
			continue
		}
		subs, _ := root.ReadSubKeyNames(-1)
		root.Close()
		for _, sub := range subs {
			sk, err := registry.OpenKey(ak.h.key, ak.path+`\`+sub, registry.QUERY_VALUE)
			if err != nil {
				continue
			}
			def, _, _ := sk.GetStringValue("")
			sk.Close()
			if miss := missingTarget(def); miss != "" {
				loc := ak.h.prefix + `\` + ak.path + `\` + sub
				out = append(out, Issue{
					ID: enc("key", loc, ""), Category: "apppaths",
					Title:    i18n.Pick("Путь приложения: ", "App path: ") + sub,
					Detail:   i18n.Pick("файл не найден: ", "file not found: ") + miss,
					Location: loc,
				})
			}
		}
	}

	// 3) Записи удаления с несуществующим деинсталлятором.
	for _, uk := range uninstallKeys {
		root, err := registry.OpenKey(uk.h.key, uk.path, registry.ENUMERATE_SUB_KEYS)
		if err != nil {
			continue
		}
		subs, _ := root.ReadSubKeyNames(-1)
		root.Close()
		for _, sub := range subs {
			sk, err := registry.OpenKey(uk.h.key, uk.path+`\`+sub, registry.QUERY_VALUE)
			if err != nil {
				continue
			}
			name, _, _ := sk.GetStringValue("DisplayName")
			us, _, _ := sk.GetStringValue("UninstallString")
			sk.Close()
			if name == "" || us == "" {
				continue // системные/служебные записи не трогаем
			}
			if miss := missingTarget(us); miss != "" {
				loc := uk.h.prefix + `\` + uk.path + `\` + sub
				out = append(out, Issue{
					ID: enc("key", loc, ""), Category: "uninstall",
					Title:    i18n.Pick("Остаток удаления: ", "Leftover uninstall: ") + name,
					Detail:   i18n.Pick("деинсталлятор не найден: ", "uninstaller not found: ") + miss,
					Location: loc,
				})
			}
		}
	}
	return out, nil
}

// ---------- Исправление с резервной копией ----------

func reg(args ...string) error {
	cmd := exec.Command("reg", args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	return cmd.Run()
}

func backupDir() (string, error) {
	dir := filepath.Join(os.Getenv("LOCALAPPDATA"), "HealCoreOps", "RegBackups", time.Now().Format("20060102-150405"))
	return dir, os.MkdirAll(dir, 0o755)
}

// Fix делает резервную копию затронутых ключей и удаляет висячие записи.
func Fix(ids []string) (FixReport, error) {
	rep := FixReport{Failed: []string{}}
	if len(ids) == 0 {
		return rep, errors.New(i18n.T("ничего не выбрано"))
	}
	dir, err := backupDir()
	if err != nil {
		return rep, err
	}
	rep.Backup = dir

	// Сначала резервная копия всех затронутых корневых ключей.
	backed := map[string]bool{}
	n := 0
	for _, id := range ids {
		p := dec(id)
		if len(p) != 3 {
			continue
		}
		backupKey := p[1] // для значения — ключ Run; для подключа — он сам
		if !backed[backupKey] {
			backed[backupKey] = true
			n++
			_ = reg("export", backupKey, filepath.Join(dir, sanitize(backupKey)+".reg"), "/y")
		}
	}

	// Затем удаление.
	for _, id := range ids {
		p := dec(id)
		if len(p) != 3 {
			rep.Failed = append(rep.Failed, id)
			continue
		}
		kind, loc, name := p[0], p[1], p[2]
		var e error
		if kind == "val" {
			e = reg("delete", loc, "/v", name, "/f")
		} else {
			e = reg("delete", loc, "/f")
		}
		if e != nil {
			who := name
			if who == "" {
				who = loc
			}
			rep.Failed = append(rep.Failed, who)
			continue
		}
		rep.Removed++
	}
	if rep.Removed == 0 && len(rep.Failed) > 0 && !ctl.IsElevated() {
		return rep, errors.New(i18n.T("не удалось изменить реестр — нужны права администратора"))
	}
	return rep, nil
}

var badChars = regexp.MustCompile(`[\\/:*?"<>|]+`)

func sanitize(s string) string {
	return strings.Trim(badChars.ReplaceAllString(s, "_"), "_")
}

// Restore импортирует резервные копии .reg из папки (откат исправлений).
func Restore(dir string) error {
	files, _ := filepath.Glob(filepath.Join(dir, "*.reg"))
	if len(files) == 0 {
		return errors.New(i18n.Pick("резервная копия не найдена: ", "backup not found: ") + dir)
	}
	failed := 0
	for _, f := range files {
		if reg("import", f) != nil {
			failed++
		}
	}
	if failed > 0 {
		if !ctl.IsElevated() {
			return errors.New(i18n.T("не удалось изменить реестр — нужны права администратора"))
		}
		return fmt.Errorf(i18n.Pick("не импортировано файлов: %d", "files not imported: %d"), failed)
	}
	return nil
}
