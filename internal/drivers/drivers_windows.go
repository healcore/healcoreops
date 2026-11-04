//go:build windows

package drivers

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
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

// Supported — работа с драйверами доступна в Windows.
func Supported() bool { return true }

func powershell(script string, timeout time.Duration) ([]byte, error) {
	full := "$ProgressPreference='SilentlyContinue'\n[Console]::OutputEncoding=[Text.Encoding]::UTF8\n" + script
	u := utf16.Encode([]rune(full))
	b := make([]byte, len(u)*2)
	for i, v := range u {
		b[2*i], b[2*i+1] = byte(v), byte(v>>8)
	}
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass",
		"-EncodedCommand", base64.StdEncoding.EncodeToString(b))
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	type res struct {
		out []byte
		err error
	}
	ch := make(chan res, 1)
	go func() { o, e := cmd.Output(); ch <- res{o, e} }()
	select {
	case r := <-ch:
		return r.out, r.err
	case <-time.After(timeout):
		_ = cmd.Process.Kill()
		return nil, errors.New(i18n.T("превышено время ожидания"))
	}
}

// категории устройств: класс PnP → понятное название.
var categories = map[string]string{
	"Display": "Видеокарта", "Net": "Сеть", "Media": "Звук", "AudioEndpoint": "Звук", "MEDIA": "Звук",
	"Bluetooth": "Bluetooth", "System": "Система и чипсет", "Processor": "Процессор",
	"SCSIAdapter": "Контроллеры дисков", "DiskDrive": "Накопители", "Monitor": "Мониторы",
	"Printer": "Принтеры", "Image": "Камеры и сканеры", "Camera": "Камеры и сканеры",
	"USB": "Контроллеры USB", "HIDClass": "Устройства ввода", "Keyboard": "Клавиатуры", "Mouse": "Мыши",
}

// Собираем материнскую плату, процессор, видеокарту/сеть/звук и сведения о системе.
// Класс «Processor» НЕ берём из PnP — он перечисляет каждый логический процессор;
// вместо этого добавляем одну запись ЦП из Win32_Processor.
const listScript = `$ErrorActionPreference='SilentlyContinue'
$bb  = Get-CimInstance Win32_BaseBoard | Select-Object -First 1
$cpu = Get-CimInstance Win32_Processor | Select-Object -First 1
$os  = Get-CimInstance Win32_OperatingSystem
$cs  = Get-CimInstance Win32_ComputerSystem
$signed = @{}
Get-CimInstance Win32_PnPSignedDriver | ForEach-Object { if($_.DeviceID){ $signed[$_.DeviceID] = $_ } }
$main = 'Display','Net','Media','AudioEndpoint','MEDIA'
$devs = Get-PnpDevice -PresentOnly | Where-Object { $main -contains $_.Class }
$out = foreach($d in $devs){
  $s = $signed[$d.InstanceId]
  $dt = ''
  if($s.DriverDate){ try { $dt = ([Management.ManagementDateTimeConverter]::ToDateTime($s.DriverDate)).ToString('yyyy-MM-dd') } catch { } }
  [pscustomobject]@{
    name=[string]$d.FriendlyName; class=[string]$d.Class; status=[string]$d.Status;
    manu=[string]$s.Manufacturer; version=[string]$s.DriverVersion; date=$dt; id=[string]$d.InstanceId
  }
}
[pscustomobject]@{
  boardManu=[string]$bb.Manufacturer; boardProduct=[string]$bb.Product;
  cpuName=[string]$cpu.Name; cpuManu=[string]$cpu.Manufacturer;
  osCaption=[string]$os.Caption; osBuild=[string]$os.BuildNumber; osVersion=[string]$os.Version;
  ram=[int64]$cs.TotalPhysicalMemory;
  devices=@($out)
} | ConvertTo-Json -Depth 4 -Compress`

type rawDev struct {
	Name, Class, Status, Manu, Version, Date, ID string
}

type rawList struct {
	BoardManu, BoardProduct string
	CPUName, CPUManu        string
	OSCaption, OSBuild      string
	RAM                     int64
	Devices                 []rawDev
}

func catName(class string) string {
	ru := categories[class]
	if ru == "" {
		return i18n.Pick("Прочее", "Other")
	}
	en := map[string]string{"Display": "Graphics", "Net": "Network", "Media": "Audio", "AudioEndpoint": "Audio", "MEDIA": "Audio"}
	e := en[class]
	if e == "" {
		e = ru
	}
	return i18n.Pick(ru, e)
}

