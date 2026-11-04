package core

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/process"

	"github.com/healcore/healcoreops/internal/ctl"
	"github.com/healcore/healcoreops/internal/diag"
	"github.com/healcore/healcoreops/internal/drivers"
	"github.com/healcore/healcoreops/internal/health"
	"github.com/healcore/healcoreops/internal/netconn"
	"github.com/healcore/healcoreops/internal/programs"
	"github.com/healcore/healcoreops/internal/sys"
	"github.com/healcore/healcoreops/internal/winupdate"
)

// Inventory — машиночитаемая инвентаризация компьютера для сбора по парку
// (например, запуском через групповую политику в общую папку).
type Inventory struct {
	Schema    int                      `json:"schema"`
	Generated string                   `json:"generated"` // RFC 3339
	App       string                   `json:"app"`
	Host      string                   `json:"host"`
	User      string                   `json:"user"`
	Admin     bool                     `json:"admin"`
	OS        string                   `json:"os"`
	Hardware  drivers.SysInfo          `json:"hardware"`
	Cores     int                      `json:"logicalCores"`
	RAM       uint64                   `json:"ramBytes"`
	LastBoot  string                   `json:"lastBoot"`
	Volumes   []diskDTO                `json:"volumes"`
	Health    health.Snapshot          `json:"hardwareHealth"`
	Reboot    diag.Reboot              `json:"pendingReboot"`
	Security  []diag.SecItem           `json:"security"`
	Programs  []programs.Program       `json:"programs"`
	Updates   []winupdate.HistoryEntry `json:"updateHistory"`
	Ports     []netconn.Port           `json:"listeningPorts"`
	Adapters  []diag.Adapter           `json:"network"`
	Alerts    []Alert                  `json:"alerts"`
}

// BuildInventory собирает инвентаризацию (10–60 с).
func (c *Core) BuildInventory() Inventory {
	s := c.Snapshot()
	if s == nil {
		s = c.col.Collect()
	}
	h := c.col.Host()
	inv := Inventory{Schema: 1, Generated: time.Now().Format(time.RFC3339), App: "HealCoreOps " + Version,
		Host: h.Hostname, User: currentUser(), Admin: ctl.IsElevated(), OS: h.OS, Cores: h.Logical, RAM: h.MemTotal,
		Volumes: []diskDTO{}, Security: []diag.SecItem{}, Programs: []programs.Program{}, Updates: []winupdate.HistoryEntry{},
		Ports: []netconn.Port{}, Adapters: []diag.Adapter{}, Alerts: []Alert{}}
	if bt, err := host.BootTime(); err == nil {
		inv.LastBoot = time.Unix(int64(bt), 0).Format(time.RFC3339)
	}
	for _, d := range s.Disks {
		inv.Volumes = append(inv.Volumes, diskDTO{d.Mount, d.Total, d.Used, d.Pct})
	}
	var wg sync.WaitGroup
	run := func(f func()) { wg.Add(1); go func() { defer wg.Done(); f() }() }
	run(func() {
		if hs := health.Get(); hs.Ready {
			inv.Health = hs
		} else {
			inv.Health = health.CollectNow()
		}
	})
	run(func() { inv.Adapters = diag.Adapters() })
	run(func() {
		if p, err := netconn.Listening(c.procNamePath); err == nil {
			for _, x := range p {
				if x.Exposure != "local" {
					inv.Ports = append(inv.Ports, x)
				}
			}
		}
	})
	if drivers.Supported() {
		run(func() { inv.Hardware = drivers.GetSysInfo() })
	}
	if diag.Supported() {
		run(func() { inv.Reboot = diag.PendingReboot() })
		run(func() { inv.Security = diag.Security() })
	}
	if programs.Supported() {
		run(func() {
			if p, err := programs.List(); err == nil {
				for i := range p {
					p[i].Icon = ""
				}
				inv.Programs = p
			}
		})
	}
	if winupdate.Supported() {
		run(func() {
			if u, err := winupdate.History(60); err == nil {
				inv.Updates = u
			}
		})
	}
	wg.Wait()
	if inv.Hardware.CPU == "" {
		inv.Hardware.CPU, inv.Hardware.RAM, inv.Hardware.OS = h.CPUModel, sys.Bytes(h.MemTotal), h.OS
	}
	cfg := c.Config()
	in := alertInput{disks: s.Disks, health: inv.Health, memPct: s.MemPct, reboot: inv.Reboot}
	a := cfg.Alerts
	a.MemPct, a.Services = 0, nil // в разовом запуске длительность и службы не оцениваем
	inv.Alerts = computeAlerts(in, a)
	if inv.Alerts == nil {
		inv.Alerts = []Alert{}
	}
	return inv
}

// procNamePath — имя и путь процесса по PID (из последнего снимка или напрямую).
func (c *Core) procNamePath(pid int32) (string, string) {
	if s := c.Snapshot(); s != nil {
		for _, p := range s.Procs {
			if p.PID == pid {
				return p.Name, procExe(pid)
			}
		}
	}
	return procExeName(pid)
}

func procExe(pid int32) string {
	if p, err := process.NewProcess(pid); err == nil {
		if e, err := p.Exe(); err == nil {
			return e
		}
	}
	return ""
}

