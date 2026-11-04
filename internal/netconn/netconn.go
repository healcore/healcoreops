// Package netconn — сетевые подключения процессов и открытые (слушающие) порты.
// Данные — из таблиц TCP/UDP системы (как netstat -ano), без прав администратора.
package netconn

import (
	"context"
	"net"
	"sort"
	"strings"
	"sync"
	"time"

	gnet "github.com/shirou/gopsutil/v4/net"
)

// Conn — одно подключение.
type Conn struct {
	Proto  string `json:"proto"` // tcp | udp
	Local  string `json:"local"`
	Remote string `json:"remote"`
	RHost  string `json:"rhost,omitempty"` // имя удалённого узла (обратный DNS)
	State  string `json:"state"`
	PID    int32  `json:"pid"`
}

// ProcConns — подключения одного процесса.
type ProcConns struct {
	PID         int32    `json:"pid"`
	Name        string   `json:"name"`
	Path        string   `json:"path,omitempty"`
	Established int      `json:"established"`
	Listening   int      `json:"listening"`
	Remotes     []string `json:"remotes"` // уникальные удалённые узлы (до 12)
	Conns       []Conn   `json:"conns"`
}

// Port — слушающий порт.
type Port struct {
	Proto    string `json:"proto"`
	Port     uint32 `json:"port"`
	Addr     string `json:"addr"`
	Exposure string `json:"exposure"` // local — только этот ПК; all — все сети; iface — один адаптер
	PID      int32  `json:"pid"`
	Name     string `json:"name"`
	Path     string `json:"path,omitempty"`
	Known    string `json:"known,omitempty"` // известная служба на этом порту
	Firewall string `json:"firewall,omitempty"`
}

// Names сопоставляет PID с именем и путём процесса.
type Names func(pid int32) (name, path string)

var known = map[uint32]string{
	20: "FTP", 21: "FTP", 22: "SSH", 23: "Telnet", 25: "SMTP", 53: "DNS", 67: "DHCP", 68: "DHCP",
	80: "HTTP", 110: "POP3", 123: "NTP", 135: "RPC", 137: "NetBIOS", 138: "NetBIOS", 139: "NetBIOS",
	143: "IMAP", 161: "SNMP", 389: "LDAP", 443: "HTTPS", 445: "SMB", 500: "IKE", 1433: "SQL Server",
	1900: "SSDP/UPnP", 3306: "MySQL", 3389: "RDP", 4500: "IPsec NAT-T", 5040: "CDP", 5353: "mDNS",
	5355: "LLMNR", 5357: "WSD", 5432: "PostgreSQL", 5900: "VNC", 5985: "WinRM", 5986: "WinRM HTTPS",
	6379: "Redis", 7680: "Delivery Optimization", 8080: "HTTP", 27017: "MongoDB",
}

// risky — порты, открытые наружу без нужды опасны (удалённый доступ и общие папки).
var risky = map[uint32]bool{21: true, 23: true, 135: true, 139: true, 445: true, 3389: true, 5900: true, 5985: true, 5986: true, 1433: true, 3306: true, 5432: true, 6379: true, 27017: true}

// Risky — стоит ли обратить внимание на открытый наружу порт.
func Risky(p Port) bool { return p.Exposure != "local" && risky[p.Port] }

func addr(a gnet.Addr) string {
	if a.IP == "" {
		return ""
	}
	return net.JoinHostPort(a.IP, itoa(a.Port))
}

