package core

import (
	"encoding/csv"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/healcore/healcoreops/internal/config"
	"github.com/healcore/healcoreops/internal/diag"
	"github.com/healcore/healcoreops/internal/health"
	"github.com/healcore/healcoreops/internal/sys"
)

func TestInventoryCSVMerge(t *testing.T) {
	p := filepath.Join(t.TempDir(), "fleet.csv")
	for _, h := range []string{"PC-1", "PC-2", "PC-1"} {
		inv := Inventory{Host: h, OS: "Windows 11", Volumes: []diskDTO{{"C:\\", 100, 80, 80}}}
		if _, err := SaveInventory(inv, p); err != nil {
			t.Fatal(err)
		}
	}
	b, _ := os.ReadFile(p)
	if !strings.HasPrefix(string(b), "\ufeffhost,") || strings.Count(string(b), "\ufeff") != 1 {
		t.Fatalf("bom/header: %q", string(b[:20]))
	}
	rows, _ := csv.NewReader(strings.NewReader(strings.TrimPrefix(string(b), "\ufeff"))).ReadAll()
	if len(rows) != 3 || rows[1][0] != "PC-2" || rows[2][0] != "PC-1" || rows[2][7] != "20" {
		t.Fatalf("%v", rows)
	}
	if _, err := os.Stat(p + ".lock"); err == nil {
		t.Fatal("lock left behind")
	}
	jp, err := SaveInventory(Inventory{Host: "PC 3"}, filepath.Join(filepath.Dir(p), "%COMPUTERNAME%.json"))
	if err != nil || filepath.Base(jp) != "PC_3.json" {
		t.Fatal(jp, err)
	}
}

func TestComputeAlerts(t *testing.T) {
	a := config.Default().Alerts
	a.Services = []string{"Spooler", "Missing"}
	in := alertInput{
		disks:      []sys.Disk{{Mount: "C:", Total: 500 << 30, Used: 480 << 30, Pct: 96}, {Mount: "E:", Total: 1 << 30, Pct: 99}},
		health:     health.Snapshot{Disks: []health.Disk{{Name: "SSD", Level: 2, Reason: "errors", ReadErr: 3}}, GPUs: []health.GPU{{Name: "RTX", TempC: 91}}},
		memPct:     95,
		memHighFor: 12 * 60 * 1e9,
		reboot:     diag.Reboot{Pending: true, Reasons: []string{"CBS"}},
		services:   map[string]string{"spooler": "stopped"},
	}
	got := map[string]int{}
	for _, x := range computeAlerts(in, a) {
		got[x.Key] = x.Level
	}
	want := map[string]int{"disk_free:C:": 2, "disk_health:SSD": 2, "gpu_temp:RTX": 2, "mem": 1, "reboot": 1, "svc:spooler": 2, "svc:missing": 2}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s: %d want %d (all %v)", k, got[k], v, got)
		}
	}
	if _, ok := got["disk_free:E:"]; ok {
		t.Error("small volume must be ignored")
	}
	a.Enabled = true
	a.DiskFreePct, a.GPUTemp, a.MemPct, a.Reboot, a.DiskHealth, a.Services = 0, 0, 0, false, false, nil
	if l := computeAlerts(in, a); len(l) != 0 {
		t.Errorf("disabled checks fired: %+v", l)
	}
}
