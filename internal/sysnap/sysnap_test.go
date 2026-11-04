package sysnap

import (
	"errors"
	"testing"
)

func TestCompare(t *testing.T) {
	a := Snapshot{ID: "20260101-100000", Data: map[string]map[string]string{
		AreaPrograms:  {"7-Zip": "24.08", "Old App": "1.0"},
		AreaServices:  {"svc1": "auto"},
		AreaAutostart: {"x": "y"},
	}}
	b := Snapshot{ID: "20260101-110000", Data: map[string]map[string]string{
		AreaPrograms:  {"7-Zip": "24.09", "New App": "2.0"},
		AreaServices:  {"svc1": "auto", "NewSvc": "auto"},
		AreaAutostart: {},
	}, Errors: map[string]string{AreaAutostart: "access denied"}}
	d := Compare(a, b)
	if d.Summary[AreaPrograms] != 3 || d.Summary[AreaServices] != 1 {
		t.Fatalf("%+v", d.Summary)
	}
	if d.Summary[AreaAutostart] != 0 {
		t.Fatal("area with error must be skipped")
	}
	kinds := map[string]string{}
	for _, c := range d.Changes {
		kinds[c.Key] = c.Kind
	}
	if kinds["7-Zip"] != "changed" || kinds["Old App"] != "removed" || kinds["New App"] != "added" || kinds["NewSvc"] != "added" {
		t.Fatalf("%v", kinds)
	}
}

func TestStore(t *testing.T) {
	st := Store{Dir: t.TempDir()}
	s := Capture("", "pc", map[string]Source{
		AreaPrograms: func() (map[string]string, error) { return map[string]string{"a": "1"}, nil },
		AreaDrivers:  func() (map[string]string, error) { return nil, errors.New("boom") },
		AreaPorts:    func() (map[string]string, error) { panic("oops") },
	})
	if s.Errors[AreaDrivers] == "" || s.Errors[AreaPorts] == "" {
		t.Fatalf("errors %+v", s.Errors)
	}
	if err := st.Save(&s); err != nil {
		t.Fatal(err)
	}
	s2 := s
	if err := st.Save(&s2); err != nil || s2.ID == s.ID {
		t.Fatalf("dup id: %v %s", err, s2.ID)
	}
	if l := st.List(); len(l) != 2 || l[0].Counts[AreaPrograms] != 1 {
		t.Fatalf("%+v", l)
	}
	if _, err := st.Get("../../etc/passwd"); err == nil {
		t.Fatal("path traversal")
	}
	if err := st.Rename(s.ID, "до установки"); err != nil {
		t.Fatal(err)
	}
	g, _ := st.Get(s.ID)
	if g.Name != "до установки" {
		t.Fatal(g.Name)
	}
}
