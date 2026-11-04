package loadhist

import (
	"path/filepath"
	"testing"
	"time"
)

func TestMinuteAggregationAndQuery(t *testing.T) {
	dir := t.TempDir()
	s := Open(filepath.Join(dir, "h.json.gz"), DefaultKeep)
	base := time.Now().Truncate(time.Minute).Add(-10 * time.Minute)
	for m := 0; m < 5; m++ {
		for k := 0; k < 30; k++ { // 30 замеров в минуту
			s.Add(Sample{Time: base.Add(time.Duration(m)*time.Minute + time.Duration(k)*2*time.Second),
				CPU: float64(10 * (m + 1)), Mem: 50,
				Procs: []ProcSample{{"game.exe", float64(8 * (m + 1)), 2 << 30}, {"chrome.exe", 1, 500 << 20}, {"chrome.exe", 1, 300 << 20}}})
		}
	}
	s.Add(Sample{Time: base.Add(6 * time.Minute)}) // закрывает пятую минуту
	r := s.Query(base.Add(-time.Minute), base.Add(10*time.Minute), 100)
	if r.Minutes != 5 || len(r.Points) != 5 {
		t.Fatalf("minutes=%d points=%d", r.Minutes, len(r.Points))
	}
	if r.Points[0].CPU != 10 || r.Points[4].CPU != 50 {
		t.Fatalf("cpu %v %v", r.Points[0].CPU, r.Points[4].CPU)
	}
	if r.TopCPU[0].Name != "game.exe" || r.TopCPU[0].V != 24 { // (8+16+24+32+40)/5
		t.Fatalf("topcpu %+v", r.TopCPU)
	}
	// память процессов с одинаковым именем суммируется
	var chrome float64
	for _, x := range r.TopMem {
		if x.Name == "chrome.exe" {
			chrome = x.V
		}
	}
	if chrome != float64(800<<20) {
		t.Fatalf("chrome mem %v", chrome)
	}
	if err := s.Save(true); err != nil {
		t.Fatal(err)
	}
	s2 := Open(filepath.Join(dir, "h.json.gz"), DefaultKeep)
	if got := s2.Query(base.Add(-time.Minute), base.Add(10*time.Minute), 100).Minutes; got != 5 {
		t.Fatalf("reloaded %d", got)
	}
}

func TestDownsample(t *testing.T) {
	var pts []Point
	from := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 24*60; i++ {
		pts = append(pts, Point{T: from.Add(time.Duration(i) * time.Minute).UnixMilli(), CPU: float64(i % 100), CPUMax: 99})
	}
	r := build(pts, from, from.Add(24*time.Hour), 300, pts[0].T)
	if len(r.Points) > 300 || len(r.Points) < 150 {
		t.Fatalf("points %d step %d", len(r.Points), r.Step)
	}
	if r.Points[0].CPUMax != 99 {
		t.Fatal("max lost")
	}
}

func TestPrune(t *testing.T) {
	s := &Store{keep: time.Hour}
	now := time.Now()
	s.points = []Point{{T: now.Add(-2 * time.Hour).UnixMilli()}, {T: now.Add(-10 * time.Minute).UnixMilli()}}
	s.prune(now)
	if len(s.points) != 1 {
		t.Fatal(len(s.points))
	}
}
