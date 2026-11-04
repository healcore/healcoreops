//go:build windows

package main

import (
	"os"
	"path/filepath"
	"runtime"
	"time"
	"unsafe"

	webview2 "github.com/jchv/go-webview2"
	"golang.org/x/sys/windows"

	"github.com/healcore/healcoreops/internal/autostart"
	"github.com/healcore/healcoreops/internal/core"
	"github.com/healcore/healcoreops/internal/i18n"
)

// Окно WebView2 должно жить в том же системном потоке, где создано.
func init() { runtime.LockOSThread() }

var (
	user32            = windows.NewLazySystemDLL("user32.dll")
	dwmapi            = windows.NewLazySystemDLL("dwmapi.dll")
	procGetDpi        = user32.NewProc("GetDpiForSystem")
	procMetrics       = user32.NewProc("GetSystemMetrics")
	procFindWindow    = user32.NewProc("FindWindowW")
	procShowWindow    = user32.NewProc("ShowWindow")
	procSetForeground = user32.NewProc("SetForegroundWindow")
	procSetWindowPos  = user32.NewProc("SetWindowPos")
	procDwmSetAttr    = dwmapi.NewProc("DwmSetWindowAttribute")
)

const windowTitle = "HealCoreOps"

func hasArg(flag string) bool {
	for _, a := range os.Args[1:] {
		if a == flag {
			return true
		}
	}
	return false
}

func focusExisting() {
	cls, _ := windows.UTF16PtrFromString("webview")
	title, _ := windows.UTF16PtrFromString(windowTitle)
	hwnd, _, _ := procFindWindow.Call(uintptr(unsafe.Pointer(cls)), uintptr(unsafe.Pointer(title)))
	if hwnd != 0 {
		procShowWindow.Call(hwnd, 9) // SW_RESTORE
		procSetForeground.Call(hwnd)
	}
}

// handoffToRunningInstance обеспечивает единственный экземпляр.
// Обычный повторный запуск выводит на передний план уже открытое окно и выходит.
// Перезапуск от имени администратора (--relaunch) наоборот ждёт, пока прежняя
// копия закроется и освободит мьютекс, после чего запускается сам.
func handoffToRunningInstance() bool {
	name, _ := windows.UTF16PtrFromString("Local\\HealCoreOps.SingleInstance")
	relaunch := hasArg("--relaunch")
	deadline := time.Now().Add(15 * time.Second)
	for {
		h, err := windows.CreateMutex(nil, false, name)
		if err == nil {
			// Создали заново — мы единственный экземпляр, держим мьютекс до выхода.
			if relaunch {
				time.Sleep(600 * time.Millisecond) // дать прежней копии освободить WebView2
			}
			return false
		}
		// ERROR_ALREADY_EXISTS — экземпляр уже есть; ACCESS_DENIED — есть, но с
		// более высокими правами (администратор) и недоступен обычному процессу.
		if err == windows.ERROR_ALREADY_EXISTS || err == windows.ERROR_ACCESS_DENIED {
			if h != 0 {
				windows.CloseHandle(h)
			}
			if relaunch && time.Now().Before(deadline) {
				time.Sleep(150 * time.Millisecond) // ждём выхода прежней копии
				continue
			}
			if relaunch {
				return false // прежняя не вышла вовремя — всё равно запускаемся
			}
			focusExisting()
			return true
		}
		return false
	}
}

func setDarkTitle(hwnd uintptr, dark bool) {
	if procDwmSetAttr.Find() != nil {
		return
	}
	var v int32
	if dark {
		v = 1
	}
	// 20 — DWMWA_USE_IMMERSIVE_DARK_MODE (Windows 10 20H1+), 19 — ранние сборки.
	if r, _, _ := procDwmSetAttr.Call(hwnd, 20, uintptr(unsafe.Pointer(&v)), 4); r != 0 {
		procDwmSetAttr.Call(hwnd, 19, uintptr(unsafe.Pointer(&v)), 4)
	}
	// SWP_NOMOVE|SWP_NOSIZE|SWP_NOZORDER|SWP_FRAMECHANGED — перерисовать рамку.
	procSetWindowPos.Call(hwnd, 0, 0, 0, 0, 0, 0x0002|0x0001|0x0004|0x0020)
}

