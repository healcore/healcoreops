//go:build windows

package maintenance

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unicode/utf16"

	"github.com/healcore/healcoreops/internal/ctl"
	"github.com/healcore/healcoreops/internal/i18n"
)

var errBusy = errors.New("busy")

func init() {
	errBusy = errors.New(i18n.T("дождитесь завершения текущей операции"))
}

// Supported — обслуживание доступно в Windows.
func Supported() bool { return true }

type actionDef struct {
	id, group        string
	ru, en, dru, den string
	admin            bool
	mins             int
	fn               func(emit func(string)) error
}

func hidden(c *exec.Cmd) *exec.Cmd {
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	return c
}

// runCmd выполняет команду, сообщая о старте и результате; текст вывода
// самих утилит не показываем (разные кодовые страницы), только итог.
func runCmd(emit func(string), label string, timeout time.Duration, name string, args ...string) error {
	emit("▸ " + label)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := hidden(exec.CommandContext(ctx, name, args...))
	err := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		emit("× " + i18n.T("превышено время ожидания"))
		return errors.New(i18n.T("превышено время ожидания"))
	}
	if err != nil {
		code := -1
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		}
		emit(fmt.Sprintf("× %s (%s %d)", i18n.T("ошибка"), i18n.T("код"), code))
		return err
	}
	emit("✓ " + i18n.T("готово"))
	return nil
}

func powershell(emit func(string), label, script string, timeout time.Duration) error {
	emit("▸ " + label)
	full := "$ProgressPreference='SilentlyContinue'\n[Console]::OutputEncoding=[Text.Encoding]::UTF8\n" + script
	u := utf16.Encode([]rune(full))
	b := make([]byte, len(u)*2)
	for i, v := range u {
		b[2*i], b[2*i+1] = byte(v), byte(v>>8)
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := hidden(exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive",
		"-ExecutionPolicy", "Bypass", "-EncodedCommand", base64.StdEncoding.EncodeToString(b)))
	out, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		emit("× " + i18n.T("превышено время ожидания"))
		return errors.New(i18n.T("превышено время ожидания"))
	}
	if err != nil {
		if s := strings.TrimSpace(string(out)); s != "" {
			emit("× " + lastLine(s))
		}
		return err
	}
	emit("✓ " + i18n.T("готово"))
	return nil
}

