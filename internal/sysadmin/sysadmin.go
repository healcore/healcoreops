// Package sysadmin — службы Windows и задачи планировщика: просмотр,
// запуск/остановка, тип запуска, включение/отключение задач.
package sysadmin

import "strings"

// Service — служба Windows.
type Service struct {
	Name     string `json:"name"`
	Display  string `json:"display"`
	Desc     string `json:"desc"`
	State    string `json:"state"` // running | stopped | paused | starting | stopping
	Start    string `json:"start"` // auto | delayed | manual | disabled | boot | system
	PID      uint32 `json:"pid"`
	Path     string `json:"path"`
	Account  string `json:"account"`
	Windows  bool   `json:"windows"`  // служба Windows (а не сторонней программы)
	Critical bool   `json:"critical"` // нельзя отключать/останавливать
}

// Task — задача планировщика.
type Task struct {
	Path     string `json:"path"`
	Name     string `json:"name"`
	Folder   string `json:"folder"`
	Enabled  bool   `json:"enabled"`
	State    int    `json:"state"` // 1 отключена, 2 в очереди, 3 готова, 4 выполняется
	LastRun  string `json:"lastRun"`
	NextRun  string `json:"nextRun"`
	Result   int64  `json:"result"`
	Action   string `json:"action"`
	Author   string `json:"author"`
	Desc     string `json:"desc"`
	Triggers []int  `json:"triggers"`
	Windows  bool   `json:"windows"` // задача Microsoft
}

// criticalServices — без них Windows не загрузится или перестанет работать
// безопасно; их нельзя отключать и останавливать из программы.
var criticalServices = map[string]bool{
	"rpcss": true, "rpceptmapper": true, "dcomlaunch": true, "lsm": true, "brokerinfrastructure": true,
	"coremessagingregistrar": true, "gpsvc": true, "power": true, "plugplay": true, "profsvc": true,
	"samss": true, "schedule": true, "systemeventsbroker": true, "staterepository": true, "timebrokersvc": true,
	"usermanager": true, "winmgmt": true, "eventlog": true, "dhcp": true, "dnscache": true, "nsi": true,
	"bfe": true, "mpssvc": true, "cryptsvc": true, "appinfo": true, "trustedinstaller": true, "windefend": true,
	"securityhealthservice": true, "wscsvc": true, "lanmanworkstation": false, "wuauserv": true, "keyiso": true,
	"audiosrv": false, "netprofm": true, "nlasvc": true, "wdnissvc": true, "sense": true, "ssdpsrv": false,
	"sgrmbroker": true, "seclogon": false, "wlansvc": false, "devicesetupmanager": false, "eaphost": false,
	"msiserver": true, "sppsvc": true, "clipsvc": true, "camsvc": true, "bits": false, "cdpsvc": false,
	"lsass": true, "netlogon": false, "w32time": false, "fontcache": false, "themes": false,
	"coremessaging": true, "dispbrokerdesktopsvc": true, "lanmanserver": false, "dps": false,
}

// IsCritical — служба из списка жизненно важных.
func IsCritical(name string) bool { return criticalServices[strings.ToLower(name)] }

// IsWindowsBinary — файл службы лежит в каталоге Windows (служба системы).
func IsWindowsBinary(path, systemRoot string) bool {
	p := strings.ToLower(strings.TrimLeft(path, `"`))
	sr := strings.ToLower(systemRoot)
	if sr == "" {
		sr = `c:\windows`
	}
	return strings.HasPrefix(p, sr+`\`) || strings.HasPrefix(p, `%systemroot%\`) || strings.HasPrefix(p, `\systemroot\`) ||
		strings.HasPrefix(p, `system32\`) || strings.HasPrefix(p, `\??\`+sr+`\`)
}

// startNames — допустимые типы запуска.
var startNames = map[string]bool{"auto": true, "delayed": true, "manual": true, "disabled": true}

// ValidStart проверяет тип запуска.
func ValidStart(s string) bool { return startNames[s] }
