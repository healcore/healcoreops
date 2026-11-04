//go:build windows

package drivers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/healcore/healcoreops/internal/ctl"
	"github.com/healcore/healcoreops/internal/i18n"
	"github.com/healcore/healcoreops/internal/winps"
)

const userAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) HealCoreOps"

func httpGet(ctx context.Context, u string) ([]byte, error) {
	req, _ := http.NewRequestWithContext(ctx, "GET", u, nil)
	req.Header.Set("User-Agent", userAgent)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 32<<20))
}

// ---------- NVIDIA ----------

func nvCheck(d Device, emit func(string)) (Avail, error) {
	a := Avail{ID: d.ID, Current: NvidiaVersion(d.Version)}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	b, err := httpGet(ctx, "https://www.nvidia.com/Download/API/lookupValueSearch.aspx?TypeID=3")
	if err != nil {
		return a, err
	}
	fams, err := ParseNvLookup(b)
	if err != nil {
		return a, err
	}
	f, ok := MatchNvFamily(d.Name, fams)
	if !ok {
		return a, errors.New(i18n.Pick("модель не найдена в справочнике NVIDIA", "model not found in NVIDIA catalog"))
	}
	q := fmt.Sprintf("https://gfwsl.geforce.com/services_toolkit/services/com/nvidia/services/AjaxDriverService.php?func=DriverManualLookup&psid=%s&pfid=%s&osID=57&languageCode=1033&beta=0&isWHQL=1&dltype=-1&dch=1&upCRD=0&qnf=0&sort1=0&numberOfResults=1", f.Series, f.ID)
	b, err = httpGet(ctx, q)
	if err != nil {
		return a, err
	}
	v, link, size, err := ParseNvDriver(b)
	if err != nil {
		return a, err
	}
	a.Version, a.url, a.Size = v, link, size
	a.Title = "NVIDIA Game Ready " + v
	if a.Current != "" && CompareVersions(a.Current, v) >= 0 {
		a.Source = "current"
	} else {
		a.Source = "nvidia"
	}
	emit(fmt.Sprintf("  %s: %s → %s (nvidia.com)", d.Name, orDash(a.Current), v))
	return a, nil
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func download(ctx context.Context, link, dst string, emit func(string)) error {
	if !NvidiaHostOK(link) {
		return errors.New("download host not allowed")
	}
	req, _ := http.NewRequestWithContext(ctx, "GET", link, nil)
	req.Header.Set("User-Agent", userAgent)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	if !NvidiaHostOK(resp.Request.URL.String()) { // после перенаправлений — тоже NVIDIA
		return errors.New("download redirected to a foreign host")
	}
	_ = os.MkdirAll(filepath.Dir(dst), 0o755)
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	total := resp.ContentLength
	var done int64
	next := 10
	buf := make([]byte, 1<<20)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := f.Write(buf[:n]); werr != nil {
				f.Close()
				return werr
			}
			done += int64(n)
			if total > 0 && int(done*100/total) >= next {
				emit(fmt.Sprintf("  %s %d%% (%d / %d МБ)", i18n.Pick("загрузка", "downloading"), next, done>>20, total>>20))
				next += 10
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			f.Close()
			return rerr
		}
	}
	return f.Close()
}

// verifySigned проверяет цифровую подпись файла и издателя.
func verifySigned(path, publisher string) error {
	out, err := winps.Run(`$s = Get-AuthenticodeSignature -LiteralPath `+winps.Quote(path)+`
$s.Status.ToString() + '|' + $s.SignerCertificate.Subject`, 2*time.Minute)
	if err != nil {
		return err
	}
	res := strings.TrimSpace(string(out))
	if !strings.HasPrefix(res, "Valid|") || !strings.Contains(res, publisher) {
		return fmt.Errorf(i18n.Pick("подпись файла не прошла проверку (%s) — установка отменена", "signature check failed (%s) — install cancelled"), res)
	}
	return nil
}

// installedVersion — текущая версия драйвера устройства.
func installedVersion(id string) string {
	out, _ := winps.Run(`(Get-CimInstance Win32_PnPSignedDriver | Where-Object { $_.DeviceID -eq `+winps.Quote(id)+` } | Select-Object -First 1).DriverVersion`, time.Minute)
	return strings.TrimSpace(string(out))
}

