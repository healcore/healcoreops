//go:build windows

package sysadmin

import (
	"errors"
	"os"
	"sort"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"github.com/healcore/healcoreops/internal/ctl"
	"github.com/healcore/healcoreops/internal/i18n"
)

// Supported — службы и задачи доступны в Windows.
func Supported() bool { return true }

func openSCM(access uint32) (windows.Handle, error) {
	return windows.OpenSCManager(nil, nil, access)
}

var stateNames = map[uint32]string{
	windows.SERVICE_STOPPED: "stopped", windows.SERVICE_START_PENDING: "starting", windows.SERVICE_STOP_PENDING: "stopping",
	windows.SERVICE_RUNNING: "running", windows.SERVICE_CONTINUE_PENDING: "starting", windows.SERVICE_PAUSE_PENDING: "stopping",
	windows.SERVICE_PAUSED: "paused",
}

func startName(t uint32, delayed bool) string {
	switch t {
	case windows.SERVICE_AUTO_START:
		if delayed {
			return "delayed"
		}
		return "auto"
	case windows.SERVICE_DEMAND_START:
		return "manual"
	case windows.SERVICE_DISABLED:
		return "disabled"
	case windows.SERVICE_BOOT_START:
		return "boot"
	case windows.SERVICE_SYSTEM_START:
		return "system"
	}
	return "manual"
}

type svcConfig struct {
	start   uint32
	path    string
	account string
	delayed bool
}

func queryConfig(h windows.Handle) (svcConfig, error) {
	var need uint32
	var buf []byte
	for i := 0; i < 3; i++ {
		var p *windows.QUERY_SERVICE_CONFIG
		if len(buf) > 0 {
			p = (*windows.QUERY_SERVICE_CONFIG)(unsafe.Pointer(&buf[0]))
		}
		err := windows.QueryServiceConfig(h, p, uint32(len(buf)), &need)
		if err == nil && p != nil {
			c := svcConfig{start: p.StartType, path: windows.UTF16PtrToString(p.BinaryPathName), account: windows.UTF16PtrToString(p.ServiceStartName)}
			// отложенный автозапуск
			var d windows.SERVICE_DELAYED_AUTO_START_INFO
			var n uint32
			if windows.QueryServiceConfig2(h, windows.SERVICE_CONFIG_DELAYED_AUTO_START_INFO, (*byte)(unsafe.Pointer(&d)), uint32(unsafe.Sizeof(d)), &n) == nil {
				c.delayed = d.IsDelayedAutoStartUp != 0
			}
			return c, nil
		}
		if err != nil && err != windows.ERROR_INSUFFICIENT_BUFFER {
			return svcConfig{}, err
		}
		buf = make([]byte, need)
	}
	return svcConfig{}, errors.New("config")
}

