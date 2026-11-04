//go:build windows

package health

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"

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
	// Опрос железа стоит запуска PowerShell (заметная нагрузка), поэтому он идёт
	// только когда нужен: открыта страница со сведениями (каждые 10 с) или
	// включены оповещения о железе (фоном, раз в несколько минут).
	go func() {
		var last time.Time
		for {
			every := time.Duration(0)
			if Active() {
				every = 10 * time.Second
			} else if bg := Background(); bg > 0 {
				every = bg
			}
			if every > 0 && time.Since(last) >= every {
				if s, ok := collect(); ok {
					store(s)
				}
				last = time.Now()
			}
			time.Sleep(time.Second)
		}
	}()
	// Видеокарты NVIDIA — каждые 2 с, но только пока сведения кто-то смотрит.
	go func() {
		if nvidiaSMI() == "" {
			return
		}
		for {
			if Active() {
				if gs := nvidia(); len(gs) > 0 {
					for i := range gs {
						gs[i].Level = GPULevel(gs[i])
					}
					setGPUs(gs)
				}
			}
			time.Sleep(2 * time.Second)
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
$bc = @(Get-CimInstance -Namespace root\wmi -ClassName BatteryCycleCount | Select-Object -ExpandProperty CycleCount)
$disks = foreach($d in Get-PhysicalDisk){ $r = $d | Get-StorageReliabilityCounter; [pscustomobject]@{ name=$d.FriendlyName; media=[string]$d.MediaType; health=[string]$d.HealthStatus; size=[int64]$d.Size; temp=[int]$r.Temperature; tmax=[int]$r.TemperatureMax; wear=[int]$r.Wear; hours=[int]$r.PowerOnHours; rerr=[int64]$r.ReadErrorsUncorrected; werr=[int64]$r.WriteErrorsUncorrected } }
[pscustomobject]@{
  gpuName=[string]$vc.Name; gpuRam=[int64]$vc.AdapterRAM; gpuUse=[int]([math]::Round($use.Sum,0)); vramUsed=[int64]$vram.Sum;
  batPresent=[bool]$bat; batPct=[int]$bat.EstimatedChargeRemaining; batStatus=[int]$bat.BatteryStatus;
  design=[int64]($design | Select-Object -First 1); full=[int64]($full | Select-Object -First 1); cycles=[int]($bc | Select-Object -First 1);
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
	Cycles     int   `json:"cycles"`

	Disks []struct {
		Name   string `json:"name"`
		Media  string `json:"media"`
		Health string `json:"health"`
		Size   int64  `json:"size"`
		Temp   int    `json:"temp"`
		TMax   int    `json:"tmax"`
		Wear   int    `json:"wear"`
		Hours  int    `json:"hours"`
		RErr   int64  `json:"rerr"`
		WErr   int64  `json:"werr"`
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
	s.Limited = !elevated()
	if r.GPUName != "" {
		u := float64(r.GPUUse)
		if u > 100 {
			u = 100
		}
		s.GPUs = append(s.GPUs, GPU{Name: r.GPUName, Usage: u, VramUsed: u64(r.VramUsed), VramTotal: u64(r.GPURam), FanPct: -1})
	}
	// NVIDIA: температура, вентилятор, мощность и точный объём видеопамяти — из nvidia-smi.
	if nv := nvidia(); len(nv) > 0 {
		s.GPUs = nv
	}
	for i := range s.GPUs {
		s.GPUs[i].Level = GPULevel(s.GPUs[i])
	}
	if r.BatPresent {
		b := Battery{Present: true, Percent: r.BatPct, Charging: r.BatStatus == 2}
		// Ёмкости в root\wmi даны в мВт·ч.
		b.DesignWh, b.FullWh, b.Cycles = float64(r.Design), float64(r.Full), r.Cycles
		if b.DesignWh > 0 && b.FullWh > 0 && b.FullWh <= b.DesignWh {
			b.WearPct = (1 - b.FullWh/b.DesignWh) * 100
		}
		b.Level = BatteryLevel(b)
		s.Battery = b
	}
	for _, d := range r.Disks {
		dk := Disk{
			Name: d.Name, Media: mediaType(d.Media), Health: d.Health, Size: u64(d.Size),
			TempC: d.Temp, TempMax: d.TMax, WearPct: d.Wear, PowerOnH: d.Hours, ReadErr: d.RErr, WriteErr: d.WErr,
		}
		dk.Level, dk.Reason = DiskLevel(dk)
		s.Disks = append(s.Disks, dk)
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

func elevated() bool { return windows.GetCurrentProcessToken().IsElevated() }

// nvidia — показатели видеокарт NVIDIA через официальную утилиту драйвера.
func nvidiaSMI() string {
	for _, p := range []string{
		filepath.Join(os.Getenv("SystemRoot"), "System32", "nvidia-smi.exe"),
		filepath.Join(os.Getenv("ProgramFiles"), "NVIDIA Corporation", "NVSMI", "nvidia-smi.exe"),
	} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

func nvidia() []GPU {
	exe := nvidiaSMI()
	if exe == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, "--query-gpu=name,temperature.gpu,fan.speed,power.draw,utilization.gpu,memory.used,memory.total", "--format=csv,noheader,nounits")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	return ParseNvidiaSMI(string(out))
}

// CollectNow собирает снимок сразу (для командной строки, где фонового опроса нет).
func CollectNow() Snapshot {
	if s, ok := collect(); ok {
		store(s)
	}
	return Get()
}