func nvInstall(d Device, a Avail, emit func(string)) error {
	emit("▸ " + d.Name + ": NVIDIA " + a.Version)
	dir := filepath.Join(os.TempDir(), "HealCoreOps", "drivers")
	dst := filepath.Join(dir, "nvidia-"+a.Version+".exe")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Minute)
	defer cancel()
	emit("  " + i18n.Pick("загрузка с download.nvidia.com", "downloading from download.nvidia.com"))
	if err := download(ctx, a.url, dst, emit); err != nil {
		return err
	}
	defer os.Remove(dst)
	emit("  " + i18n.Pick("проверка цифровой подписи NVIDIA…", "checking the NVIDIA digital signature…"))
	if err := verifySigned(dst, "NVIDIA Corporation"); err != nil {
		return err
	}
	emit("  ✓ " + i18n.Pick("подпись NVIDIA Corporation действительна", "NVIDIA Corporation signature is valid"))
	emit("  " + i18n.Pick("тихая установка (экран может мигнуть)…", "silent install (the screen may flicker)…"))
	ictx, icancel := context.WithTimeout(context.Background(), 45*time.Minute)
	defer icancel()
	cmd := exec.CommandContext(ictx, dst, "-s", "-noreboot")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	runErr := cmd.Run()
	if nv := NvidiaVersion(installedVersion(d.ID)); nv != "" && CompareVersions(nv, a.Version) >= 0 {
		emit("  ✓ " + i18n.Pick("установлена версия ", "installed version ") + nv)
		return nil
	}
	// Тихий режим не сработал — запускаем официальный установщик с окном.
	if runErr != nil {
		emit("  " + i18n.Pick("тихая установка не завершилась: ", "silent install did not finish: ") + runErr.Error())
	}
	keep := filepath.Join(dir, "nvidia-"+a.Version+"-setup.exe")
	_ = os.Rename(dst, keep)
	if err := exec.Command(keep).Start(); err != nil {
		return err
	}
	emit("  " + i18n.Pick("открыт официальный установщик NVIDIA — завершите установку в его окне", "the official NVIDIA installer is open — finish the install in its window"))
	return nil
}

// ---------- Windows Update ----------

const wuSearchScript = `$ErrorActionPreference='Stop'
try {
  $ses = New-Object -ComObject Microsoft.Update.Session
  $sr = $ses.CreateUpdateSearcher()
  $r = $sr.Search("IsInstalled=0 and IsHidden=0 and Type='Driver'")
  $out = foreach($u in $r.Updates){
    $d = ''; try { if ($u.DriverVerDate) { $d = ([datetime]$u.DriverVerDate).ToString('yyyy-MM-dd') } } catch {}
    [pscustomobject]@{ id=[string]$u.Identity.UpdateID; title=[string]$u.Title; date=$d; manu=[string]$u.DriverManufacturer;
      cls=[string]$u.DriverClass; hw=[string]$u.DriverHardwareID; model=[string]$u.DriverModel; size=[int64]$u.MaxDownloadSize }
  }
  'JSON:' + (ConvertTo-Json -InputObject @($out) -Depth 3 -Compress)
} catch { 'ERR:' + $_.Exception.Message }`

func wuSearch() ([]WUDriver, error) {
	out, err := powershell(wuSearchScript, 5*time.Minute)
	if err != nil {
		return nil, err
	}
	s := strings.TrimSpace(string(out))
	if i := strings.Index(s, "ERR:"); i >= 0 {
		return nil, errors.New(strings.TrimSpace(s[i+4:]))
	}
	var ups []WUDriver
	if i := strings.Index(s, "JSON:"); i >= 0 {
		p := strings.TrimSpace(s[i+5:])
		if json.Unmarshal([]byte(p), &ups) != nil {
			var one WUDriver
			if json.Unmarshal([]byte(p), &one) == nil && one.ID != "" {
				ups = []WUDriver{one}
			}
		}
	}
	return ups, nil
}

