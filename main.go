// HealCoreOps — настольное приложение для распределения, управления
// и оптимизации ресурсов компьютера.
//
// Ядро на Go поднимает локальный сервер на 127.0.0.1 со случайным токеном,
// а интерфейс (HTML/CSS/JS) показывается в собственном окне WebView2 (Windows)
// или в отдельном окне браузера (Linux/macOS).
package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"os"

	"github.com/healcore/healcoreops/internal/core"
	"github.com/healcore/healcoreops/internal/web"
)

func main() {
	if isCLI(os.Args[1:]) {
		os.Exit(runCLI(os.Args[1:]))
	}
	if handoffToRunningInstance() {
		return
	}
	c := core.New()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fatal("не удалось открыть локальный порт: " + err.Error())
	}
	buf := make([]byte, 24)
	_, _ = rand.Read(buf)
	token := hex.EncodeToString(buf)

	srv := &http.Server{Handler: c.Handler(web.FS(), token)}
	go func() { _ = srv.Serve(ln) }()
	c.Start()

	url := fmt.Sprintf("http://127.0.0.1:%d/#%s", ln.Addr().(*net.TCPAddr).Port, token)
	runHost(c, url)
	c.SaveLoadHistory()
	os.Exit(0)
}
