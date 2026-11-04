//go:build windows

package diag

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/healcore/healcoreops/internal/i18n"
	"github.com/healcore/healcoreops/internal/maintenance"
)

// ICMP через iphlpapi (IcmpSendEcho) — как ping и tracert, без прав
// администратора и без зависимости от консоли и кодировок.

var (
	iphlp      = windows.NewLazySystemDLL("iphlpapi.dll")
	pIcmpOpen  = iphlp.NewProc("IcmpCreateFile")
	pIcmpClose = iphlp.NewProc("IcmpCloseHandle")
	pIcmpSend  = iphlp.NewProc("IcmpSendEcho")
)

const (
	ipSuccess       = 0
	ipTTLExpired    = 11013
	ipReqTimedOut   = 11010
	ipDestHostUnrch = 11003
	ipDestNetUnrch  = 11002
	ipDestPortUnrch = 11005
)

type ipOptionInfo struct {
	TTL, TOS, Flags, OptSize uint8
	OptData                  uintptr
}

type icmpEchoReply struct {
	Address  uint32
	Status   uint32
	RTT      uint32
	DataSize uint16
	Reserved uint16
	Data     uintptr
	Options  ipOptionInfo
}

type echoResult struct {
	From   net.IP
	RTT    uint32
	Status uint32
}

// icmpEcho отправляет один эхо-запрос с заданным TTL.
func icmpEcho(dst net.IP, ttl uint8, timeout time.Duration) (echoResult, error) {
	ip4 := dst.To4()
	if ip4 == nil {
		return echoResult{}, errors.New("IPv4 only")
	}
	h, _, err := pIcmpOpen.Call()
	if h == uintptr(windows.InvalidHandle) || h == 0 {
		return echoResult{}, err
	}
	defer pIcmpClose.Call(h)
	data := []byte("HealCoreOps-trace-probe-32bytes!")
	opt := ipOptionInfo{TTL: ttl}
	buf := make([]byte, int(unsafe.Sizeof(icmpEchoReply{}))+len(data)+256)
	r, _, e := pIcmpSend.Call(h, uintptr(binary.LittleEndian.Uint32(ip4)),
		uintptr(unsafe.Pointer(&data[0])), uintptr(len(data)), uintptr(unsafe.Pointer(&opt)),
		uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), uintptr(timeout/time.Millisecond))
	rep := (*icmpEchoReply)(unsafe.Pointer(&buf[0]))
	if r == 0 && rep.Address == 0 {
		// нет ответа: тайм-аут или ошибка
		if en, ok := e.(windows.Errno); ok && uint32(en) != 0 {
			return echoResult{Status: uint32(en)}, nil
		}
		return echoResult{Status: ipReqTimedOut}, nil
	}
	from := make(net.IP, 4)
	binary.LittleEndian.PutUint32(from, rep.Address)
	return echoResult{From: from, RTT: rep.RTT, Status: rep.Status}, nil
}

