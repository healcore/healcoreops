//go:build windows

package main

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// Эффект размытия «как в Windows 11» через SetWindowCompositionAttribute
// (ACCENT_ENABLE_ACRYLICBLURBEHIND). Работает и на Windows 10 (1803+),
// и на Windows 11 — единый путь, адаптированный под обе системы.

var (
	ntdll              = windows.NewLazySystemDLL("ntdll.dll")
	procRtlGetVersion  = ntdll.NewProc("RtlGetVersion")
	procSetWinCompAttr = user32.NewProc("SetWindowCompositionAttribute")
	procDwmSetAttr2    = dwmapi.NewProc("DwmSetWindowAttribute")
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

const (
	wcaAccentPolicy         = 19
	accentDisabled          = 0
	accentEnableAcrylicBlur = 4
	dwmSystemBackdropType   = 38 // Windows 11: DWMWA_SYSTEMBACKDROP_TYPE
	dwmBackdropAcrylic      = 3  // DWMSBT_TRANSIENTWINDOW
	dwmBackdropNone         = 1  // DWMSBT_NONE
)

// applyAcrylic включает или выключает размытие фона окна.
func applyAcrylic(hwnd uintptr, on, dark bool) {
	// Windows 11 22H2+ — «правильный» системный фон через DWM.
	if windowsBuild() >= 22621 && procDwmSetAttr2.Find() == nil {
		v := int32(dwmBackdropNone)
		if on {
			v = dwmBackdropAcrylic
		}
		procDwmSetAttr2.Call(hwnd, dwmSystemBackdropType, uintptr(unsafe.Pointer(&v)), 4)
	}
	// Универсальный акрил (Windows 10 и 11) через композицию.
	if procSetWinCompAttr.Find() != nil {
		return
	}
	a := accentPolicy{AccentState: accentDisabled}
	if on {
		a.AccentState = accentEnableAcrylicBlur
		a.AccentFlags = 2
		// Тон подложки: AABBGGRR. Плотная тёмная заливка — лёгкое «стекло»,
		// а не сквозное окно: сквозь блюр не читается рабочий стол под окном.
		if dark {
			a.GradientColor = 0xF4101014
		} else {
			a.GradientColor = 0xF4ECECEC
		}
	}
	data := winCompAttrData{Attribute: wcaAccentPolicy, Data: unsafe.Pointer(&a), SizeOfData: unsafe.Sizeof(a)}
	procSetWinCompAttr.Call(hwnd, uintptr(unsafe.Pointer(&data)))
}