// List возвращает материнскую плату, процессор и основные устройства со
// ссылками на официальные страницы драйверов производителей.
func List() ([]Device, error) {
	out, err := powershell(listScript, 60*time.Second)
	if err != nil {
		return nil, err
	}
	var r rawList
	if e := json.Unmarshal(out, &r); e != nil {
		return nil, e
	}

	var gpuName string
	devs := make([]Device, 0, len(r.Devices)+2)

	// 1) Материнская плата — все её драйверы на странице вендора.
	if r.BoardManu != "" || r.BoardProduct != "" {
		name := join(r.BoardManu, r.BoardProduct)
		vendor, url, wg := vendorSite(r.BoardManu + " " + r.BoardProduct)
		devs = append(devs, Device{
			ID: "board", Name: name, Class: "BaseBoard",
			Category: i18n.Pick("Материнская плата", "Motherboard"),
			Manu:     r.BoardManu, Vendor: vendor, VendorURL: url, Winget: wg, Status: "OK",
		})
	}
	// 2) Процессор — одна запись (а не по ядру/потоку).
	if r.CPUName != "" {
		vendor, url, wg := vendorSite(r.CPUName + " " + r.CPUManu)
		devs = append(devs, Device{
			ID: "cpu", Name: r.CPUName, Class: "Processor",
			Category: i18n.Pick("Процессор", "Processor"),
			Manu:     r.CPUManu, Vendor: vendor, VendorURL: url, Winget: wg, Status: "OK",
		})
	}
	// 3) Видеокарта, сеть, звук.
	for _, d := range r.Devices {
		name := d.Name
		if name == "" {
			name = i18n.Pick("Неизвестное устройство", "Unknown device")
		}
		if d.Class == "Display" && gpuName == "" {
			gpuName = name
		}
		vendor, url, wg := vendorSite(name + " " + d.Manu)
		devs = append(devs, Device{
			ID: d.ID, Name: name, Class: d.Class, Category: catName(d.Class),
			Manu: d.Manu, Version: d.Version, Date: d.Date, Status: d.Status,
			NeedsDriver: d.Status != "OK" || d.Version == "",
			Vendor:      vendor, VendorURL: url, Winget: wg,
		})
	}

	setSysInfo(SysInfo{
		Board: join(r.BoardManu, r.BoardProduct),
		CPU:   r.CPUName,
		GPU:   gpuName,
		OS:    strings.TrimSpace(r.OSCaption + " (" + i18n.Pick("сборка", "build") + " " + r.OSBuild + ")"),
		RAM:   bytesGB(r.RAM),
	})
	return devs, nil
}

func join(a, b string) string {
	a, b = strings.TrimSpace(a), strings.TrimSpace(b)
	switch {
	case a == "":
		return b
	case b == "":
		return a
	default:
		return a + " " + b
	}
}

func bytesGB(b int64) string {
	if b <= 0 {
		return ""
	}
	return fmt.Sprintf("%.0f %s", float64(b)/(1<<30), i18n.Pick("ГБ", "GB"))
}

// Scan ищет обновления драйверов в Windows Update (может занять минуту).
func Scan() error {
	return runJob("scan", func(e func(string)) error {
		e("▸ " + i18n.T("Поиск обновлений драйверов в Windows Update…"))
		const s = `$ErrorActionPreference='Stop'
try {
  $ses = New-Object -ComObject Microsoft.Update.Session
  $sr = $ses.CreateUpdateSearcher()
  $r = $sr.Search("IsInstalled=0 and Type='Driver'")
  $out = foreach($u in $r.Updates){ [pscustomobject]@{ id=[string]$u.Identity.UpdateID; title=[string]$u.Title; date=[string]$u.DriverVerDate } }
  'JSON:' + (ConvertTo-Json -InputObject @($out) -Depth 3 -Compress)
} catch { 'ERR:' + $_.Exception.Message }`
		out, err := powershell(s, 5*time.Minute)
		if err != nil {
			return err
		}
		line := strings.TrimSpace(string(out))
		if i := strings.Index(line, "ERR:"); i >= 0 {
			return errors.New(strings.TrimSpace(line[i+4:]))
		}
		var ups []Update
		if i := strings.Index(line, "JSON:"); i >= 0 {
			payload := strings.TrimSpace(line[i+5:])
			if json.Unmarshal([]byte(payload), &ups) != nil {
				var one Update
				if json.Unmarshal([]byte(payload), &one) == nil && one.ID != "" {
					ups = []Update{one}
				}
			}
		}
		jmu.Lock()
		updates, scanned = ups, true
		jmu.Unlock()
		e(fmt.Sprintf(i18n.T("Найдено обновлений драйверов: %d"), len(ups)))
		return nil
	})
}

