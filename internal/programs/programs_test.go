package programs

import (
	"reflect"
	"strings"
	"testing"
)

func TestCleanName(t *testing.T) {
	cases := map[string]string{
		"7-Zip 23.01 (x64)":        "7-Zip",
		"Mozilla Firefox (x64 ru)": "Mozilla Firefox",
		"Notepad++ (64-bit x64)":   "Notepad++",
		"Steam":                    "Steam",
		"VLC media player":         "VLC media player",
		"Python 3.12.4 (64-bit)":   "Python",
		"Microsoft Visual C++ 2015-2022 Redistributable (x64) - 14.38.33130": "Microsoft Visual C++ 2015-2022 Redistributable",
		"Discord":                    "Discord",
		"WinRAR 7.01 (64-разрядная)": "WinRAR",
	}
	for in, want := range cases {
		if got := CleanName(in); got != want {
			t.Errorf("CleanName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSplitCommand(t *testing.T) {
	fs := map[string]bool{`C:\Program Files\Some App\uninst.exe`: true, `C:\Tools\u.exe`: true}
	ex := func(p string) bool { return fs[p] }
	cases := []struct{ in, exe, args string }{
		{`"C:\Program Files\Some App\uninst.exe" /S`, `C:\Program Files\Some App\uninst.exe`, "/S"},
		{`C:\Program Files\Some App\uninst.exe /S /x`, `C:\Program Files\Some App\uninst.exe`, "/S /x"},
		{`C:\Tools\u.exe`, `C:\Tools\u.exe`, ""},
		{`MsiExec.exe /X{AAAAAAAA-BBBB-CCCC-DDDD-EEEEEEEEEEEE}`, "MsiExec.exe", "/X{AAAAAAAA-BBBB-CCCC-DDDD-EEEEEEEEEEEE}"},
	}
	for _, c := range cases {
		exe, args := SplitCommand(c.in, ex)
		if exe != c.exe || args != c.args {
			t.Errorf("SplitCommand(%q) = %q,%q", c.in, exe, args)
		}
	}
	if g := MSIProduct(`MsiExec.exe /I{12345678-1234-1234-1234-123456789ABC}`); g != "{12345678-1234-1234-1234-123456789ABC}" {
		t.Errorf("MSIProduct = %q", g)
	}
	if g := MSIProduct(`"C:\x\unins000.exe"`); g != "" {
		t.Errorf("MSIProduct non-msi = %q", g)
	}
}

func TestParseWinget(t *testing.T) {
	ru := "   - \r   \\ \r" + `Имя                                   ИД                         Версия       Доступно     Источник
---------------------------------------------------------------------------------------------------------
Mozilla Firefox (x64 ru)              Mozilla.Firefox.ru         128.0        129.0.1      winget
7-Zip 23.01 (x64)                     7zip.7zip                  23.01        24.08        winget
Notepad++ (64-bit x64)                Notepad++.Notepad++        8.6.2        8.6.9        winget
Доступно обновлений: 3.
`
	got := ParseWingetTable(ru)
	want := []Upgrade{
		{"Mozilla Firefox (x64 ru)", "Mozilla.Firefox.ru", "128.0", "129.0.1", "winget"},
		{"7-Zip 23.01 (x64)", "7zip.7zip", "23.01", "24.08", "winget"},
		{"Notepad++ (64-bit x64)", "Notepad++.Notepad++", "8.6.2", "8.6.9", "winget"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v", got)
	}
	if ParseWingetTable("No installed package found matching input criteria.") != nil {
		t.Fatal("пустой вывод должен давать nil")
	}
}

func TestLeftoverNames(t *testing.T) {
	got := strings.Join(LeftoverNames("Mozilla Firefox (x64 ru)", "Mozilla"), "|")
	if got != "Mozilla Firefox (x64 ru)|Mozilla Firefox|Firefox" {
		t.Fatalf("got %q", got)
	}
	if n := LeftoverNames("Microsoft", "Microsoft Corporation"); len(n) != 0 {
		t.Fatalf("общие имена: %v", n)
	}
	if p := CleanPublisher("Valve Corporation"); p != "Valve" {
		t.Fatal(p)
	}
}

func TestLocationMatches(t *testing.T) {
	ok := [][3]string{{`C:\Program Files\7-Zip\`, "7-Zip 24.08 (x64)", "Igor Pavlov"}, {`C:\Program Files (x86)\Steam`, "Steam", "Valve Corporation"},
		{`C:\Users\u\AppData\Local\Discord`, "Discord", "Discord Inc."}}
	for _, c := range ok {
		if !LocationMatches(c[0], c[1], c[2]) {
			t.Errorf("должно совпасть: %v", c)
		}
	}
	bad := [][3]string{{`C:\Program Files\Common Files`, "Some Tool", "Acme"}, {`D:\Games`, "Cyberpunk 2077", "CD PROJEKT RED"}, {`C:\`, "X", "Y"}}
	for _, c := range bad {
		if LocationMatches(c[0], c[1], c[2]) {
			t.Errorf("не должно совпасть: %v", c)
		}
	}
}
