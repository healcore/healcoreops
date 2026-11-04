//go:build windows

package main

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// Прозрачность «как в Windows»: за содержимым окна — системный Acrylic (размытие
// того, что позади окна), а текст и элементы интерфейса остаются чёткими. Окно лишь
// включает эффект с лёгким тоном; насколько он просвечивает, решает страница — её
// фоны полупрозрачны (цвета темы сохраняются), текст нет.
//
//   - Windows 11 22H2+: DWMWA_SYSTEMBACKDROP_TYPE = Acrylic + рамка на всё окно.
//   - Windows 10 (1803+) и Windows 11 21H2: Acrylic через политику оформления
//     окна (SetWindowCompositionAttribute), как у меню «Пуск» Windows 10. На время
//     перетаскивания — лёгкое размытие: у Acrylic в Windows 10 известны подтормаживания.
//
// Подход перенесён из healcore-project (github.com/healcore/healcore-project).

var (
	ntdll              = windows.NewLazySystemDLL("ntdll.dll")
	procRtlGetVersion  = ntdll.NewProc("RtlGetVersion")
	procSetWinCompAttr = user32.NewProc("SetWindowCompositionAttribute")
	procDwmExtend      = dwmapi.NewProc("DwmExtendFrameIntoClientArea")

	buildCache   uint32
	glassApplied = -1
	darkApplied  = -1
	accentActive bool
	glassHwnd    uintptr
)

func windowsBuild() uint32 {
	if buildCache == 0 {
		var v struct {
			size, major, minor, build, platform uint32
			csd                                 [128]uint16
		}
		v.size = uint32(unsafe.Sizeof(v))
		procRtlGetVersion.Call(uintptr(unsafe.Pointer(&v)))
		buildCache = v.build
	}
	return buildCache
}

type dwmMargins struct{ Left, Right, Top, Bottom int32 }

// Системный Acrylic Windows 11 22H2+ (официальный API).
func modernBackdrop() bool { return windowsBuild() >= 22621 }

// glassSupported — прозрачность доступна на этой системе.
func glassSupported() bool { return modernBackdrop() || procSetWinCompAttr.Find() == nil }

const (
	accentDisabled      = 0
	accentBlurBehind    = 3 // лёгкое размытие (все Windows 10)
	accentAcrylicBehind = 4 // Acrylic (Windows 10 1803+)
)

// setAccent — политика оформления окна для Windows 10: эффект и его тон (AABBGGRR).
func setAccent(state uint32, dark bool) {
	if procSetWinCompAttr.Find() != nil || glassHwnd == 0 {
		return
	}
	tint := uint32(0x300E0C0C) // тёмный тон ~19 %: основную подложку даёт страница
	if !dark {
		tint = 0x30F4F2F2
	}
	accent := struct{ state, flags, color, anim uint32 }{state: state, color: tint}
	if state == accentAcrylicBehind {
		accent.flags = 2 // учитывать тон
	}
	data := struct {
		attrib uint32
		data   unsafe.Pointer
		size   uintptr
	}{19 /* WCA_ACCENT_POLICY */, unsafe.Pointer(&accent), unsafe.Sizeof(accent)}
	procSetWinCompAttr.Call(glassHwnd, uintptr(unsafe.Pointer(&data)))
}

func acrylicState() uint32 {
	if windowsBuild() >= 17134 { // Windows 10 1803
		return accentAcrylicBehind
	}
	return accentBlurBehind
}

func setGlass(on, dark bool) {
	if modernBackdrop() {
		m := dwmMargins{}
		if on {
			m = dwmMargins{-1, -1, -1, -1} // «лист стекла»: фон окна прозрачен для эффекта
		}
		procDwmExtend.Call(glassHwnd, uintptr(unsafe.Pointer(&m)))
		t := int32(1) // DWMSBT_NONE
		if on {
			t = 3 // DWMSBT_TRANSIENTWINDOW — Acrylic
		}
		procDwmSetAttr.Call(glassHwnd, 38 /* DWMWA_SYSTEMBACKDROP_TYPE */, uintptr(unsafe.Pointer(&t)), 4)
		return
	}
	if on {
		setAccent(acrylicState(), dark)
	} else {
		setAccent(accentDisabled, dark)
	}
	accentActive = on
}

// onSizeMove — WM_ENTERSIZEMOVE / WM_EXITSIZEMOVE: в Windows 10 на время
// перетаскивания лёгкое размытие вместо Acrylic, чтобы окно не подтормаживало.
func onSizeMove(start bool) {
	if !accentActive || acrylicState() != accentAcrylicBehind {
		return
	}
	if start {
		setAccent(accentBlurBehind, darkApplied == 1)
	} else {
		setAccent(accentAcrylicBehind, darkApplied == 1)
	}
}

// applyAppearance — прозрачность фона (opacity — непрозрачность фона, 20–100 %)
// и тёмная/светлая рамка окна.
func applyAppearance(hwnd uintptr, opacity int, dark bool) {
	glassHwnd = hwnd
	glass := 0
	if opacity > 0 && opacity < 100 && glassSupported() {
		glass = 1
	}
	d := 0
	if dark {
		d = 1
	}
	changed := false
	if d != darkApplied {
		v := int32(d)
		// Тёмный заголовок: 20 — Windows 10 20H1+ и Windows 11, 19 — Windows 10 1809–1909.
		procDwmSetAttr.Call(hwnd, 20, uintptr(unsafe.Pointer(&v)), 4)
		if windowsBuild() < 19041 {
			procDwmSetAttr.Call(hwnd, 19, uintptr(unsafe.Pointer(&v)), 4)
		}
		darkApplied, changed = d, true
		if accentActive {
			setAccent(acrylicState(), dark) // тон Acrylic под новую тему
		}
	}
	if glass != glassApplied {
		setGlass(glass == 1, dark)
		glassApplied, changed = glass, true
	}
	if changed {
		// Цвет заголовка (Windows 11): при Acrylic — системный, иначе — под шапку программы.
		caption := uint32(0xFFFFFFFF) // DWMWA_COLOR_DEFAULT
		if glass == 0 || !modernBackdrop() {
			caption = 0x00181515 // #151518 (COLORREF 00BBGGRR)
			if !dark {
				caption = 0x00FFFFFF
			}
		}
		procDwmSetAttr.Call(hwnd, 35 /* DWMWA_CAPTION_COLOR */, uintptr(unsafe.Pointer(&caption)), 4)
		// SWP_NOMOVE|SWP_NOSIZE|SWP_NOZORDER|SWP_NOACTIVATE|SWP_FRAMECHANGED
		procSetWindowPos.Call(hwnd, 0, 0, 0, 0, 0, 0x0002|0x0001|0x0004|0x0010|0x0020)
	}
}
