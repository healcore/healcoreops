package netconn

import (
	"strings"
	"testing"
)

func TestExposure(t *testing.T) {
	for ip, want := range map[string]string{"127.0.0.1": "local", "::1": "local", "0.0.0.0": "all", "::": "all", "192.168.1.5": "iface"} {
		if g := Exposure(ip); g != want {
			t.Errorf("%s: %s want %s", ip, g, want)
		}
	}
}

func TestMatchFirewall(t *testing.T) {
	exp := func(s string) string { return strings.ReplaceAll(s, "%SystemRoot%", `C:\Windows`) }
	rules := []FwRule{
		{Name: "Remote Desktop", Proto: "TCP", Ports: "3389", App: "Any"},
		{Name: "Game", Proto: "Any", Ports: "Any", App: `C:\Games\game.exe`},
		{Name: "svc", Proto: "UDP", Ports: "5000-5010", App: `%SystemRoot%\system32\svchost.exe`},
		{Name: "wide", Proto: "Any", Ports: "Any", App: "Any"},
	}
	cases := []struct {
		p    Port
		want string
	}{
		{Port{Proto: "tcp", Port: 3389, PID: 1}, "allowed"},
		{Port{Proto: "udp", Port: 3389, PID: 1}, "norule"},
		{Port{Proto: "tcp", Port: 7777, PID: 2, Path: `c:\games\GAME.exe`}, "allowed"},
		{Port{Proto: "udp", Port: 5005, PID: 3, Path: `C:\Windows\system32\svchost.exe`}, "allowed"},
		{Port{Proto: "udp", Port: 5011, PID: 3, Path: `C:\Windows\system32\svchost.exe`}, "norule"},
		{Port{Proto: "tcp", Port: 8080, PID: 9}, "norule"},
	}
	for _, c := range cases {
		if v := MatchFirewall(c.p, rules, true, exp); v.State != c.want {
			t.Errorf("%+v: %s want %s", c.p, v.State, c.want)
		}
	}
	if v := MatchFirewall(Port{Proto: "tcp", Port: 1}, rules, false, exp); v.State != "off" {
		t.Error("off")
	}
}

func TestRisky(t *testing.T) {
	if !Risky(Port{Port: 3389, Exposure: "all"}) || Risky(Port{Port: 3389, Exposure: "local"}) || Risky(Port{Port: 8080, Exposure: "all"}) {
		t.Fatal("risky")
	}
}
