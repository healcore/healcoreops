//go:build windows

package drivers

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"syscall"
	"time"
	"unicode/utf16"

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
$hwids = @{}
Get-CimInstance Win32_PnPEntity | ForEach-Object { if($_.PNPDeviceID){ $hwids[$_.PNPDeviceID] = @($_.HardwareID) } }
$main = 'Display','Net','Media','AudioEndpoint','MEDIA'
$devs = Get-PnpDevice -PresentOnly | Where-Object { $main -contains $_.Class }
$out = foreach($d in $devs){
  $s = $signed[$d.InstanceId]
  $dt = ''
  if($s.DriverDate){ try { $dt = ([Management.ManagementDateTimeConverter]::ToDateTime($s.DriverDate)).ToString('yyyy-MM-dd') } catch { } }
  [pscustomobject]@{
    name=[string]$d.FriendlyName; class=[string]$d.Class; status=[string]$d.Status;
    manu=[string]$s.Manufacturer; version=[string]$s.DriverVersion; date=$dt; id=[string]$d.InstanceId;
    hw=@($hwids[$d.InstanceId] | Where-Object { $_ })
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
	HW                                           []string
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
		vendor, url := vendorSite(r.BoardManu + " " + r.BoardProduct)
		devs = append(devs, Device{
			ID: "board", Name: name, Class: "BaseBoard",
			Category: i18n.Pick("Материнская плата", "Motherboard"),
			Manu:     r.BoardManu, Vendor: vendor, VendorURL: url, Status: "OK",
		})
	}
	// 2) Процессор — одна запись (а не по ядру/потоку).
	if r.CPUName != "" {
		vendor, url := vendorSite(r.CPUName + " " + r.CPUManu)
		devs = append(devs, Device{
			ID: "cpu", Name: r.CPUName, Class: "Processor",
			Category: i18n.Pick("Процессор", "Processor"),
			Manu:     r.CPUManu, Vendor: vendor, VendorURL: url, Status: "OK",
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
		vendor, url := vendorSite(name + " " + d.Manu)
		devs = append(devs, Device{
			ID: d.ID, Name: name, Class: d.Class, Category: catName(d.Class),
			Manu: d.Manu, Version: d.Version, Date: d.Date, Status: d.Status,
			NeedsDriver: d.Status != "OK" || d.Version == "",
			Vendor:      vendor, VendorURL: url, HW: d.HW,
		})
	}

	setLastDevices(devs)
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