func wuInstall(ids []string, emit func(string)) (reboot bool, err error) {
	for _, id := range ids {
		if strings.ContainsAny(id, "'\"`$;") {
			return false, errors.New("bad update id")
		}
	}
	s := `$ErrorActionPreference='Stop'
$ids = @('` + strings.Join(ids, "','") + `')
try {
  $ses = New-Object -ComObject Microsoft.Update.Session
  $r = $ses.CreateUpdateSearcher().Search("IsInstalled=0 and Type='Driver'")
  $coll = New-Object -ComObject Microsoft.Update.UpdateColl
  foreach($u in $r.Updates){ if($ids -contains [string]$u.Identity.UpdateID){ if(-not $u.EulaAccepted){ $u.AcceptEula() }; [void]$coll.Add($u) } }
  if($coll.Count -eq 0){ 'ERR:updates not found'; exit }
  $dl = $ses.CreateUpdateDownloader(); $dl.Updates = $coll; [void]$dl.Download()
  $inst = $ses.CreateUpdateInstaller(); $inst.Updates = $coll; $res = $inst.Install()
  $ok = 0; for($i=0; $i -lt $coll.Count; $i++){ if($res.GetUpdateResult($i).ResultCode -eq 2){ $ok++ } }
  'CODE:' + [string]$res.ResultCode + ':' + $ok + ':' + [string]$res.RebootRequired
} catch { 'ERR:' + $_.Exception.Message }`
	out, err := powershell(s, 45*time.Minute)
	if err != nil {
		return false, err
	}
	line := strings.TrimSpace(string(out))
	if i := strings.Index(line, "ERR:"); i >= 0 {
		return false, errors.New(strings.TrimSpace(line[i+4:]))
	}
	i := strings.Index(line, "CODE:")
	if i < 0 {
		return false, errors.New(i18n.T("Windows Update вернул ошибку установки"))
	}
	parts := strings.Split(line[i+5:], ":")
	if len(parts) < 3 {
		return false, errors.New(i18n.T("Windows Update вернул ошибку установки"))
	}
	reboot = strings.EqualFold(strings.TrimSpace(parts[2]), "True")
	emit(fmt.Sprintf("  ✓ %s %s / %d", i18n.Pick("установлено пакетов:", "packages installed:"), parts[1], len(ids)))
	if parts[0] != "2" && parts[0] != "3" { // 2 — успешно, 3 — успешно с ошибками
		return reboot, errors.New(i18n.T("Windows Update вернул ошибку установки"))
	}
	return reboot, nil
}

// ---------- Проверка и обновление ----------

func wuVersion(title string) string {
	if i := strings.LastIndex(title, " - "); i >= 0 {
		v := strings.TrimSpace(title[i+3:])
		if v != "" && strings.ContainsAny(v[:1], "0123456789") {
			return v
		}
	}
	return ""
}

