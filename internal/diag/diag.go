package diag

import (
	"context"
	"errors"
	"net"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Crash — синий экран (из дампа или события 1001).
type Crash struct {
	Time   string   `json:"time"`
	Bug    BugCheck `json:"bug"`
	File   string   `json:"file,omitempty"`
	Source string   `json:"source"` // dump | event
}

// AppCrash — программа, которая падала или зависала.
type AppCrash struct {
	App       string `json:"app"`
	Module    string `json:"module"`
	Exception string `json:"exception"`
	Kind      string `json:"kind"` // crash | hang
	Count     int    `json:"count"`
	Last      string `json:"last"`
	Path      string `json:"path"`
}

// EventGroup — повторяющаяся ошибка из журнала событий.
type EventGroup struct {
	Log      string `json:"log"`
	Provider string `json:"provider"`
	ID       int    `json:"id"`
	Level    int    `json:"level"` // 1 критическая, 2 ошибка
	Count    int    `json:"count"`
	Last     string `json:"last"`
	Message  string `json:"message"`
}

// Report — сводка сбоев.
type Report struct {
	Days       int          `json:"days"`
	BSOD       []Crash      `json:"bsod"`
	Unexpected []string     `json:"unexpected"` // внезапные выключения без синего экрана
	Apps       []AppCrash   `json:"apps"`
	Events     []EventGroup `json:"events"`
	Note       string       `json:"note,omitempty"`
}

// BootTime — длительность одной загрузки, мс.
type BootTime struct {
	Time     string `json:"time"`
	Total    int64  `json:"total"`
	MainPath int64  `json:"main"`
	PostBoot int64  `json:"post"`
}

// Degrader — что замедляло загрузку.
type Degrader struct {
	Kind        string `json:"kind"` // app | driver | service
	Name        string `json:"name"`
	Friendly    string `json:"friendly"`
	Count       int    `json:"count"`
	Degradation int64  `json:"degradation"` // худшая задержка, мс
	Last        string `json:"last"`
}

// Boot — сведения о загрузке Windows.
type Boot struct {
	LastBoot string     `json:"lastBoot"`
	Boots    []BootTime `json:"boots"`
	Slow     []Degrader `json:"slow"`
	Note     string     `json:"note,omitempty"`
}

// Reboot — ожидающая перезагрузка.
type Reboot struct {
	Pending bool     `json:"pending"`
	Reasons []string `json:"reasons"`
}

// SecItem — пункт проверки безопасности.
type SecItem struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	State  string `json:"state"`
	Level  int    `json:"level"` // 0 хорошо, 1 внимание, 2 плохо, -1 неизвестно
	Detail string `json:"detail,omitempty"`
	Page   string `json:"page,omitempty"`
}

// Adapter — сетевой адаптер.
type Adapter struct {
	Name    string   `json:"name"`
	Desc    string   `json:"desc"`
	Status  string   `json:"status"`
	Speed   string   `json:"speed"`
	MAC     string   `json:"mac"`
	Virtual bool     `json:"virtual"`
	Wifi    bool     `json:"wifi"`
	IPv4    []string `json:"ipv4"`
	IPv6    []string `json:"ipv6"`
	Gateway []string `json:"gateway"`
	DNS     []string `json:"dns"`
}

// ---------- Сетевые проверки (все ОС) ----------

var hostRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.\-:]{0,252}$`)

// ValidHost — допустимое имя хоста или IP-адрес.
func ValidHost(h string) bool { return hostRe.MatchString(h) && !strings.Contains(h, "..") }

// PingResult — итог TCP-проверки связи.
type PingResult struct {
	Host  string  `json:"host"`
	Port  int     `json:"port"`
	IP    string  `json:"ip"`
	DNSms int64   `json:"dnsMs"`
	Times []int64 `json:"times"` // мс, -1 — нет ответа
	Avg   int64   `json:"avg"`
	Lost  int     `json:"lost"`
	Error string  `json:"error,omitempty"`
}

// TCPPing измеряет время установки TCP-соединения (работает без прав администратора
// и там, где ICMP закрыт).
func TCPPing(host string, port, count int) PingResult {
	r := PingResult{Host: host, Port: port, Times: []int64{}}
	if !ValidHost(host) || port <= 0 || port > 65535 {
		r.Error = "bad host"
		return r
	}
	if count <= 0 || count > 10 {
		count = 4
	}
	t0 := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	cancel()
	r.DNSms = time.Since(t0).Milliseconds()
	if err != nil || len(ips) == 0 {
		r.Error = "dns"
		r.Lost = count
		return r
	}
	ip := ips[0].IP
	for _, a := range ips {
		if a.IP.To4() != nil {
			ip = a.IP
			break
		}
	}
	r.IP = ip.String()
	var sum, ok int64
	for i := 0; i < count; i++ {
		s := time.Now()
		c, err := net.DialTimeout("tcp", net.JoinHostPort(r.IP, itoa(port)), 3*time.Second)
		if err != nil {
			r.Times = append(r.Times, -1)
			r.Lost++
			continue
		}
		ms := time.Since(s).Milliseconds()
		c.Close()
		r.Times = append(r.Times, ms)
		sum += ms
		ok++
		time.Sleep(150 * time.Millisecond)
	}
	if ok > 0 {
		r.Avg = sum / ok
	}
	return r
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// NetCheck — быстрая проверка интернета.
type NetCheck struct {
	DNS     bool   `json:"dns"`
	DNSms   int64  `json:"dnsMs"`
	TCP     bool   `json:"tcp"`
	TCPms   int64  `json:"tcpMs"`
	Verdict string `json:"verdict"` // ok | dns | offline | slow
}

// CheckInternet проверяет DNS и доступность внешних узлов.
func CheckInternet() NetCheck {
	var c NetCheck
	t0 := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	_, err := net.DefaultResolver.LookupHost(ctx, "www.microsoft.com")
	cancel()
	c.DNSms = time.Since(t0).Milliseconds()
	c.DNS = err == nil
	best := int64(-1)
	for _, a := range []string{"1.1.1.1:443", "8.8.8.8:443", "9.9.9.9:443"} {
		s := time.Now()
		conn, err := net.DialTimeout("tcp", a, 3*time.Second)
		if err == nil {
			conn.Close()
			ms := time.Since(s).Milliseconds()
			if best < 0 || ms < best {
				best = ms
			}
		}
	}
	c.TCP, c.TCPms = best >= 0, best
	switch {
	case !c.TCP && !c.DNS:
		c.Verdict = "offline"
	case c.TCP && !c.DNS:
		c.Verdict = "dns"
	case c.TCPms > 250 || c.DNSms > 1500:
		c.Verdict = "slow"
	default:
		c.Verdict = "ok"
	}
	return c
}

// PortCheck проверяет, открыт ли TCP-порт.
func PortCheck(host string, port int) (bool, int64, error) {
	if !ValidHost(host) || port <= 0 || port > 65535 {
		return false, 0, errors.New("bad host or port")
	}
	s := time.Now()
	c, err := net.DialTimeout("tcp", net.JoinHostPort(host, itoa(port)), 4*time.Second)
	if err != nil {
		return false, 0, nil
	}
	c.Close()
	return true, time.Since(s).Milliseconds(), nil
}

// basicAdapters — список интерфейсов средствами Go (запасной вариант).
func basicAdapters() []Adapter {
	ifs, _ := net.Interfaces()
	var out []Adapter
	for _, i := range ifs {
		if i.Flags&net.FlagLoopback != 0 {
			continue
		}
		a := Adapter{Name: i.Name, MAC: i.HardwareAddr.String(), Status: "Down", IPv4: []string{}, IPv6: []string{}, Gateway: []string{}, DNS: []string{}}
		if i.Flags&net.FlagUp != 0 {
			a.Status = "Up"
		}
		addrs, _ := i.Addrs()
		for _, ad := range addrs {
			if ipn, ok := ad.(*net.IPNet); ok {
				if ipn.IP.To4() != nil {
					a.IPv4 = append(a.IPv4, ipn.IP.String())
				} else {
					a.IPv6 = append(a.IPv6, ipn.IP.String())
				}
			}
		}
		out = append(out, a)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Status == "Up" && out[j].Status != "Up" })
	return out
}

// groupSlow сводит события замедления загрузки по имени.
func groupSlow(items []Degrader) []Degrader {
	m := map[string]*Degrader{}
	var order []string
	for _, d := range items {
		k := d.Kind + "|" + strings.ToLower(d.Name)
		g, ok := m[k]
		if !ok {
			cp := d
			cp.Count = 0
			m[k] = &cp
			g = &cp
			order = append(order, k)
		}
		g.Count++
		if d.Degradation > g.Degradation {
			g.Degradation = d.Degradation
		}
		if d.Last > g.Last {
			g.Last = d.Last
		}
	}
	out := make([]Degrader, 0, len(order))
	for _, k := range order {
		out = append(out, *m[k])
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Degradation > out[j].Degradation })
	if len(out) > 25 {
		out = out[:25]
	}
	return out
}
