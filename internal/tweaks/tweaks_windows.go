//go:build windows

package tweaks

import (
	"context"
	"encoding/base64"
	"encoding/json"
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

// Supported — твики доступны только в Windows.
func Supported() bool { return true }

// ---------- PowerShell ----------

func hidden(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
}

func powershell(script string, timeout time.Duration) (string, error) {
	full := "$ProgressPreference='SilentlyContinue'\n[Console]::OutputEncoding=[Text.Encoding]::UTF8\n" + script
	u := utf16.Encode([]rune(full))
	b := make([]byte, len(u)*2)
	for i, v := range u {
		b[2*i], b[2*i+1] = byte(v), byte(v>>8)
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass",
		"-EncodedCommand", base64.StdEncoding.EncodeToString(b))
	hidden(cmd)
	out, err := cmd.Output()
	if ctx.Err() != nil {
		return string(out), errors.New(i18n.T("превышено время ожидания PowerShell"))
	}
	return string(out), err
}

func psQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

func psArray(items []string) string {
	q := make([]string, len(items))
	for i, s := range items {
		q[i] = psQuote(s)
	}
	return "@(" + strings.Join(q, ",") + ")"
}

// lastJSONArray извлекает JSON-массив строк из последней подходящей строки вывода.
func lastJSONArray(out string) []string {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		l := strings.TrimSpace(lines[i])
		if strings.HasPrefix(l, "[") || strings.HasPrefix(l, "\"") {
			var arr []string
			if json.Unmarshal([]byte(l), &arr) == nil {
				return arr
			}
			var one string
			if json.Unmarshal([]byte(l), &one) == nil {
				return []string{one}
			}
		}
	}
	return nil
}

// ---------- Приложения ----------

// ListApps возвращает каталог с отметкой, какие приложения установлены.
func ListApps() ([]App, error) {
	admin := ctl.IsElevated()
	script := "$ErrorActionPreference='SilentlyContinue'\n"
	if admin {
		script += "$n = @(Get-AppxPackage -AllUsers | Select-Object -ExpandProperty Name)\n" +
			"$n += @(Get-AppxProvisionedPackage -Online | Select-Object -ExpandProperty DisplayName)\n"
	} else {
		script += "$n = @(Get-AppxPackage | Select-Object -ExpandProperty Name)\n"
	}
	script += "ConvertTo-Json -InputObject @($n | Sort-Object -Unique) -Compress"
	out, err := powershell(script, 90*time.Second)
	if err != nil {
		return nil, fmt.Errorf(i18n.T("не удалось получить список приложений: %v"), err)
	}
	have := map[string]bool{}
	for _, n := range lastJSONArray(out) {
		have[strings.ToLower(n)] = true
	}
	apps := make([]App, 0, len(appDefs))
	for _, d := range appDefs {
		a := d.app()
		if d.id == oneDriveID {
			a.Installed = oneDriveInstalled()
		} else {
			for _, p := range d.pkgs {
				if have[strings.ToLower(p)] {
					a.Installed = true
				}
			}
		}
		apps = append(apps, a)
	}
	return apps, nil
}

// RemoveApps удаляет выбранные приложения. allUsers — для всех пользователей
// и без автоустановки для новых (нужны права администратора).
func RemoveApps(ids []string) []Result {
	all := ctl.IsElevated()
	var res []Result
	var names []string
	owners := map[string]string{}
	var defs []appDef
	for _, id := range ids {
		d, ok := findApp(id)
		if !ok {
			continue
		}
		if d.id == oneDriveID {
			if err := removeOneDrive(); err != nil {
				res = append(res, Result{ID: id, Msg: err.Error()})
			} else {
				res = append(res, Result{ID: id, OK: true, Msg: i18n.T("удалено")})
			}
			continue
		}
		defs = append(defs, d)
		for _, p := range d.pkgs {
			names = append(names, p)
			owners[p] = id
		}
	}
	if len(names) == 0 {
		return res
	}

	allFlag := "$false"
	if all {
		allFlag = "$true"
	}
	script := "$ErrorActionPreference='SilentlyContinue'\n$all=" + allFlag + "\n$names=" + psArray(names) + `
foreach($n in $names){
  if($all){ Get-AppxPackage -AllUsers -Name $n | Remove-AppxPackage -AllUsers }
  else { Get-AppxPackage -Name $n | Remove-AppxPackage }
  if($all){ Get-AppxProvisionedPackage -Online | Where-Object { $_.DisplayName -eq $n } | Remove-AppxProvisionedPackage -Online | Out-Null }
}
$left=@()
foreach($n in $names){
  if($all){ $x = Get-AppxPackage -AllUsers -Name $n } else { $x = Get-AppxPackage -Name $n }
  if($x){ $left += $n }
}
ConvertTo-Json -InputObject @($left) -Compress`
	out, err := powershell(script, 10*time.Minute)
	if err != nil {
		for _, d := range defs {
			res = append(res, Result{ID: d.id, Msg: err.Error()})
		}
		return res
	}
	left := map[string]bool{}
	for _, n := range lastJSONArray(out) {
		left[owners[n]] = true
	}
	for _, d := range defs {
		if left[d.id] {
			res = append(res, Result{ID: d.id, Msg: i18n.T("Windows не разрешила удалить (системный компонент или нужны права администратора)")})
		} else {
			res = append(res, Result{ID: d.id, OK: true, Msg: i18n.T("удалено")})
		}
	}
	return res
}

