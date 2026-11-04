// Package health — мониторинг «здоровья» железа только на чтение:
// загрузка и память видеокарты, износ батареи, состояние накопителей.
// Данные дорогие (опрос через WMI/счётчики), поэтому обновляются редко и кэшируются.
package health

import "sync"

// GPU — сведения о видеокарте.
type GPU struct {
	Name      string  `json:"name"`
	Usage     float64 `json:"usage"`     // %, 0..100
	VramUsed  uint64  `json:"vramUsed"`  // байт
	VramTotal uint64  `json:"vramTotal"` // байт, 0 если неизвестно
}

// Battery — состояние и износ аккумулятора.
type Battery struct {
	Present  bool    `json:"present"`
	Percent  int     `json:"percent"`
	Charging bool    `json:"charging"`
	DesignWh float64 `json:"designWh"` // проектная ёмкость, мВт·ч
	FullWh   float64 `json:"fullWh"`   // текущая полная ёмкость, мВт·ч
	WearPct  float64 `json:"wearPct"`  // износ, %
}

// Disk — состояние накопителя (SMART-сводка).
type Disk struct {
	Name     string `json:"name"`
	Media    string `json:"media"` // SSD | HDD | ...
	Health   string `json:"health"`
	TempC    int    `json:"tempC"`
	WearPct  int    `json:"wearPct"`  // износ SSD, %
	PowerOnH int    `json:"powerOnH"` // наработка, часов
}

// Snapshot — весь снимок здоровья железа.
type Snapshot struct {
	GPUs    []GPU   `json:"gpus"`
	Battery Battery `json:"battery"`
	Disks   []Disk  `json:"disks"`
	Updated int64   `json:"updated"` // unix-мс последнего обновления
	Ready   bool    `json:"ready"`
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

func store(s Snapshot) {
	mu.Lock()
	s.Ready = true
	snap = s
	mu.Unlock()
}
