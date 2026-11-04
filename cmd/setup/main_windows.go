//go:build windows

// Установщик HealCoreOps: один exe, внутри которого лежит приложение.
// Ставит программу в профиль пользователя (права администратора не нужны),
// создаёт ярлыки, регистрирует удаление в «Приложения и возможности».
// Запуск с ключом /uninstall удаляет программу.
package main

import (
	"context"
	_ "embed"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	webview2 "github.com/jchv/go-webview2"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"github.com/healcore/healcoreops/internal/i18n"
)

//go:embed payload/HealCoreOps.exe
var payload []byte

//go:embed setup.html
var pageHTML string

//go:embed icon.png
var iconPNG []byte

const (
	appName    = "HealCoreOps"
	appVersion = "1.8.7"
	exeName    = "HealCoreOps.exe"
	uninstName = "uninstall.exe"
	uninstKey  = `Software\Microsoft\Windows\CurrentVersion\Uninstall\HealCoreOps`
)

func init() { runtime.LockOSThread() }

var (
	user32      = windows.NewLazySystemDLL("user32.dll")
	dwmapi      = windows.NewLazySystemDLL("dwmapi.dll")
	procGetDpi  = user32.NewProc("GetDpiForSystem")
	procDwmAttr = dwmapi.NewProc("DwmSetWindowAttribute")
)

func hidden(cmd *exec.Cmd) *exec.Cmd {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	return cmd
}

// ---------- Состояние операции ----------

type progress struct {
	Pct  int    `json:"pct"`
	Step string `json:"step"`
	Done bool   `json:"done"`
	Err  string `json:"err"`
}

var (
	pmu  sync.Mutex
	prog progress
)

// logFile — журнал тихой установки (/LOG=файл).
var logFile *os.File

func logLine(s string) {
	if logFile != nil {
		fmt.Fprintf(logFile, "%s %s\r\n", time.Now().Format("2006-01-02 15:04:05"), s)
	}
}

func setStep(pct int, ru, en string) {
	pmu.Lock()
	prog.Pct, prog.Step = pct, i18n.Pick(ru, en)
	pmu.Unlock()
	logLine(fmt.Sprintf("[%3d%%] %s", pct, i18n.Pick(ru, en)))
}

func finish(err error) {
	pmu.Lock()
	prog.Done = true
	if err != nil {
		prog.Err = err.Error()
	} else {
		prog.Pct = 100
	}
	pmu.Unlock()
}

// ---------- Пути ----------

func defaultDir(allUsers bool) string {
	if allUsers {
		return filepath.Join(os.Getenv("ProgramFiles"), appName)
	}
	return filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs", appName)
}

func regRoot(allUsers bool) registry.Key {
	if allUsers {
		return registry.LOCAL_MACHINE
	}
	return registry.CURRENT_USER
}

func regValue(allUsers bool, name string) string {
	k, err := registry.OpenKey(regRoot(allUsers), uninstKey, registry.QUERY_VALUE|registry.WOW64_64KEY)
	if err != nil {
		return ""
	}
	defer k.Close()
	s, _, _ := k.GetStringValue(name)
	return s
}

// installedScope — где установлена программа: для пользователя или для всех.
func installedScope() (dir string, allUsers bool) {
	if d := regValue(false, "InstallLocation"); d != "" {
		return d, false
	}
	if d := regValue(true, "InstallLocation"); d != "" {
		return d, true
	}
	return "", false
}

func installedDir() string { d, _ := installedScope(); return d }

func installedVersion() string {
	_, all := installedScope()
	return regValue(all, "DisplayVersion")
}

func isElevated() bool { return windows.GetCurrentProcessToken().IsElevated() }

func psQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

