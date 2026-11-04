//go:build windows

package winupdate

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"golang.org/x/sys/windows/registry"

	"github.com/healcore/healcoreops/internal/ctl"
	"github.com/healcore/healcoreops/internal/i18n"
	"github.com/healcore/healcoreops/internal/winps"
)

// Supported — Центр обновлений доступен в Windows.
func Supported() bool { return true }

const uxKey = `SOFTWARE\Microsoft\WindowsUpdate\UX\Settings`

func searchUpdates() ([]Update, error) {
	var out []Update
	err := winps.JSON(`$ErrorActionPreference='Stop'
try {
  $s = New-Object -ComObject Microsoft.Update.Session
  $s.ClientApplicationID = 'HealCoreOps'
  $r = $s.CreateUpdateSearcher().Search("IsInstalled=0 and IsHidden=0")
  $o = foreach($u in $r.Updates){ [pscustomobject]@{
    id = [string]$u.Identity.UpdateID; title = [string]$u.Title; kb = (@($u.KBArticleIDs) -join ',')
    size = [int64]$u.MaxDownloadSize; cat = (@($u.Categories | ForEach-Object { $_.Name }) -join ', ')
    sev = [string]$u.MsrcSeverity; reboot = [bool]$u.RebootRequired; driver = ([int]$u.Type -eq 2)
    optional = (-not [bool]$u.AutoSelectOnWebSites) } }
  ConvertTo-Json -InputObject @($o) -Compress -Depth 3
} catch { ConvertTo-Json -InputObject @(@{ id = 'ERR'; title = $_.Exception.Message }) -Compress }`, 15*time.Minute, &out)
	if err != nil {
		return nil, err
	}
	if len(out) == 1 && out[0].ID == "ERR" {
		return nil, errors.New(out[0].Title)
	}
	if out == nil {
		out = []Update{}
	}
	return out, nil
}

// History — журнал установок (последние n записей).
func History(n int) ([]HistoryEntry, error) {
	if n <= 0 || n > 300 {
		n = 80
	}
	var out []HistoryEntry
	err := winps.JSON(fmt.Sprintf(`$ErrorActionPreference='Stop'
$s = New-Object -ComObject Microsoft.Update.Session
$se = $s.CreateUpdateSearcher()
$c = $se.GetTotalHistoryCount()
$o = @()
if($c -gt 0){
  foreach($e in $se.QueryHistory(0, [math]::Min($c, %d))){
    if(-not $e.Title){ continue }
    $hr = [BitConverter]::ToUInt32([BitConverter]::GetBytes([int32]$e.HResult), 0)
    $o += [pscustomobject]@{ date = $e.Date.ToUniversalTime().ToString('o'); title = [string]$e.Title
      result = [int]$e.ResultCode; hr = ('0x{0:X8}' -f $hr); op = [int]$e.Operation }
  }
}
ConvertTo-Json -InputObject @($o) -Compress`, n), 2*time.Minute, &out)
	if err != nil {
		return nil, err
	}
	for i := range out {
		if out[i].Result == 4 || out[i].Result == 3 || out[i].Result == 5 {
			out[i].Hint = Hint(out[i].HResult)
		}
	}
	if out == nil {
		out = []HistoryEntry{}
	}
	return out, nil
}

var idRe = regexp.MustCompile(`^[0-9a-fA-F-]{36}$`)

