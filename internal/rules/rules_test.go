package rules

import (
	"reflect"
	"testing"
)

func TestMatches(t *testing.T) {
	cases := []struct {
		pat, name string
		want      bool
	}{
		{"chrome", "chrome.exe", true},
		{"Chrome.exe", "chrome", true},
		{"chrome*", "chrome_crashpad", true},
		{"*worker*", "kworker/0:1", true},
		{"steam", "steamwebhelper.exe", false},
		{"", "anything", false},
	}
	for _, c := range cases {
		if got := (Rule{Pattern: c.pat}).Matches(c.name); got != c.want {
			t.Errorf("%q ~ %q = %v, want %v", c.pat, c.name, got, c.want)
		}
	}
}

func TestCores(t *testing.T) {
	got, err := ParseCores("0-3, 6,5", 8)
	if err != nil || !reflect.DeepEqual(got, []int{0, 1, 2, 3, 5, 6}) {
		t.Fatalf("ParseCores = %v, %v", got, err)
	}
	if s := FormatCores(got); s != "0-3,5-6" {
		t.Fatalf("FormatCores = %q", s)
	}
	if _, err := ParseCores("9", 8); err == nil {
		t.Fatal("ожидалась ошибка для несуществующего ядра")
	}
	if _, err := ParseCores("3-1", 8); err == nil {
		t.Fatal("ожидалась ошибка для обратного диапазона")
	}
}
