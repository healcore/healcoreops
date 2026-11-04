// Package loadhist — история нагрузки за несколько дней: раз в минуту
// сохраняются средняя загрузка ЦП, памяти, диска и сети и самые «тяжёлые»
// процессы этой минуты. Позволяет ответить на вопрос «что тормозило вчера в 14:00».
package loadhist

import (
	"compress/gzip"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Top — процесс и его вклад.
type Top struct {
	Name string  `json:"n"`
	V    float64 `json:"v"` // ЦП: средний % всей мощности; память: байт
}

// Point — одна минута (или один интервал после сжатия).
type Point struct {
	T      int64   `json:"t"` // unix-мс начала минуты
	CPU    float64 `json:"cpu"`
	CPUMax float64 `json:"cpuMax"`
	Mem    float64 `json:"mem"` // % занятой памяти
	DiskR  float64 `json:"dr"`  // байт/с
	DiskW  float64 `json:"dw"`
	NetRx  float64 `json:"nr"`
	NetTx  float64 `json:"nt"`
	TopCPU []Top   `json:"tc"`
	TopMem []Top   `json:"tm"`
}

// ProcSample — процесс в одном замере.
type ProcSample struct {
	Name string
	CPU  float64
	RSS  uint64
}

// Sample — один замер системы.
type Sample struct {
	Time                       time.Time
	CPU, Mem                   float64
	DiskR, DiskW, NetRx, NetTx float64
	Procs                      []ProcSample
}

// Store — кольцевая история с сохранением на диск.
type Store struct {
	mu     sync.Mutex
	path   string
	keep   time.Duration
	points []Point
	cur    *acc
	dirty  int
}

type acc struct {
	minute           int64
	n                int
	cpu, cpuMax, mem float64
	dr, dw, nr, nt   float64
	pcpu             map[string]float64
	pmem             map[string]uint64
}

// DefaultKeep — сколько дней хранить историю.
const DefaultKeep = 7 * 24 * time.Hour

// Open загружает историю из файла (gzip JSON); отсутствие файла — не ошибка.
func Open(path string, keep time.Duration) *Store {
	s := &Store{path: path, keep: keep}
	if f, err := os.Open(path); err == nil {
		if zr, err := gzip.NewReader(f); err == nil {
			_ = json.NewDecoder(zr).Decode(&s.points)
			zr.Close()
		}
		f.Close()
	}
	s.prune(time.Now())
	return s
}

// Add добавляет замер; по завершении минуты она фиксируется в истории.
func (s *Store) Add(x Sample) {
	m := x.Time.Truncate(time.Minute).UnixMilli()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cur != nil && s.cur.minute != m {
		s.flushLocked()
	}
	if s.cur == nil {
		s.cur = &acc{minute: m, pcpu: map[string]float64{}, pmem: map[string]uint64{}}
	}
	a := s.cur
	a.n++
	a.cpu += x.CPU
	if x.CPU > a.cpuMax {
		a.cpuMax = x.CPU
	}
	a.mem += x.Mem
	a.dr += x.DiskR
	a.dw += x.DiskW
	a.nr += x.NetRx
	a.nt += x.NetTx
	mem := map[string]uint64{}
	for _, p := range x.Procs {
		if p.Name == "" {
			continue
		}
		if r := []rune(p.Name); len(r) > 64 { // на Linux имя бывает целой командной строкой
			p.Name = string(r[:64])
		}
		a.pcpu[p.Name] += p.CPU
		mem[p.Name] += p.RSS // несколько процессов с одним именем (браузеры) — суммой
	}
	for n, v := range mem {
		if v > a.pmem[n] {
			a.pmem[n] = v
		}
	}
}

func (s *Store) flushLocked() {
	a := s.cur
	s.cur = nil
	if a == nil || a.n == 0 {
		return
	}
	k := float64(a.n)
	p := Point{T: a.minute, CPU: a.cpu / k, CPUMax: a.cpuMax, Mem: a.mem / k,
		DiskR: a.dr / k, DiskW: a.dw / k, NetRx: a.nr / k, NetTx: a.nt / k}
	for n, v := range a.pcpu {
		p.TopCPU = append(p.TopCPU, Top{n, v / k})
	}
	for n, v := range a.pmem {
		p.TopMem = append(p.TopMem, Top{n, float64(v)})
	}
	p.TopCPU = topN(p.TopCPU, 5, 0.5)
	p.TopMem = topN(p.TopMem, 5, 0)
	s.points = append(s.points, p)
	s.dirty++
	s.prune(time.Now())
}

func topN(t []Top, n int, min float64) []Top {
	sort.Slice(t, func(i, j int) bool { return t[i].V > t[j].V })
	out := make([]Top, 0, n)
	for _, x := range t {
		if len(out) == n || x.V < min {
			break
		}
		out = append(out, x)
	}
	return out
}

func (s *Store) prune(now time.Time) {
	cut := now.Add(-s.keep).UnixMilli()
	i := 0
	for i < len(s.points) && s.points[i].T < cut {
		i++
	}
	if i > 0 {
		s.points = append([]Point(nil), s.points[i:]...)
	}
}

// Save записывает историю на диск, если есть новые точки (или force).
func (s *Store) Save(force bool) error {
	s.mu.Lock()
	if s.dirty == 0 && !force {
		s.mu.Unlock()
		return nil
	}
	pts := append([]Point(nil), s.points...)
	s.dirty = 0
	s.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	zw := gzip.NewWriter(f)
	err = json.NewEncoder(zw).Encode(pts)
	if e := zw.Close(); err == nil {
		err = e
	}
	if e := f.Close(); err == nil {
		err = e
	}
	if err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, s.path)
}

