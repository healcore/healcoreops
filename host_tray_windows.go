//go:build windows

package main

import (
	"fmt"
	"sync/atomic"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/healcore/healcoreops/internal/core"
	"github.com/healcore/healcoreops/internal/ctl"
	"github.com/healcore/healcoreops/internal/i18n"
	"github.com/healcore/healcoreops/internal/sys"
)

var (
	shell32            = windows.NewLazySystemDLL("shell32.dll")
	kernel32t          = windows.NewLazySystemDLL("kernel32.dll")
	procShellNotify    = shell32.NewProc("Shell_NotifyIconW")
	procSetWindowLong  = user32.NewProc("SetWindowLongPtrW")
	procGetWindowLong  = user32.NewProc("GetWindowLongPtrW")
	procCallWindowProc = user32.NewProc("CallWindowProcW")
	procSetLayered     = user32.NewProc("SetLayeredWindowAttributes")
	procCreatePopup    = user32.NewProc("CreatePopupMenu")
	procAppendMenu     = user32.NewProc("AppendMenuW")
	procTrackPopup     = user32.NewProc("TrackPopupMenu")
	procDestroyMenu    = user32.NewProc("DestroyMenu")
	procGetCursorPos   = user32.NewProc("GetCursorPos")
	procPostMessage    = user32.NewProc("PostMessageW")
	procLoadImage      = user32.NewProc("LoadImageW")
	procGetModuleH     = kernel32t.NewProc("GetModuleHandleW")
)

const (
	wmClose       = 0x0010
	wmTrayCB      = 0x8000 + 1 // WM_APP+1
	gwlExStyle    = -20
	gwlpWndProc   = -4
	wsExLayered   = 0x00080000
	lwaAlpha      = 0x02
	swHide        = 0
	swShow        = 5
	swRestore     = 9
	nimAdd        = 0
	nimModify     = 1
	nimDelete     = 2
	nifMessage    = 0x01
	nifIcon       = 0x02
	nifTip        = 0x04
	nifInfo       = 0x10
	imageIcon     = 1
	lrDefaultSize = 0x40
	lrShared      = 0x8000
	mfString      = 0x00
	mfSeparator   = 0x800
	tpmReturnCmd  = 0x0100
	tpmRightBtn   = 0x0002
	cmdOpen       = 1
	cmdExit       = 2
	cmdTrim       = 3
	cmdClean      = 4
	cmdGame       = 5
	cmdPower      = 10 // +0..2 — профили питания
	mfPopup       = 0x10
	mfChecked     = 0x08
)

type notifyIconData struct {
	CbSize           uint32
	HWnd             uintptr
	UID              uint32
	UFlags           uint32
	UCallbackMessage uint32
	HIcon            uintptr
	SzTip            [128]uint16
	DwState          uint32
	DwStateMask      uint32
	SzInfo           [256]uint16
	UVersion         uint32
	SzInfoTitle      [64]uint16
	DwInfoFlags      uint32
	GuidItem         windows.GUID
	HBalloonIcon     uintptr
}

type pointT struct{ X, Y int32 }

// nIndex переводит отрицательный индекс GWL_* в uintptr в рантайме
// (прямая конвертация константы вызывает переполнение при компиляции).
func nIndex(i int) uintptr { return uintptr(int32(i)) }

var (
	trayHwnd  uintptr
	trayIcon  uintptr
	origProc  uintptr
	quitting  int32
	closeTray func() bool
)

func copyU16(dst []uint16, s string) {
	src := windows.StringToUTF16(s)
	n := copy(dst, src)
	if n < len(dst) {
		dst[n] = 0
	} else {
		dst[len(dst)-1] = 0
	}
}

func loadAppIcon() uintptr {
	hinst, _, _ := procGetModuleH.Call(0)
	ic, _, _ := procLoadImage.Call(hinst, 1, imageIcon, 0, 0, lrDefaultSize|lrShared)
	return ic
}

func baseNID() notifyIconData {
	nid := notifyIconData{HWnd: trayHwnd, UID: 1}
	nid.CbSize = uint32(unsafe.Sizeof(nid))
	return nid
}

// addTray добавляет значок в область уведомлений.
func addTray(hwnd uintptr) {
	trayHwnd = hwnd
	trayIcon = loadAppIcon()
	nid := baseNID()
	nid.UFlags = nifMessage | nifIcon | nifTip
	nid.UCallbackMessage = wmTrayCB
	nid.HIcon = trayIcon
	copyU16(nid.SzTip[:], "HealCoreOps")
	procShellNotify.Call(nimAdd, uintptr(unsafe.Pointer(&nid)))
}

func removeTray() {
	if trayHwnd == 0 {
		return
	}
	nid := baseNID()
	procShellNotify.Call(nimDelete, uintptr(unsafe.Pointer(&nid)))
	trayHwnd = 0
}

// notifyBalloon показывает всплывающее уведомление из значка в трее.
func notifyBalloon(title, text string) {
	if trayHwnd == 0 {
		return
	}
	nid := baseNID()
	nid.UFlags = nifInfo
	copyU16(nid.SzInfoTitle[:], title)
	copyU16(nid.SzInfo[:], text)
	procShellNotify.Call(nimModify, uintptr(unsafe.Pointer(&nid)))
}