func oneDriveInstalled() bool {
	for _, p := range []string{
		filepath.Join(os.Getenv("LOCALAPPDATA"), "Microsoft", "OneDrive", "OneDrive.exe"),
		filepath.Join(os.Getenv("ProgramFiles"), "Microsoft OneDrive", "OneDrive.exe"),
		filepath.Join(os.Getenv("ProgramFiles(x86)"), "Microsoft OneDrive", "OneDrive.exe"),
	} {
		if _, err := os.Stat(p); err == nil {
			return true
		}
	}
	return false
}

func removeOneDrive() error {
	kill := exec.Command("taskkill", "/f", "/im", "OneDrive.exe")
	hidden(kill)
	_ = kill.Run()
	cands := []string{
		filepath.Join(os.Getenv("SystemRoot"), "System32", "OneDriveSetup.exe"),
		filepath.Join(os.Getenv("SystemRoot"), "SysWOW64", "OneDriveSetup.exe"),
	}
	if m, _ := filepath.Glob(filepath.Join(os.Getenv("LOCALAPPDATA"), "Microsoft", "OneDrive", "*", "OneDriveSetup.exe")); len(m) > 0 {
		cands = append(m, cands...)
	}
	if m, _ := filepath.Glob(filepath.Join(os.Getenv("ProgramFiles"), "Microsoft OneDrive", "*", "OneDriveSetup.exe")); len(m) > 0 {
		cands = append(m, cands...)
	}
	for _, c := range cands {
		if _, err := os.Stat(c); err != nil {
			continue
		}
		args := []string{"/uninstall"}
		if strings.Contains(strings.ToLower(c), strings.ToLower(os.Getenv("ProgramFiles"))) {
			args = append(args, "/allusers")
		}
		cmd := exec.Command(c, args...)
		hidden(cmd)
		_ = cmd.Run()
		time.Sleep(2 * time.Second)
		if !oneDriveInstalled() {
			return nil
		}
	}
	if oneDriveInstalled() {
		return errors.New(i18n.T("не удалось удалить OneDrive"))
	}
	return nil
}

// CreateRestorePoint создаёт точку восстановления системы.
func CreateRestorePoint() error {
	if !ctl.IsElevated() {
		return errors.New(i18n.T("нужны права администратора — нажмите «Перезапустить от имени администратора»"))
	}
	// Снимаем ограничение «не чаще раза в сутки» и включаем защиту системного диска.
	_ = regOp{hklm, `SOFTWARE\Microsoft\Windows NT\CurrentVersion\SystemRestore`, "SystemRestorePointCreationFrequency", dw(0), none()}.set(dw(0))
	script := `$ErrorActionPreference='Stop'
try { Enable-ComputerRestore -Drive "$env:SystemDrive\" } catch {}
try { Checkpoint-Computer -Description 'HealCoreOps' -RestorePointType 'MODIFY_SETTINGS'; 'OK' } catch { 'ERR: ' + $_.Exception.Message }`
	out, err := powershell(script, 5*time.Minute)
	if err != nil {
		return err
	}
	out = strings.TrimSpace(out)
	if strings.HasSuffix(out, "OK") {
		return nil
	}
	if i := strings.Index(out, "ERR: "); i >= 0 {
		return errors.New(strings.TrimSpace(out[i+5:]))
	}
	return errors.New(i18n.T("не удалось создать точку восстановления"))
}

// RestartExplorer перезапускает Проводник, чтобы применились изменения интерфейса.
func RestartExplorer() error {
	kill := exec.Command("taskkill", "/f", "/im", "explorer.exe")
	hidden(kill)
	_ = kill.Run()
	// Windows обычно перезапускает оболочку сама; если нет — запускаем.
	time.Sleep(2500 * time.Millisecond)
	check := exec.Command("tasklist", "/fi", "imagename eq explorer.exe", "/nh")
	hidden(check)
	out, _ := check.Output()
	if strings.Contains(strings.ToLower(string(out)), "explorer.exe") {
		return nil
	}
	start := exec.Command(filepath.Join(os.Getenv("SystemRoot"), "explorer.exe"))
	return start.Start()
}
