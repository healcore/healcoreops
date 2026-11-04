//go:build windows

package i18n

import "golang.org/x/sys/windows"

var procGetUserDefaultUILanguage = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetUserDefaultUILanguage")

func systemLang() string {
	r, _, _ := procGetUserDefaultUILanguage.Call()
	if r&0x3FF == 0x19 { // LANG_RUSSIAN
		return "ru"
	}
	return "en"
}
