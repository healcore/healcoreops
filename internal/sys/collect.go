// Package sys собирает метрики системы и список процессов.
package sys

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/load"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/net"
	"github.com/shirou/gopsutil/v4/process"

	"github.com/healcore/healcoreops/internal/ctl"
)

// Proc — сведения о процессе на момент снимка.
type Proc struct {
	PID      int32
	Name     string
	User     string
	CPU      float64 // доля от всей мощности ЦП, %
	RSS      uint64  // резидентная память, байт
	MemPct   float64
	Threads  int32
	Priority ctl.Priority
	PrioOK   bool
	Create   int64
}

// Disk — заполненность раздела.
type Disk struct {
	Mount string
	FS    string
	Total uint64
	Used  uint64
	Pct   float64
}

// Host — неизменяемые сведения о машине.
type Host struct {
	Hostname string
	OS       string
	CPUModel string
	Logical  int
	Physical int
	MemTotal uint64
}

// Snapshot — состояние системы в момент времени.
type Snapshot struct {
	Time  time.Time
	CPU   float64
	Cores []float64

	MemTotal, MemUsed, MemAvail uint64
	MemPct                      float64
	SwapTotal, SwapUsed         uint64
	SwapPct                     float64

	NetRx, NetTx float64 // байт/с
	DiskR, DiskW float64 // байт/с

	Disks   []Disk
	Uptime  uint64
	Load    [3]float64
	HasLoad bool

	Procs   []Proc
	Threads int64
}

type entry struct {
	p      *process.Process
	name   string
	user   string
	create int64
}

// Collector периодически снимает метрики, сохраняя состояние между вызовами
// (для расчёта скоростей и загрузки ЦП процессами).
type Collector struct {
	host  Host
	cache map[int32]*entry
	self  int32

	lastT              time.Time
	lastRx, lastTx     uint64
	lastRead, lastWrit uint64
	disks              []Disk
	tick               int
}

// NewCollector создаёт сборщик и читает статические сведения о машине.
func NewCollector() *Collector {
	c := &Collector{cache: map[int32]*entry{}, self: int32(os.Getpid())}
	h := Host{Logical: runtime.NumCPU()}
	if hi, err := host.Info(); err == nil {
		h.Hostname = hi.Hostname
		name := hi.Platform
		if name == "" {
			name = hi.OS
		}
		h.OS = strings.TrimSpace(name + " " + hi.PlatformVersion)
	}
	if ci, err := cpu.Info(); err == nil && len(ci) > 0 {
		h.CPUModel = strings.Join(strings.Fields(ci[0].ModelName), " ")
	}
	if n, err := cpu.Counts(false); err == nil {
		h.Physical = n
	}
	if vm, err := mem.VirtualMemory(); err == nil {
		h.MemTotal = vm.Total
	}
	c.host = h
	cpu.Percent(0, true) // прогрев: следующий вызов вернёт загрузку за интервал
	return c
}

// Host возвращает сведения о машине.
func (c *Collector) Host() Host { return c.host }

// Self — PID самого приложения.
func (c *Collector) Self() int32 { return c.self }

// Collect делает новый снимок. Не потокобезопасен: вызывать из одной горутины.
func (c *Collector) Collect() *Snapshot {
	now := time.Now()
	s := &Snapshot{Time: now}
	dt := now.Sub(c.lastT).Seconds()
	first := c.lastT.IsZero()
	c.lastT = now

	if v, err := cpu.Percent(0, true); err == nil && len(v) > 0 {
		s.Cores = v
		sum := 0.0
		for _, x := range v {
			sum += x
		}
		s.CPU = sum / float64(len(v))
	}
	if vm, err := mem.VirtualMemory(); err == nil {
		s.MemTotal, s.MemUsed, s.MemAvail, s.MemPct = vm.Total, vm.Total-vm.Available, vm.Available, vm.UsedPercent
	}
	if sw, err := mem.SwapMemory(); err == nil {
		s.SwapTotal, s.SwapUsed, s.SwapPct = sw.Total, sw.Used, sw.UsedPercent
	}

	if io, err := net.IOCounters(false); err == nil && len(io) > 0 {
		if !first && dt > 0 {
			s.NetRx = rate(io[0].BytesRecv, c.lastRx, dt)
			s.NetTx = rate(io[0].BytesSent, c.lastTx, dt)
		}
		c.lastRx, c.lastTx = io[0].BytesRecv, io[0].BytesSent
	}
	if dio, err := disk.IOCounters(); err == nil {
		var r, w uint64
		for _, d := range dio {
			r += d.ReadBytes
			w += d.WriteBytes
		}
		if !first && dt > 0 {
			s.DiskR = rate(r, c.lastRead, dt)
			s.DiskW = rate(w, c.lastWrit, dt)
		}
		c.lastRead, c.lastWrit = r, w
	}

	if c.tick%10 == 0 {
		c.disks = readDisks()
	}
	c.tick++
	s.Disks = c.disks

	if up, err := host.Uptime(); err == nil {
		s.Uptime = up
	}
	if runtime.GOOS != "windows" {
		if l, err := load.Avg(); err == nil {
			s.Load = [3]float64{l.Load1, l.Load5, l.Load15}
			s.HasLoad = true
		}
	}

	s.Procs, s.Threads = c.procs(s.MemTotal)
	return s
}