func showMainWindow() {
	if trayHwnd == 0 {
		return
	}
	procShowWindow.Call(trayHwnd, swShow)
	procShowWindow.Call(trayHwnd, swRestore)
	procSetForeground.Call(trayHwnd)
	setVisible(true)
}

// setVisible сообщает ядру, видно ли окно: в трее и свёрнутым программа опрашивает систему реже.
func setVisible(v bool) {
	if trayCore != nil {
		trayCore.SetWindowVisible(v)
	}
}

// requestQuit закрывает приложение по-настоящему (минуя сворачивание в трей).
func requestQuit() {
	atomic.StoreInt32(&quitting, 1)
	hwnd := trayHwnd // removeTray обнуляет trayHwnd, поэтому запоминаем окно заранее
	removeTray()
	if hwnd != 0 {
		procPostMessage.Call(hwnd, wmClose, 0, 0)
	}
}

func appendItem(menu uintptr, flags uint32, id uintptr, text string) {
	p := windows.StringToUTF16Ptr(text)
	procAppendMenu.Call(menu, uintptr(flags), id, uintptr(unsafe.Pointer(p)))
}

func showTrayMenu() {
	menu, _, _ := procCreatePopup.Call()
	if menu == 0 {
		return
	}
	defer procDestroyMenu.Call(menu)
	P := i18n.Pick
	appendItem(menu, mfString, cmdOpen, P("Открыть HealCoreOps", "Open HealCoreOps"))
	procAppendMenu.Call(menu, mfSeparator, 0, 0)
	if trayCore != nil {
		appendItem(menu, mfString, cmdTrim, P("Освободить память", "Free up memory"))
		appendItem(menu, mfString, cmdClean, P("Быстрая очистка мусора", "Quick junk cleanup"))
		var gf uint32
		if trayCore.Config().GameMode {
			gf = mfChecked
		}
		appendItem(menu, mfString|gf, cmdGame, P("Игровой режим", "Game mode"))
		sub, _, _ := procCreatePopup.Call()
		cur, _ := ctl.CurrentPowerProfile()
		for i, n := range ctl.PowerNames {
			var f uint32
			if int(cur) == i {
				f = mfChecked
			}
			appendItem(sub, mfString|f, uintptr(cmdPower+i), i18n.T(n))
		}
		appendItem(menu, mfPopup, sub, P("Профиль питания", "Power profile"))
		procAppendMenu.Call(menu, mfSeparator, 0, 0)
	}
	appendItem(menu, mfString, cmdExit, P("Выход", "Exit"))
	var pt pointT
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	procSetForeground.Call(trayHwnd)
	cmd, _, _ := procTrackPopup.Call(menu, tpmReturnCmd|tpmRightBtn, uintptr(pt.X), uintptr(pt.Y), 0, trayHwnd, 0)
	c := trayCore
	switch {
	case cmd == cmdOpen:
		showMainWindow()
	case cmd == cmdExit:
		requestQuit()
	case c == nil:
	case cmd == cmdTrim:
		go func() {
			if msg, err := c.TrimMemory(); err == nil {
				trayNotify(msg)
			}
		}()
	case cmd == cmdClean:
		go func() {
			r, _ := c.QuickClean()
			trayNotify(fmt.Sprintf(P("Очистка: освобождено %s", "Cleanup: %s freed"), sys.Bytes(uint64(r.Freed))))
		}()
	case cmd == cmdGame:
		on := c.ToggleGameMode()
		trayNotify(map[bool]string{true: P("Игровой режим включён", "Game mode on"), false: P("Игровой режим выключен", "Game mode off")}[on])
	case cmd >= cmdPower && cmd < cmdPower+3:
		go func() {
			if err := c.SetPower(ctl.PowerProfile(cmd - cmdPower)); err != nil {
				trayNotify(err.Error())
			} else {
				trayNotify(P("Профиль питания: ", "Power profile: ") + i18n.T(ctl.PowerNames[cmd-cmdPower]))
			}
		}()
	}
}

// trayCore — ядро для быстрых действий из меню трея.
var trayCore *core.Core

func trayNotify(text string) {
	if trayCore != nil && trayCore.OnNotify != nil {
		trayCore.OnNotify("HealCoreOps", text)
	}
}

func trayWndProc(hwnd, msg, wparam, lparam uintptr) uintptr {
	switch msg {
	case wmTrayCB:
		switch lparam & 0xFFFF {
		case 0x0202, 0x0203: // WM_LBUTTONUP, WM_LBUTTONDBLCLK
			showMainWindow()
		case 0x0205: // WM_RBUTTONUP
			showTrayMenu()
		}
		return 0
	case wmClose:
		if atomic.LoadInt32(&quitting) == 0 && closeTray != nil && closeTray() {
			procShowWindow.Call(hwnd, swHide)
			setVisible(false)
			return 0
		}
	case 0x0005: // WM_SIZE: 1 — свёрнуто, 0/2 — обычное или развёрнутое
		if wparam == 1 {
			setVisible(false)
		} else if wparam == 0 || wparam == 2 {
			setVisible(true)
		}
	}
	r, _, _ := procCallWindowProc.Call(origProc, hwnd, msg, wparam, lparam)
	return r
}

func subclass(hwnd uintptr) {
	cb := windows.NewCallback(trayWndProc)
	origProc, _, _ = procSetWindowLong.Call(hwnd, nIndex(gwlpWndProc), cb)
}