func procExeName(pid int32) (string, string) {
	p, err := process.NewProcess(pid)
	if err != nil {
		return "", ""
	}
	n, _ := p.Name()
	e, _ := p.Exe()
	return n, e
}

// InventoryCSVHeader — столбцы сводной таблицы парка.
var InventoryCSVHeader = []string{"host", "generated", "os", "cpu", "ram_gb", "board", "gpu", "system_free_pct",
	"disk_worst", "battery_wear_pct", "pending_reboot", "security_issues", "programs", "last_update", "failed_updates", "exposed_ports", "alerts", "user", "app"}

// CSVRow — одна строка сводной таблицы (один компьютер).
func (inv Inventory) CSVRow() []string {
	freePct := ""
	for _, v := range inv.Volumes {
		if strings.EqualFold(strings.TrimRight(v.Mount, `\/`), "C:") || v.Mount == "/" {
			freePct = fmt.Sprintf("%.0f", 100-v.Pct)
		}
	}
	worst, worstLvl := "ok", 0
	for _, d := range inv.Health.Disks {
		if d.Level > worstLvl {
			worstLvl, worst = d.Level, d.Name+": "+d.Reason
		}
	}
	if len(inv.Health.Disks) == 0 {
		worst = ""
	}
	bat := ""
	if inv.Health.Battery.Present && inv.Health.Battery.WearPct > 0 {
		bat = fmt.Sprintf("%.0f", inv.Health.Battery.WearPct)
	}
	sec := 0
	for _, s := range inv.Security {
		if s.Level == 2 {
			sec++
		}
	}
	last, failed := "", 0
	cut := time.Now().AddDate(0, 0, -30).Format(time.RFC3339)
	for _, u := range inv.Updates {
		if u.Result == 2 && u.Date > last {
			last = u.Date
		}
		if (u.Result == 4 || u.Result == 5) && u.Date >= cut {
			failed++
		}
	}
	if len(last) >= 10 {
		last = last[:10]
	}
	ports := make([]string, 0, len(inv.Ports))
	for _, p := range inv.Ports {
		if netconn.Risky(p) {
			ports = append(ports, fmt.Sprintf("%s/%d", p.Proto, p.Port))
		}
	}
	sort.Strings(ports)
	alerts := make([]string, 0, len(inv.Alerts))
	for _, a := range inv.Alerts {
		alerts = append(alerts, a.Title)
	}
	return []string{inv.Host, inv.Generated, inv.OS, inv.Hardware.CPU, fmt.Sprintf("%.1f", float64(inv.RAM)/(1<<30)),
		inv.Hardware.Board, inv.Hardware.GPU, freePct, worst, bat, fmt.Sprint(inv.Reboot.Pending), fmt.Sprint(sec),
		fmt.Sprint(len(inv.Programs)), last, fmt.Sprint(failed), strings.Join(ports, " "), strings.Join(alerts, "; "), inv.User, inv.App}
}

// SaveInventory пишет инвентаризацию: .json — полный документ, .csv — строка в сводную
// таблицу (дописывается; заголовок — только в новый файл, строка этого компьютера заменяется).
// В пути можно указать %COMPUTERNAME%.
func SaveInventory(inv Inventory, path string) (string, error) {
	if path == "" {
		return "", errors.New("no path")
	}
	path = strings.NewReplacer("%COMPUTERNAME%", safeName(inv.Host), "%computername%", safeName(inv.Host)).Replace(path)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil && filepath.Dir(path) != "." {
		return "", err
	}
	if !strings.EqualFold(filepath.Ext(path), ".csv") {
		b, _ := json.MarshalIndent(inv, "", "  ")
		return path, writeAtomic(path, b)
	}
	unlock, err := lockFile(path + ".lock")
	if err != nil {
		return "", err
	}
	defer unlock()
	var rows [][]string
	if b, err := os.ReadFile(path); err == nil {
		rows, _ = csv.NewReader(strings.NewReader(strings.TrimPrefix(string(b), "\ufeff"))).ReadAll()
	}
	if len(rows) == 0 || len(rows[0]) == 0 || rows[0][0] != InventoryCSVHeader[0] {
		rows = [][]string{InventoryCSVHeader}
	}
	out := [][]string{rows[0]}
	for _, r := range rows[1:] {
		if len(r) > 0 && !strings.EqualFold(r[0], inv.Host) {
			out = append(out, r)
		}
	}
	out = append(out, inv.CSVRow())
	var sb strings.Builder
	sb.WriteString("\ufeff") // BOM — Excel откроет кириллицу правильно
	w := csv.NewWriter(&sb)
	_ = w.WriteAll(out)
	return path, writeAtomic(path, []byte(sb.String()))
}

// lockFile — простая блокировка общего файла (много компьютеров пишут в одну таблицу).
func lockFile(p string) (func(), error) {
	deadline := time.Now().Add(60 * time.Second)
	for {
		f, err := os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			f.Close()
			return func() { os.Remove(p) }, nil
		}
		if st, e := os.Stat(p); e == nil && time.Since(st.ModTime()) > 3*time.Minute {
			os.Remove(p) // осталась от прерванного запуска
			continue
		}
		if time.Now().After(deadline) {
			return nil, errors.New("inventory file is locked: " + p)
		}
		time.Sleep(time.Duration(200+time.Now().UnixNano()%600) * time.Millisecond)
	}
}

func writeAtomic(path string, b []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