func lastLine(s string) string {
	ls := strings.Split(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(ls[len(ls)-1])
}

// setDNS задаёт DNS-серверы на всех активных сетевых адаптерах.
func setDNS(name string, servers ...string) func(func(string)) error {
	return func(emit func(string)) error {
		var script string
		if len(servers) == 0 {
			script = `Get-DnsClientServerAddress -AddressFamily IPv4 | Where-Object {$_.ServerAddresses} | ForEach-Object { Set-DnsClientServerAddress -InterfaceIndex $_.InterfaceIndex -ResetServerAddresses }; Clear-DnsClientCache`
		} else {
			list := "'" + strings.Join(servers, "','") + "'"
			script = `$up = Get-NetAdapter | Where-Object {$_.Status -eq 'Up'}
foreach($a in $up){ Set-DnsClientServerAddress -InterfaceIndex $a.ifIndex -ServerAddresses (` + list + `) }
Clear-DnsClientCache`
		}
		return powershell(emit, name, script, 60*time.Second)
	}
}

func wuReset(emit func(string)) error {
	for _, s := range []string{"wuauserv", "bits", "cryptsvc", "msiserver"} {
		_ = runCmd(emit, i18n.Pick("Остановка службы ", "Stopping service ")+s, 30*time.Second, "net", "stop", s)
	}
	win := os.Getenv("SystemRoot")
	stamp := time.Now().Format("20060102-150405")
	for _, d := range []string{filepath.Join(win, "SoftwareDistribution"), filepath.Join(win, "System32", "catroot2")} {
		if _, err := os.Stat(d); err == nil {
			bak := d + ".bak-" + stamp
			emit("▸ " + i18n.Pick("Переименование ", "Renaming ") + filepath.Base(d))
			if err := os.Rename(d, bak); err != nil {
				emit("× " + err.Error())
			} else {
				emit("✓ " + i18n.T("готово"))
			}
		}
	}
	for _, s := range []string{"cryptsvc", "bits", "wuauserv", "msiserver"} {
		_ = runCmd(emit, i18n.Pick("Запуск службы ", "Starting service ")+s, 30*time.Second, "net", "start", s)
	}
	emit("✓ " + i18n.Pick("Центр обновления сброшен. Проверьте обновления заново.", "Windows Update reset. Check for updates again."))
	return nil
}

var actionDefs = []actionDef{
	// ===== Ремонт =====
	{id: "sfc", group: "repair", admin: true, mins: 10,
		ru: "Проверить системные файлы (SFC)", en: "Check system files (SFC)",
		dru: "sfc /scannow находит и восстанавливает повреждённые системные файлы", den: "sfc /scannow finds and repairs corrupted system files",
		fn: func(e func(string)) error { return runCmd(e, "sfc /scannow", 30*time.Minute, "sfc", "/scannow") }},
	{id: "dism_restore", group: "repair", admin: true, mins: 15,
		ru: "Восстановить образ Windows (DISM)", en: "Repair Windows image (DISM)",
		dru: "DISM /RestoreHealth чинит хранилище компонентов — запускайте перед SFC, если он не справился", den: "DISM /RestoreHealth repairs the component store — run before SFC if SFC failed",
		fn: func(e func(string)) error {
			return runCmd(e, "DISM /RestoreHealth", 40*time.Minute, "DISM", "/Online", "/Cleanup-Image", "/RestoreHealth")
		}},
	{id: "dism_cleanup", group: "repair", admin: true, mins: 15,
		ru: "Очистить хранилище компонентов", en: "Clean up the component store",
		dru: "DISM /StartComponentCleanup удаляет устаревшие версии компонентов и освобождает место на диске", den: "DISM /StartComponentCleanup removes superseded components and frees disk space",
		fn: func(e func(string)) error {
			return runCmd(e, "DISM /StartComponentCleanup", 40*time.Minute, "DISM", "/Online", "/Cleanup-Image", "/StartComponentCleanup")
		}},
	{id: "wu_reset", group: "repair", admin: true, mins: 3,
		ru: "Сбросить Центр обновления Windows", en: "Reset Windows Update",
		dru: "Останавливает службы обновления, пересоздаёт кэш SoftwareDistribution и catroot2 — лечит зависшие обновления", den: "Stops update services and rebuilds SoftwareDistribution and catroot2 caches — fixes stuck updates",
		fn: wuReset},
	{id: "reregister_start", group: "repair", mins: 2,
		ru: "Перерегистрировать меню «Пуск»", en: "Re-register the Start menu",
		dru: "Переустанавливает пакеты оболочки — помогает, когда «Пуск» или поиск не открываются", den: "Re-registers the shell packages — helps when Start or Search won't open",
		fn: func(e func(string)) error {
			return powershell(e, i18n.Pick("Перерегистрация оболочки", "Re-registering the shell"),
				`Get-AppxPackage -Name Microsoft.Windows.ShellExperienceHost, Microsoft.Windows.StartMenuExperienceHost | ForEach-Object { Add-AppxPackage -DisableDevelopmentMode -Register "$($_.InstallLocation)\AppXManifest.xml" -ErrorAction SilentlyContinue }`,
				3*time.Minute)
		}},

	// ===== Сеть =====
	{id: "flush_dns", group: "network", mins: 1,
		ru: "Очистить кэш DNS", en: "Flush the DNS cache",
		dru: "ipconfig /flushdns — помогает, когда сайты не открываются после смены провайдера или хостинга", den: "ipconfig /flushdns — helps when sites won't open after a provider or hosting change",
		fn: func(e func(string)) error {
			return runCmd(e, "ipconfig /flushdns", 30*time.Second, "ipconfig", "/flushdns")
		}},
	{id: "winsock_reset", group: "network", admin: true, mins: 1,
		ru: "Сбросить Winsock", en: "Reset Winsock",
		dru: "netsh winsock reset возвращает сетевой стек к значениям по умолчанию — лечит «интернет есть, а сайты не грузятся»", den: "netsh winsock reset restores the network stack defaults — fixes “connected but no sites load”",
		fn: func(e func(string)) error {
			return runCmd(e, "netsh winsock reset", 60*time.Second, "netsh", "winsock", "reset")
		}},
	{id: "ip_reset", group: "network", admin: true, mins: 1,
		ru: "Сбросить настройки TCP/IP", en: "Reset TCP/IP",
		dru: "netsh int ip reset — требуется перезагрузка после применения", den: "netsh int ip reset — a reboot is needed afterwards",
		fn: func(e func(string)) error {
			return runCmd(e, "netsh int ip reset", 60*time.Second, "netsh", "int", "ip", "reset")
		}},
	{id: "dns_cloudflare", group: "network", admin: true, mins: 1,
		ru: "DNS: Cloudflare (1.1.1.1)", en: "DNS: Cloudflare (1.1.1.1)",
		dru: "Быстрый DNS Cloudflare для всех сетевых адаптеров", den: "Cloudflare's fast DNS on every adapter",
		fn: setDNS("Cloudflare", "1.1.1.1", "1.0.0.1")},
	{id: "dns_google", group: "network", admin: true, mins: 1,
		ru: "DNS: Google (8.8.8.8)", en: "DNS: Google (8.8.8.8)",
		dru: "Публичный DNS Google для всех адаптеров", den: "Google Public DNS on every adapter",
		fn: setDNS("Google", "8.8.8.8", "8.8.4.4")},
	{id: "dns_quad9", group: "network", admin: true, mins: 1,
		ru: "DNS: Quad9 (9.9.9.9)", en: "DNS: Quad9 (9.9.9.9)",
		dru: "Quad9 с блокировкой вредоносных доменов", den: "Quad9 with malware-domain blocking",
		fn: setDNS("Quad9", "9.9.9.9", "149.112.112.112")},
	{id: "dns_auto", group: "network", admin: true, mins: 1,
		ru: "DNS: вернуть автоматический", en: "DNS: back to automatic",
		dru: "Возвращает получение DNS от роутера/провайдера (DHCP)", den: "Restores DNS from your router/provider (DHCP)",
		fn: setDNS("")},
}

// Actions возвращает список операций обслуживания.
func Actions() []Action {
	out := make([]Action, 0, len(actionDefs))
	for _, d := range actionDefs {
		out = append(out, Action{
			ID: d.id, Group: d.group, Title: i18n.Pick(d.ru, d.en),
			Desc: i18n.Pick(d.dru, d.den), Admin: d.admin, Mins: d.mins,
		})
	}
	return out
}

// Run запускает операцию в фоне.
func Run(id string) error {
	for _, d := range actionDefs {
		if d.id != id {
			continue
		}
		if d.admin && !ctl.IsElevated() {
			return errors.New(i18n.T("нужны права администратора — нажмите «Перезапустить от имени администратора»"))
		}
		return run(d.id, i18n.Pick(d.ru, d.en), d.fn)
	}
	return errors.New(i18n.T("неизвестная операция"))
}