func powershell(script string) error {
	u := windows.StringToUTF16(script)
	u = u[:len(u)-1]
	b := make([]byte, len(u)*2)
	for i, v := range u {
		b[2*i], b[2*i+1] = byte(v), byte(v>>8)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cmd := hidden(exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass",
		"-EncodedCommand", base64.StdEncoding.EncodeToString(b)))
	out, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return errors.New(i18n.Pick("превышено время ожидания PowerShell", "PowerShell timed out"))
	}
	if err != nil {
		return fmt.Errorf("%v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// stopApp закрывает все экземпляры программы (в т. ч. свёрнутый в трей) и
// ждёт, пока её файл освободится.
func stopApp(exe string) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_ = hidden(exec.CommandContext(ctx, "taskkill", "/f", "/t", "/im", exeName)).Run()
	// Ждём до ~6 с, пока файл перестанет быть занятым.
	for i := 0; i < 24; i++ {
		if exe == "" || fileWritable(exe) {
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
}

// fileWritable проверяет, что файл отсутствует или не удерживается другим процессом.
func fileWritable(path string) bool {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return true
	}
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return false
	}
	f.Close()
	return true
}

func writeFileRetry(path string, data []byte) error {
	// Записываем payload во временный файл ОДИН раз (а не на каждой попытке),
	// затем только переименовываем, пока старый файл не освободится.
	tmp := path + ".tmp-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	if err := os.WriteFile(tmp, data, 0o755); err != nil {
		return err
	}
	var err error
	for i := 0; i < 24; i++ { // ~6 секунд
		_ = os.Remove(path)
		if err = os.Rename(tmp, path); err == nil {
			return nil
		}
		time.Sleep(250 * time.Millisecond)
	}
	_ = os.Remove(tmp)
	return errors.New(i18n.Pick(
		"файл занят — закройте HealCoreOps (в том числе значок в трее) и повторите",
		"the file is in use — close HealCoreOps (including the tray icon) and try again"))
}

// ---------- Установка ----------

func install(dir string, desktop, startMenu, allUsers bool) error {
	if allUsers && !isElevated() {
		return errNeedAdmin
	}
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return errors.New(i18n.Pick("укажите папку установки", "choose an install folder"))
	}
	if !filepath.IsAbs(dir) {
		return errors.New(i18n.Pick("путь должен быть полным, например C:\\Programs\\HealCoreOps", "use a full path, e.g. C:\\Programs\\HealCoreOps"))
	}
	exe := filepath.Join(dir, exeName)

	setStep(10, "Закрытие запущенной программы…", "Closing the running app…")
	stopApp(exe)

	setStep(25, "Копирование файлов…", "Copying files…")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf(i18n.Pick("не удалось создать папку: %v", "cannot create folder: %v"), err)
	}
	if err := writeFileRetry(exe, payload); err != nil {
		return fmt.Errorf(i18n.Pick("не удалось записать программу: %v", "cannot write the app: %v"), err)
	}
	self, _ := os.Executable()
	if data, err := os.ReadFile(self); err == nil && !strings.EqualFold(self, filepath.Join(dir, uninstName)) {
		_ = writeFileRetry(filepath.Join(dir, uninstName), data)
	}

	setStep(60, "Создание ярлыков…", "Creating shortcuts…")
	var sc strings.Builder
	sc.WriteString("$ErrorActionPreference='Stop'\n$sh=New-Object -ComObject WScript.Shell\n")
	sc.WriteString("function Mk($p){ $l=$sh.CreateShortcut($p); $l.TargetPath=" + psQuote(exe) + "; $l.WorkingDirectory=" + psQuote(dir) +
		"; $l.IconLocation=" + psQuote(exe+",0") + "; $l.Description='HealCoreOps'; $l.Save() }\n")
	progFolder, deskFolder := "Programs", "Desktop"
	if allUsers {
		progFolder, deskFolder = "CommonPrograms", "CommonDesktopDirectory"
	}
	if startMenu {
		sc.WriteString("Mk (Join-Path ([Environment]::GetFolderPath('" + progFolder + "')) 'HealCoreOps.lnk')\n")
	}
	if desktop {
		sc.WriteString("Mk (Join-Path ([Environment]::GetFolderPath('" + deskFolder + "')) 'HealCoreOps.lnk')\n")
	}
	if startMenu || desktop {
		if err := powershell(sc.String()); err != nil {
			return fmt.Errorf(i18n.Pick("не удалось создать ярлыки: %v", "cannot create shortcuts: %v"), err)
		}
	}

	setStep(85, "Регистрация в системе…", "Registering with Windows…")
	k, _, err := registry.CreateKey(regRoot(allUsers), uninstKey, registry.SET_VALUE|registry.WOW64_64KEY)
	if err != nil {
		return err
	}
	defer k.Close()
	uninst := `"` + filepath.Join(dir, uninstName) + `" /uninstall`
	if allUsers {
		uninst += " /ALLUSERS"
	}
	for name, v := range map[string]string{
		"DisplayName": appName, "DisplayVersion": appVersion, "Publisher": appName,
		"DisplayIcon": exe, "InstallLocation": dir, "UninstallString": uninst,
		"QuietUninstallString": uninst + " /S", "InstallDate": time.Now().Format("20060102"),
	} {
		_ = k.SetStringValue(name, v)
	}
	_ = k.SetDWordValue("NoModify", 1)
	_ = k.SetDWordValue("NoRepair", 1)
	_ = k.SetDWordValue("EstimatedSize", uint32((len(payload)*2)/1024))

	setStep(100, "Готово", "Done")
	return nil
}

