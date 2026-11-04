package diag

import (
	"encoding/binary"
	"testing"
)

func TestParseDumpHeader(t *testing.T) {
	b := make([]byte, 0x2000)
	copy(b, "PAGEDU64")
	binary.LittleEndian.PutUint32(b[0x38:], 0x116)
	binary.LittleEndian.PutUint64(b[0x40:], 0xffff9a0c12345678)
	code, p, err := ParseDumpHeader(b)
	if err != nil || code != 0x116 || p[0] != 0xffff9a0c12345678 {
		t.Fatalf("64: %x %x %v", code, p, err)
	}
	b32 := make([]byte, 0x1000)
	copy(b32, "PAGEDUMP")
	binary.LittleEndian.PutUint32(b32[0x28:], 0xD1)
	if code, _, err := ParseDumpHeader(b32); err != nil || code != 0xD1 {
		t.Fatalf("32: %x %v", code, err)
	}
	if _, _, err := ParseDumpHeader([]byte("MDMP....")); err == nil {
		t.Fatal("user-mode dump must be rejected")
	}
}

func TestBugCheckText(t *testing.T) {
	ru := "Компьютер был перезагружен после критической ошибки.  Код ошибки: 0x00000116 (0xffffc30b9a3c8010, 0xfffff80568d2e0b4, 0xffffffffc000009a, 0x0000000000000004). Дамп памяти сохранен в: C:\\WINDOWS\\Minidump\\101524-10421-01.dmp."
	if c, ok := ParseBugCheckText(ru); !ok || c != 0x116 {
		t.Fatalf("got %x %v", c, ok)
	}
	if c, ok := ParseBugCheckText("The bugcheck was: 0x0000007e (0x...)"); !ok || c != 0x7e {
		t.Fatalf("en got %x %v", c, ok)
	}
	if _, ok := ParseBugCheckText("no code here 0x12"); ok {
		t.Fatal("short hex must be ignored")
	}
	d := DescribeBugCheck(0x124, false)
	if d.Name != "WHEA_UNCORRECTABLE_ERROR" || d.Hex != "0x00000124" || d.Hint == "" {
		t.Fatalf("%+v", d)
	}
	if DescribeBugCheck(0x1000007E, true).Name != "SYSTEM_THREAD_EXCEPTION_NOT_HANDLED" {
		t.Fatal("_M suffix")
	}
	if DescribeBugCheck(0xDEAD, true).Name != "BUGCHECK" {
		t.Fatal("unknown")
	}
}
