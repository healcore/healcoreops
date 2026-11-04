package drivers

import "testing"

func TestNvidiaVersion(t *testing.T) {
	cases := map[string]string{"32.0.15.6636": "566.36", "31.0.15.5161": "551.61", "32.0.16.1088": "610.88", "31.0.15.5009": "550.09", "27.21.14.5671": "456.71", "1.2": ""}
	for in, want := range cases {
		if got := NvidiaVersion(in); got != want {
			t.Errorf("NvidiaVersion(%q)=%q want %q", in, got, want)
		}
	}
	if CompareVersions("566.36", "565.90") != 1 || CompareVersions("551.61", "551.61") != 0 || CompareVersions("10.0.1", "10.0.19041") != -1 {
		t.Fatal("CompareVersions")
	}
}

const nvXML = `<?xml version="1.0" encoding="utf-8"?>
<LookupValueSearch><LookupValues>
<LookupValue ParentID="129" RequiresProduct="True"><Name>GeForce RTX 4060 Ti</Name><Value>1022</Value></LookupValue>
<LookupValue ParentID="129" RequiresProduct="True"><Name>GeForce RTX 4060</Name><Value>1023</Value></LookupValue>
<LookupValue ParentID="130" RequiresProduct="True"><Name>GeForce RTX 4060 Laptop GPU</Name><Value>1007</Value></LookupValue>
<LookupValue ParentID="120" RequiresProduct="True"><Name>GeForce RTX 3080</Name><Value>930</Value></LookupValue>
</LookupValues></LookupValueSearch>`

func TestNvLookup(t *testing.T) {
	fams, err := ParseNvLookup([]byte(nvXML))
	if err != nil || len(fams) != 4 {
		t.Fatal(err, fams)
	}
	cases := map[string]string{"NVIDIA GeForce RTX 4060 Ti": "1022", "NVIDIA GeForce RTX 4060": "1023",
		"NVIDIA GeForce RTX 4060 Laptop GPU": "1007", "NVIDIA GeForce RTX 3080 (TM)": "930"}
	for name, want := range cases {
		f, ok := MatchNvFamily(name, fams)
		if !ok || f.ID != want {
			t.Errorf("%s → %+v", name, f)
		}
	}
	if _, ok := MatchNvFamily("NVIDIA GeForce GT 710", fams); ok {
		t.Error("unknown model must not match")
	}
}

func TestNvDriverJSON(t *testing.T) {
	js := `{"Success":"1","IDS":[{"downloadInfo":{"Success":"1","Version":"566.36","Name":"GeForce%20Game%20Ready%20Driver",
"DownloadURL":"https%3A%2F%2Fus.download.nvidia.com%2FWindows%2F566.36%2F566.36-desktop-win10-win11-64bit-international-dch-whql.exe","DownloadURLFileSize":"685.4 MB"}}]}`
	v, link, size, err := ParseNvDriver([]byte(js))
	if err != nil || v != "566.36" || link != "https://us.download.nvidia.com/Windows/566.36/566.36-desktop-win10-win11-64bit-international-dch-whql.exe" || size < 680<<20 {
		t.Fatalf("%q %q %d %v", v, link, size, err)
	}
	bad := `{"Success":"1","IDS":[{"downloadInfo":{"Version":"1","DownloadURL":"https://evil.example.com/x.exe"}}]}`
	if _, _, _, err := ParseNvDriver([]byte(bad)); err == nil {
		t.Fatal("foreign host must be rejected")
	}
	if NvidiaHostOK("http://us.download.nvidia.com/x.exe") || NvidiaHostOK("https://nvidia.com.evil.io/x") || !NvidiaHostOK("https://international.download.nvidia.com/x.exe") {
		t.Fatal("host check")
	}
}

func TestMatchWU(t *testing.T) {
	devs := []Device{
		{ID: "board", Name: "Micro-Star International Co., Ltd. PRO B650-S WIFI"},
		{ID: "cpu", Name: "AMD Ryzen 7 7800X3D 8-Core Processor", Manu: "AuthenticAMD"},
		{ID: `PCI\VEN_10EC&DEV_8125\01`, Name: "Realtek Gaming 2.5GbE Family Controller", Class: "Net", HW: []string{`PCI\VEN_10EC&DEV_8125&SUBSYS_7D781462&REV_05`, `PCI\VEN_10EC&DEV_8125`}},
		{ID: `PCI\VEN_14C3&DEV_0616\01`, Name: "RZ616 Wi-Fi 6E 160MHz", Class: "Net", HW: []string{`PCI\VEN_14C3&DEV_0616&SUBSYS_E0CC105B`}},
	}
	ups := []WUDriver{
		{ID: "u1", Title: "Realtek - Net - 10.70.1003.2024", Manu: "Realtek", Class: "Net", HW: `pci\ven_10ec&dev_8125`, Date: "2024-10-03"},
		{ID: "u2", Title: "Advanced Micro Devices, Inc - System - 6.0.0.55", Manu: "Advanced Micro Devices, Inc", Class: "System", HW: `ACPI\AMDI0052`, Date: "2024-06-01"},
		{ID: "u3", Title: "Micro-Star - System - 1.0.0.12", Manu: "Micro-Star International", Class: "System", HW: `ACPI\MSI0001`},
		{ID: "u4", Title: "MSI - Firmware - 7.2", Manu: "Micro-Star", Class: "Firmware", HW: `UEFI\RES_{...}`},
		{ID: "u5", Title: "Printer driver", Class: "Printer", HW: `USBPRINT\HP`},
	}
	m := MatchWU(devs, ups)
	if len(m[devs[2].ID]) != 1 || m[devs[2].ID][0].ID != "u1" {
		t.Errorf("realtek: %+v", m[devs[2].ID])
	}
	if len(m["cpu"]) != 1 || m["cpu"][0].ID != "u2" {
		t.Errorf("cpu: %+v", m["cpu"])
	}
	if len(m["board"]) != 1 || m["board"][0].ID != "u3" {
		t.Errorf("board (без прошивки и принтера): %+v", m["board"])
	}
	if len(m[devs[3].ID]) != 0 {
		t.Errorf("wifi: %+v", m[devs[3].ID])
	}
}
