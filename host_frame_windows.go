//go:build windows

package main

import "unsafe"

// applyAppearance — тёмная или светлая рамка окна и цвет заголовка под шапку программы.
func applyAppearance(hwnd uintptr, dark bool) {
	if procDwmSetAttr.Find() != nil {
		return
	}
	v := int32(0)
	if dark {
		v = 1
	}
	// 20 — DWMWA_USE_IMMERSIVE_DARK_MODE (Windows 10 20H1+ и 11), 19 — ранние сборки Windows 10.
	if r, _, _ := procDwmSetAttr.Call(hwnd, 20, uintptr(unsafe.Pointer(&v)), 4); r != 0 {
		procDwmSetAttr.Call(hwnd, 19, uintptr(unsafe.Pointer(&v)), 4)
	}
	// Цвет заголовка (Windows 11) — как у шапки программы. COLORREF: 00BBGGRR.
	caption := uint32(0x00181515) // #151518
	if !dark {
		caption = 0x00FFFFFF
	}
	procDwmSetAttr.Call(hwnd, 35 /* DWMWA_CAPTION_COLOR */, uintptr(unsafe.Pointer(&caption)), 4)
	// SWP_NOMOVE|SWP_NOSIZE|SWP_NOZORDER|SWP_NOACTIVATE|SWP_FRAMECHANGED — перерисовать рамку.
	procSetWindowPos.Call(hwnd, 0, 0, 0, 0, 0, 0x0002|0x0001|0x0004|0x0010|0x0020)
}
