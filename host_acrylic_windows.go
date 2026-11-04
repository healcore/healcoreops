//go:build windows

package main

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// Системный фон окна — так же, как у приложений Windows 11:
//
//   - Windows 11 22H2+: DWM рисует Mica или Acrylic (DWMWA_SYSTEMBACKDROP_TYPE),
//     рамка растянута на всё окно (DwmExtendFrameIntoClientArea), WebView2
//     прозрачен — интерфейс лежит прямо на системном материале. Размытие,
//     тон, шум и переход в плотный цвет у неактивного окна делает сама Windows.
//   - Windows 11 21H2: Mica через DWMWA_MICA_EFFECT.
//   - Windows 10: размытие через SetWindowCompositionAttribute (у Windows 10
//     нет Mica, поэтому всегда акрил).

var (
	ntdll              = windows.NewLazySystemDLL("ntdll.dll")
	procRtlGetVersion  = ntdll.NewProc("RtlGetVersion")
	procSetWinCompAttr = user32.NewProc("SetWindowCompositionAttribute")
	procDwmSetAttr2    = dwmapi.NewProc("DwmSetWindowAttribute")
	procDwmExtend      = dwmapi.NewProc("DwmExtendFrameIntoClientArea")
)

type osVersionInfo struct {
	dwOSVersionInfoSize uint32
	dwMajorVersion      uint32
	dwMinorVersion      uint32
	dwBuildNumber       uint32
	dwPlatformID        uint32
	szCSDVersion        [128]uint16
}

func windowsBuild() uint32 {
	var v osVersionInfo
	v.dwOSVersionInfoSize = uint32(unsafe.Sizeof(v))
	procRtlGetVersion.Call(uintptr(unsafe.Pointer(&v)))
	return v.dwBuildNumber
}

type accentPolicy struct {
	AccentState   uint32
	AccentFlags   uint32
	GradientColor uint32
	AnimationID   uint32
}

type winCompAttrData struct {
	Attribute  uint32
	Data       unsafe.Pointer
	SizeOfData uintptr
}

type dwmMargins struct{ Left, Right, Top, Bottom int32 }

const (
	wcaAccentPolicy         = 19
	accentDisabled          = 0
	accentEnableAcrylicBlur = 4
	dwmSystemBackdropType   = 38   // DWMWA_SYSTEMBACKDROP_TYPE (22H2+)
	dwmMicaEffect           = 1029 // DWMWA_MICA_EFFECT (21H2)
	dwmBackdropNone         = 1    // DWMSBT_NONE
	dwmBackdropMica         = 2    // DWMSBT_MAINWINDOW
	dwmBackdropAcrylic      = 3    // DWMSBT_TRANSIENTWINDOW
)

func dwmSet(hwnd uintptr, attr uint32, v int32) {
	procDwmSetAttr2.Call(hwnd, uintptr(attr), uintptr(unsafe.Pointer(&v)), 4)
}

func extendFrame(hwnd uintptr, on bool) {
	if procDwmExtend.Find() != nil {
		return
	}
	m := dwmMargins{}
	if on {
		m = dwmMargins{-1, -1, -1, -1} // «лист стекла» на всё окно
	}
	procDwmExtend.Call(hwnd, uintptr(unsafe.Pointer(&m)))
}

func setAccent(hwnd uintptr, on, dark bool) {
	if procSetWinCompAttr.Find() != nil {
		return
	}
	a := accentPolicy{AccentState: accentDisabled}
	if on {
		a.AccentState = accentEnableAcrylicBlur
		a.AccentFlags = 2
		// AABBGGRR: нейтральный серый тон, как у акрила Windows 11.
		if dark {
			a.GradientColor = 0x99202020
		} else {
			a.GradientColor = 0x99F3F3F3
		}
	}
	data := winCompAttrData{Attribute: wcaAccentPolicy, Data: unsafe.Pointer(&a), SizeOfData: unsafe.Sizeof(a)}
	procSetWinCompAttr.Call(hwnd, uintptr(unsafe.Pointer(&data)))
}

// applyBackdrop включает или выключает системный фон окна. Тон (тёмный или
// светлый) Windows берёт из DWMWA_USE_IMMERSIVE_DARK_MODE (setDarkTitle).
func applyBackdrop(hwnd uintptr, on bool, kind string, dark bool) {
	b := windowsBuild()
	switch {
	case b >= 22621 && procDwmSetAttr2.Find() == nil:
		setAccent(hwnd, false, dark) // старый акрил Windows 10 мешает родному — снять
		v := int32(dwmBackdropNone)
		if on {
			v = dwmBackdropAcrylic
			if kind == "mica" {
				v = dwmBackdropMica
			}
		}
		extendFrame(hwnd, on)
		dwmSet(hwnd, dwmSystemBackdropType, v)
	case b >= 22000 && kind == "mica" && procDwmSetAttr2.Find() == nil:
		setAccent(hwnd, false, dark)
		extendFrame(hwnd, on)
		v := int32(0)
		if on {
			v = 1
		}
		dwmSet(hwnd, dwmMicaEffect, v)
	default:
		if b >= 22000 && procDwmSetAttr2.Find() == nil {
			dwmSet(hwnd, dwmMicaEffect, 0)
			extendFrame(hwnd, false)
		}
		setAccent(hwnd, on, dark)
	}
}
