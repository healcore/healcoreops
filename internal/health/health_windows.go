//go:build windows

package health

import (
	"encoding/base64"
	"encoding/json"
	"os/exec"
	"syscall"
	"time"
	"unicode/utf16"
)

// Supported — мониторинг железа доступен в Windows.
func Supported() bool { return true }

// Start запускает фоновое обновление снимка здоровья железа.
func Start() {
	go func() {
		for {
			if s, ok := collect(); ok {
				store(s)
			}
			time.Sleep(10 * time.Second)
		}
	}()
}

// Единый скрипт собирает всё за один запуск PowerShell и отдаёт JSON.
const script = `$ErrorActionPreference='SilentlyContinue'
$vc = Get-CimInstance Win32_VideoController | Sort-Object AdapterRAM -Descending | Select-Object -First 1
$use = (Get-Counter '\GPU Engine(*engtype_3D)\Utilization Percentage').CounterSamples | Measure-Object CookedValue -Sum
$vram = (Get-Counter '\GPU Adapter Memory(*)\Dedicated Usage').CounterSamples | Measure-Object CookedValue -Sum
$bat = Get-CimInstance Win32_Battery | Select-Object -First 1
$design = @(Get-CimInstance -Namespace root\wmi -ClassName BatteryStaticData | Select-Object -ExpandProperty DesignedCapacity)
$full = @(Get-CimInstance -Namespace root\wmi -ClassName BatteryFullChargedCapacity | Select-Object -ExpandProperty FullChargedCapacity)
$disks = foreach($d in Get-PhysicalDisk){ $r = $d | Get-StorageReliabilityCounter; [pscustomobject]@{ name=$d.FriendlyName; media=[string]$d.MediaType; health=[string]$d.HealthStatus; temp=[int]$r.Temperature; wear=[int]$r.Wear; hours=[int]$r.PowerOnHours } }
[pscustomobject]@{
  gpuName=[string]$vc.Name; gpuRam=[int64]$vc.AdapterRAM; gpuUse=[int]([math]::Round($use.Sum,0)); vramUsed=[int64]$vram.Sum;
  batPresent=[bool]$bat; batPct=[int]$bat.EstimatedChargeRemaining; batStatus=[int]$bat.BatteryStatus;
  design=[int64]($design | Select-Object -First 1); full=[int64]($full | Select-Object -First 1);
  disks=@($disks)
} | ConvertTo-Json -Depth 4 -Compress`

type raw struct {
	GPUName  string `json:"gpuName"`
	GPURam   int64  `json:"gpuRam"`
	GPUUse   int    `json:"gpuUse"`
	VramUsed int64  `json:"vramUsed"`

	BatPresent bool  `json:"batPresent"`
	BatPct     int   `json:"batPct"`
	BatStatus  int   `json:"batStatus"`
	Design     int64 `json:"design"`
	Full       int64 `json:"full"`

	Disks []struct {
		Name   string `json:"name"`
		Media  string `json:"media"`
		Health string `json:"health"`
		Temp   int    `json:"temp"`
		Wear   int    `json:"wear"`
		Hours  int    `json:"hours"`
	} `json:"disks"`
}

func collect() (Snapshot, bool) {
	out, err := powershell(script, 25*time.Second)
	if err != nil || len(out) == 0 {
		return Snapshot{}, false
	}
	var r raw
	if err := json.Unmarshal(out, &r); err != nil {
		return Snapshot{}, false
	}

	var s Snapshot
	s.Updated = time.Now().UnixMilli()
	if r.GPUName != "" {
		u := float64(r.GPUUse)
		if u > 100 {
			u = 100
		}
		s.GPUs = append(s.GPUs, GPU{Name: r.GPUName, Usage: u, VramUsed: u64(r.VramUsed), VramTotal: u64(r.GPURam)})
	}
	if r.BatPresent {
		b := Battery{Present: true, Percent: r.BatPct, Charging: r.BatStatus == 2}
		// Ёмкости в root\wmi даны в мВт·ч.
		b.DesignWh, b.FullWh = float64(r.Design), float64(r.Full)
		if b.DesignWh > 0 && b.FullWh > 0 && b.FullWh <= b.DesignWh {
			b.WearPct = (1 - b.FullWh/b.DesignWh) * 100
		}
		s.Battery = b
	}
	for _, d := range r.Disks {
		s.Disks = append(s.Disks, Disk{
			Name: d.Name, Media: mediaType(d.Media), Health: d.Health,
			TempC: d.Temp, WearPct: d.Wear, PowerOnH: d.Hours,
		})
	}
	return s, true
}

func u64(v int64) uint64 {
	if v < 0 {
		return 0
	}
	return uint64(v)
}

// mediaType переводит код/строку типа носителя Get-PhysicalDisk в понятное слово.
func mediaType(m string) string {
	switch m {
	case "4", "SSD":
		return "SSD"
	case "3", "HDD":
		return "HDD"
	case "5", "SCM":
		return "SCM"
	case "0", "Unspecified", "":
		return "—"
	}
	return m
}

func powershell(s string, timeout time.Duration) ([]byte, error) {
	full := "$ProgressPreference='SilentlyContinue'\n[Console]::OutputEncoding=[Text.Encoding]::UTF8\n" + s
	u := utf16.Encode([]rune(full))
	b := make([]byte, len(u)*2)
	for i, v := range u {
		b[2*i], b[2*i+1] = byte(v), byte(v>>8)
	}
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass",
		"-EncodedCommand", base64.StdEncoding.EncodeToString(b))
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	done := make(chan struct{})
	var out []byte
	var cerr error
	go func() { out, cerr = cmd.Output(); close(done) }()
	select {
	case <-done:
		return out, cerr
	case <-time.After(timeout):
		_ = cmd.Process.Kill()
		return nil, context_deadline
	}
}

var context_deadline = errTimeout{}

type errTimeout struct{}

func (errTimeout) Error() string { return "timeout" }
