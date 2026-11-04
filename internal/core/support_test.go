package core

import (
	"archive/zip"
	"path/filepath"
	"strings"
	"testing"
)

func TestRedactor(t *testing.T) {
	t.Setenv("USERPROFILE", `C:\Users\ivanov`)
	r := redactor()
	in := `{"path":"C:\\Users\\ivanov\\AppData\\x.exe","p2":"C:\Users\ivanov\Desktop"}`
	out := string(r([]byte(in)))
	if strings.Contains(strings.ToLower(out), "ivanov") {
		t.Fatalf("not redacted: %s", out)
	}
	if !strings.Contains(out, `%USERPROFILE%\\AppData`) || !strings.Contains(out, `%USERPROFILE%\Desktop`) {
		t.Fatalf("bad: %s", out)
	}
}

func TestSupportBundle(t *testing.T) {
	c := New()
	c.Prime()
	p, err := c.SupportBundle(filepath.Join(t.TempDir(), "b.zip"))
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zip.OpenReader(p)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	got := map[string]bool{}
	for _, f := range zr.File {
		got[f.Name] = true
	}
	for _, n := range []string{"README.txt", "report.json", "inventory.json", "history.json", "config.json", "log-session.txt", "jobs.json"} {
		if !got[n] {
			t.Errorf("missing %s (%v)", n, got)
		}
	}
}