// Install скачивает и устанавливает выбранные обновления, построчно сообщая ход.
// Возвращает, нужна ли перезагрузка.
func Install(ids []string, emit func(string)) (bool, error) {
	if !ctl.IsElevated() {
		return false, errors.New(i18n.T("Нужны права администратора"))
	}
	if len(ids) == 0 {
		return false, errors.New("no updates selected")
	}
	for _, id := range ids {
		if !idRe.MatchString(id) {
			return false, errors.New("bad update id")
		}
	}
	reboot := false
	var failed []string
	installed := map[string]bool{}
	script := `$ErrorActionPreference='Stop'
function Say($s){ [Console]::Out.WriteLine($s); [Console]::Out.Flush() }
$ids = @('` + strings.Join(ids, "','") + `')
try {
  $ses = New-Object -ComObject Microsoft.Update.Session
  $ses.ClientApplicationID = 'HealCoreOps'
  Say 'STEP:search'
  $r = $ses.CreateUpdateSearcher().Search("IsInstalled=0 and IsHidden=0")
  $all = @(); foreach($u in $r.Updates){ if($ids -contains [string]$u.Identity.UpdateID){ $all += $u } }
  if($all.Count -eq 0){ Say 'ERR:notfound'; exit }
  $i = 0
  foreach($u in $all){
    $i++
    Say ('ITEM:' + $i + '/' + $all.Count + ':' + $u.Title)
    if(-not $u.EulaAccepted){ $u.AcceptEula() }
    $c = New-Object -ComObject Microsoft.Update.UpdateColl; [void]$c.Add($u)
    $d = $ses.CreateUpdateDownloader(); $d.Updates = $c
    Say 'STEP:download'
    $dr = $d.Download()
    if($dr.ResultCode -ne 2 -and $dr.ResultCode -ne 3){ Say ('FAIL:' + $u.Identity.UpdateID + ':download:' + ('0x{0:X8}' -f [BitConverter]::ToUInt32([BitConverter]::GetBytes([int32]$dr.HResult),0))); continue }
    $in = $ses.CreateUpdateInstaller(); $in.Updates = $c
    Say 'STEP:install'
    $ir = $in.Install()
    $hr = '0x{0:X8}' -f [BitConverter]::ToUInt32([BitConverter]::GetBytes([int32]$ir.HResult),0)
    if($ir.ResultCode -eq 2 -or $ir.ResultCode -eq 3){ Say ('OK:' + $u.Identity.UpdateID + ':' + [string]$ir.RebootRequired) }
    else { Say ('FAIL:' + $u.Identity.UpdateID + ':install:' + $hr) }
  }
} catch { Say ('ERR:' + $_.Exception.Message) }`
	var fatal error
	err := winps.Stream(script, 4*time.Hour, func(l string) {
		switch {
		case strings.HasPrefix(l, "STEP:search"):
			emit(i18n.Pick("▸ Поиск выбранных обновлений…", "▸ Locating selected updates…"))
		case strings.HasPrefix(l, "ITEM:"):
			rest := strings.TrimPrefix(l, "ITEM:")
			n, title, _ := strings.Cut(rest, ":")
			emit(fmt.Sprintf("▸ [%s] %s", n, title))
		case l == "STEP:download":
			emit(i18n.Pick("  скачивание…", "  downloading…"))
		case l == "STEP:install":
			emit(i18n.Pick("  установка…", "  installing…"))
		case strings.HasPrefix(l, "OK:"):
			p := strings.Split(l, ":")
			if len(p) >= 3 {
				installed[p[1]] = true
				if strings.EqualFold(p[2], "True") {
					reboot = true
				}
			}
			emit(i18n.Pick("  ✓ установлено", "  ✓ installed"))
		case strings.HasPrefix(l, "FAIL:"):
			p := strings.Split(l, ":")
			hr := ""
			if len(p) >= 4 {
				hr = p[3]
			}
			failed = append(failed, hr)
			msg := i18n.Pick("  × ошибка ", "  × error ") + hr
			if h := Hint(hr); h != "" {
				msg += " — " + h
			}
			emit(msg)
		case l == "ERR:notfound":
			fatal = errors.New(i18n.Pick("выбранные обновления больше не предлагаются — повторите поиск", "the selected updates are no longer offered — search again"))
		case strings.HasPrefix(l, "ERR:"):
			fatal = errors.New(strings.TrimPrefix(l, "ERR:"))
		}
	})
	dropInstalled(installed)
	if fatal != nil {
		return reboot, fatal
	}
	if err != nil {
		return reboot, err
	}
	emit("")
	emit(fmt.Sprintf(i18n.Pick("Установлено: %d из %d.", "Installed: %d of %d."), len(installed), len(ids)))
	if reboot {
		emit(i18n.Pick("Для завершения нужна перезагрузка.", "A restart is required to finish."))
	}
	if len(failed) > 0 {
		return reboot, fmt.Errorf(i18n.Pick("не установлено: %d", "not installed: %d"), len(failed))
	}
	return reboot, nil
}

// Pause — состояние паузы обновлений.
func Pause() PauseState {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, uxKey, registry.QUERY_VALUE)
	if err != nil {
		return PauseState{}
	}
	defer k.Close()
	v, _, _ := k.GetStringValue("PauseUpdatesExpiryTime")
	return pausedUntil(v, time.Now())
}

// ReadPause возвращает текущие значения паузы (для отката).
func ReadPause() map[string]string {
	out := map[string]string{}
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, uxKey, registry.QUERY_VALUE)
	if err != nil {
		return out
	}
	defer k.Close()
	for _, n := range PauseKeys {
		if v, _, err := k.GetStringValue(n); err == nil {
			out[n] = v
		}
	}
	return out
}

// WritePause записывает значения паузы; отсутствующие в vals ключи удаляются.
func WritePause(vals map[string]string) error {
	if !ctl.IsElevated() {
		return errors.New(i18n.T("Нужны права администратора"))
	}
	k, _, err := registry.CreateKey(registry.LOCAL_MACHINE, uxKey, registry.SET_VALUE|registry.QUERY_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	for _, n := range PauseKeys {
		if v, ok := vals[n]; ok && v != "" {
			if err := k.SetStringValue(n, v); err != nil {
				return err
			}
		} else {
			_ = k.DeleteValue(n)
		}
	}
	return nil
}

// SetPause приостанавливает обновления на days дней (0 — снять паузу).
func SetPause(days int) error {
	if days <= 0 {
		return WritePause(map[string]string{})
	}
	return WritePause(PauseTimes(time.Now(), days))
}
