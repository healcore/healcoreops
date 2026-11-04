//go:build !windows

package hw

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// GPU вне Windows: загрузка видеокарты по процессам не поддерживается.
type GPU struct{}

// NewGPU создаёт заглушку.
func NewGPU() *GPU { return &GPU{} }

// Sample возвращает пустые показатели.
func (g *GPU) Sample() GPUStat { return GPUStat{Top: []GPUProc{}} }

func readInt(path string) (int64, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	n, err := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64)
	return n, err == nil
}

func readStr(path string) string {
	b, _ := os.ReadFile(path)
	return strings.TrimSpace(string(b))
}

// battery читает /sys/class/power_supply (Linux).
func battery() Battery {
	b := Battery{Minutes: -1}
	dirs, _ := filepath.Glob("/sys/class/power_supply/BAT*")
	if len(dirs) == 0 {
		if ac, ok := readInt("/sys/class/power_supply/AC/online"); ok {
			b.OnAC = ac == 1
		}
		return b
	}
	d := dirs[0]
	b.Present = true
	if v, ok := readInt(filepath.Join(d, "capacity")); ok {
		b.Percent = int(v)
	}
	st := readStr(filepath.Join(d, "status"))
	b.Charging = st == "Charging"
	b.OnAC = st != "Discharging"
	full, ok1 := readInt(filepath.Join(d, "energy_full"))
	design, ok2 := readInt(filepath.Join(d, "energy_full_design"))
	if !ok1 || !ok2 {
		full, ok1 = readInt(filepath.Join(d, "charge_full"))
		design, ok2 = readInt(filepath.Join(d, "charge_full_design"))
	}
	if ok1 && ok2 && design > 0 {
		b.Full, b.Design = uint64(full/1000), uint64(design/1000)
		b.Health = float64(full) / float64(design) * 100
		if b.Health > 100 {
			b.Health = 100
		}
	}
	if v, ok := readInt(filepath.Join(d, "cycle_count")); ok {
		b.Cycles = int(v)
	}
	return b
}

// disks читает /sys/block (Linux): модель, размер, тип.
func disks() []Disk {
	var out []Disk
	ents, _ := os.ReadDir("/sys/block")
	for _, e := range ents {
		n := e.Name()
		if strings.HasPrefix(n, "loop") || strings.HasPrefix(n, "ram") || strings.HasPrefix(n, "zram") || strings.HasPrefix(n, "dm-") {
			continue
		}
		base := filepath.Join("/sys/block", n)
		sectors, ok := readInt(filepath.Join(base, "size"))
		if !ok || sectors == 0 {
			continue
		}
		d := Disk{Name: readStr(filepath.Join(base, "device", "model")), Size: uint64(sectors) * 512, Wear: -1, TempC: -1, PowerOnH: -1}
		if d.Name == "" {
			d.Name = n
		}
		if rot, ok := readInt(filepath.Join(base, "queue", "rotational")); ok {
			if rot == 1 {
				d.Media = "HDD"
			} else {
				d.Media = "SSD"
			}
		}
		if strings.HasPrefix(n, "nvme") {
			d.Bus = "NVMe"
		}
		out = append(out, d)
	}
	return out
}

// Collect собирает сведения об оборудовании.
func Collect() Info {
	return Info{GPUs: []GPUAdapter{}, Battery: battery(), Disks: disks()}
}
