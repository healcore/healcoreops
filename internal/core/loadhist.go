package core

import (
	"path/filepath"
	"time"

	"github.com/healcore/healcoreops/internal/loadhist"
	"github.com/healcore/healcoreops/internal/sys"
)

func (c *Core) startLoadHistory() {
	days := c.Config().LoadHistoryDays
	if days <= 0 {
		return
	}
	st := loadhist.Open(filepath.Join(DataDir(), "loadhistory.json.gz"), time.Duration(days)*24*time.Hour)
	c.mu.Lock()
	c.loadHist = st
	c.mu.Unlock()
	go func() {
		for {
			time.Sleep(5 * time.Minute)
			_ = st.Save(false)
		}
	}()
}

// SaveLoadHistory сохраняет историю нагрузки (при выходе из программы).
func (c *Core) SaveLoadHistory() {
	c.mu.Lock()
	st := c.loadHist
	c.mu.Unlock()
	if st != nil {
		_ = st.Save(false)
	}
}

func (c *Core) recordLoad(s *sys.Snapshot) {
	c.mu.Lock()
	st := c.loadHist
	c.mu.Unlock()
	if st == nil || s == nil {
		return
	}
	x := loadhist.Sample{Time: s.Time, CPU: s.CPU, Mem: s.MemPct, DiskR: s.DiskR, DiskW: s.DiskW, NetRx: s.NetRx, NetTx: s.NetTx}
	self := c.col.Self()
	for _, p := range s.Procs {
		if p.PID == self || p.PID <= 4 {
			continue
		}
		if p.CPU < 0.2 && p.RSS < 150<<20 { // мелочь в историю не нужна
			continue
		}
		x.Procs = append(x.Procs, loadhist.ProcSample{Name: p.Name, CPU: p.CPU, RSS: p.RSS})
	}
	st.Add(x)
}

// LoadHistory — история нагрузки за последние hours часов (или период from–to).
func (c *Core) LoadHistory(from, to time.Time, max int) (loadhist.Range, bool) {
	c.mu.Lock()
	st := c.loadHist
	c.mu.Unlock()
	if st == nil {
		return loadhist.Range{Points: []loadhist.Point{}, TopCPU: []loadhist.Top{}, TopMem: []loadhist.Top{}}, false
	}
	return st.Query(from, to, max), true
}
