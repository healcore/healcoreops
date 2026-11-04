package diskspace

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
	"time"
	"unicode/utf16"
)

func winEnv() protEnv {
	return protEnv{
		windows: true, systemRoot: `C:\Windows`, programData: `C:\ProgramData`,
		programFiles: []string{`C:\Program Files`, `C:\Program Files (x86)`},
		users:        `C:\Users`, profile: `C:\Users\u`, appData: `C:\Users\u\AppData`,
		self: `C:\Users\u\AppData\Local\Programs\HealCoreOps`,
	}
}

func TestProtectedWindows(t *testing.T) {
	e := winEnv()
	prot := []string{`C:\`, `C:`, `D:\`, `C:\Windows`, `c:\windows\system32\drivers\etc\hosts`, `C:\Program Files\App`,
		`C:\Program Files (x86)\Steam`, `C:\ProgramData`, `C:\Users`, `C:\Users\u`, `C:\Users\u\AppData`,
		`C:\Users\u\AppData\Local`, `C:\Users\u\AppData\Local\Microsoft\Windows\INetCache`, `C:\pagefile.sys`,
		`D:\$RECYCLE.BIN`, `C:\System Volume Information`, `C:\Users\u\AppData\Local\Programs\HealCoreOps\HealCoreOps.exe`}
	for _, p := range prot {
		if !e.protected(p) {
			t.Errorf("должен быть защищён: %s", p)
		}
	}
	free := []string{`C:\Users\u\Downloads\big.iso`, `D:\Games\old`, `C:\Users\u\Videos`, `C:\Users\u\AppData\Local\Temp\x`,
		`C:\Users\u\AppData\Local\Google\Chrome\User Data\Default\Cache`, `E:\backup.zip`, `C:\Users\u\Documents\pagefile.sys`}
	for _, p := range free {
		if e.protected(p) {
			t.Errorf("не должен быть защищён: %s", p)
		}
	}
}

func TestProtectedUnix(t *testing.T) {
	e := protEnv{profile: "/home/u"}
	for _, p := range []string{"/", "/usr/lib", "/etc", "/home", "/home/u"} {
		if !e.protected(p) {
			t.Errorf("должен быть защищён: %s", p)
		}
	}
	for _, p := range []string{"/home/u/Downloads/x.iso", "/home/u/.cache/thumbnails", "/mnt/data/old"} {
		if e.protected(p) {
			t.Errorf("не должен быть защищён: %s", p)
		}
	}
}

func mkI(ver uint64, path string) []byte {
	u := utf16.Encode([]rune(path))
	b := make([]byte, 24)
	binary.LittleEndian.PutUint64(b[0:], ver)
	binary.LittleEndian.PutUint64(b[8:], 12345)
	ft := uint64(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC).UnixNano()/100 + 116444736000000000)
	binary.LittleEndian.PutUint64(b[16:], ft)
	if ver == 2 {
		n := make([]byte, 4)
		binary.LittleEndian.PutUint32(n, uint32(len(u)+1))
		b = append(b, n...)
		for _, c := range append(u, 0) {
			b = binary.LittleEndian.AppendUint16(b, c)
		}
		return b
	}
	raw := make([]byte, 520)
	for i, c := range u {
		binary.LittleEndian.PutUint16(raw[2*i:], c)
	}
	return append(b, raw...)
}

func TestParseRecycleInfo(t *testing.T) {
	for _, v := range []uint64{1, 2} {
		ri, err := ParseRecycleInfo(mkI(v, `C:\Users\u\Видео\фильм.mkv`))
		if err != nil || ri.Path != `C:\Users\u\Видео\фильм.mkv` || ri.Size != 12345 || ri.Deleted.Year() != 2026 {
			t.Fatalf("v%d: %+v %v", v, ri, err)
		}
	}
	if _, err := ParseRecycleInfo([]byte{1, 2, 3}); err == nil {
		t.Fatal("ожидалась ошибка")
	}
}

func TestScanAndClean(t *testing.T) {
	root := t.TempDir()
	write := func(rel string, n int) {
		p := filepath.Join(root, rel)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		_ = os.WriteFile(p, make([]byte, n), 0o644)
	}
	write("a/big.bin", 5000)
	write("a/b/small.txt", 10)
	write("c/mid.bin", 2000)
	write("root.txt", 100)
	res := scanDir(root)
	if res.Bytes != 7110 || res.Files != 4 {
		t.Fatalf("bytes=%d files=%d", res.Bytes, res.Files)
	}
	if res.Folders[0].Name != "a" || res.Folders[0].Size != 5010 || res.Largest[0].Name != "big.bin" {
		t.Fatalf("order: %+v / %+v", res.Folders[0], res.Largest[0])
	}
	r := cleanPaths([]string{filepath.Join(root, "a")}, 0)
	if r.Removed != 2 || r.Freed != 5010 {
		t.Fatalf("clean: %+v", r)
	}
	if _, err := os.Stat(filepath.Join(root, "a")); err != nil {
		t.Fatal("корневая папка категории должна остаться")
	}
	if _, err := os.Stat(filepath.Join(root, "a", "b")); err == nil {
		t.Fatal("пустая подпапка должна удалиться")
	}
}