func resolve4(host string) (net.IP, error) {
	if ip := net.ParseIP(host); ip != nil {
		if ip.To4() == nil {
			return nil, errors.New(i18n.Pick("поддерживаются только адреса IPv4", "only IPv4 addresses are supported"))
		}
		return ip, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ips, err := net.DefaultResolver.LookupIP(ctx, "ip4", host)
	if err != nil || len(ips) == 0 {
		return nil, errors.New(i18n.Pick("узел не найден (DNS)", "host not found (DNS)"))
	}
	return ips[0], nil
}

func fmtRTT(r echoResult) string {
	if r.From == nil || (r.Status != ipSuccess && r.Status != ipTTLExpired) {
		return "   *  "
	}
	if r.RTT < 1 {
		return " <1 ms"
	}
	return fmt.Sprintf("%3d ms", r.RTT)
}

func reverseName(ip net.IP) string {
	ctx, cancel := context.WithTimeout(context.Background(), 900*time.Millisecond)
	defer cancel()
	names, err := net.DefaultResolver.LookupAddr(ctx, ip.String())
	if err != nil || len(names) == 0 {
		return ""
	}
	return strings.TrimSuffix(names[0], ".")
}

// Traceroute — трассировка маршрута до узла (в фоне, журнал — как у обслуживания).
func Traceroute(host string) error {
	if !ValidHost(host) {
		return errors.New(i18n.Pick("неверное имя узла — укажите, например, google.com или 8.8.8.8", "invalid host — use e.g. google.com or 8.8.8.8"))
	}
	if pIcmpSend.Find() != nil {
		return maintenance.StartJob("tracert", "tracert "+host, func(emit func(string)) error { return tracertCmd(host, emit) })
	}
	return maintenance.StartJob("tracert", "tracert "+host, func(emit func(string)) error {
		emit(fmt.Sprintf(i18n.Pick("Определяю адрес %s…", "Resolving %s…"), host))
		dst, err := resolve4(host)
		if err != nil {
			return err
		}
		const maxHops = 30
		emit(fmt.Sprintf(i18n.Pick("Трассировка до %s [%s], не более %d переходов:", "Tracing route to %s [%s] over a maximum of %d hops:"), host, dst, maxHops))
		emit("")
		silent := 0
		for ttl := 1; ttl <= maxHops; ttl++ {
			var res [3]echoResult
			var hop net.IP
			reached := false
			for i := range res {
				r, err := icmpEcho(dst, uint8(ttl), 1200*time.Millisecond)
				if err != nil || (r.Status != 0 && r.Status < 11000) {
					// ICMP API недоступен или вернул системную ошибку — системный tracert
					emit(i18n.Pick("ICMP API недоступен, запускаю системный tracert…", "ICMP API unavailable, running system tracert…"))
					return tracertCmd(host, emit)
				}
				res[i] = r
				if r.From != nil && (r.Status == ipSuccess || r.Status == ipTTLExpired) {
					hop = r.From
					if r.Status == ipSuccess && r.From.Equal(dst) {
						reached = true
					}
				}
			}
			line := fmt.Sprintf("%3d  %s  %s  %s  ", ttl, fmtRTT(res[0]), fmtRTT(res[1]), fmtRTT(res[2]))
			if hop == nil {
				silent++
				emit(line + i18n.Pick("превышен интервал ожидания", "request timed out"))
				if silent >= 5 {
					emit("")
					emit(i18n.Pick("Следующие узлы не отвечают на ICMP — трассировка остановлена.", "Further hops don't answer ICMP — trace stopped."))
					return nil
				}
				continue
			}
			silent = 0
			name := reverseName(hop)
			if name != "" {
				line += fmt.Sprintf("%s [%s]", name, hop)
			} else {
				line += hop.String()
			}
			emit(line)
			if reached {
				emit("")
				emit(fmt.Sprintf(i18n.Pick("✓ Трассировка завершена: %d переходов.", "✓ Trace complete: %d hops."), ttl))
				return nil
			}
			for _, r := range res {
				if r.Status == ipDestHostUnrch || r.Status == ipDestNetUnrch || r.Status == ipDestPortUnrch {
					emit(i18n.Pick("Узел недоступен.", "Destination unreachable."))
					return nil
				}
			}
		}
		emit(i18n.Pick("Достигнут предел переходов.", "Maximum hops reached."))
		return nil
	})
}

// tracertCmd — запасной путь: системный tracert с выводом в UTF-8, построчно.
func tracertCmd(host string, emit func(string)) error {
	if !ValidHost(host) {
		return errors.New("bad host")
	}
	cmd := exec.Command("cmd.exe", "/d", "/c", "chcp 65001 >nul & tracert -d -w 1000 -h 30 "+host)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		return err
	}
	sc := bufio.NewScanner(out)
	for sc.Scan() {
		emit(strings.TrimRight(sc.Text(), "\r "))
	}
	return cmd.Wait()
}

// Ping — проверка связи: ICMP (как ping); если ICMP закрыт — по TCP.
func Ping(host string, port, count int) PingResult {
	if count <= 0 || count > 10 {
		count = 4
	}
	r := PingResult{Host: host, Port: port, Proto: "icmp", Times: []int64{}}
	if !ValidHost(host) {
		r.Error = "bad host"
		return r
	}
	t0 := time.Now()
	ip, err := resolve4(host)
	r.DNSms = time.Since(t0).Milliseconds()
	if err != nil {
		r.Error = "dns"
		r.Lost = count
		return r
	}
	r.IP = ip.String()
	var sum, ok int64
	var mu sync.Mutex
	for i := 0; i < count; i++ {
		e, err := icmpEcho(ip, 128, 2*time.Second)
		mu.Lock()
		if err != nil || e.Status != ipSuccess {
			r.Times = append(r.Times, -1)
			r.Lost++
		} else {
			r.Times = append(r.Times, int64(e.RTT))
			sum += int64(e.RTT)
			ok++
		}
		mu.Unlock()
		time.Sleep(200 * time.Millisecond)
	}
	if ok > 0 {
		r.Avg = sum / ok
		return r
	}
	// ICMP не отвечает (часто закрыт брандмауэром) — пробуем TCP
	tr := TCPPing(host, port, count)
	tr.Proto, tr.Note = "tcp", "icmp-blocked"
	return tr
}
