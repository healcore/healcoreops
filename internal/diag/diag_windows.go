//go:build windows

package diag

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/windows/registry"

	"github.com/healcore/healcoreops/internal/ctl"
	"github.com/healcore/healcoreops/internal/i18n"
	"github.com/healcore/healcoreops/internal/maintenance"
	"github.com/healcore/healcoreops/internal/winps"
)

// Supported — диагностика Windows доступна.
func Supported() bool { return true }

func en() bool { return i18n.Get() == "en" }

// ---------- Сбои ----------

const crashScript = `
$since = (Get-Date).AddDays(-%d)
$r = [ordered]@{ bsod=@(); unexp=@(); apps=@(); ev=@() }
try { $r.bsod = @(Get-WinEvent -FilterHashtable @{LogName='System'; ProviderName=@('Microsoft-Windows-WER-SystemErrorReporting','BugCheck'); Id=1001; StartTime=$since} -MaxEvents 40 -EA Stop | ForEach-Object { [pscustomobject]@{ t=$_.TimeCreated.ToString('s'); m=[string]$_.Message; p=[string]($_.Properties | Select-Object -First 1).Value } }) } catch {}
try { $r.unexp = @(Get-WinEvent -FilterHashtable @{LogName='System'; ProviderName='Microsoft-Windows-Kernel-Power'; Id=41; StartTime=$since} -MaxEvents 40 -EA Stop | ForEach-Object { [pscustomobject]@{ t=$_.TimeCreated.ToString('s'); c=[int64]$_.Properties[0].Value } }) } catch {}
try { $r.apps = @(Get-WinEvent -FilterHashtable @{LogName='Application'; Id=1000,1002; StartTime=$since} -MaxEvents 400 -EA Stop | Where-Object { $_.ProviderName -in 'Application Error','Application Hang' } | ForEach-Object {
  $p = $_.Properties
  if ($_.Id -eq 1000) { [pscustomobject]@{ k='crash'; t=$_.TimeCreated.ToString('s'); a=[string]$p[0].Value; mo=[string]$p[3].Value; ex=[string]$p[6].Value; pa=[string]$p[10].Value } }
  else { [pscustomobject]@{ k='hang'; t=$_.TimeCreated.ToString('s'); a=[string]$p[0].Value; mo=''; ex=''; pa=[string]$p[5].Value } }
}) } catch {}
try {
  $ev = Get-WinEvent -FilterHashtable @{LogName=@('System','Application'); Level=1,2; StartTime=(Get-Date).AddDays(-7)} -MaxEvents 1500 -EA Stop
  $r.ev = @($ev | Group-Object LogName,ProviderName,Id | Sort-Object Count -Descending | Select-Object -First 30 | ForEach-Object {
    $f = $_.Group[0]; $msg = [string]$f.Message; if ($msg.Length -gt 260) { $msg = $msg.Substring(0,260) + '…' }
    [pscustomobject]@{ l=$f.LogName; p=$f.ProviderName; i=$f.Id; v=[int]$f.Level; c=$_.Count; t=$f.TimeCreated.ToString('s'); m=($msg -split "` + "`r?`n" + `")[0] } })
} catch {}
$r | ConvertTo-Json -Compress -Depth 4
`

type rawCrash struct {
	BSOD []struct {
		T, M, P string
	} `json:"bsod"`
	Unexp []struct {
		T string
		C int64
	} `json:"unexp"`
	Apps []struct {
		K, T, A, Mo, Ex, Pa string
	} `json:"apps"`
	Ev []struct {
		L, P, T, M string
		I, V, C    int
	} `json:"ev"`
}

