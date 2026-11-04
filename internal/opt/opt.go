// Package opt — анализ состояния системы и действия по оптимизации.
package opt

import (
	"github.com/healcore/healcoreops/internal/i18n"

	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/healcore/healcoreops/internal/ctl"
	"github.com/healcore/healcoreops/internal/sys"
)

// Level — важность рекомендации.
type Level int

const (
	Info Level = iota
	Warn
	Crit
)

// Recommendation — найденная проблема и совет.
type Recommendation struct {
	Level  Level  `json:"level"`
	Title  string `json:"title"`
	Detail string `json:"detail"`
}

// Analyze оценивает систему по шкале 0–100 и формирует рекомендации.
func Analyze(s *sys.Snapshot, tempBytes int64) (int, []Recommendation) {
	if s == nil {
		return 0, nil
	}
	// Итоговый балл складывается из непрерывных штрафов по нескольким реальным
	// показателям; чем хуже значение, тем больше штраф (а не фиксированные ступени).
	pen := 0.0
	worst := Info
	var recs []Recommendation
	add := func(p float64, l Level, title, detail string) {
		pen += p
		if l > worst {
			worst = l
		}
		recs = append(recs, Recommendation{l, title, i18n.T(detail)})
	}
	ncpu := float64(runtime.NumCPU())

	// ----- Процессор -----
	switch {
	case s.CPU >= 92:
		add(20+(s.CPU-92)*0.6, Crit, fmt.Sprintf(i18n.T("ЦП перегружен: %.0f%%"), s.CPU),
			"Понизьте приоритет фоновых задач или завершите лишние процессы.")
	case s.CPU >= 70:
		add(6+(s.CPU-70)*0.5, Warn, fmt.Sprintf(i18n.T("Высокая загрузка ЦП: %.0f%%"), s.CPU),
			"Проверьте вкладку «Процессы», отсортировав по ЦП.")
	case s.CPU >= 50:
		pen += (s.CPU - 50) * 0.15 // лёгкий штраф без отдельной рекомендации
	}

	// ----- Память -----
	switch {
	case s.MemPct >= 92:
		add(20+(s.MemPct-92)*0.8, Crit, fmt.Sprintf(i18n.T("Память почти исчерпана: %.0f%%"), s.MemPct),
			"Освободите память или закройте тяжёлые приложения — система начнёт активно использовать подкачку.")
	case s.MemPct >= 78:
		add(5+(s.MemPct-78)*0.6, Warn, fmt.Sprintf(i18n.T("Память занята на %.0f%%"), s.MemPct),
			"Включите автоосвобождение памяти или закройте неиспользуемые программы.")
	case s.MemPct >= 60:
		pen += (s.MemPct - 60) * 0.2
	}
	if s.SwapTotal > 0 && s.SwapPct >= 40 {
		add(4+(s.SwapPct-40)*0.2, Warn, fmt.Sprintf(i18n.T("Файл подкачки занят на %.0f%%"), s.SwapPct),
			"Нехватка ОЗУ замедляет работу: данные вытесняются на диск.")
	}

	// ----- Диски -----
	for _, d := range s.Disks {
		free := sys.Bytes(d.Total - d.Used)
		switch {
		case d.Pct >= 94:
			add(12+(d.Pct-94)*1.5, Crit, fmt.Sprintf(i18n.T("Диск %s заполнен на %.0f%%"), d.Mount, d.Pct),
				fmt.Sprintf(i18n.T("Свободно %s. Очистите временные файлы и ненужные данные."), free))
		case d.Pct >= 80:
			add(4+(d.Pct-80)*0.5, Warn, fmt.Sprintf(i18n.T("Диск %s заполнен на %.0f%%"), d.Mount, d.Pct),
				fmt.Sprintf(i18n.T("Свободно %s."), free))
		case d.Pct >= 70:
			pen += (d.Pct - 70) * 0.2
		}
	}

	// ----- Самый прожорливый процесс -----
	var top *sys.Proc
	for i := range s.Procs {
		if top == nil || s.Procs[i].CPU > top.CPU {
			top = &s.Procs[i]
		}
	}
	if top != nil && top.CPU*ncpu >= 80 && !IsProtected(top.Name) {
		add(4, Warn, fmt.Sprintf(i18n.T("%s нагружает ЦП: %.0f%%"), top.Name, top.CPU),
			"Если это фоновая задача — понизьте её приоритет или ограничьте ядрами через правило.")
	}

	// ----- Прочее -----
	if len(s.Procs) > 300 {
		add(math.Min(float64(len(s.Procs)-300)/25, 8), Info, fmt.Sprintf(i18n.T("Запущено много процессов: %d"), len(s.Procs)),
			"Проверьте автозагрузку и отключите ненужные программы.")
	}
	if tempBytes > 1<<30 {
		add(math.Min(float64(tempBytes)/(1<<30)*1.5, 6), Info, i18n.T("Временные файлы: ")+sys.Bytes(uint64(tempBytes)),
			"Их можно удалить в разделе «Оптимизация → Очистка диска».")
	}
	if s.Uptime > 14*86400 {
		add(3, Info, fmt.Sprintf(i18n.T("Без перезагрузки %d дн."), s.Uptime/86400),
			"Перезагрузка освобождает память и завершает установку обновлений.")
	}

	score := int(math.Round(100 - pen))
	// Балл не должен противоречить найденным проблемам.
	switch worst {
	case Crit:
		if score > 45 {
			score = 45
		}
	case Warn:
		if score > 82 {
			score = 82
		}
	}
	if score < 0 {
		score = 0
	} else if score > 100 {
		score = 100
	}
	sort.SliceStable(recs, func(i, j int) bool { return recs[i].Level > recs[j].Level })
	return score, recs
}

