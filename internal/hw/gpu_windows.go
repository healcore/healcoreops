//go:build windows

package hw

import (
	"encoding/binary"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// Счётчики производительности Windows (PDH) — те же, что использует Диспетчер задач.
var (
	pdh                   = windows.NewLazySystemDLL("pdh.dll")
	procOpenQuery         = pdh.NewProc("PdhOpenQueryW")
	procAddEnglishCounter = pdh.NewProc("PdhAddEnglishCounterW")
	procCollectQueryData  = pdh.NewProc("PdhCollectQueryData")
	procGetFormattedArray = pdh.NewProc("PdhGetFormattedCounterArrayW")
	procCloseQuery        = pdh.NewProc("PdhCloseQuery")
)

const (
	pdhFmtDouble = 0x00000200
	pdhMoreData  = 0x800007D2
)

// pdhItem соответствует PDH_FMT_COUNTERVALUE_ITEM_W для amd64/arm64.
type pdhItem struct {
	Name   *uint16
	Status uint32
	_      uint32
	Value  float64
}

// GPU собирает живые показатели видеокарты. Не потокобезопасен для Sample.
type GPU struct {
	once      sync.Once
	ok        bool
	query     uintptr
	cEngine   uintptr
	cProcMem  uintptr
	cAdapMem  uintptr
	name      string
	vramTotal uint64
}

// NewGPU создаёт сборщик (инициализация отложена до первого Sample).
func NewGPU() *GPU { return &GPU{} }

func (g *GPU) init() {
	g.name, g.vramTotal = primaryAdapter()
	if procOpenQuery.Find() != nil {
		return
	}
	if r, _, _ := procOpenQuery.Call(0, 0, uintptr(unsafe.Pointer(&g.query))); r != 0 {
		return
	}
	add := func(path string, h *uintptr) bool {
		p, _ := windows.UTF16PtrFromString(path)
		r, _, _ := procAddEnglishCounter.Call(g.query, uintptr(unsafe.Pointer(p)), 0, uintptr(unsafe.Pointer(h)))
		return r == 0
	}
	if !add(`\GPU Engine(*)\Utilization Percentage`, &g.cEngine) {
		procCloseQuery.Call(g.query)
		return
	}
	add(`\GPU Process Memory(*)\Dedicated Usage`, &g.cProcMem)
	add(`\GPU Adapter Memory(*)\Dedicated Usage`, &g.cAdapMem)
	procCollectQueryData.Call(g.query) // первая выборка — база для процентов
	g.ok = true
}

func (g *GPU) array(counter uintptr) map[string]float64 {
	out := map[string]float64{}
	if counter == 0 {
		return out
	}
	var size, count uint32
	r, _, _ := procGetFormattedArray.Call(counter, pdhFmtDouble, uintptr(unsafe.Pointer(&size)), uintptr(unsafe.Pointer(&count)), 0)
	if uint32(r) != pdhMoreData || size == 0 {
		return out
	}
	buf := make([]byte, size)
	r, _, _ = procGetFormattedArray.Call(counter, pdhFmtDouble, uintptr(unsafe.Pointer(&size)), uintptr(unsafe.Pointer(&count)), uintptr(unsafe.Pointer(&buf[0])))
	if r != 0 {
		return out
	}
	items := unsafe.Slice((*pdhItem)(unsafe.Pointer(&buf[0])), count)
	for _, it := range items {
		if it.Status != 0 && it.Status != 1 { // PDH_CSTATUS_VALID_DATA / NEW_DATA
			continue
		}
		out[windows.UTF16PtrToString(it.Name)] += it.Value
	}
	return out
}

// pidOf извлекает PID из имени экземпляра вида «pid_1234_luid_…».
func pidOf(inst string) int32 {
	if !strings.HasPrefix(inst, "pid_") {
		return -1
	}
	rest := inst[4:]
	if i := strings.IndexByte(rest, '_'); i > 0 {
		rest = rest[:i]
	}
	n, err := strconv.Atoi(rest)
	if err != nil {
		return -1
	}
	return int32(n)
}

func engType(inst string) string {
	if i := strings.Index(inst, "engtype_"); i >= 0 {
		return inst[i+8:]
	}
	return "?"
}

// Sample возвращает показатели за интервал с предыдущего вызова.
func (g *GPU) Sample() GPUStat {
	g.once.Do(g.init)
	st := GPUStat{Name: g.name, VRAMTotal: g.vramTotal, Top: []GPUProc{}}
	if !g.ok {
		return st
	}
	if r, _, _ := procCollectQueryData.Call(g.query); r != 0 {
		return st
	}
	st.OK = true

	// Загрузка: по каждому типу движка суммируем экземпляры, итог — максимум
	// (так считает Диспетчер задач). По процессам — то же самое.
	byType := map[string]float64{}
	byPID := map[int32]map[string]float64{}
	for inst, v := range g.array(g.cEngine) {
		t := engType(inst)
		byType[t] += v
		if pid := pidOf(inst); pid > 0 {
			if byPID[pid] == nil {
				byPID[pid] = map[string]float64{}
			}
			byPID[pid][t] += v
		}
	}
	for _, v := range byType {
		if v > st.Util {
			st.Util = v
		}
	}
	if st.Util > 100 {
		st.Util = 100
	}

	vram := map[int32]uint64{}
	for inst, v := range g.array(g.cProcMem) {
		if pid := pidOf(inst); pid > 0 {
			vram[pid] += uint64(v)
		}
	}
	for _, v := range g.array(g.cAdapMem) {
		st.VRAMUsed += uint64(v)
	}

	seen := map[int32]bool{}
	add := func(pid int32) {
		if seen[pid] {
			return
		}
		seen[pid] = true
		var u float64
		for _, v := range byPID[pid] {
			if v > u {
				u = v
			}
		}
		if u > 100 {
			u = 100
		}
		if u >= 0.1 || vram[pid] >= 32<<20 {
			st.Top = append(st.Top, GPUProc{PID: pid, Util: u, VRAM: vram[pid]})
		}
	}
	for pid := range byPID {
		add(pid)
	}
	for pid := range vram {
		add(pid)
	}
	sort.Slice(st.Top, func(i, j int) bool {
		if st.Top[i].Util == st.Top[j].Util {
			return st.Top[i].VRAM > st.Top[j].VRAM
		}
		return st.Top[i].Util > st.Top[j].Util
	})
	if len(st.Top) > 8 {
		st.Top = st.Top[:8]
	}
	return st
}

const displayClass = `SYSTEM\CurrentControlSet\Control\Class\{4d36e968-e325-11ce-bfc1-08002be10318}`

// adapters читает видеоадаптеры из реестра драйверов.
func adapters() []GPUAdapter {
	root, err := registry.OpenKey(registry.LOCAL_MACHINE, displayClass, registry.ENUMERATE_SUB_KEYS)
	if err != nil {
		return nil
	}
	defer root.Close()
	subs, _ := root.ReadSubKeyNames(-1)
	var out []GPUAdapter
	for _, s := range subs {
		if len(s) != 4 {
			continue
		}
		k, err := registry.OpenKey(root, s, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		name, _, _ := k.GetStringValue("DriverDesc")
		var mem uint64
		if b, _, err := k.GetBinaryValue("HardwareInformation.qwMemorySize"); err == nil && len(b) >= 8 {
			mem = binary.LittleEndian.Uint64(b)
		} else if n, _, err := k.GetIntegerValue("HardwareInformation.qwMemorySize"); err == nil {
			mem = n
		} else if b, _, err := k.GetBinaryValue("HardwareInformation.MemorySize"); err == nil && len(b) >= 4 {
			mem = uint64(binary.LittleEndian.Uint32(b))
		} else if n, _, err := k.GetIntegerValue("HardwareInformation.MemorySize"); err == nil {
			mem = n
		}
		k.Close()
		if name == "" || strings.Contains(name, "Basic Display") || strings.Contains(name, "Basic Render") {
			continue
		}
		out = append(out, GPUAdapter{Name: name, VRAM: mem})
	}
	return out
}

// primaryAdapter — адаптер с наибольшей видеопамятью (обычно дискретный).
func primaryAdapter() (string, uint64) {
	var name string
	var mem uint64
	for _, a := range adapters() {
		if name == "" || a.VRAM > mem {
			name, mem = a.Name, a.VRAM
		}
	}
	return name, mem
}