func runHost(c *core.Core, url string) {
	dark := c.Config().Theme == "dark"
	// Прозрачный фон WebView2: при включённом размытии подложка видна сквозь
	// полупрозрачные панели; в обычном режиме фон рисует сам HTML (непрозрачно).
	os.Setenv("WEBVIEW2_DEFAULT_BACKGROUND_COLOR", "00FFFFFF")

	scale := 1.0
	if procGetDpi.Find() == nil {
		if dpi, _, _ := procGetDpi.Call(); dpi > 0 {
			scale = float64(dpi) / 96
		}
	}
	sw, _, _ := procMetrics.Call(0) // SM_CXSCREEN
	sh, _, _ := procMetrics.Call(1) // SM_CYSCREEN
	w := uint(1380 * scale)
	h := uint(900 * scale)
	if sw > 0 && w > uint(float64(sw)*0.94) {
		w = uint(float64(sw) * 0.94)
	}
	if sh > 0 && h > uint(float64(sh)*0.90) {
		h = uint(float64(sh) * 0.90)
	}

	data := filepath.Join(os.Getenv("LOCALAPPDATA"), "HealCoreOps", "WebView2")
	_ = os.MkdirAll(data, 0o755)

	wv := webview2.NewWithOptions(webview2.WebViewOptions{
		DataPath:  data,
		AutoFocus: true,
		WindowOptions: webview2.WindowOptions{
			Title: windowTitle, Width: w, Height: h, IconId: 1, Center: true,
		},
	})
	if wv == nil {
		noWebView2()
		return
	}
	defer wv.Destroy()

	hwnd := uintptr(wv.Window())
	setDarkTitle(hwnd, dark)

	// Фоновый режим: значок в трее, сворачивание вместо закрытия, уведомления.
	cfg := c.Config()
	closeTray = func() bool { return c.Config().CloseToTray }
	trayCore = c
	addTray(hwnd)
	subclass(hwnd)
	_ = autostart.SetSelfStartup(cfg.RunAtStartup) // сверяем запись автозапуска

	c.OnTheme = func(d bool) {
		wv.Dispatch(func() {
			setDarkTitle(hwnd, d)
			if cc := c.Config(); cc.Acrylic {
				applyBackdrop(hwnd, true, cc.Backdrop, d) // перекрасить фон под новую тему
			}
		})
	}
	c.OnQuit = func() { wv.Dispatch(requestQuit) }
	c.OnNotify = func(title, text string) { wv.Dispatch(func() { notifyBalloon(title, text) }) }
	// Системный фон (Mica / Acrylic) рисует Windows; ползунок плотности
	// меняет только затемнение интерфейса поверх него (в CSS).
	if b := windowsBuild(); b >= 22000 {
		c.Backdrops = []string{"mica", "acrylic"}
	} else if b >= 17134 {
		c.Backdrops = []string{"acrylic"}
	}
	c.OnOpacity = func(int) {}
	c.OnAcrylic = func(bool) {
		wv.Dispatch(func() {
			cc := c.Config()
			applyBackdrop(hwnd, cc.Acrylic, cc.Backdrop, cc.Theme == "dark")
		})
	}
	if cfg.Acrylic {
		applyBackdrop(hwnd, true, cfg.Backdrop, dark)
	}
	// Запуск из автозагрузки с ключом --minimized: сразу в трей.
	if minimizedStart() && cfg.CloseToTray {
		procShowWindow.Call(hwnd, swHide)
	}

	wv.SetSize(int(980*scale), int(640*scale), webview2.HintMin)
	wv.Navigate(url)
	wv.Run()
	removeTray()
}

func minimizedStart() bool {
	for _, a := range os.Args[1:] {
		if a == "--minimized" || a == "/minimized" {
			return true
		}
	}
	return false
}

func noWebView2() {
	text := i18n.Pick(
		"Для работы HealCoreOps нужен компонент Microsoft Edge WebView2 (входит в Windows 10/11).\n\nОткрыть страницу загрузки?",
		"HealCoreOps requires Microsoft Edge WebView2 (included in Windows 10/11).\n\nOpen the download page?")
	t, _ := windows.UTF16PtrFromString(text)
	cp, _ := windows.UTF16PtrFromString("HealCoreOps")
	if r, _ := windows.MessageBox(0, t, cp, windows.MB_ICONWARNING|windows.MB_YESNO); r == 6 { // IDYES
		verb, _ := windows.UTF16PtrFromString("open")
		link, _ := windows.UTF16PtrFromString("https://go.microsoft.com/fwlink/p/?LinkId=2124703")
		_ = windows.ShellExecute(0, verb, link, nil, nil, windows.SW_SHOWNORMAL)
	}
}

func fatal(msg string) {
	t, _ := windows.UTF16PtrFromString(msg)
	cp, _ := windows.UTF16PtrFromString("HealCoreOps")
	windows.MessageBox(0, t, cp, windows.MB_ICONERROR)
	os.Exit(1)
}

