package history

import (
	"path/filepath"
	"testing"
)

func TestStorePersistAndUndo(t *testing.T) {
	p := filepath.Join(t.TempDir(), "h.json")
	s := Open(p)
	var got []Entry
	s.OnAdd = func(e Entry) { got = append(got, e) }
	a := s.Add(Entry{Area: "tweaks", Title: "A", Undo: &Undo{Kind: "tweak", Args: map[string]string{"id": "x", "on": "0"}}})
	b := s.Add(Entry{Area: "disk", Title: "B"})
	if a.ID != 1 || b.ID != 2 || len(got) != 2 {
		t.Fatalf("ids %d %d hooks %d", a.ID, b.ID, len(got))
	}
	s.MarkUndone(a.ID)

	r := Open(p) // перечитали с диска
	l := r.List()
	if len(l) != 2 || l[0].ID != 2 || l[1].UndoneAt == nil || l[1].Undo.Args["id"] != "x" {
		t.Fatalf("reload: %+v", l)
	}
	if c := r.Add(Entry{Title: "C"}); c.ID != 3 || c.Source != "ui" {
		t.Fatalf("next id after reload: %+v", c)
	}
}
