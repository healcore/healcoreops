// Package rules — правила распределения ресурсов: какому процессу какой
// приоритет и какие ядра ЦП назначать автоматически.
package rules

import (
	"github.com/healcore/healcoreops/internal/i18n"

	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/healcore/healcoreops/internal/ctl"
	"github.com/healcore/healcoreops/internal/sys"
)

// Rule — правило для процессов, чьё имя совпадает с шаблоном.
type Rule struct {
	ID       string `json:"id"`
	Pattern  string `json:"pattern"`  // имя или шаблон: chrome*, *.exe
	Priority int    `json:"priority"` // -1 — не менять
	Cores    []int  `json:"cores,omitempty"`
	Enabled  bool   `json:"enabled"`
}

// NewID генерирует идентификатор правила.
func NewID() string { return strconv.FormatInt(time.Now().UnixNano(), 36) }

func normName(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	return strings.TrimSuffix(s, ".exe")
}

// Matches проверяет, подходит ли имя процесса под правило.
func (r Rule) Matches(name string) bool {
	pat, n := normName(r.Pattern), normName(name)
	if pat == "" {
		return false
	}
	if strings.ContainsAny(pat, "*?") {
		return globRe(pat).MatchString(n)
	}
	return pat == n
}

var (
	reMu    sync.Mutex
	reCache = map[string]*regexp.Regexp{}
)

// globRe превращает шаблон с * и ? в регулярное выражение (* совпадает и с «/»).
func globRe(pat string) *regexp.Regexp {
	reMu.Lock()
	defer reMu.Unlock()
	if re, ok := reCache[pat]; ok {
		return re
	}
	q := regexp.QuoteMeta(pat)
	q = strings.ReplaceAll(q, `\*`, ".*")
	q = strings.ReplaceAll(q, `\?`, ".")
	re := regexp.MustCompile("^" + q + "$")
	reCache[pat] = re
	return re
}

// Describe — человекочитаемое описание действий правила.
func (r Rule) Describe() string {
	var parts []string
	if r.Priority >= 0 {
		parts = append(parts, i18n.T("приоритет: ")+ctl.Priority(r.Priority).String())
	}
	if len(r.Cores) > 0 {
		parts = append(parts, i18n.T("ядра: ")+FormatCores(r.Cores))
	}
	if len(parts) == 0 {
		return i18n.T("без действий")
	}
	return strings.Join(parts, " · ")
}

// FormatCores сворачивает список ядер в диапазоны: 0-3,6.
func FormatCores(c []int) string {
	if len(c) == 0 {
		return i18n.T("все")
	}
	s := append([]int(nil), c...)
	sort.Ints(s)
	var out []string
	for i := 0; i < len(s); {
		j := i
		for j+1 < len(s) && s[j+1] == s[j]+1 {
			j++
		}
		if j > i {
			out = append(out, fmt.Sprintf("%d-%d", s[i], s[j]))
		} else {
			out = append(out, strconv.Itoa(s[i]))
		}
		i = j + 1
	}
	return strings.Join(out, ",")
}

// ParseCores разбирает строку вида «0-3, 6» в список ядер.
func ParseCores(s string, max int) ([]int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	set := map[int]bool{}
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		lo, hi := part, part
		if i := strings.Index(part, "-"); i > 0 {
			lo, hi = part[:i], part[i+1:]
		}
		a, err1 := strconv.Atoi(strings.TrimSpace(lo))
		b, err2 := strconv.Atoi(strings.TrimSpace(hi))
		if err1 != nil || err2 != nil || a < 0 || b < a {
			return nil, fmt.Errorf(i18n.T("неверный формат ядер: «%s»"), part)
		}
		if b >= max {
			return nil, fmt.Errorf(i18n.T("ядра %d нет: доступны 0-%d"), b, max-1)
		}
		for i := a; i <= b; i++ {
			set[i] = true
		}
	}
	out := make([]int, 0, len(set))
	for c := range set {
		out = append(out, c)
	}
	sort.Ints(out)
	return out, nil
}

// Engine применяет правила к процессам, не повторяя уже выполненное.
type Engine struct {
	mu    sync.Mutex
	rules []Rule
	done  map[string]bool
}

// NewEngine создаёт пустой движок правил.
func NewEngine() *Engine { return &Engine{done: map[string]bool{}} }

// SetRules заменяет набор правил; при следующем проходе они применятся заново.
func (e *Engine) SetRules(rs []Rule) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.rules = append([]Rule(nil), rs...)
	e.done = map[string]bool{}
}

// Active — число включённых правил.
func (e *Engine) Active() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	n := 0
	for _, r := range e.rules {
		if r.Enabled {
			n++
		}
	}
	return n
}

// Event — результат применения правила к процессу.
type Event struct {
	OK  bool
	Msg string
}

// Apply применяет включённые правила. force — применить повторно ко всем.
func (e *Engine) Apply(procs []sys.Proc, force bool) []Event {
	e.mu.Lock()
	defer e.mu.Unlock()
	if force {
		e.done = map[string]bool{}
	}
	var evs []Event
	next := map[string]bool{}
	for _, p := range procs {
		for _, r := range e.rules {
			if !r.Enabled || !r.Matches(p.Name) {
				continue
			}
			key := fmt.Sprintf("%d:%d:%s", p.PID, p.Create, r.ID)
			next[key] = true
			if e.done[key] {
				continue
			}
			var errs []string
			if r.Priority >= 0 && (!p.PrioOK || int(p.Priority) != r.Priority) {
				if err := ctl.SetPriority(p.PID, ctl.Priority(r.Priority)); err != nil {
					errs = append(errs, err.Error())
				}
			}
			if len(r.Cores) > 0 && ctl.AffinitySupported() {
				if err := ctl.SetAffinity(p.PID, r.Cores); err != nil {
					errs = append(errs, err.Error())
				}
			}
			label := fmt.Sprintf("%s (PID %d)", p.Name, p.PID)
			if len(errs) > 0 {
				evs = append(evs, Event{false, fmt.Sprintf(i18n.T("Правило «%s» → %s: %s"), r.Pattern, label, strings.Join(errs, "; "))})
			} else {
				evs = append(evs, Event{true, fmt.Sprintf(i18n.T("Правило «%s» → %s: %s"), r.Pattern, label, r.Describe())})
			}
		}
	}
	e.done = next
	return evs
}