// ---------- Удаление ----------

func uninstall(removeData bool) error {
	dir, allUsers := installedScope()
	if allUsers && !isElevated() {
		return errNeedAdmin
	}
	if dir == "" {
		self, _ := os.Executable()
		dir = filepath.Dir(self)
		if a := argValue("/dir"); a != "" {
			dir = a
		}
	}
	setStep(15, "Закрытие программы…", "Closing the app…")
	stopApp(filepath.Join(dir, exeName))

	setStep(40, "Удаление ярлыков…", "Removing shortcuts…")
	_ = powershell(`$ErrorActionPreference='SilentlyContinue'
foreach ($f in 'Programs','Desktop','CommonPrograms','CommonDesktopDirectory') { Remove-Item (Join-Path ([Environment]::GetFolderPath($f)) 'HealCoreOps.lnk') -Force }`)

	setStep(60, "Удаление файлов…", "Removing files…")
	if dir != "" && strings.Contains(strings.ToLower(dir), "healcoreops") {
		var err error
		for i := 0; i < 24; i++ { // ~6 секунд
			if err = os.RemoveAll(dir); err == nil {
				break
			}
			time.Sleep(250 * time.Millisecond)
		}
		if err != nil {
			return errors.New(i18n.Pick(
				"не удалось удалить файлы — закройте HealCoreOps (в том числе значок в трее) и повторите",
				"could not remove files — close HealCoreOps (including the tray icon) and try again"))
		}
	}
	if removeData {
		setStep(80, "Удаление настроек…", "Removing settings…")
		if cfg, err := os.UserConfigDir(); err == nil {
			_ = os.RemoveAll(filepath.Join(cfg, appName))
		}
		_ = os.RemoveAll(filepath.Join(os.Getenv("LOCALAPPDATA"), appName))
	}

	setStep(95, "Очистка реестра…", "Cleaning registry…")
	_ = registry.DeleteKey(regRoot(allUsers), uninstKey)
	setStep(100, "Готово", "Done")
	return nil
}

func argValue(name string) string {
	for i, a := range os.Args {
		if strings.EqualFold(a, name) && i+1 < len(os.Args) {
			return os.Args[i+1]
		}
	}
	return ""
}

func hasArg(names ...string) bool {
	for _, a := range os.Args[1:] {
		for _, n := range names {
			if strings.EqualFold(a, n) {
				return true
			}
		}
	}
	return false
}

// argPrefixed — значение ключа вида /D=путь или /LOG=файл (путь может быть с пробелами,
// поэтому для /D= берётся всё до конца командной строки, как в NSIS).
func argPrefixed(prefix string) string {
	for i, a := range os.Args[1:] {
		if len(a) > len(prefix) && strings.EqualFold(a[:len(prefix)], prefix) {
			v := a[len(prefix):]
			if strings.EqualFold(prefix, "/D=") {
				v = strings.Join(append([]string{v}, os.Args[i+2:]...), " ")
			}
			return strings.Trim(v, `"`)
		}
	}
	return ""
}

