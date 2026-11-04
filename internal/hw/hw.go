// Package hw — сведения об оборудовании: видеокарта (загрузка и видеопамять
// по процессам), батарея (заряд и износ), физические диски (тип и здоровье).
package hw

// GPUProc — потребление видеокарты одним процессом.
type GPUProc struct {
	PID  int32   `json:"pid"`
	Util float64 `json:"util"` // %
	VRAM uint64  `json:"vram"` // байт выделенной видеопамяти
}

// GPUStat — живые показатели видеокарты.
type GPUStat struct {
	OK        bool      `json:"ok"`
	Name      string    `json:"name"`
	Util      float64   `json:"util"`
	VRAMUsed  uint64    `json:"vramUsed"`
	VRAMTotal uint64    `json:"vramTotal"`
	Top       []GPUProc `json:"top"`
}

// Battery — состояние аккумулятора.
type Battery struct {
	Present  bool    `json:"present"`
	Percent  int     `json:"percent"`
	Charging bool    `json:"charging"`
	OnAC     bool    `json:"onAC"`
	Design   uint64  `json:"design"` // мВт·ч
	Full     uint64  `json:"full"`   // мВт·ч
	Health   float64 `json:"health"` // full/design, %; 0 — неизвестно
	Cycles   int     `json:"cycles"`
	Minutes  int     `json:"minutes"` // оставшееся время, -1 — неизвестно
}

// Disk — физический накопитель.
type Disk struct {
	Name     string `json:"name"`
	Media    string `json:"media"` // SSD | HDD | …
	Bus      string `json:"bus"`
	Size     uint64 `json:"size"`
	Health   string `json:"health"` // Healthy | Warning | Unhealthy | ""
	Wear     int    `json:"wear"`   // % износа SSD, -1 — неизвестно
	TempC    int    `json:"tempC"`  // -1 — неизвестно
	PowerOnH int    `json:"powerOnH"`
}

// Info — сведения, которые собираются по запросу (медленно).
type Info struct {
	GPUs    []GPUAdapter `json:"gpus"`
	Battery Battery      `json:"battery"`
	Disks   []Disk       `json:"disks"`
}

// GPUAdapter — видеоадаптер.
type GPUAdapter struct {
	Name string `json:"name"`
	VRAM uint64 `json:"vram"`
}
