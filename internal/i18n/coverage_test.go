package i18n

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// TestCoverage проверяет, что у каждой строки, переданной в T(), есть английский перевод.
func TestCoverage(t *testing.T) {
	reT := regexp.MustCompile(`i18n\.T\(("(?:[^"\\]|\\.)*")\)`)
	reDetail := regexp.MustCompile(`(?m)^\s+("[А-ЯЁ][^"]*\.")\)\s*$`) // подробности рекомендаций в opt.go
	reQuoted := regexp.MustCompile(`(?:procAction\("[^"]+", |PriorityNames = \[\]string\{|PowerNames = \[\]string\{)(.*)`)
	missing := 0
	_ = filepath.Walk("../..", func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		b, _ := os.ReadFile(p)
		src := string(b)
		var keys []string
		for _, m := range reT.FindAllStringSubmatch(src, -1) {
			keys = append(keys, m[1])
		}
		for _, m := range reDetail.FindAllStringSubmatch(src, -1) {
			keys = append(keys, m[1])
		}
		for _, m := range reQuoted.FindAllStringSubmatch(src, -1) {
			for _, q := range regexp.MustCompile(`"[^"]*[А-Яа-я][^"]*"`).FindAllString(m[1], -1) {
				keys = append(keys, q)
			}
		}
		for _, k := range keys {
			s, err := strconv.Unquote(k)
			if err != nil {
				continue
			}
			if _, ok := en[s]; !ok {
				t.Errorf("%s: нет перевода для %q", p, s)
				missing++
			}
		}
		return nil
	})
	if missing == 0 {
		t.Log("все строки переведены")
	}
}
