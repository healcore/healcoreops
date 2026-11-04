package policy

import "testing"

func TestPolicy(t *testing.T) {
	p := FromValues(Values{"DisableTweaks": uint64(1), "HideSections": "registry, tweaks", "EnforcedProfile": `C:\p.json`}, "test")
	if !p.Managed || !p.DisableTweaks || p.ReadOnly || len(p.HideSections) != 2 || p.Profile != `C:\p.json` {
		t.Fatalf("%+v", p)
	}
	if !p.Blocks(RuleFor("POST", "/api/tweaks/settings")) {
		t.Fatal("tweaks must be blocked")
	}
	if p.Blocks(RuleFor("POST", "/api/tweaks/restore-point")) {
		t.Fatal("restore point is always allowed")
	}
	if p.Blocks(RuleFor("GET", "/api/tweaks/settings")) || p.Blocks(RuleFor("POST", "/api/disk/clean")) {
		t.Fatal("reads and other areas are allowed")
	}
	ro := FromValues(Values{"ReadOnly": 1.0}, "f")
	for _, path := range []string{"/api/disk/clean", "/api/proc/kill", "/api/autostart", "/api/history/undo", "/api/services/start"} {
		if !ro.Blocks(RuleFor("POST", path)) {
			t.Errorf("read-only must block %s", path)
		}
	}
	if ro.Blocks(RuleFor("POST", "/api/net/ping")) || ro.Blocks(RuleFor("POST", "/api/report")) {
		t.Fatal("diagnostics stay allowed in read-only")
	}
	if FromValues(Values{}, "").Managed {
		t.Fatal("empty policy is not managed")
	}
}