// Range — выборка за период: точки (не больше max, сжатые усреднением)
// и сводка «кто нагружал больше всего» за весь период.
type Range struct {
	From    int64   `json:"from"`
	To      int64   `json:"to"`
	Step    int64   `json:"step"` // мс на точку
	Points  []Point `json:"points"`
	TopCPU  []Top   `json:"topCpu"`  // процессы по средней загрузке ЦП за период, %
	TopMem  []Top   `json:"topMem"`  // пиковая память
	Minutes int     `json:"minutes"` // сколько минут с данными
	Oldest  int64   `json:"oldest"`  // самая ранняя точка в истории
}

// Query возвращает историю за [from, to].
func (s *Store) Query(from, to time.Time, max int) Range {
	s.mu.Lock()
	var src []Point
	for _, p := range s.points {
		if p.T >= from.UnixMilli() && p.T <= to.UnixMilli() {
			src = append(src, p)
		}
	}
	oldest := int64(0)
	if len(s.points) > 0 {
		oldest = s.points[0].T
	}
	s.mu.Unlock()
	return build(src, from, to, max, oldest)
}

func build(src []Point, from, to time.Time, max int, oldest int64) Range {
	r := Range{From: from.UnixMilli(), To: to.UnixMilli(), Minutes: len(src), Oldest: oldest, Points: []Point{}, TopCPU: []Top{}, TopMem: []Top{}}
	if max < 10 {
		max = 10
	}
	span := to.Sub(from).Milliseconds()
	step := int64(60_000)
	for span/step > int64(max) {
		step *= 2
	}
	r.Step = step

	totCPU := map[string]float64{}
	peakMem := map[string]float64{}
	var b *bucket
	flush := func() {
		if b != nil {
			r.Points = append(r.Points, b.point())
			b = nil
		}
	}
	for _, p := range src {
		for _, t := range p.TopCPU {
			totCPU[t.Name] += t.V
		}
		for _, t := range p.TopMem {
			if t.V > peakMem[t.Name] {
				peakMem[t.Name] = t.V
			}
		}
		key := p.T - (p.T-r.From)%step
		if b != nil && b.t != key {
			flush()
		}
		if b == nil {
			b = &bucket{t: key, cpu: map[string]float64{}, mem: map[string]float64{}}
		}
		b.add(p)
	}
	flush()
	for n, v := range totCPU {
		r.TopCPU = append(r.TopCPU, Top{n, v / float64(len(src))})
	}
	for n, v := range peakMem {
		r.TopMem = append(r.TopMem, Top{n, v})
	}
	r.TopCPU = topN(r.TopCPU, 10, 0)
	r.TopMem = topN(r.TopMem, 10, 0)
	return r
}

type bucket struct {
	t        int64
	n        int
	sum      Point
	cpuMax   float64
	cpu, mem map[string]float64
}

func (b *bucket) add(p Point) {
	b.n++
	b.sum.CPU += p.CPU
	b.sum.Mem += p.Mem
	b.sum.DiskR += p.DiskR
	b.sum.DiskW += p.DiskW
	b.sum.NetRx += p.NetRx
	b.sum.NetTx += p.NetTx
	if p.CPUMax > b.cpuMax {
		b.cpuMax = p.CPUMax
	}
	for _, t := range p.TopCPU {
		b.cpu[t.Name] += t.V
	}
	for _, t := range p.TopMem {
		if t.V > b.mem[t.Name] {
			b.mem[t.Name] = t.V
		}
	}
}

func (b *bucket) point() Point {
	k := float64(b.n)
	p := Point{T: b.t, CPU: b.sum.CPU / k, CPUMax: b.cpuMax, Mem: b.sum.Mem / k,
		DiskR: b.sum.DiskR / k, DiskW: b.sum.DiskW / k, NetRx: b.sum.NetRx / k, NetTx: b.sum.NetTx / k}
	for n, v := range b.cpu {
		p.TopCPU = append(p.TopCPU, Top{n, v / k})
	}
	for n, v := range b.mem {
		p.TopMem = append(p.TopMem, Top{n, v})
	}
	p.TopCPU = topN(p.TopCPU, 5, 0.5)
	p.TopMem = topN(p.TopMem, 5, 0)
	return p
}