var errNeedAdmin = errors.New("нужны права администратора (установка для всех пользователей) / administrator rights are required (all-users install)")

// Коды возврата тихой установки.
const (
	exitOK       = 0
	exitFail     = 1
	exitNeedRoot = 5 // как ERROR_ACCESS_DENIED
)

// silentMain — установка/удаление без окна для GPO, Intune, SCCM, PDQ и скриптов.
func silentMain(uninstallMode bool) int {
	if lp := argPrefixed("/LOG="); lp != "" {
		if f, err := os.OpenFile(lp, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
			logFile = f
			defer f.Close()
		}
	}
	logLine("HealCoreOps setup " + appVersion + " " + strings.Join(os.Args[1:], " "))
	var err error
	if uninstallMode {
		err = uninstall(hasArg("/PURGE"))
	} else {
		all := hasArg("/ALLUSERS")
		dir := argPrefixed("/D=")
		if dir == "" {
			if d, a := installedScope(); d != "" && a == all {
				dir = d
			} else {
				dir = defaultDir(all)
			}
		}
		err = install(dir, !hasArg("/NODESKTOP"), !hasArg("/NOSTARTMENU"), all)
		if err == nil && hasArg("/LAUNCH") {
			_ = exec.Command(filepath.Join(dir, exeName), "--minimized").Start()
		}
	}
	if err != nil {
		logLine("ERROR: " + err.Error())
		if errors.Is(err, errNeedAdmin) {
			return exitNeedRoot
		}
		return exitFail
	}
	logLine("OK")
	return exitOK
}

// relaunchFromTemp: uninstall.exe лежит в папке программы и не может удалить
// сам себя, поэтому копируется во временную папку и перезапускается оттуда.
func relaunchFromTemp() bool {
	self, _ := os.Executable()
	tmpDir := os.TempDir()
	if strings.HasPrefix(strings.ToLower(self), strings.ToLower(tmpDir)) {
		return false
	}
	data, err := os.ReadFile(self)
	if err != nil {
		return false
	}
	tmp := filepath.Join(tmpDir, fmt.Sprintf("HealCoreOps-uninstall-%d.exe", time.Now().UnixNano()))
	if os.WriteFile(tmp, data, 0o755) != nil {
		return false
	}
	args := append([]string{}, os.Args[1:]...)
	if !hasArg("/uninstall") {
		args = append(args, "/uninstall")
	}
	args = append(args, "/dir", filepath.Dir(self))
	cmd := exec.Command(tmp, args...)
	if silentRequested() {
		// тихий режим: ждём копию и возвращаем её код (для систем развёртывания)
		if err := cmd.Run(); err != nil {
			if ee, ok := err.(*exec.ExitError); ok {
				os.Exit(ee.ExitCode())
			}
			return false
		}
		os.Exit(exitOK)
	}
	return cmd.Start() == nil
}

func silentRequested() bool { return hasArg("/S", "/silent", "/quiet", "/VERYSILENT") }

// ---------- Окно ----------

func systemDark() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	v, _, err := k.GetIntegerValue("AppsUseLightTheme")
	return err == nil && v == 0
}

func pickFolder(current string) string {
	script := `Add-Type -AssemblyName System.Windows.Forms
$d = New-Object System.Windows.Forms.FolderBrowserDialog
$d.SelectedPath = ` + psQuote(current) + `
$d.ShowNewFolderButton = $true
if ($d.ShowDialog() -eq 'OK') { [Console]::OutputEncoding=[Text.Encoding]::UTF8; $d.SelectedPath }`
	u := windows.StringToUTF16(script)
	u = u[:len(u)-1]
	b := make([]byte, len(u)*2)
	for i, v := range u {
		b[2*i], b[2*i+1] = byte(v), byte(v>>8)
	}
	out, err := hidden(exec.Command("powershell.exe", "-NoProfile", "-STA", "-ExecutionPolicy", "Bypass",
		"-EncodedCommand", base64.StdEncoding.EncodeToString(b))).Output()
	if err != nil {
		return ""
	}
	p := strings.TrimSpace(string(out))
	if p == "" {
		return ""
	}
	if !strings.EqualFold(filepath.Base(p), appName) {
		p = filepath.Join(p, appName)
	}
	return p
}