// Verdict — словесная оценка балла.
func Verdict(score int) string {
	switch {
	case score >= 85:
		return i18n.T("Отличное состояние")
	case score >= 65:
		return i18n.T("Хорошее состояние")
	case score >= 45:
		return i18n.T("Требует внимания")
	default:
		return i18n.T("Критическое состояние")
	}
}

var protected = map[string]bool{
	"system": true, "registry": true, "smss": true, "csrss": true, "wininit": true,
	"winlogon": true, "services": true, "lsass": true, "svchost": true, "dwm": true,
	"explorer": true, "fontdrvhost": true, "memory compression": true, "secure system": true,
	"audiodg": true, "spoolsv": true, "sihost": true, "ctfmon": true, "taskhostw": true,
	"msmpeng": true, "system idle process": true,
	"systemd": true, "init": true, "kthreadd": true, "xorg": true, "xwayland": true,
	"gnome-shell": true, "kwin_x11": true, "kwin_wayland": true, "plasmashell": true,
	"pulseaudio": true, "pipewire": true, "wireplumber": true, "dbus-daemon": true,
	"launchd": true, "windowserver": true, "kernel_task": true, "loginwindow": true,
	"healcoreops": true,
}

// IsProtected — системный процесс, который нельзя трогать автоматически.
func IsProtected(name string) bool {
	n := strings.TrimSuffix(strings.ToLower(name), ".exe")
	if protected[n] {
		return true
	}
	return strings.HasPrefix(n, "kworker") || strings.HasPrefix(n, "ksoftirqd") ||
		strings.HasPrefix(n, "migration/") || strings.HasPrefix(n, "rcu_")
}

// FindHogs ищет несистемные процессы, занимающие не меньше threshold% одного ядра.
func FindHogs(procs []sys.Proc, threshold float64, self int32) []sys.Proc {
	ncpu := float64(runtime.NumCPU())
	var out []sys.Proc
	for _, p := range procs {
		if p.PID == self || p.PID <= 4 || IsProtected(p.Name) {
			continue
		}
		if !p.PrioOK || p.Priority <= ctl.PrioBelowNormal {
			continue
		}
		if p.CPU*ncpu >= threshold {
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CPU > out[j].CPU })
	return out
}

// TempScan — результат поиска устаревших временных файлов.
type TempScan struct {
	Roots []string
	Files []string
	Bytes int64
}

var skipPrefixes = []string{".x11-unix", ".ice-unix", ".xim-unix", ".font-unix", ".test-unix",
	"systemd-private-", "snap-private-tmp", "tmux-", "ssh-", "healcoreops"}

func skipDir(name string) bool {
	n := strings.ToLower(name)
	for _, p := range skipPrefixes {
		if strings.HasPrefix(n, p) {
			return true
		}
	}
	return false
}

func tempRoots() []string {
	roots := []string{os.TempDir()}
	if runtime.GOOS == "windows" && ctl.IsElevated() {
		if w := os.Getenv("WINDIR"); w != "" {
			roots = append(roots, filepath.Join(w, "Temp"))
		}
	}
	seen := map[string]bool{}
	var out []string
	for _, r := range roots {
		if a, err := filepath.Abs(r); err == nil && !seen[strings.ToLower(a)] {
			seen[strings.ToLower(a)] = true
			out = append(out, a)
		}
	}
	return out
}

// ScanTemp находит обычные файлы во временных каталогах старше maxAge.
func ScanTemp(maxAge time.Duration) TempScan {
	cutoff := time.Now().Add(-maxAge)
	res := TempScan{Roots: tempRoots()}
	for _, root := range res.Roots {
		_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				if d != nil && d.IsDir() && p != root {
					return fs.SkipDir
				}
				return nil
			}
			if d.IsDir() {
				if p != root && skipDir(d.Name()) {
					return fs.SkipDir
				}
				return nil
			}
			if !d.Type().IsRegular() {
				return nil
			}
			info, err := d.Info()
			if err != nil || !info.ModTime().Before(cutoff) {
				return nil
			}
			res.Files = append(res.Files, p)
			res.Bytes += info.Size()
			if len(res.Files) >= 500000 {
				return fs.SkipAll
			}
			return nil
		})
	}
	return res
}

// Clean удаляет найденные файлы. Занятые другими программами файлы пропускаются.
func (t TempScan) Clean() (removed int, freed int64, failed int) {
	for _, f := range t.Files {
		info, err := os.Lstat(f)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		if err := os.Remove(f); err != nil {
			failed++
			continue
		}
		removed++
		freed += info.Size()
	}
	return
}