// Crashes собирает сведения о сбоях за days дней.
func Crashes(days int) (Report, error) {
	if days <= 0 || days > 365 {
		days = 30
	}
	rep := Report{Days: days, BSOD: []Crash{}, Unexpected: []string{}, Apps: []AppCrash{}, Events: []EventGroup{}}
	var raw []rawCrash
	if err := winps.JSON(fmt.Sprintf(crashScript, days), 2*time.Minute, &raw); err != nil {
		return rep, err
	}
	var r rawCrash
	if len(raw) > 0 {
		r = raw[0]
	}
	// Синие экраны: из дампов и из событий
	dumps, dumpErr := readDumps(days)
	rep.BSOD = append(rep.BSOD, dumps...)
	for _, b := range r.BSOD {
		code, ok := ParseBugCheckText(b.P)
		if !ok {
			code, ok = ParseBugCheckText(b.M)
		}
		if !ok {
			continue
		}
		if dup(rep.BSOD, b.T, code) {
			continue
		}
		rep.BSOD = append(rep.BSOD, Crash{Time: b.T, Bug: DescribeBugCheck(code, en()), Source: "event"})
	}
	sort.Slice(rep.BSOD, func(i, j int) bool { return rep.BSOD[i].Time > rep.BSOD[j].Time })
	for _, u := range r.Unexp {
		if u.C == 0 { // без синего экрана: пропало питание, зависание, удержание кнопки
			rep.Unexpected = append(rep.Unexpected, u.T)
		}
	}
	// Программы
	m := map[string]*AppCrash{}
	for _, a := range r.Apps {
		if a.A == "" {
			continue
		}
		k := strings.ToLower(a.A) + "|" + a.K
		g, ok := m[k]
		if !ok {
			g = &AppCrash{App: a.A, Kind: a.K, Module: a.Mo, Exception: a.Ex, Path: a.Pa}
			m[k] = g
		}
		g.Count++
		if a.T > g.Last {
			g.Last, g.Module, g.Exception = a.T, a.Mo, a.Ex
		}
	}
	for _, g := range m {
		if g.Exception != "" && !strings.HasPrefix(g.Exception, "0x") {
			g.Exception = "0x" + g.Exception
		}
		rep.Apps = append(rep.Apps, *g)
	}
	sort.Slice(rep.Apps, func(i, j int) bool {
		if rep.Apps[i].Count != rep.Apps[j].Count {
			return rep.Apps[i].Count > rep.Apps[j].Count
		}
		return rep.Apps[i].Last > rep.Apps[j].Last
	})
	for _, e := range r.Ev {
		rep.Events = append(rep.Events, EventGroup{Log: e.L, Provider: e.P, ID: e.I, Level: e.V, Count: e.C, Last: e.T, Message: strings.TrimSpace(e.M)})
	}
	if dumpErr != nil && !ctl.IsElevated() {
		rep.Note = i18n.Pick("Файлы дампов синих экранов доступны только администратору — коды взяты из журнала событий.",
			"BSOD dump files are readable only by administrators — codes are taken from the event log.")
	}
	return rep, nil
}

func dup(list []Crash, t string, code uint32) bool {
	tt, err := time.ParseInLocation("2006-01-02T15:04:05", t, time.Local)
	if err != nil {
		return false
	}
	for _, c := range list {
		ct, err := time.ParseInLocation("2006-01-02T15:04:05", c.Time, time.Local)
		if err == nil && c.Bug.Code == code && absDur(ct.Sub(tt)) < 30*time.Minute {
			return true
		}
	}
	return false
}

func absDur(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}

func readDumps(days int) ([]Crash, error) {
	sr := os.Getenv("SystemRoot")
	files, err := filepath.Glob(filepath.Join(sr, "Minidump", "*.dmp"))
	if err != nil {
		return nil, err
	}
	files = append(files, filepath.Join(sr, "MEMORY.DMP"))
	cut := time.Now().AddDate(0, 0, -days)
	var out []Crash
	var lastErr error
	for _, f := range files {
		st, err := os.Stat(f)
		if err != nil || st.ModTime().Before(cut) {
			continue
		}
		fh, err := os.Open(f)
		if err != nil {
			lastErr = err
			continue
		}
		buf := make([]byte, 0x100)
		_, err = io.ReadFull(fh, buf)
		fh.Close()
		if err != nil {
			continue
		}
		code, _, err := ParseDumpHeader(buf)
		if err != nil {
			continue
		}
		out = append(out, Crash{Time: st.ModTime().Format("2006-01-02T15:04:05"), Bug: DescribeBugCheck(code, en()), File: f, Source: "dump"})
	}
	if len(files) > 1 && len(out) == 0 && lastErr != nil {
		return nil, lastErr
	}
	return out, nil
}

// ---------- Загрузка ----------