func itoa(n uint32) string {
	if n == 0 {
		return "0"
	}
	var b [10]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// Exposure — кому доступен адрес, на котором слушает порт.
func Exposure(ip string) string {
	p := net.ParseIP(ip)
	switch {
	case p == nil:
		return "all"
	case p.IsLoopback():
		return "local"
	case p.IsUnspecified():
		return "all"
	}
	return "iface"
}

func protoName(t uint32) string {
	if t == 2 { // SOCK_DGRAM
		return "udp"
	}
	return "tcp"
}

func list() ([]gnet.ConnectionStat, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	return gnet.ConnectionsWithContext(ctx, "inet")
}

// ---------- обратный DNS с кэшем ----------

var (
	dnsMu    sync.Mutex
	dnsCache = map[string]string{}
	dnsBusy  = map[string]bool{}
)

func lookupAsync(ip string) string {
	p := net.ParseIP(ip)
	if p == nil || p.IsLoopback() || p.IsUnspecified() || p.IsPrivate() || p.IsLinkLocalUnicast() {
		return ""
	}
	dnsMu.Lock()
	defer dnsMu.Unlock()
	if v, ok := dnsCache[ip]; ok {
		return v
	}
	if !dnsBusy[ip] && len(dnsBusy) < 64 {
		dnsBusy[ip] = true
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			names, _ := net.DefaultResolver.LookupAddr(ctx, ip)
			cancel()
			n := ""
			if len(names) > 0 {
				n = strings.TrimSuffix(names[0], ".")
			}
			dnsMu.Lock()
			dnsCache[ip] = n
			delete(dnsBusy, ip)
			if len(dnsCache) > 4000 {
				dnsCache = map[string]string{}
			}
			dnsMu.Unlock()
		}()
	}
	return ""
}

