//go:build windows

package hw

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os/exec"
	"strings"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

var procGetSystemPowerStatus = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetSystemPowerStatus")

type systemPowerStatus struct {
	ACLineStatus        byte
	BatteryFlag         byte
	BatteryLifePercent  byte
	SystemStatusFlag    byte
	BatteryLifeTime     uint32
	BatteryFullLifeTime uint32
}

func powershellJSON(script string, v any) error {
	full := "$ProgressPreference='SilentlyContinue'\n$ErrorActionPreference='SilentlyContinue'\n[Console]::OutputEncoding=[Text.Encoding]::UTF8\n" + script
	u := utf16.Encode([]rune(full))
	b := make([]byte, len(u)*2)
	for i, x := range u {
		b[2*i], b[2*i+1] = byte(x), byte(x>>8)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass",
		"-EncodedCommand", base64.StdEncoding.EncodeToString(b))
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	out, err := cmd.Output()
	if err != nil {
		return err
	}
	s := strings.TrimSpace(string(out))
	if i := strings.IndexAny(s, "{["); i > 0 {
		s = s[i:]
	}
	return json.Unmarshal([]byte(s), v)
}

func battery() Battery {
	var b Battery
	b.Minutes = -1
	var ps systemPowerStatus
	if r, _, _ := procGetSystemPowerStatus.Call(uintptr(unsafe.Pointer(&ps))); r == 0 {
		return b
	}
	b.OnAC = ps.ACLineStatus == 1
	if ps.BatteryFlag&128 != 0 || ps.BatteryFlag == 255 { // нет батареи / неизвестно
		return b
	}
	b.Present = true
	if ps.BatteryLifePercent <= 100 {
		b.Percent = int(ps.BatteryLifePercent)
	}
	b.Charging = ps.BatteryFlag&8 != 0
	if ps.BatteryLifeTime != 0xFFFFFFFF {
		b.Minutes = int(ps.BatteryLifeTime / 60)
	}

	// Износ: проектная ёмкость против текущей полной (как в powercfg /batteryreport).
	var w struct {
		Design uint64 `json:"design"`
		Full   uint64 `json:"full"`
		Cycles int    `json:"cycles"`
	}
	_ = powershellJSON(`$d = (Get-CimInstance -Namespace root\wmi -ClassName BatteryStaticData | Select-Object -First 1).DesignedCapacity
$f = (Get-CimInstance -Namespace root\wmi -ClassName BatteryFullChargedCapacity | Select-Object -First 1).FullChargedCapacity
$c = (Get-CimInstance -Namespace root\wmi -ClassName BatteryCycleCount | Select-Object -First 1).CycleCount
@{ design = [uint64]$d; full = [uint64]$f; cycles = [int]$c } | ConvertTo-Json -Compress`, &w)
	b.Design, b.Full, b.Cycles = w.Design, w.Full, w.Cycles
	if w.Design > 0 && w.Full > 0 {
		b.Health = float64(w.Full) / float64(w.Design) * 100
		if b.Health > 100 {
			b.Health = 100
		}
	}
	return b
}

func disks() []Disk {
	var raw []struct {
		Name   string `json:"name"`
		Media  string `json:"media"`
		Bus    string `json:"bus"`
		Size   uint64 `json:"size"`
		Health string `json:"health"`
		Wear   *int   `json:"wear"`
		Temp   *int   `json:"temp"`
		Hours  *int   `json:"hours"`
	}
	// Счётчики надёжности (износ, температура) доступны только администратору — без них
	// показываем тип и общий статус здоровья.
	_ = powershellJSON(`$r = @()
foreach($d in Get-PhysicalDisk){
  $rc = $null; try { $rc = $d | Get-StorageReliabilityCounter } catch {}
  $r += [pscustomobject]@{
    name = $d.FriendlyName; media = [string]$d.MediaType; bus = [string]$d.BusType
    size = [uint64]$d.Size; health = [string]$d.HealthStatus
    wear = if($rc -and $rc.Wear -ne $null){[int]$rc.Wear}else{$null}
    temp = if($rc -and $rc.Temperature){[int]$rc.Temperature}else{$null}
    hours = if($rc -and $rc.PowerOnHours){[int]$rc.PowerOnHours}else{$null}
  }
}
ConvertTo-Json -InputObject @($r) -Compress`, &raw)
	out := make([]Disk, 0, len(raw))
	for _, r := range raw {
		d := Disk{Name: strings.TrimSpace(r.Name), Media: r.Media, Bus: r.Bus, Size: r.Size, Health: r.Health, Wear: -1, TempC: -1, PowerOnH: -1}
		if r.Media == "Unspecified" {
			d.Media = ""
		}
		if r.Wear != nil {
			d.Wear = *r.Wear
		}
		if r.Temp != nil {
			d.TempC = *r.Temp
		}
		if r.Hours != nil {
			d.PowerOnH = *r.Hours
		}
		out = append(out, d)
	}
	return out
}

// Collect собирает сведения об оборудовании (несколько секунд).
func Collect() Info {
	return Info{GPUs: adapters(), Battery: battery(), Disks: disks()}
}
