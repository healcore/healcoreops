//go:build windows

package programs

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"github.com/healcore/healcoreops/internal/ctl"
	"github.com/healcore/healcoreops/internal/diskspace"
	"github.com/healcore/healcoreops/internal/i18n"
	"github.com/healcore/healcoreops/internal/maintenance"
)

// Supported — менеджер программ доступен в Windows.
func Supported() bool { return true }

type root struct {
	key   registry.Key
	pfx   string
	path  string
	scope string
	arch  string
}

var roots = []root{
	{registry.LOCAL_MACHINE, "HKLM", `SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall`, "machine", "x64"},
	{registry.LOCAL_MACHINE, "HKLM", `SOFTWARE\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall`, "machine", "x86"},
	{registry.CURRENT_USER, "HKCU", `SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall`, "user", ""},
}

func enc(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }
func dec(s string) string { b, _ := base64.RawURLEncoding.DecodeString(s); return string(b) }

type rec struct {
	Program
	uninstall, quiet string
	key              root
	sub              string
}

func str(k registry.Key, n string) string {
	v, _, _ := k.GetStringValue(n)
	return strings.TrimSpace(v)
}
func num(k registry.Key, n string) uint64 { v, _, _ := k.GetIntegerValue(n); return v }

func readAll() []rec {
	var out []rec
	seen := map[string]bool{}
	for _, r := range roots {
		k, err := registry.OpenKey(r.key, r.path, registry.ENUMERATE_SUB_KEYS)
		if err != nil {
			continue
		}
		subs, _ := k.ReadSubKeyNames(-1)
		k.Close()
		for _, sub := range subs {
			sk, err := registry.OpenKey(r.key, r.path+`\`+sub, registry.QUERY_VALUE)
			if err != nil {
				continue
			}
			name := str(sk, "DisplayName")
			un := str(sk, "UninstallString")
			if name == "" || un == "" || num(sk, "SystemComponent") == 1 || str(sk, "ParentKeyName") != "" ||
				strings.EqualFold(str(sk, "ReleaseType"), "Update") || strings.EqualFold(str(sk, "ReleaseType"), "Hotfix") {
				sk.Close()
				continue
			}
			p := rec{key: r, sub: sub, uninstall: un, quiet: str(sk, "QuietUninstallString")}
			p.ID = enc(r.pfx + `\` + r.path + `\` + sub)
			p.Name, p.Publisher, p.Version = name, str(sk, "Publisher"), str(sk, "DisplayVersion")
			p.Location = strings.Trim(str(sk, "InstallLocation"), `"`)
			p.Scope, p.Arch = r.scope, r.arch
			if d := str(sk, "InstallDate"); len(d) == 8 {
				p.InstallDate = d[:4] + "-" + d[4:6] + "-" + d[6:]
			}
			p.Size = int64(num(sk, "EstimatedSize")) * 1024
			p.MSI = MSIProduct(un) != "" || num(sk, "WindowsInstaller") == 1
			p.Quiet = p.quiet != "" || p.MSI
			p.Icon = str(sk, "DisplayIcon")
			sk.Close()
			dk := strings.ToLower(name + "|" + p.Version)
			if seen[dk] { // одна программа в 32- и 64-битной ветке
				continue
			}
			seen[dk] = true
			out = append(out, p)
		}
	}
	return out
}

// List возвращает установленные программы по алфавиту.
func List() ([]Program, error) {
	rs := readAll()
	out := make([]Program, 0, len(rs))
	for _, r := range rs {
		out = append(out, r.Program)
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out, nil
}

func find(id string) (rec, bool) {
	for _, r := range readAll() {
		if r.ID == id {
			return r, true
		}
	}
	return rec{}, false
}

func stillInstalled(id string) bool {
	full := dec(id)
	for _, r := range roots {
		pfx := r.pfx + `\` + r.path + `\`
		if strings.HasPrefix(full, pfx) {
			k, err := registry.OpenKey(r.key, r.path+`\`+strings.TrimPrefix(full, pfx), registry.QUERY_VALUE)
			if err != nil {
				return false
			}
			k.Close()
			return true
		}
	}
	return false
}

// ---------- Запуск деинсталлятора ----------

var (
	shell32         = windows.NewLazySystemDLL("shell32.dll")
	procShellExecEx = shell32.NewProc("ShellExecuteExW")
)

type shellExecInfo struct {
	cbSize       uint32
	fMask        uint32
	hwnd         uintptr
	lpVerb       *uint16
	lpFile       *uint16
	lpParameters *uint16
	lpDirectory  *uint16
	nShow        int32
	hInstApp     uintptr
	lpIDList     uintptr
	lpClass      *uint16
	hkeyClass    uintptr
	dwHotKey     uint32
	hIcon        uintptr
	hProcess     windows.Handle
}

// startProcess запускает программу через ShellExecuteEx (с запросом UAC, если
// деинсталлятору нужны права администратора) и возвращает дескриптор процесса.
func startProcess(file, args string, show bool) (windows.Handle, error) {
	verb, _ := windows.UTF16PtrFromString("open")
	f, _ := windows.UTF16PtrFromString(file)
	var a *uint16
	if args != "" {
		a, _ = windows.UTF16PtrFromString(args)
	}
	info := shellExecInfo{lpVerb: verb, lpFile: f, lpParameters: a, fMask: 0x40 | 0x100} // NOCLOSEPROCESS|NOASYNC
	info.cbSize = uint32(unsafe.Sizeof(info))
	info.nShow = windows.SW_SHOWNORMAL
	if !show {
		info.nShow = windows.SW_HIDE
	}
	r, _, err := procShellExecEx.Call(uintptr(unsafe.Pointer(&info)))
	if r == 0 {
		if errors.Is(err, windows.ERROR_CANCELLED) {
			return 0, errors.New(i18n.Pick("запуск отменён (UAC)", "launch cancelled (UAC)"))
		}
		return 0, err
	}
	return info.hProcess, nil
}

func waitProcess(h windows.Handle, max time.Duration) (uint32, bool) {
	if h == 0 {
		return 0, true
	}
	defer windows.CloseHandle(h)
	ev, _ := windows.WaitForSingleObject(h, uint32(max/time.Millisecond))
	var code uint32
	_ = windows.GetExitCodeProcess(h, &code)
	return code, ev == windows.WAIT_OBJECT_0
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

func uninstallOne(r rec, quiet bool, emit func(string)) error {
	var file, args string
	if guid := MSIProduct(r.uninstall); guid != "" {
		file, args = "msiexec.exe", "/x "+guid
		if quiet {
			args += " /qn /norestart"
		}
	} else {
		cmd := r.uninstall
		if quiet && r.quiet != "" {
			cmd = r.quiet
		}
		cmd, _ = registry.ExpandString(cmd)
		file, args = SplitCommand(cmd, exists)
	}
	emit("▸ " + i18n.Pick("Удаление: ", "Uninstalling: ") + r.Name)
	h, err := startProcess(file, args, !quiet)
	if err != nil {
		emit("× " + err.Error())
		return err
	}
	if !quiet {
		emit("  " + i18n.Pick("Открыт деинсталлятор программы — следуйте его указаниям", "The app's uninstaller is open — follow its instructions"))
	}
	code, finished := waitProcess(h, 2*time.Hour)
	if !finished {
		emit("× " + i18n.T("превышено время ожидания"))
		return errors.New(i18n.T("превышено время ожидания"))
	}
	// Многие деинсталляторы перезапускают себя из временной папки и сразу
	// выходят — ждём, пока запись о программе исчезнет (до 15 минут).
	deadline := time.Now().Add(15 * time.Minute)
	for stillInstalled(r.ID) && time.Now().Before(deadline) {
		time.Sleep(2 * time.Second)
	}
	if stillInstalled(r.ID) {
		emit(fmt.Sprintf("! %s (%s %d)", i18n.Pick("Программа всё ещё числится установленной — возможно, удаление отменено", "The app is still listed as installed — the uninstall may have been cancelled"), i18n.T("код"), code))
		return errors.New(i18n.Pick("удаление не завершено", "uninstall not completed"))
	}
	emit("✓ " + i18n.Pick("Удалено: ", "Removed: ") + r.Name)
	return nil
}

var (
	lmu      sync.Mutex
	lastDone = map[string]rec{} // удалённые в этой сессии — для поиска остатков
	// OnRemoved вызывается после успешного удаления (запись в историю).
	OnRemoved func(name string)
)

// Uninstall удаляет программы по очереди в фоне (журнал — как у обслуживания).
func Uninstall(ids []string, quiet bool) error {
	var list []rec
	for _, id := range ids {
		if r, ok := find(id); ok {
			list = append(list, r)
		}
	}
	if len(list) == 0 {
		return errors.New(i18n.Pick("программа не найдена", "program not found"))
	}
	if quiet {
		for _, r := range list {
			if !r.Quiet {
				return fmt.Errorf(i18n.Pick("«%s» не поддерживает тихое удаление", "“%s” does not support silent uninstall"), r.Name)
			}
		}
	}
	title := list[0].Name
	if len(list) > 1 {
		title = fmt.Sprintf(i18n.Pick("Удаление программ: %d", "Uninstalling programs: %d"), len(list))
	}
	return maintenance.StartJob("uninstall", title, func(emit func(string)) error {
		failed := 0
		for _, r := range list {
			if err := uninstallOne(r, quiet, emit); err != nil {
				failed++
				continue
			}
			lmu.Lock()
			lastDone[r.ID] = r
			lmu.Unlock()
			if OnRemoved != nil {
				OnRemoved(r.Name)
			}
		}
		emit("")
		emit(i18n.Pick("Проверьте остатки программы — кнопка «Найти остатки».", "Check for leftovers with “Find leftovers”."))
		if failed > 0 {
			return fmt.Errorf(i18n.Pick("не удалено: %d", "not removed: %d"), failed)
		}
		return nil
	})
}

// ---------- Остатки ----------

var (
	leftMu sync.Mutex
	lefts  = map[string]Leftover{}
)

func dirSize(p string) int64 {
	var n int64
	_ = filepath.WalkDir(p, func(_ string, d fs.DirEntry, err error) error {
		if err == nil && d.Type().IsRegular() {
			if i, e := d.Info(); e == nil {
				n += i.Size()
			}
		}
		return nil
	})
	return n
}

// Leftovers ищет папки и ключи реестра, оставшиеся после удаления программы.
func Leftovers(id string) ([]Leftover, error) {
	lmu.Lock()
	r, ok := lastDone[id]
	lmu.Unlock()
	if !ok {
		if rr, found := find(id); found {
			r = rr
		} else {
			return nil, errors.New(i18n.Pick("программа не найдена", "program not found"))
		}
	}
	if stillInstalled(id) {
		return nil, errors.New(i18n.Pick("программа ещё установлена — сначала удалите её", "the program is still installed — uninstall it first"))
	}
	names := LeftoverNames(r.Name, r.Publisher)
	pub := CleanPublisher(r.Publisher)
	var dirs []string
	if r.Location != "" && !diskspace.IsProtectedRoot(r.Location) && LocationMatches(r.Location, r.Name, r.Publisher) {
		dirs = append(dirs, r.Location)
	}
	bases := []string{os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)"), os.Getenv("APPDATA"), os.Getenv("LOCALAPPDATA"),
		os.Getenv("ProgramData"), filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs")}
	for _, b := range bases {
		if b == "" {
			continue
		}
		for _, n := range names {
			dirs = append(dirs, filepath.Join(b, n))
			if pub != "" && !strings.EqualFold(pub, n) && !genericNames[strings.ToLower(pub)] {
				dirs = append(dirs, filepath.Join(b, pub, n), filepath.Join(b, r.Publisher, n))
			}
		}
	}
	var out []Leftover
	seen := map[string]bool{}
	for _, d := range dirs {
		l := strings.ToLower(filepath.Clean(d))
		if seen[l] {
			continue
		}
		seen[l] = true
		if st, err := os.Stat(d); err == nil && st.IsDir() && !diskspace.IsProtectedRoot(d) {
			out = append(out, Leftover{Kind: "folder", Path: d, Size: dirSize(d)})
		}
	}
	type rk struct {
		k    registry.Key
		pfx  string
		path string
	}
	var keys []rk
	for _, base := range []rk{{registry.CURRENT_USER, "HKCU", `SOFTWARE`}, {registry.LOCAL_MACHINE, "HKLM", `SOFTWARE`}, {registry.LOCAL_MACHINE, "HKLM", `SOFTWARE\WOW6432Node`}} {
		for _, n := range names {
			keys = append(keys, rk{base.k, base.pfx, base.path + `\` + n})
			if pub != "" && !strings.EqualFold(pub, n) && !genericNames[strings.ToLower(pub)] {
				keys = append(keys, rk{base.k, base.pfx, base.path + `\` + pub + `\` + n}, rk{base.k, base.pfx, base.path + `\` + r.Publisher + `\` + n})
			}
		}
	}
	for _, k := range keys {
		low := strings.ToLower(k.path)
		if seen[k.pfx+low] || strings.Contains(low, `\microsoft`) {
			continue
		}
		seen[k.pfx+low] = true
		if h, err := registry.OpenKey(k.k, k.path, registry.QUERY_VALUE); err == nil {
			h.Close()
			out = append(out, Leftover{Kind: "registry", Path: k.pfx + `\` + k.path})
		}
	}
	leftMu.Lock()
	for i := range out {
		out[i].ID = enc(out[i].Kind + "|" + out[i].Path)
		lefts[out[i].ID] = out[i]
	}
	leftMu.Unlock()
	if out == nil {
		out = []Leftover{}
	}
	return out, nil
}

func hiddenCmd(name string, args ...string) *exec.Cmd {
	c := exec.Command(name, args...)
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	return c
}

// RemoveLeftovers удаляет найденные остатки: папки — в Корзину, ключи — с
// резервной копией .reg. Принимаются только ID из последнего поиска.
func RemoveLeftovers(ids []string) (removed int, backup string, failed []string) {
	failed = []string{}
	for _, id := range ids {
		leftMu.Lock()
		l, ok := lefts[id]
		leftMu.Unlock()
		if !ok {
			continue
		}
		switch l.Kind {
		case "folder":
			if err := diskspace.RecycleLeftover(l.Path); err != nil {
				failed = append(failed, l.Path+": "+err.Error())
				continue
			}
		case "registry":
			if backup == "" {
				backup = filepath.Join(os.Getenv("LOCALAPPDATA"), "HealCoreOps", "RegBackups", time.Now().Format("20060102-150405")+"-leftovers")
				_ = os.MkdirAll(backup, 0o755)
			}
			file := filepath.Join(backup, strings.NewReplacer(`\`, "_", ":", "_").Replace(l.Path)+".reg")
			_ = hiddenCmd("reg", "export", l.Path, file, "/y").Run()
			if err := hiddenCmd("reg", "delete", l.Path, "/f").Run(); err != nil {
				msg := l.Path
				if !ctl.IsElevated() && strings.HasPrefix(l.Path, "HKLM") {
					msg += ": " + i18n.Pick("нужны права администратора", "administrator rights required")
				}
				failed = append(failed, msg)
				continue
			}
		}
		removed++
	}
	return
}

// ---------- winget ----------

func wingetPath() string {
	if p, err := exec.LookPath("winget.exe"); err == nil {
		return p
	}
	p := filepath.Join(os.Getenv("LOCALAPPDATA"), `Microsoft\WindowsApps\winget.exe`)
	if exists(p) {
		return p
	}
	return ""
}

// WingetAvailable — установлен ли winget.
func WingetAvailable() bool { return wingetPath() != "" }

// Upgrades возвращает программы, для которых в winget есть новые версии.
func Upgrades() ([]Upgrade, error) {
	w := wingetPath()
	if w == "" {
		return nil, errors.New(i18n.Pick("winget не найден — установите «Установщик приложений» из Microsoft Store", "winget not found — install “App Installer” from the Microsoft Store"))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, w, "upgrade", "--include-unknown", "--accept-source-agreements", "--disable-interactivity")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	cmd.Env = append(os.Environ(), "WINGET_DISABLE_PROGRESS=1")
	out, _ := cmd.Output() // код возврата ненулевой, когда обновлений нет
	if ctx.Err() != nil {
		return nil, errors.New(i18n.T("превышено время ожидания"))
	}
	res := ParseWingetTable(string(out))
	if res == nil {
		res = []Upgrade{}
	}
	return res, nil
}

// UpgradeApps обновляет программы через winget (все, если ids пуст) в фоне.
func UpgradeApps(ids []string) error {
	w := wingetPath()
	if w == "" {
		return errors.New(i18n.Pick("winget не найден", "winget not found"))
	}
	for _, id := range ids {
		if strings.ContainsAny(id, " \"'&|<>^") {
			return errors.New("bad id")
		}
	}
	title := i18n.Pick("Обновление программ", "Updating programs")
	return maintenance.StartJob("winget", title, func(emit func(string)) error {
		common := []string{"--silent", "--accept-package-agreements", "--accept-source-agreements", "--disable-interactivity"}
		var runs [][]string
		if len(ids) == 0 {
			runs = append(runs, append([]string{"upgrade", "--all", "--include-unknown"}, common...))
		} else {
			for _, id := range ids {
				runs = append(runs, append([]string{"upgrade", "--id", id, "-e"}, common...))
			}
		}
		failed := 0
		for _, args := range runs {
			label := "winget " + strings.Join(args[:3], " ")
			emit("▸ " + label)
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Minute)
			cmd := exec.CommandContext(ctx, w, args...)
			cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
			err := cmd.Run()
			cancel()
			if err != nil {
				failed++
				code := -1
				if ee, ok := err.(*exec.ExitError); ok {
					code = ee.ExitCode()
				}
				emit(fmt.Sprintf("× %s (%s %d)", i18n.T("ошибка"), i18n.T("код"), code))
				continue
			}
			emit("✓ " + i18n.T("готово"))
		}
		if failed > 0 {
			return fmt.Errorf(i18n.Pick("с ошибкой: %d", "failed: %d"), failed)
		}
		return nil
	})
}