// ByProcess — активные подключения, сгруппированные по процессам.
func ByProcess(names Names) ([]ProcConns, error) {
	cs, err := list()
	if err != nil {
		return nil, err
	}
	m := map[int32]*ProcConns{}
	for _, c := range cs {
		if c.Pid <= 0 {
			continue
		}
		p := m[c.Pid]
		if p == nil {
			n, path := names(c.Pid)
			p = &ProcConns{PID: c.Pid, Name: n, Path: path, Remotes: []string{}, Conns: []Conn{}}
			m[c.Pid] = p
		}
		cn := Conn{Proto: protoName(c.Type), Local: addr(c.Laddr), Remote: addr(c.Raddr), State: c.Status, PID: c.Pid}
		switch {
		case c.Status == "LISTEN" || (cn.Proto == "udp" && cn.Remote == ""):
			p.Listening++
			continue // слушающие порты — на отдельной вкладке
		case c.Status == "ESTABLISHED":
			p.Established++
		case c.Status == "TIME_WAIT" || c.Status == "CLOSE_WAIT" || c.Status == "CLOSED":
			continue
		}
		if c.Raddr.IP != "" {
			cn.RHost = lookupAsync(c.Raddr.IP)
			label := cn.RHost
			if label == "" {
				label = c.Raddr.IP
			}
			if len(p.Remotes) < 12 && !contains(p.Remotes, label) {
				p.Remotes = append(p.Remotes, label)
			}
		}
		p.Conns = append(p.Conns, cn)
	}
	out := make([]ProcConns, 0, len(m))
	for _, p := range m {
		if len(p.Conns) == 0 {
			continue
		}
		sort.Slice(p.Conns, func(i, j int) bool { return p.Conns[i].Remote < p.Conns[j].Remote })
		out = append(out, *p)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Established != out[j].Established {
			return out[i].Established > out[j].Established
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out, nil
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// Listening — открытые (слушающие) порты, по одному на порт и процесс.
func Listening(names Names) ([]Port, error) {
	cs, err := list()
	if err != nil {
		return nil, err
	}
	seen := map[string]int{}
	var out []Port
	for _, c := range cs {
		proto := protoName(c.Type)
		if !(c.Status == "LISTEN" || (proto == "udp" && c.Raddr.IP == "")) || c.Laddr.Port == 0 {
			continue
		}
		p := Port{Proto: proto, Port: c.Laddr.Port, Addr: c.Laddr.IP, Exposure: Exposure(c.Laddr.IP), PID: c.Pid, Known: known[c.Laddr.Port]}
		if c.Pid > 0 {
			p.Name, p.Path = names(c.Pid)
		}
		if c.Pid == 4 && p.Name == "" {
			p.Name = "System"
		}
		key := proto + ":" + itoa(p.Port) + ":" + itoa(uint32(c.Pid))
		if i, ok := seen[key]; ok {
			// IPv4 и IPv6 одного сокета — один порт; берём самую «открытую» доступность.
			if rank(p.Exposure) > rank(out[i].Exposure) {
				out[i].Exposure, out[i].Addr = p.Exposure, p.Addr
			}
			continue
		}
		seen[key] = len(out)
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool {
		if (out[i].Exposure == "local") != (out[j].Exposure == "local") {
			return out[j].Exposure == "local"
		}
		if out[i].Proto != out[j].Proto {
			return out[i].Proto == "tcp"
		}
		return out[i].Port < out[j].Port
	})
	return out, nil
}

func rank(e string) int {
	switch e {
	case "all":
		return 2
	case "iface":
		return 1
	}
	return 0
}

// ---------- Брандмауэр ----------

// FwRule — разрешающее входящее правило брандмауэра Windows.
type FwRule struct {
	Name  string `json:"n"`
	Proto string `json:"proto"` // TCP | UDP | Any | число
	Ports string `json:"port"`  // "Any", "80", "80,443", "5000-5010", "RPC" …
	App   string `json:"app"`   // путь программы или "Any"
}

// FwVerdict — что брандмауэр скажет о входящем подключении к порту.
type FwVerdict struct {
	Key   string `json:"key"`   // proto:port:pid
	State string `json:"state"` // allowed | norule | off
	Rule  string `json:"rule,omitempty"`
}

// PortKey — ключ порта для сопоставления с вердиктом брандмауэра.
func PortKey(p Port) string { return p.Proto + ":" + itoa(p.Port) + ":" + itoa(uint32(p.PID)) }

func portIn(spec string, port uint32) bool {
	for _, part := range strings.Split(spec, ",") {
		part = strings.TrimSpace(part)
		if strings.EqualFold(part, "any") {
			return true
		}
		if a, b, ok := strings.Cut(part, "-"); ok {
			lo, hi := atoi(a), atoi(b)
			if lo > 0 && hi >= lo && port >= uint32(lo) && port <= uint32(hi) {
				return true
			}
			continue
		}
		if n := atoi(part); n > 0 && uint32(n) == port {
			return true
		}
	}
	return false
}

func atoi(s string) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
	}
	return n
}

func protoOK(rule, proto string) bool {
	r := strings.ToLower(strings.TrimSpace(rule))
	switch r {
	case "any", "":
		return true
	case "6":
		r = "tcp"
	case "17":
		r = "udp"
	}
	return r == proto
}

// MatchFirewall определяет, разрешает ли какое-либо правило входящие подключения
// к порту. expand раскрывает переменные окружения в путях правил (%SystemRoot% и т. п.).
func MatchFirewall(p Port, rules []FwRule, enabled bool, expand func(string) string) FwVerdict {
	v := FwVerdict{Key: PortKey(p), State: "norule"}
	if !enabled {
		v.State = "off"
		return v
	}
	for _, r := range rules {
		if !protoOK(r.Proto, p.Proto) {
			continue
		}
		appAny := r.App == "" || strings.EqualFold(r.App, "any")
		portAny := r.Ports == "" || strings.EqualFold(r.Ports, "any")
		if appAny && portAny {
			continue // «разрешить всё» без условий встречается только у служебных правил с иными ограничениями
		}
		if !portAny && !portIn(r.Ports, p.Port) {
			continue
		}
		if !appAny {
			if p.Path == "" || !strings.EqualFold(expand(r.App), p.Path) {
				continue
			}
		}
		v.State, v.Rule = "allowed", r.Name
		return v
	}
	return v
}
