//go:build windows

package main

import (
	"io"
	"os"

	"golang.org/x/sys/windows"
)

// attachConsole подключается к консоли, из которой запущена программа
// (exe собран как оконное приложение и своей консоли не имеет).
func attachConsole() io.Writer {
	k := windows.NewLazySystemDLL("kernel32.dll")
	if r, _, _ := k.NewProc("AttachConsole").Call(uintptr(^uint32(0))); r == 0 { // ATTACH_PARENT_PROCESS
		return nil
	}
	f, err := os.OpenFile("CONOUT$", os.O_WRONLY, 0)
	if err != nil {
		return nil
	}
	os.Stdout, os.Stderr = f, f
	_, _ = f.WriteString("\r\n")
	// UTF-8 для кириллицы
	_ = windows.SetConsoleOutputCP(65001)
	return f
}