func doCheck(allowWU bool, emit func(string)) (map[string]Avail, error) {
	devs := lastDevices()
	if len(devs) == 0 {
		var err error
		if devs, err = List(); err != nil {
			return nil, err
		}
	}
	res := map[string]Avail{}
	for _, d := range devs {
		if IsNvidiaGPU(d) {
			emit("▸ " + i18n.Pick("NVIDIA: поиск последнего драйвера…", "NVIDIA: looking up the latest driver…"))
			if a, err := nvCheck(d, emit); err == nil {
				res[d.ID] = a
			} else {
				emit("  NVIDIA: " + err.Error())
			}
		}
	}
	var matched map[string][]WUDriver
	if allowWU {
		emit("▸ " + i18n.Pick("Поиск драйверов производителей в каталоге Windows Update… (до пары минут)", "Searching manufacturers' drivers in the Windows Update catalog… (up to a couple of minutes)"))
		ups, err := wuSearch()
		if err != nil {
			emit("  Windows Update: " + err.Error())
		} else {
			matched = MatchWU(devs, ups)
			emit(fmt.Sprintf("  %s %d", i18n.Pick("найдено пакетов:", "packages found:"), len(ups)))
		}
	}
	for _, d := range devs {
		a, ok := res[d.ID]
		if ok && a.Source == "nvidia" {
			continue
		}
		if list := matched[d.ID]; len(list) > 0 {
			na := Avail{ID: d.ID, Source: "wu", Current: d.Version, Title: list[0].Title, Version: wuVersion(list[0].Title)}
			for _, u := range list {
				na.wuIDs = append(na.wuIDs, u.ID)
				na.Size += u.Size
			}
			if len(list) > 1 {
				na.Title = fmt.Sprintf(i18n.Pick("%s и ещё %d", "%s and %d more"), list[0].Title, len(list)-1)
			}
			res[d.ID] = na
			continue
		}
		if ok {
			continue // NVIDIA: актуальная версия
		}
		switch {
		case d.Version != "" || d.ID == "board" || d.ID == "cpu":
			res[d.ID] = Avail{ID: d.ID, Source: "current", Current: d.Version}
		default:
			res[d.ID] = Avail{ID: d.ID, Source: "none", Note: i18n.Pick("драйвер не найден в официальных источниках", "no driver found in official sources")}
		}
	}
	n := 0
	for _, a := range res {
		if a.HasUpdate() {
			n++
		}
	}
	emit(fmt.Sprintf("✓ %s %d", i18n.Pick("Устройств с доступным обновлением:", "Devices with an available update:"), n))
	return res, nil
}

// Check ищет обновления драйверов для всех устройств (в фоне).
func Check(allowWU bool) error {
	return runJob("check", func(emit func(string)) error {
		res, err := doCheck(allowWU, emit)
		if err != nil {
			return err
		}
		setAvails(res)
		return nil
	})
}

// Update устанавливает обновления для выбранных устройств (all — для всех, где есть обновление).
func Update(ids []string, all, allowWU bool) error {
	if !ctl.IsElevated() {
		return errors.New(i18n.T("нужны права администратора — нажмите «Перезапустить от имени администратора»"))
	}
	return runJob("update", func(emit func(string)) error {
		av, ok := Avails()
		if !ok {
			var err error
			if av, err = doCheck(allowWU, emit); err != nil {
				return err
			}
			setAvails(av)
		}
		devs := map[string]Device{}
		for _, d := range lastDevices() {
			devs[d.ID] = d
		}
		if all {
			ids = ids[:0]
			for id, a := range av {
				if a.HasUpdate() {
					ids = append(ids, id)
				}
			}
		}
		var wuIDs []string
		var wuDevs []string
		failed, done := 0, 0
		for _, id := range ids {
			a, d := av[id], devs[id]
			switch a.Source {
			case "nvidia":
				if err := nvInstall(d, a, emit); err != nil {
					emit("  × " + err.Error())
					failed++
				} else {
					done++
					a.Source, a.Current = "current", a.Version
					av[id] = a
				}
			case "wu":
				wuIDs = append(wuIDs, a.wuIDs...)
				wuDevs = append(wuDevs, id)
				emit("▸ " + d.Name + ": " + a.Title)
			default:
				emit("  " + d.Name + ": " + i18n.Pick("установлен актуальный драйвер", "the driver is up to date"))
			}
		}
		if len(wuIDs) > 0 {
			emit("▸ " + i18n.Pick("Загрузка и установка пакетов производителей…", "Downloading and installing manufacturers' packages…"))
			reboot, err := wuInstall(wuIDs, emit)
			if err != nil {
				emit("  × " + err.Error())
				failed += len(wuDevs)
			} else {
				done += len(wuDevs)
				for _, id := range wuDevs {
					a := av[id]
					a.Source, a.Note = "current", i18n.Pick("установлено", "installed")
					av[id] = a
				}
			}
			if reboot {
				emit("! " + i18n.T("Для завершения требуется перезагрузка"))
			}
		}
		setAvails(av)
		_, _ = List() // обновим версии
		emit(fmt.Sprintf("✓ %s %d · %s %d", i18n.Pick("обновлено:", "updated:"), done, i18n.Pick("ошибок:", "errors:"), failed))
		if failed > 0 {
			return fmt.Errorf(i18n.Pick("не установлено: %d", "not installed: %d"), failed)
		}
		return nil
	})
}
