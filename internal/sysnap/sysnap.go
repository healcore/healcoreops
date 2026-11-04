// Package sysnap — снимки состояния системы и сравнение «до и после»:
// автозагрузка, службы, задачи планировщика, драйверы, программы и открытые порты.
// Помогает увидеть, что именно поменяла установка программы или обновление.
package sysnap

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Области снимка.
const (
	AreaAutostart = "autostart"
	AreaServices  = "services"
	AreaTasks     = "tasks"
	AreaDrivers   = "drivers"
	AreaPrograms  = "programs"
	AreaPorts     = "ports"
)

// Areas — порядок областей в интерфейсе.
var Areas = []string{AreaPrograms, AreaAutostart, AreaServices, AreaTasks, AreaDrivers, AreaPorts}

// Snapshot — снимок: область → ключ (понятное имя) → значение (что сравниваем).
type Snapshot struct {
	ID     string                       `json:"id"`
	Name   string                       `json:"name"`
	Time   int64                        `json:"time"` // unix-мс
	Host   string                       `json:"host"`
	Data   map[string]map[string]string `json:"data"`
	Errors map[string]string            `json:"errors,omitempty"` // область → почему не собрана
}

// Meta — краткие сведения для списка.
type Meta struct {
	ID     string         `json:"id"`
	Name   string         `json:"name"`
	Time   int64          `json:"time"`
	Counts map[string]int `json:"counts"`
}

// Change — одно отличие.
type Change struct {
	Area   string `json:"area"`
	Kind   string `json:"kind"` // added | removed | changed
	Key    string `json:"key"`
	Before string `json:"before,omitempty"`
	After  string `json:"after,omitempty"`
}

// Diff — результат сравнения.
type Diff struct {
	A       Meta           `json:"a"`
	B       Meta           `json:"b"`
	Changes []Change       `json:"changes"`
	Summary map[string]int `json:"summary"` // область → число изменений
}

// Source — сборщик одной области.
type Source func() (map[string]string, error)

// Capture собирает снимок из источников (параллельно).
func Capture(name, host string, src map[string]Source) Snapshot {
	now := time.Now()
	s := Snapshot{ID: now.Format("20060102-150405"), Name: strings.TrimSpace(name), Time: now.UnixMilli(), Host: host,
		Data: map[string]map[string]string{}, Errors: map[string]string{}}
	if s.Name == "" {
		s.Name = now.Format("02.01.2006 15:04")
	}
	type res struct {
		area string
		m    map[string]string
		err  error
	}
	ch := make(chan res, len(src))
	for a, f := range src {
		go func(a string, f Source) {
			defer func() {
				if r := recover(); r != nil {
					ch <- res{a, nil, fmt.Errorf("%v", r)}
				}
			}()
			m, err := f()
			ch <- res{a, m, err}
		}(a, f)
	}
	for range src {
		r := <-ch
		if r.err != nil {
			s.Errors[r.area] = r.err.Error()
		}
		if r.m == nil {
			r.m = map[string]string{}
		}
		s.Data[r.area] = r.m
	}
	if len(s.Errors) == 0 {
		s.Errors = nil
	}
	return s
}

// MetaOf — краткие сведения о снимке.
func MetaOf(s Snapshot) Meta {
	m := Meta{ID: s.ID, Name: s.Name, Time: s.Time, Counts: map[string]int{}}
	for a, d := range s.Data {
		m.Counts[a] = len(d)
	}
	return m
}

// Compare сравнивает два снимка (a — раньше, b — позже). Область, которую
// не удалось собрать в одном из снимков, не сравнивается — иначе всё в ней
// выглядело бы «удалённым».
func Compare(a, b Snapshot) Diff {
	d := Diff{A: MetaOf(a), B: MetaOf(b), Changes: []Change{}, Summary: map[string]int{}}
	for _, area := range Areas {
		if a.Errors[area] != "" || b.Errors[area] != "" {
			continue
		}
		da, db := a.Data[area], b.Data[area]
		if da == nil || db == nil {
			continue
		}
		var keys []string
		for k := range da {
			keys = append(keys, k)
		}
		for k := range db {
			if _, ok := da[k]; !ok {
				keys = append(keys, k)
			}
		}
		sort.Slice(keys, func(i, j int) bool { return strings.ToLower(keys[i]) < strings.ToLower(keys[j]) })
		for _, k := range keys {
			va, inA := da[k]
			vb, inB := db[k]
			switch {
			case inA && !inB:
				d.Changes = append(d.Changes, Change{Area: area, Kind: "removed", Key: k, Before: va})
			case !inA && inB:
				d.Changes = append(d.Changes, Change{Area: area, Kind: "added", Key: k, After: vb})
			case va != vb:
				d.Changes = append(d.Changes, Change{Area: area, Kind: "changed", Key: k, Before: va, After: vb})
			default:
				continue
			}
			d.Summary[area]++
		}
	}
	return d
}

// ---------- Хранилище ----------

// Store — снимки в папке (по файлу на снимок).
type Store struct{ Dir string }

// MaxKeep — сколько снимков хранить (старые удаляются).
const MaxKeep = 40

var idRe = regexp.MustCompile(`^[0-9]{8}-[0-9]{6}(-[0-9]+)?$`)

func (st Store) path(id string) (string, error) {
	if !idRe.MatchString(id) {
		return "", errors.New("bad snapshot id")
	}
	return filepath.Join(st.Dir, id+".json"), nil
}

// Save сохраняет снимок (при совпадении ID добавляет суффикс).
func (st Store) Save(s *Snapshot) error {
	if err := os.MkdirAll(st.Dir, 0o755); err != nil {
		return err
	}
	base := s.ID
	for i := 2; ; i++ {
		p, err := st.path(s.ID)
		if err != nil {
			return err
		}
		if _, err := os.Stat(p); errors.Is(err, os.ErrNotExist) {
			b, _ := json.Marshal(s)
			if err := os.WriteFile(p, b, 0o644); err != nil {
				return err
			}
			break
		}
		s.ID = fmt.Sprintf("%s-%d", base, i)
	}
	st.trim()
	return nil
}

// Get загружает снимок.
func (st Store) Get(id string) (Snapshot, error) {
	var s Snapshot
	p, err := st.path(id)
	if err != nil {
		return s, err
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return s, err
	}
	err = json.Unmarshal(b, &s)
	return s, err
}

// Delete удаляет снимок.
func (st Store) Delete(id string) error {
	p, err := st.path(id)
	if err != nil {
		return err
	}
	return os.Remove(p)
}

// Rename переименовывает снимок.
func (st Store) Rename(id, name string) error {
	s, err := st.Get(id)
	if err != nil {
		return err
	}
	s.Name = strings.TrimSpace(name)
	p, _ := st.path(id)
	b, _ := json.Marshal(s)
	return os.WriteFile(p, b, 0o644)
}

// List — снимки, новые сначала.
func (st Store) List() []Meta {
	ents, _ := os.ReadDir(st.Dir)
	out := []Meta{}
	for _, e := range ents {
		id := strings.TrimSuffix(e.Name(), ".json")
		if e.IsDir() || !idRe.MatchString(id) {
			continue
		}
		if s, err := st.Get(id); err == nil {
			out = append(out, MetaOf(s))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Time > out[j].Time })
	return out
}

func (st Store) trim() {
	l := st.List()
	for i := MaxKeep; i < len(l); i++ {
		_ = st.Delete(l[i].ID)
	}
}