const bootScript = `
$o = [ordered]@{ boots=@(); slow=@(); err='' }
try {
  $ev = Get-WinEvent -FilterHashtable @{LogName='Microsoft-Windows-Diagnostics-Performance/Operational'; Id=100,101,102,103} -MaxEvents 400 -EA Stop
  foreach ($e in $ev) {
    $x = [xml]$e.ToXml(); $d = @{}
    foreach ($n in $x.Event.EventData.Data) { $d[$n.Name] = $n.'#text' }
    if ($e.Id -eq 100) { $o.boots += [pscustomobject]@{ t=$e.TimeCreated.ToString('s'); total=[int64]$d['BootTime']; main=[int64]$d['MainPathBootTime']; post=[int64]$d['BootPostBootTime'] } }
    else { $o.slow += [pscustomobject]@{ k=$e.Id; t=$e.TimeCreated.ToString('s'); n=[string]$d['Name']; f=[string]$d['FriendlyName']; deg=[int64]$d['DegradationTime'] } }
  }
} catch { $o.err = $_.Exception.Message }
$o | ConvertTo-Json -Compress -Depth 4
`

// BootInfo — время загрузки и что её замедляет.
func BootInfo(lastBoot time.Time) (Boot, error) {
	b := Boot{LastBoot: lastBoot.Format("2006-01-02T15:04:05"), Boots: []BootTime{}, Slow: []Degrader{}}
	var raw []struct {
		Boots []struct {
			T                 string
			Total, Main, Post int64
		}
		Slow []struct {
			K       int
			T, N, F string
			Deg     int64
		}
		Err string
	}
	if err := winps.JSON(bootScript, 90*time.Second, &raw); err != nil {
		return b, err
	}
	if len(raw) == 0 {
		return b, nil
	}
	for i, x := range raw[0].Boots {
		if i >= 15 {
			break
		}
		b.Boots = append(b.Boots, BootTime{Time: x.T, Total: x.Total, MainPath: x.Main, PostBoot: x.Post})
	}
	var slow []Degrader
	kinds := map[int]string{101: "app", 102: "driver", 103: "service"}
	for _, s := range raw[0].Slow {
		if s.N == "" {
			continue
		}
		slow = append(slow, Degrader{Kind: kinds[s.K], Name: s.N, Friendly: s.F, Degradation: s.Deg, Last: s.T})
	}
	b.Slow = groupSlow(slow)
	if raw[0].Err != "" && len(b.Boots) == 0 {
		if !ctl.IsElevated() {
			b.Note = i18n.Pick("Журнал производительности загрузки доступен только администратору.", "The boot performance log is readable only by administrators.")
		} else {
			b.Note = i18n.Pick("Windows ещё не записала данные о загрузке.", "Windows has not recorded boot data yet.")
		}
	}
	return b, nil
}

// ---------- Ожидающая перезагрузка ----------

