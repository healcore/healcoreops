// Package health — мониторинг «здоровья» железа только на чтение:
// загрузка и память видеокарты, износ батареи, состояние накопителей.
// Данные дорогие (опрос через WMI/счётчики), поэтому обновляются редко и кэшируются.
package health

import (
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// GPU — сведения о видеокарте.
type GPU struct {
	Name      string  `json:"name"`
	Usage     float64 `json:"usage"`     // %, 0..100
	VramUsed  uint64  `json:"vramUsed"`  // байт
	VramTotal uint64  `json:"vramTotal"` // байт, 0 если неизвестно
	TempC     int     `json:"tempC"`     // °C, 0 — неизвестно (температура есть у NVIDIA через nvidia-smi)
	FanPct    int     `json:"fanPct"`    // %, -1 — неизвестно
	PowerW    float64 `json:"powerW"`    // Вт, 0 — неизвестно
	Level     int     `json:"level"`     // 0 хорошо, 1 внимание, 2 плохо
}

// Battery — состояние и износ аккумулятора.
type Battery struct {
	Present  bool    `json:"present"`
	Percent  int     `json:"percent"`
	Charging bool    `json:"charging"`
	DesignWh float64 `json:"designWh"` // проектная ёмкость, мВт·ч
	FullWh   float64 `json:"fullWh"`   // текущая полная ёмкость, мВт·ч
	WearPct  float64 `json:"wearPct"`  // износ, %
	Cycles   int     `json:"cycles"`   // циклы заряда, 0 — неизвестно
	Level    int     `json:"level"`
}

// Disk — состояние накопителя (SMART-сводка).
type Disk struct {
	Name     string `json:"name"`
	Media    string `json:"media"` // SSD | HDD | ...
	Health   string `json:"health"`
	TempC    int    `json:"tempC"`
	WearPct  int    `json:"wearPct"`  // износ SSD, %
	PowerOnH int    `json:"powerOnH"` // наработка, часов
	TempMax  int    `json:"tempMax"`  // максимум за время работы, °C
	ReadErr  int64  `json:"readErr"`  // неисправленные ошибки чтения
	WriteErr int64  `json:"writeErr"` // неисправленные ошибки записи
	Size     uint64 `json:"size"`
	Level    int    `json:"level"`  // 0 хорошо, 1 внимание, 2 плохо
	Reason   string `json:"reason"` // почему не «хорошо» (ключ: health|wear|errors|temp)
}

// Snapshot — весь снимок здоровья железа.
type Snapshot struct {
	GPUs    []GPU   `json:"gpus"`
	Battery Battery `json:"battery"`
	Disks   []Disk  `json:"disks"`
	Updated int64   `json:"updated"` // unix-мс последнего обновления
	Ready   bool    `json:"ready"`
	Limited bool    `json:"limited"` // нет прав администратора — износ и температура дисков могут быть недоступны
}

// DiskLevel — оценка накопителя: уровень и причина.
func DiskLevel(d Disk) (int, string) {
	switch {
	case d.Health == "Unhealthy":
		return 2, "health"
	case d.ReadErr > 0 || d.WriteErr > 0:
		return 2, "errors"
	case d.WearPct >= 90:
		return 2, "wear"
	case d.TempC >= 70:
		return 2, "temp"
	case d.Health == "Warning":
		return 1, "health"
	case d.WearPct >= 70:
		return 1, "wear"
	case d.TempC >= 60:
		return 1, "temp"
	}
	return 0, ""
}

// BatteryLevel — оценка износа батареи.
func BatteryLevel(b Battery) int {
	switch {
	case !b.Present || b.WearPct <= 0:
		return 0
	case b.WearPct >= 50:
		return 2
	case b.WearPct >= 30:
		return 1
	}
	return 0
}

// GPULevel — оценка температуры видеокарты.
func GPULevel(g GPU) int {
	switch {
	case g.TempC >= 90:
		return 2
	case g.TempC >= 83:
		return 1
	}
	return 0
}

var (
	mu   sync.RWMutex
	snap Snapshot
)

// Get возвращает последний снимок (копию).
func Get() Snapshot {
	mu.RLock()
	defer mu.RUnlock()
	cp := snap
	cp.GPUs = append([]GPU(nil), snap.GPUs...)
	cp.Disks = append([]Disk(nil), snap.Disks...)
	return cp
}

var (
	wantAt  atomic.Int64 // когда сведения в последний раз запрашивали (unix-мс)
	bgEvery atomic.Int64 // фоновый опрос для оповещений, нс (0 — не нужен)
)

// Want отмечает, что сведения сейчас кто-то смотрит (интерфейс): опрос становится частым.
func Want() { wantAt.Store(time.Now().UnixMilli()) }

// Active — сведения запрашивали в последние 45 секунд.
func Active() bool { return time.Now().UnixMilli()-wantAt.Load() < 45_000 }

// SetBackground задаёт фоновый опрос (для оповещений); 0 — без фонового опроса.
func SetBackground(d time.Duration) { bgEvery.Store(int64(d)) }

// Background — интервал фонового опроса.
func Background() time.Duration { return time.Duration(bgEvery.Load()) }

// setGPUs обновляет только сведения о видеокартах (быстрый опрос).
func setGPUs(gs []GPU) {
	mu.Lock()
	snap.GPUs = gs
	if snap.Ready {
		snap.Updated = time.Now().UnixMilli()
	}
	mu.Unlock()
}

func store(s Snapshot) {
	mu.Lock()
	s.Ready = true
	snap = s
	mu.Unlock()
}

// ParseNvidiaSMI разбирает вывод nvidia-smi --query-gpu=name,temperature.gpu,fan.speed,
// power.draw,utilization.gpu,memory.used,memory.total --format=csv,noheader,nounits.
func ParseNvidiaSMI(out string) []GPU {
	var gs []GPU
	num := func(s string) float64 {
		v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
		if err != nil {
			return -1 // «[N/A]», «[Not Supported]»
		}
		return v
	}
	for _, line := range strings.Split(out, "\n") {
		f := strings.Split(strings.TrimSpace(line), ",")
		if len(f) < 7 || strings.TrimSpace(f[0]) == "" {
			continue
		}
		g := GPU{Name: strings.TrimSpace(f[0]), FanPct: -1}
		if t := num(f[1]); t > 0 {
			g.TempC = int(t)
		}
		if v := num(f[2]); v >= 0 {
			g.FanPct = int(v)
		}
		if v := num(f[3]); v > 0 {
			g.PowerW = v
		}
		if v := num(f[4]); v >= 0 {
			g.Usage = v
		}
		if v := num(f[5]); v >= 0 {
			g.VramUsed = uint64(v) << 20
		}
		if v := num(f[6]); v > 0 {
			g.VramTotal = uint64(v) << 20
		}
		gs = append(gs, g)
	}
	return gs
}