func main() {
	i18n.Set(i18n.SystemLang())
	uninstallMode := hasArg("/uninstall") || strings.EqualFold(filepath.Base(os.Args[0]), uninstName)
	if uninstallMode && relaunchFromTemp() {
		return
	}
	if silentRequested() {
		os.Exit(silentMain(uninstallMode))
	}

	dark := systemDark()
	if dark {
		os.Setenv("WEBVIEW2_DEFAULT_BACKGROUND_COLOR", "FF0C0C0E")
	} else {
		os.Setenv("WEBVIEW2_DEFAULT_BACKGROUND_COLOR", "FFF2F2F4")
	}
	scale := 1.0
	if procGetDpi.Find() == nil {
		if dpi, _, _ := procGetDpi.Call(); dpi > 0 {
			scale = float64(dpi) / 96
		}
	}
	data := filepath.Join(os.TempDir(), "HealCoreOps-Setup-WebView2")
	w := webview2.NewWithOptions(webview2.WebViewOptions{
		DataPath: data,
		WindowOptions: webview2.WindowOptions{
			Title: i18n.Pick("Установка HealCoreOps", "HealCoreOps Setup"), IconId: 1, Center: true,
			Width: uint(680 * scale), Height: uint(560 * scale),
		},
	})
	if w == nil {
		t, _ := windows.UTF16PtrFromString(i18n.Pick(
			"Не найден компонент Microsoft Edge WebView2 (входит в Windows 10/11).\nОбновите Windows через Центр обновления и запустите установку снова.",
			"Microsoft Edge WebView2 was not found (it ships with Windows 10/11).\nUpdate Windows via Windows Update and run setup again."))
		c, _ := windows.UTF16PtrFromString(appName)
		windows.MessageBox(0, t, c, windows.MB_ICONERROR)
		return
	}
	defer w.Destroy()
	hwnd := uintptr(w.Window())
	if dark && procDwmAttr.Find() == nil {
		v := int32(1)
		procDwmAttr.Call(hwnd, 20, uintptr(unsafe.Pointer(&v)), 4)
	}
	w.SetSize(int(680*scale), int(560*scale), webview2.HintFixed)

	installedAt := installedDir()
	_ = w.Bind("hcoInfo", func() map[string]any {
		dir := installedAt
		if dir == "" {
			dir = defaultDir(false)
		}
		return map[string]any{
			"lang": i18n.Get(), "version": appVersion, "dir": dir, "admin": isElevated(), "mode": map[bool]string{true: "uninstall", false: "install"}[uninstallMode],
			"installed": installedAt != "", "installedVersion": installedVersion(), "size": len(payload),
		}
	})
	_ = w.Bind("hcoPick", func(cur string) string { return pickFolder(cur) })
	start := func(fn func() error) {
		pmu.Lock()
		prog = progress{}
		pmu.Unlock()
		go func() { finish(fn()) }()
	}
	_ = w.Bind("hcoInstall", func(dir string, desktop, startMenu bool) {
		_, all := installedScope()
		start(func() error { return install(dir, desktop, startMenu, all && isElevated()) })
	})
	_ = w.Bind("hcoUninstall", func(removeData bool) {
		start(func() error { return uninstall(removeData) })
	})
	_ = w.Bind("hcoProgress", func() progress {
		pmu.Lock()
		defer pmu.Unlock()
		return prog
	})
	_ = w.Bind("hcoLaunch", func(dir string) {
		_ = exec.Command(filepath.Join(dir, exeName)).Start()
	})
	_ = w.Bind("hcoClose", func() { w.Dispatch(w.Terminate) })

	icon := "data:image/png;base64," + base64.StdEncoding.EncodeToString(iconPNG)
	w.SetHtml(strings.Replace(pageHTML, "{{ICON}}", icon, 1))
	w.Run()
}
