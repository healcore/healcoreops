package diskspace

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestFindDupes(t *testing.T) {
	d := t.TempDir()
	big := bytes.Repeat([]byte("abcdefgh"), 20000) // 160 КБ
	big2 := append([]byte(nil), big...)
	big2[len(big2)-1] = 'X' // совпадает начало, отличается конец
	small := []byte("same small content")
	w := func(name string, b []byte) {
		p := filepath.Join(d, name)
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	w("a/big.bin", big)
	w("b/big copy.bin", big)
	w("c/big-other.bin", big2)
	w("s1.txt", small)
	w("x/s2.txt", small)
	w("unique.txt", []byte("unique content!!!!"))
	res := findDupes(d, 1, func() bool { return false })
	if len(res.Groups) != 2 {
		for _, g := range res.Groups {
			t.Logf("%d %v", g.Size, g.Files)
		}
		t.Fatalf("groups %d", len(res.Groups))
	}
	if res.Groups[0].Size != int64(len(big)) || len(res.Groups[0].Files) != 2 {
		t.Fatalf("big group %+v", res.Groups[0])
	}
	if res.Wasted != int64(len(big)+len(small)) {
		t.Fatalf("wasted %d", res.Wasted)
	}
	if res.Files != 6 {
		t.Fatalf("files %d", res.Files)
	}
}