func keyExists(k registry.Key, p string) bool {
	h, err := registry.OpenKey(k, p, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	h.Close()
	return true
}

// PendingReboot проверяет, ждёт ли Windows перезагрузки, и почему.
func PendingReboot() Reboot {
	r := Reboot{Reasons: []string{}}
	lm := registry.LOCAL_MACHINE
	if keyExists(lm, `SOFTWARE\Microsoft\Windows\CurrentVersion\Component Based Servicing\RebootPending`) {
		r.Reasons = append(r.Reasons, i18n.Pick("установка компонентов Windows", "Windows component servicing"))
	}
	if keyExists(lm, `SOFTWARE\Microsoft\Windows\CurrentVersion\WindowsUpdate\Auto Update\RebootRequired`) {
		r.Reasons = append(r.Reasons, i18n.Pick("установлены обновления Windows", "Windows updates were installed"))
	}
	if k, err := registry.OpenKey(lm, `SYSTEM\CurrentControlSet\Control\Session Manager`, registry.QUERY_VALUE); err == nil {
		if v, _, err := k.GetStringsValue("PendingFileRenameOperations"); err == nil && len(strings.Join(v, "")) > 0 {
			r.Reasons = append(r.Reasons, i18n.Pick("файлы будут заменены при перезагрузке (обновление программ или драйверов)", "files will be replaced on reboot (app or driver updates)"))
		}
		k.Close()
	}
	if k, err := registry.OpenKey(lm, `SOFTWARE\Microsoft\Updates`, registry.QUERY_VALUE); err == nil {
		if v, _, err := k.GetIntegerValue("UpdateExeVolatile"); err == nil && v != 0 {
			r.Reasons = append(r.Reasons, i18n.Pick("установщик обновлений ждёт перезагрузки", "an update installer awaits reboot"))
		}
		k.Close()
	}
	a, _ := registry.OpenKey(lm, `SYSTEM\CurrentControlSet\Control\ComputerName\ActiveComputerName`, registry.QUERY_VALUE)
	p, _ := registry.OpenKey(lm, `SYSTEM\CurrentControlSet\Control\ComputerName\ComputerName`, registry.QUERY_VALUE)
	if a != 0 && p != 0 {
		an, _, _ := a.GetStringValue("ComputerName")
		pn, _, _ := p.GetStringValue("ComputerName")
		if an != "" && pn != "" && !strings.EqualFold(an, pn) {
			r.Reasons = append(r.Reasons, i18n.Pick("изменено имя компьютера", "the computer name was changed"))
		}
	}
	if a != 0 {
		a.Close()
	}
	if p != 0 {
		p.Close()
	}
	r.Pending = len(r.Reasons) > 0
	return r
}

// ---------- Безопасность ----------

const secScript = `
$o = [ordered]@{}
try { $m = Get-MpComputerStatus -EA Stop; $o.def = [ordered]@{ svc=[bool]$m.AMServiceEnabled; av=[bool]$m.AntivirusEnabled; rt=[bool]$m.RealTimeProtectionEnabled; age=[int]$m.AntivirusSignatureAge; tamper=[bool]$m.IsTamperProtected } } catch {}
try { $o.av = @(Get-CimInstance -Namespace root/SecurityCenter2 -ClassName AntiVirusProduct -EA Stop | ForEach-Object { [ordered]@{ n=[string]$_.displayName; s=[int64]$_.productState } }) } catch {}
try { $o.fw = @(Get-NetFirewallProfile -EA Stop | ForEach-Object { [ordered]@{ n=[string]$_.Name; e=[string]$_.Enabled } }) } catch {}
try { $o.bl = [string](New-Object -ComObject Shell.Application).NameSpace($env:SystemDrive + '\').Self.ExtendedProperty('System.Volume.BitLockerProtection') } catch {}
try { $s = New-Object -ComObject Microsoft.Update.Session; $h = $s.CreateUpdateSearcher(); $n = $h.GetTotalHistoryCount(); if ($n -gt 0) { $last = $h.QueryHistory(0, [Math]::Min($n, 60)) | Where-Object { $_.ResultCode -eq 2 } | Sort-Object Date -Descending | Select-Object -First 1; if ($last) { $o.wu = $last.Date.ToLocalTime().ToString('s') } } } catch {}
$o | ConvertTo-Json -Compress -Depth 4
`

func regDword(k registry.Key, path, name string) (uint64, bool) {
	h, err := registry.OpenKey(k, path, registry.QUERY_VALUE)
	if err != nil {
		return 0, false
	}
	defer h.Close()
	v, _, err := h.GetIntegerValue(name)
	return v, err == nil
}

// Security проверяет основные параметры защиты Windows.
func Security() []SecItem {
	P := i18n.Pick
	var raw []struct {
		Def *struct {
			Svc, Av, Rt, Tamper bool
			Age                 int
		}
		Av []struct {
			N string
			S int64
		}
		Fw []struct{ N, E string }
		Bl string
		Wu string
	}
	_ = winps.JSON(secScript, 90*time.Second, &raw)
	var out []SecItem
	if len(raw) > 0 {
		r := raw[0]
		// Антивирус
		item := SecItem{ID: "av", Title: P("Антивирус", "Antivirus"), Level: -1, State: P("нет данных", "no data")}
		var third []string
		for _, a := range r.Av {
			on := (a.S>>12)&0xF == 1
			if !strings.Contains(strings.ToLower(a.N), "defender") && on {
				third = append(third, a.N)
			}
		}
		switch {
		case len(third) > 0:
			item.Level, item.State = 0, strings.Join(third, ", ")
		case r.Def != nil && r.Def.Rt && r.Def.Av:
			item.Level, item.State = 0, P("Microsoft Defender — защита в реальном времени включена", "Microsoft Defender — real-time protection on")
			if r.Def.Age > 7 {
				item.Level = 1
				item.Detail = fmt.Sprintf(P("Антивирусные базы не обновлялись %d дн.", "Antivirus definitions are %d days old"), r.Def.Age)
			}
		case r.Def != nil:
			item.Level, item.State, item.Page = 2, P("Microsoft Defender отключён — компьютер без антивируса", "Microsoft Defender is off — no antivirus protection"), "tweaks"
		}
		out = append(out, item)
		// Брандмауэр
		if len(r.Fw) > 0 {
			var off []string
			for _, f := range r.Fw {
				if !strings.EqualFold(f.E, "True") && f.E != "1" {
					off = append(off, f.N)
				}
			}
			it := SecItem{ID: "fw", Title: P("Брандмауэр", "Firewall"), Level: 0, State: P("включён для всех сетей", "on for all networks")}
			if len(off) > 0 {
				it.Level, it.State = 2, P("выключен: ", "off: ")+strings.Join(off, ", ")
			}
			out = append(out, it)
		}
		// BitLocker
		bl := SecItem{ID: "bitlocker", Title: P("Шифрование системного диска (BitLocker)", "System drive encryption (BitLocker)"), Level: -1, State: P("не поддерживается или нет данных", "not supported or no data")}
		switch r.Bl {
		case "1", "3", "5":
			bl.Level, bl.State = 0, P("включено", "on")
		case "2", "4", "6":
			bl.Level, bl.State, bl.Detail = 1, P("выключено", "off"), P("Для ноутбуков рекомендуется шифрование диска на случай кражи.", "Drive encryption is recommended for laptops in case of theft.")
		}
		out = append(out, bl)
		// Обновления
		wu := SecItem{ID: "wu", Title: P("Обновления Windows", "Windows updates"), Level: -1, State: P("нет данных", "no data")}
		if t, err := time.ParseInLocation("2006-01-02T15:04:05", r.Wu, time.Local); err == nil {
			days := int(time.Since(t).Hours() / 24)
			wu.State = fmt.Sprintf(P("последнее установлено %d дн. назад", "last installed %d days ago"), days)
			wu.Level = 0
			if days > 45 {
				wu.Level, wu.Detail, wu.Page = 2, P("Windows давно не обновлялась — проверьте Центр обновления или сбросьте его в «Обслуживании».", "Windows hasn't updated for a long time — check Windows Update or reset it in Maintenance."), "maintenance"
			} else if days > 25 {
				wu.Level = 1
			}
		}
		out = append(out, wu)
	}
	// UAC
	lm := registry.LOCAL_MACHINE
	uac := SecItem{ID: "uac", Title: P("Контроль учётных записей (UAC)", "User Account Control (UAC)"), Level: 0, State: P("включён", "on")}
	if v, ok := regDword(lm, `SOFTWARE\Microsoft\Windows\CurrentVersion\Policies\System`, "EnableLUA"); ok && v == 0 {
		uac.Level, uac.State = 2, P("выключен — любая программа получает права администратора без запроса", "off — any app gets admin rights without asking")
	} else if v, ok := regDword(lm, `SOFTWARE\Microsoft\Windows\CurrentVersion\Policies\System`, "ConsentPromptBehaviorAdmin"); ok && v == 0 {
		uac.Level, uac.State = 1, P("повышение прав без запроса", "elevation without prompt")
	}
	out = append(out, uac)
	// Secure Boot
	sb := SecItem{ID: "secureboot", Title: "Secure Boot", Level: -1, State: P("не поддерживается (режим Legacy/CSM)", "not supported (Legacy/CSM mode)")}
	if v, ok := regDword(lm, `SYSTEM\CurrentControlSet\Control\SecureBoot\State`, "UEFISecureBootEnabled"); ok {
		if v == 1 {
			sb.Level, sb.State = 0, P("включена", "on")
		} else {
			sb.Level, sb.State, sb.Detail = 1, P("выключена", "off"), P("Включается в BIOS/UEFI. Нужна для Windows 11 и защиты от буткитов.", "Enabled in BIOS/UEFI. Required for Windows 11 and protects from bootkits.")
		}
	}
	out = append(out, sb)
	// SMBv1
	if v, ok := regDword(lm, `SYSTEM\CurrentControlSet\Services\mrxsmb10`, "Start"); ok && v != 4 {
		out = append(out, SecItem{ID: "smb1", Title: "SMBv1", Level: 2, State: P("включён — устаревший протокол, через него распространялся WannaCry", "on — an obsolete protocol abused by WannaCry"),
			Detail: P("Отключите компонент «Поддержка общего доступа к файлам SMB 1.0» в «Компонентах Windows».", "Turn off “SMB 1.0/CIFS File Sharing Support” in Windows Features.")})
	} else {
		out = append(out, SecItem{ID: "smb1", Title: "SMBv1", Level: 0, State: P("отключён", "off")})
	}
	// Удалённый рабочий стол
	if v, ok := regDword(lm, `SYSTEM\CurrentControlSet\Control\Terminal Server`, "fDenyTSConnections"); ok && v == 0 {
		out = append(out, SecItem{ID: "rdp", Title: P("Удалённый рабочий стол", "Remote Desktop"), Level: 1, State: P("разрешён", "allowed"),
			Detail: P("Если вы им не пользуетесь, отключите в «Параметры → Система → Удалённый рабочий стол».", "If you don't use it, turn it off in Settings → System → Remote Desktop.")})
	}
	// Автообновление отключено политикой
	if v, ok := regDword(lm, `SOFTWARE\Policies\Microsoft\Windows\WindowsUpdate\AU`, "NoAutoUpdate"); ok && v == 1 {
		out = append(out, SecItem{ID: "noau", Title: P("Автообновление", "Automatic updates"), Level: 2, State: P("отключено групповой политикой", "disabled by group policy")})
	}
	return out
}

// ---------- Сеть ----------

const netScript = `
$o = @()
foreach ($a in (Get-NetAdapter -EA SilentlyContinue)) {
  $c = Get-NetIPConfiguration -InterfaceIndex $a.ifIndex -EA SilentlyContinue
  $o += [pscustomobject]@{ n=$a.Name; d=$a.InterfaceDescription; s=[string]$a.Status; sp=[string]$a.LinkSpeed; m=[string]$a.MacAddress; v=[bool]$a.Virtual; w=([string]$a.PhysicalMediaType -match '802\.11|Native');
    ip4=@($c.IPv4Address | ForEach-Object { $_.IPAddress }); ip6=@($c.IPv6Address | ForEach-Object { $_.IPAddress }); gw=@($c.IPv4DefaultGateway | ForEach-Object { $_.NextHop });
    dns=@($c.DNSServer | Where-Object { $_.AddressFamily -eq 2 } | ForEach-Object { $_.ServerAddresses }) }
}
ConvertTo-Json -InputObject @($o) -Compress -Depth 4
`

// Adapters возвращает сетевые адаптеры с адресами, шлюзом и DNS.
func Adapters() []Adapter {
	var raw []struct {
		N, D, S, Sp, M    string
		V, W              bool
		Ip4, Ip6, Gw, Dns []string
	}
	if err := winps.JSON(netScript, 60*time.Second, &raw); err != nil || len(raw) == 0 {
		return basicAdapters()
	}
	var out []Adapter
	nz := func(s []string) []string {
		if s == nil {
			return []string{}
		}
		return s
	}
	for _, r := range raw {
		out = append(out, Adapter{Name: r.N, Desc: r.D, Status: r.S, Speed: r.Sp, MAC: r.M, Virtual: r.V, Wifi: r.W,
			IPv4: nz(r.Ip4), IPv6: nz(r.Ip6), Gateway: nz(r.Gw), DNS: nz(r.Dns)})
	}
	sort.SliceStable(out, func(i, j int) bool {
		ui, uj := out[i].Status == "Up", out[j].Status == "Up"
		if ui != uj {
			return ui
		}
		return !out[i].Virtual && out[j].Virtual
	})
	return out
}

// Traceroute запускает трассировку в фоне (журнал — как у обслуживания).
func Traceroute(host string) error {
	if !ValidHost(host) {
		return fmt.Errorf("bad host")
	}
	return maintenance.StartJob("tracert", "tracert "+host, func(emit func(string)) error {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		// chcp 65001 — вывод в UTF-8 вместо кодовой страницы OEM
		cmd := exec.CommandContext(ctx, "cmd.exe")
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000,
			CmdLine: `cmd.exe /d /c "chcp 65001 >nul & tracert -d -h 24 -w 800 ` + host + `"`}
		out, err := cmd.StdoutPipe()
		if err != nil {
			return err
		}
		if err := cmd.Start(); err != nil {
			return err
		}
		sc := bufio.NewScanner(out)
		for sc.Scan() {
			if l := strings.TrimRight(sc.Text(), "\r "); strings.TrimSpace(l) != "" {
				emit(l)
			}
		}
		return cmd.Wait()
	})
}