// Services возвращает список служб.
func Services() ([]Service, error) {
	scm, err := openSCM(windows.SC_MANAGER_CONNECT | windows.SC_MANAGER_ENUMERATE_SERVICE)
	if err != nil {
		return nil, err
	}
	defer windows.CloseServiceHandle(scm)
	var need, count uint32
	var buf []byte
	for {
		var p *byte
		if len(buf) > 0 {
			p = &buf[0]
		}
		err := windows.EnumServicesStatusEx(scm, windows.SC_ENUM_PROCESS_INFO, windows.SERVICE_WIN32, windows.SERVICE_STATE_ALL,
			p, uint32(len(buf)), &need, &count, nil, nil)
		if err == nil {
			break
		}
		if err != windows.ERROR_MORE_DATA || need <= uint32(len(buf)) {
			return nil, err
		}
		buf = make([]byte, need)
	}
	if count == 0 {
		return []Service{}, nil
	}
	raw := unsafe.Slice((*windows.ENUM_SERVICE_STATUS_PROCESS)(unsafe.Pointer(&buf[0])), int(count))
	sr := os.Getenv("SystemRoot")
	out := make([]Service, 0, count)
	for _, r := range raw {
		s := Service{
			Name:    windows.UTF16PtrToString(r.ServiceName),
			Display: windows.UTF16PtrToString(r.DisplayName),
			State:   stateNames[r.ServiceStatusProcess.CurrentState],
			PID:     r.ServiceStatusProcess.ProcessId,
		}
		if h, err := windows.OpenService(scm, r.ServiceName, windows.SERVICE_QUERY_CONFIG); err == nil {
			if c, err := queryConfig(h); err == nil {
				s.Start, s.Path, s.Account = startName(c.start, c.delayed), c.path, c.account
			}
			windows.CloseServiceHandle(h)
		}
		if k, err := registry.OpenKey(registry.LOCAL_MACHINE, `SYSTEM\CurrentControlSet\Services\`+s.Name, registry.QUERY_VALUE); err == nil {
			if d, err := k.GetMUIStringValue("Description"); err == nil {
				s.Desc = d
			} else if d, _, err := k.GetStringValue("Description"); err == nil && !strings.HasPrefix(d, "@") {
				s.Desc = d
			}
			k.Close()
		}
		s.Windows = IsWindowsBinary(s.Path, sr)
		s.Critical = IsCritical(s.Name)
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Display) < strings.ToLower(out[j].Display) })
	return out, nil
}

func needAdmin() error {
	if !ctl.IsElevated() {
		return errors.New(i18n.T("нужны права администратора — нажмите «Перезапустить от имени администратора»"))
	}
	return nil
}

func errCritical() error {
	return errors.New(i18n.Pick("это жизненно важная служба Windows — её нельзя отключать или останавливать", "this is a vital Windows service — it can't be disabled or stopped"))
}

func openSvc(name string, access uint32) (windows.Handle, windows.Handle, error) {
	scm, err := openSCM(windows.SC_MANAGER_CONNECT)
	if err != nil {
		return 0, 0, err
	}
	n, _ := windows.UTF16PtrFromString(name)
	h, err := windows.OpenService(scm, n, access)
	if err != nil {
		windows.CloseServiceHandle(scm)
		if err == windows.ERROR_ACCESS_DENIED {
			return 0, 0, needAdmin()
		}
		return 0, 0, err
	}
	return scm, h, nil
}

func waitState(h windows.Handle, want uint32, max time.Duration) error {
	deadline := time.Now().Add(max)
	for {
		var st windows.SERVICE_STATUS
		if err := windows.QueryServiceStatus(h, &st); err != nil {
			return err
		}
		if st.CurrentState == want {
			return nil
		}
		if time.Now().After(deadline) {
			return errors.New(i18n.T("превышено время ожидания"))
		}
		time.Sleep(300 * time.Millisecond)
	}
}

// ServiceAction запускает, останавливает или перезапускает службу.
func ServiceAction(name, action string) error {
	if err := needAdmin(); err != nil {
		return err
	}
	if (action == "stop" || action == "restart") && IsCritical(name) {
		return errCritical()
	}
	scm, h, err := openSvc(name, windows.SERVICE_START|windows.SERVICE_STOP|windows.SERVICE_QUERY_STATUS)
	if err != nil {
		return err
	}
	defer windows.CloseServiceHandle(scm)
	defer windows.CloseServiceHandle(h)
	stop := func() error {
		var st windows.SERVICE_STATUS
		if err := windows.ControlService(h, windows.SERVICE_CONTROL_STOP, &st); err != nil && err != windows.ERROR_SERVICE_NOT_ACTIVE {
			if err == windows.ERROR_DEPENDENT_SERVICES_RUNNING {
				return errors.New(i18n.Pick("от службы зависят другие работающие службы — сначала остановите их", "other running services depend on it — stop them first"))
			}
			return err
		}
		return waitState(h, windows.SERVICE_STOPPED, 30*time.Second)
	}
	start := func() error {
		if err := windows.StartService(h, 0, nil); err != nil && err != windows.ERROR_SERVICE_ALREADY_RUNNING {
			if err == windows.ERROR_SERVICE_DISABLED {
				return errors.New(i18n.Pick("служба отключена — сначала смените тип запуска", "the service is disabled — change its startup type first"))
			}
			return err
		}
		return waitState(h, windows.SERVICE_RUNNING, 30*time.Second)
	}
	switch action {
	case "start":
		return start()
	case "stop":
		return stop()
	case "restart":
		if err := stop(); err != nil {
			return err
		}
		return start()
	}
	return errors.New("bad action")
}

// ServiceStart возвращает текущий тип запуска службы (для отката).
func ServiceStart(name string) (string, error) {
	scm, h, err := openSvc(name, windows.SERVICE_QUERY_CONFIG)
	if err != nil {
		return "", err
	}
	defer windows.CloseServiceHandle(scm)
	defer windows.CloseServiceHandle(h)
	c, err := queryConfig(h)
	if err != nil {
		return "", err
	}
	return startName(c.start, c.delayed), nil
}

// SetServiceStart меняет тип запуска службы.
func SetServiceStart(name, start string) error {
	if !ValidStart(start) {
		return errors.New("bad start type")
	}
	if err := needAdmin(); err != nil {
		return err
	}
	if start == "disabled" && IsCritical(name) {
		return errCritical()
	}
	scm, h, err := openSvc(name, windows.SERVICE_CHANGE_CONFIG|windows.SERVICE_QUERY_CONFIG)
	if err != nil {
		return err
	}
	defer windows.CloseServiceHandle(scm)
	defer windows.CloseServiceHandle(h)
	t := map[string]uint32{"auto": windows.SERVICE_AUTO_START, "delayed": windows.SERVICE_AUTO_START,
		"manual": windows.SERVICE_DEMAND_START, "disabled": windows.SERVICE_DISABLED}[start]
	if err := windows.ChangeServiceConfig(h, windows.SERVICE_NO_CHANGE, t, windows.SERVICE_NO_CHANGE, nil, nil, nil, nil, nil, nil, nil); err != nil {
		return err
	}
	if t == windows.SERVICE_AUTO_START {
		d := windows.SERVICE_DELAYED_AUTO_START_INFO{}
		if start == "delayed" {
			d.IsDelayedAutoStartUp = 1
		}
		_ = windows.ChangeServiceConfig2(h, windows.SERVICE_CONFIG_DELAYED_AUTO_START_INFO, (*byte)(unsafe.Pointer(&d)))
	}
	return nil
}