func rate(cur, prev uint64, dt float64) float64 {
	if cur < prev {
		return 0
	}
	return float64(cur-prev) / dt
}

var pseudoFS = map[string]bool{
	"squashfs": true, "tmpfs": true, "devtmpfs": true, "proc": true, "sysfs": true,
	"cgroup": true, "cgroup2": true, "devpts": true, "nsfs": true, "autofs": true,
	"fusectl": true, "tracefs": true, "debugfs": true, "securityfs": true, "pstore": true,
	"bpf": true, "mqueue": true, "hugetlbfs": true, "configfs": true, "ramfs": true,
	"efivarfs": true, "binfmt_misc": true, "fuse.portal": true, "fuse.gvfsd-fuse": true,
	"devfs": true, "nullfs": true,
}

func readDisks() []Disk {
	parts, err := disk.Partitions(false)
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var out []Disk
	for _, p := range parts {
		if pseudoFS[strings.ToLower(p.Fstype)] || strings.HasPrefix(p.Mountpoint, "/snap") ||
			strings.HasPrefix(p.Mountpoint, "/System/Volumes/") {
			continue
		}
		u, err := disk.Usage(p.Mountpoint)
		if err != nil || u.Total == 0 {
			continue
		}
		key := fmt.Sprintf("%s|%d", p.Device, u.Total)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, Disk{Mount: p.Mountpoint, FS: p.Fstype, Total: u.Total, Used: u.Used, Pct: u.UsedPercent})
		if len(out) >= 8 {
			break
		}
	}
	return out
}

func (c *Collector) procs(memTotal uint64) ([]Proc, int64) {
	pids, err := process.Pids()
	if err != nil {
		return nil, 0
	}
	ncpu := float64(runtime.NumCPU())
	alive := make(map[int32]bool, len(pids))
	out := make([]Proc, 0, len(pids))
	var threads int64

	for _, pid := range pids {
		if pid == 0 {
			continue // «Бездействие системы» в Windows
		}
		alive[pid] = true
		e := c.cache[pid]
		if e == nil {
			p, err := process.NewProcess(pid)
			if err != nil {
				continue
			}
			e = &entry{p: p}
			e.name, _ = p.Name()
			if e.name == "" {
				e.name = fmt.Sprintf("pid %d", pid)
			}
			if u, err := p.Username(); err == nil {
				if i := strings.LastIndexAny(u, `\/`); i >= 0 {
					u = u[i+1:]
				}
				e.user = u
			}
			e.create, _ = p.CreateTime()
			c.cache[pid] = e
		}

		pr := Proc{PID: pid, Name: e.name, User: e.user, Create: e.create}
		if v, err := e.p.Percent(0); err == nil {
			pr.CPU = v / ncpu
		}
		if mi, err := e.p.MemoryInfo(); err == nil && mi != nil {
			pr.RSS = mi.RSS
			if memTotal > 0 {
				pr.MemPct = float64(mi.RSS) / float64(memTotal) * 100
			}
		}
		if n, err := e.p.NumThreads(); err == nil {
			pr.Threads = n
			threads += int64(n)
		}
		if prio, err := ctl.GetPriority(pid); err == nil {
			pr.Priority, pr.PrioOK = prio, true
		}
		out = append(out, pr)
	}
	for pid := range c.cache {
		if !alive[pid] {
			delete(c.cache, pid)
		}
	}
	return out, threads
}
