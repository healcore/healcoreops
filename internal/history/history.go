// Package history — журнал изменений системы, сделанных программой
// (твики, автозагрузка, службы, задачи, реестр, файлы…). Хранится на диске,
// переживает перезапуск; у обратимых изменений записан способ отката.
package history

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Undo — как откатить изменение. Kind определяет действие, Args — его параметры.
type Undo struct {
	Kind string            `json:"kind"`
	Args map[string]string `json:"args,omitempty"`
}

// Entry — одно изменение.
type Entry struct {
	ID       int64      `json:"id"`
	Time     time.Time  `json:"time"`
	Area     string     `json:"area"`   // tweaks | apps | autostart | services | tasks | registry | disk | programs | power | network | maintenance | profile | drivers
	Title    string     `json:"title"`  // что сделано
	Detail   string     `json:"detail"` // подробности
	Source   string     `json:"source"` // ui | cli | schedule | policy
	User     string     `json:"user"`
	Admin    bool       `json:"admin"`
	OK       bool       `json:"ok"`
	Undo     *Undo      `json:"undo,omitempty"` // nil — откат невозможен
	UndoNote string     `json:"undoNote,omitempty"`
	UndoneAt *time.Time `json:"undoneAt,omitempty"`
}

// Store — журнал изменений с сохранением в JSON-файл.
type Store struct {
	mu      sync.Mutex
	path    string
	entries []Entry
	next    int64
	// OnAdd вызывается после добавления записи (запись в журнал событий Windows и т. п.).
	OnAdd func(Entry)
}

const maxEntries = 2000

// Open загружает журнал из файла (или создаёт пустой).
func Open(path string) *Store {
	s := &Store{path: path}
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &s.entries)
	}
	for _, e := range s.entries {
		if e.ID > s.next {
			s.next = e.ID
		}
	}
	return s
}

// Add добавляет запись и сохраняет журнал. Возвращает запись с ID.
func (s *Store) Add(e Entry) Entry {
	s.mu.Lock()
	s.next++
	e.ID = s.next
	if e.Time.IsZero() {
		e.Time = time.Now()
	}
	if e.Source == "" {
		e.Source = "ui"
	}
	s.entries = append(s.entries, e)
	if len(s.entries) > maxEntries {
		s.entries = s.entries[len(s.entries)-maxEntries:]
	}
	s.saveLocked()
	cb := s.OnAdd
	s.mu.Unlock()
	if cb != nil {
		cb(e)
	}
	return e
}

// List возвращает записи, новые сначала.
func (s *Store) List() []Entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := append([]Entry(nil), s.entries...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	return out
}

// Get возвращает запись по ID.
func (s *Store) Get(id int64) (Entry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range s.entries {
		if e.ID == id {
			return e, true
		}
	}
	return Entry{}, false
}

// MarkUndone отмечает запись как откаченную.
func (s *Store) MarkUndone(id int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for i := range s.entries {
		if s.entries[i].ID == id {
			s.entries[i].UndoneAt = &now
		}
	}
	s.saveLocked()
}

// Clear очищает журнал.
func (s *Store) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = nil
	s.saveLocked()
}

func (s *Store) saveLocked() {
	if s.path == "" {
		return
	}
	_ = os.MkdirAll(filepath.Dir(s.path), 0o755)
	b, err := json.MarshalIndent(s.entries, "", " ")
	if err != nil {
		return
	}
	tmp := s.path + ".tmp"
	if os.WriteFile(tmp, b, 0o644) == nil {
		_ = os.Rename(tmp, s.path)
	}
}
