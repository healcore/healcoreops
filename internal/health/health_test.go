package health

import "testing"

func TestParseNvidiaSMI(t *testing.T) {
	g := ParseNvidiaSMI("NVIDIA GeForce RTX 4060 Ti, 47, 30, 22.85, 3, 1290, 8188\r\nNVIDIA T400, 55, [N/A], [N/A], 0, 10, 2048\n")
	if len(g) != 2 || g[0].TempC != 47 || g[0].FanPct != 30 || g[0].PowerW != 22.85 || g[0].VramTotal != 8188<<20 {
		t.Fatalf("%+v", g)
	}
	if g[1].FanPct != -1 || g[1].PowerW != 0 || g[1].TempC != 55 {
		t.Fatalf("%+v", g[1])
	}
}

func TestLevels(t *testing.T) {
	if l, r := DiskLevel(Disk{Health: "Healthy", WearPct: 3, TempC: 40}); l != 0 || r != "" {
		t.Fatal(l, r)
	}
	if l, r := DiskLevel(Disk{Health: "Healthy", ReadErr: 2}); l != 2 || r != "errors" {
		t.Fatal(l, r)
	}
	if l, r := DiskLevel(Disk{Health: "Healthy", WearPct: 75}); l != 1 || r != "wear" {
		t.Fatal(l, r)
	}
	if BatteryLevel(Battery{Present: true, WearPct: 35}) != 1 || GPULevel(GPU{TempC: 91}) != 2 {
		t.Fatal("levels")
	}
}
