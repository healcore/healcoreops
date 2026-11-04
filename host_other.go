//go:build !windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"syscall"

	"github.com/healcore/healcoreops/internal/core"
)

func handoffToRunningInstance() bool { return false }

// runHost открывает интерфейс в отдельном окне браузера и ждёт завершения.
func runHost(c *core.Core, url string) {
	fmt.Println("HealCoreOps:", url)
	c.OnQuit = func() { os.Exit(0) }
	if os.Getenv("HEALCOREOPS_NO_BROWSER") == "" {
		openWindow(url)
	}
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig
}

func openWindow(url string) {
	// Режим --app даёт окно без вкладок и адресной строки — как у отдельного приложения.
	for _, b := range []string{"google-chrome", "chromium", "chromium-browser", "microsoft-edge", "brave-browser"} {
		if p, err := exec.LookPath(b); err == nil {
			if exec.Command(p, "--app="+url, "--window-size=1380,900").Start() == nil {
				return
			}
		}
	}
	if runtime.GOOS == "darwin" {
		_ = exec.Command("open", url).Start()
		return
	}
	_ = exec.Command("xdg-open", url).Start()
}

func fatal(msg string) {
	fmt.Fprintln(os.Stderr, msg)
	os.Exit(1)
}