// Install скачивает и устанавливает выбранные обновления драйверов (нужен admin).
func Install(ids []string) error {
	if !ctl.IsElevated() {
		return errors.New(i18n.T("нужны права администратора — нажмите «Перезапустить от имени администратора»"))
	}
	if len(ids) == 0 {
		return errors.New(i18n.T("не выбрано ни одного обновления"))
	}
	list := "'" + strings.Join(ids, "','") + "'"
	return runJob("install", func(e func(string)) error {
		e("▸ " + i18n.T("Установка драйверов из Windows Update…"))
		s := `$ErrorActionPreference='Stop'
$ids = @(` + list + `)
try {
  $ses = New-Object -ComObject Microsoft.Update.Session
  $sr = $ses.CreateUpdateSearcher()
  $r = $sr.Search("IsInstalled=0 and Type='Driver'")
  $coll = New-Object -ComObject Microsoft.Update.UpdateColl
  foreach($u in $r.Updates){ if($ids -contains [string]$u.Identity.UpdateID){ [void]$coll.Add($u) } }
  if($coll.Count -eq 0){ 'ERR:' + 'updates not found'; exit }
  $dl = $ses.CreateUpdateDownloader(); $dl.Updates = $coll; [void]$dl.Download()
  $inst = $ses.CreateUpdateInstaller(); $inst.Updates = $coll; $res = $inst.Install()
  'CODE:' + [string]$res.ResultCode + ':' + [string]$res.RebootRequired
} catch { 'ERR:' + $_.Exception.Message }`
		out, err := powershell(s, 30*time.Minute)
		if err != nil {
			return err
		}
		line := strings.TrimSpace(string(out))
		if i := strings.Index(line, "ERR:"); i >= 0 {
			return errors.New(strings.TrimSpace(line[i+4:]))
		}
		// ResultCode 2 = Succeeded
		if strings.Contains(line, "CODE:2:") {
			e("✓ " + i18n.T("драйверы установлены"))
			if strings.HasSuffix(line, ":True") {
				e(i18n.T("Для завершения требуется перезагрузка"))
			}
			// обновим список после установки
			_ = Scan()
			return nil
		}
		return errors.New(i18n.T("Windows Update вернул ошибку установки"))
	})
}

// wingetAvailable сообщает, установлен ли менеджер пакетов winget.
func wingetAvailable() bool {
	_, err := exec.LookPath("winget")
	return err == nil
}

// InstallVendor ставит официальное средство вендора через winget (из
// официального источника с проверкой издателя и контрольной суммы). Если это
// невозможно, открывает официальную страницу и возвращает mode="page".
func InstallVendor(winget, url string) (string, error) {
	if winget != "" && WingetAllowed(winget) && wingetAvailable() {
		err := runJob("install", func(e func(string)) error {
			e("▸ " + fmt.Sprintf(i18n.T("Установка через winget: %s"), winget))
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
			defer cancel()
			cmd := exec.CommandContext(ctx, "winget", "install", "--id", winget, "-e",
				"--silent", "--accept-package-agreements", "--accept-source-agreements")
			cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
			out, err := cmd.CombinedOutput()
			if ctx.Err() == context.DeadlineExceeded {
				return errors.New(i18n.T("превышено время ожидания"))
			}
			if err != nil {
				if s := strings.TrimSpace(string(out)); s != "" {
					e(lastLines(s, 3))
				}
				// Не удалось через winget — открываем официальную страницу.
				_ = OpenVendor(url)
				return errors.New(i18n.T("winget не смог установить — открыта официальная страница"))
			}
			e("✓ " + i18n.T("установлено"))
			return nil
		})
		if err != nil {
			return "", err
		}
		return "install", nil
	}
	// Автоустановка недоступна для этого производителя — открываем его сайт.
	if err := OpenVendor(url); err != nil {
		return "", err
	}
	return "page", nil
}

func lastLines(s string, n int) string {
	ls := strings.Split(strings.TrimSpace(s), "\n")
	if len(ls) > n {
		ls = ls[len(ls)-n:]
	}
	return strings.TrimSpace(strings.Join(ls, " "))
}
