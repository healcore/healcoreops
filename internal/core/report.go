package core

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v4/host"

	"github.com/healcore/healcoreops/internal/autostart"
	"github.com/healcore/healcoreops/internal/ctl"
	"github.com/healcore/healcoreops/internal/diag"
	"github.com/healcore/healcoreops/internal/drivers"
	"github.com/healcore/healcoreops/internal/health"
	"github.com/healcore/healcoreops/internal/history"
	"github.com/healcore/healcoreops/internal/i18n"
	"github.com/healcore/healcoreops/internal/opt"
	"github.com/healcore/healcoreops/internal/policy"
	"github.com/healcore/healcoreops/internal/programs"
	"github.com/healcore/healcoreops/internal/sys"
	"github.com/healcore/healcoreops/internal/sysadmin"
)

// Report — сводка по компьютеру для пользователя, администратора или техподдержки.
type Report struct {
	App       string               `json:"app"`
	Version   string               `json:"version"`
	Generated string               `json:"generated"`
	Host      string               `json:"host"`
	OS        string               `json:"os"`
	User      string               `json:"user"`
	Admin     bool                 `json:"admin"`
	Uptime    string               `json:"uptime"`
	LastBoot  string               `json:"lastBoot"`
	Hardware  drivers.SysInfo      `json:"hardware"`
	CPU       float64              `json:"cpuPct"`
	MemPct    float64              `json:"memPct"`
	MemTotal  uint64               `json:"memTotal"`
	Disks     []diskDTO            `json:"disks"`
	Score     int                  `json:"score"`
	Recs      []opt.Recommendation `json:"recommendations"`
	Reboot    diag.Reboot          `json:"pendingReboot"`
	Security  []diag.SecItem       `json:"security"`
	Crashes   *diag.Report         `json:"crashes,omitempty"`
	Boot      *diag.Boot           `json:"boot,omitempty"`
	Health    health.Snapshot      `json:"hardwareHealth"`
	Drivers   []drivers.Device     `json:"drivers"`
	Programs  []programs.Program   `json:"programs"`
	Autostart []autostart.Item     `json:"autostart"`
	Services  []sysadmin.Service   `json:"thirdPartyServices"`
	Adapters  []diag.Adapter       `json:"network"`
	Policy    policy.Policy        `json:"policy"`
	Changes   []history.Entry      `json:"recentChanges"`
}

// BuildReport собирает отчёт (параллельно; 10–60 с в зависимости от системы).
func (c *Core) BuildReport() Report {
	s := c.Snapshot()
	if s == nil {
		s = c.col.Collect()
	}
	h := c.col.Host()
	r := Report{
		App: "HealCoreOps", Version: Version, Generated: time.Now().Format("2006-01-02 15:04"),
		Host: h.Hostname, OS: h.OS, User: currentUser(), Admin: ctl.IsElevated(),
		CPU: s.CPU, MemPct: s.MemPct, MemTotal: s.MemTotal, Policy: policy.Get(),
		Disks: []diskDTO{}, Drivers: []drivers.Device{}, Programs: []programs.Program{}, Autostart: []autostart.Item{},
		Services: []sysadmin.Service{}, Security: []diag.SecItem{}, Adapters: []diag.Adapter{},
	}
	r.Uptime = fmtUptime(s.Uptime)
	bt, _ := host.BootTime()
	boot := time.Unix(int64(bt), 0)
	r.LastBoot = boot.Format("2006-01-02 15:04")
	for _, d := range s.Disks {
		r.Disks = append(r.Disks, diskDTO{d.Mount, d.Total, d.Used, d.Pct})
	}
	r.Score, r.Recs = c.Analysis(s)
	if r.Recs == nil {
		r.Recs = []opt.Recommendation{}
	}
	ch := c.History().List()
	if len(ch) > 40 {
		ch = ch[:40]
	}
	r.Changes = ch

	var wg sync.WaitGroup
	run := func(f func()) { wg.Add(1); go func() { defer wg.Done(); f() }() }
	run(func() { r.Reboot = diag.PendingReboot() })
	run(func() { r.Health = freshHealth() })
	run(func() { r.Adapters = diag.Adapters() })
	if drivers.Supported() {
		run(func() {
			if d, err := drivers.List(); err == nil {
				r.Drivers = d
			}
			r.Hardware = drivers.GetSysInfo()
		})
	}
	if diag.Supported() {
		run(func() { r.Security = diag.Security() })
		run(func() {
			if cr, err := diag.Crashes(30); err == nil {
				r.Crashes = &cr
			}
		})
		run(func() {
			if b, err := diag.BootInfo(boot); err == nil {
				r.Boot = &b
			}
		})
	}
	if programs.Supported() {
		run(func() {
			if p, err := programs.List(); err == nil {
				r.Programs = p
			}
		})
	}
	if autostart.Supported() {
		run(func() {
			if a, err := autostart.List(); err == nil {
				r.Autostart = a
			}
		})
	}
	if sysadmin.Supported() {
		run(func() {
			if sv, err := sysadmin.Services(); err == nil {
				for _, x := range sv {
					if !x.Windows && (x.State == "running" || x.Start == "auto" || x.Start == "delayed") {
						r.Services = append(r.Services, x)
					}
				}
			}
		})
	}
	wg.Wait()
	if r.Hardware.CPU == "" {
		r.Hardware.CPU = h.CPUModel
		r.Hardware.RAM = sys.Bytes(h.MemTotal)
		r.Hardware.OS = h.OS
	}
	return r
}

func fmtUptime(sec uint64) string {
	d, hh, m := sec/86400, sec%86400/3600, sec%3600/60
	if d > 0 {
		return fmt.Sprintf(i18n.Pick("%d дн. %d ч", "%dd %dh"), d, hh)
	}
	return fmt.Sprintf(i18n.Pick("%d ч %d мин", "%dh %dm"), hh, m)
}

// ReportsDir — папка для отчётов: «Документы\HealCoreOps».
func ReportsDir() string {
	d := documentsDir()
	return filepath.Join(d, "HealCoreOps")
}

// SaveReport сохраняет отчёт в HTML (и JSON рядом) и возвращает путь к HTML.
// Если path пуст — в «Документы\HealCoreOps» с датой в имени.
func (c *Core) SaveReport(r Report, path string) (string, error) {
	if path == "" {
		name := fmt.Sprintf("HealCoreOps-%s-%s", safeName(r.Host), time.Now().Format("2006-01-02-1504"))
		path = filepath.Join(ReportsDir(), name+".html")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	if strings.EqualFold(filepath.Ext(path), ".json") {
		b, _ := json.MarshalIndent(r, "", "  ")
		return path, os.WriteFile(path, b, 0o644)
	}
	html, err := RenderReport(r)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, html, 0o644); err != nil {
		return "", err
	}
	b, _ := json.MarshalIndent(r, "", "  ")
	_ = os.WriteFile(strings.TrimSuffix(path, filepath.Ext(path))+".json", b, 0o644)
	return path, nil
}

func safeName(s string) string {
	return strings.Map(func(r rune) rune {
		if strings.ContainsRune(`\/:*?"<>| `, r) {
			return '_'
		}
		return r
	}, s)
}

func levelClass(l int) string {
	switch l {
	case 0:
		return "ok"
	case 1:
		return "warn"
	case 2:
		return "bad"
	}
	return "na"
}

// RenderReport формирует автономную HTML-страницу (печать, отправка по почте).
func RenderReport(r Report) ([]byte, error) {
	P := i18n.Pick
	L := map[string]string{
		"title": P("Отчёт о компьютере", "Computer report"), "generated": P("Сформирован", "Generated"),
		"summary": P("Сводка", "Summary"), "hw": P("Оборудование", "Hardware"), "board": P("Материнская плата", "Motherboard"),
		"cpu": P("Процессор", "Processor"), "gpu": P("Видеокарта", "Graphics"), "ram": P("Память", "Memory"), "os": P("Система", "OS"),
		"user": P("Пользователь", "User"), "admin": P("администратор", "administrator"), "uptime": P("Работает без перезагрузки", "Uptime"),
		"lastboot": P("Последняя загрузка", "Last boot"), "score": P("Индекс состояния", "Health index"), "recs": P("Рекомендации", "Recommendations"),
		"none": P("нет", "none"), "reboot": P("Ожидает перезагрузки", "Pending reboot"), "security": P("Безопасность", "Security"),
		"disks": P("Накопители", "Drives"), "smart": P("Состояние накопителей", "Drive health"), "crashes": P("Сбои за 30 дней", "Crashes in 30 days"),
		"bsod": P("Синие экраны", "Blue screens"), "unexp": P("Внезапные выключения", "Unexpected shutdowns"), "apps": P("Падения программ", "App crashes"),
		"events": P("Частые ошибки в журналах (7 дней)", "Frequent log errors (7 days)"), "boot": P("Загрузка Windows", "Windows boot"),
		"slow": P("Замедляют загрузку", "Slowing down boot"), "drivers": P("Драйверы", "Drivers"), "programs": P("Установленные программы", "Installed programs"),
		"autostart": P("Автозагрузка", "Startup"), "services": P("Сторонние службы", "Third-party services"), "net": P("Сеть", "Network"),
		"changes": P("Последние изменения через HealCoreOps", "Recent changes via HealCoreOps"), "policy": P("Политики администратора", "Admin policies"),
		"name": P("Название", "Name"), "ver": P("Версия", "Version"), "pub": P("Издатель", "Publisher"), "date": P("Дата", "Date"),
		"size": P("Размер", "Size"), "state": P("Состояние", "State"), "start": P("Запуск", "Startup"), "count": P("Раз", "Times"),
		"on": P("вкл", "on"), "off": P("выкл", "off"), "yes": P("да", "yes"), "no": P("нет", "no"), "code": P("Код", "Code"),
		"hint": P("Что делать", "What to do"), "module": P("Модуль", "Module"), "last": P("Последний раз", "Last"),
		"total": P("Всего", "Total"), "avg": P("Обычно", "Typical"), "sec": P("с", "s"), "loc": P("Расположение", "Location"),
		"cmd": P("Команда", "Command"), "ip": "IP", "gw": P("Шлюз", "Gateway"), "dns": "DNS", "mac": "MAC", "speed": P("Скорость", "Speed"),
		"what": P("Что", "What"), "who": P("Кто", "Who"), "undone": P("отменено", "undone"), "footer": P("Создано программой HealCoreOps", "Generated by HealCoreOps"),
		"cpuload": P("Загрузка ЦП", "CPU load"), "memload": P("Занято ОЗУ", "RAM used"), "wear": P("износ", "wear"), "temp": P("темп.", "temp"),
	}
	fm := template.FuncMap{
		"bytes": func(v any) string {
			switch x := v.(type) {
			case uint64:
				return sys.Bytes(x)
			case int64:
				return sys.Bytes(uint64(max(x, 0)))
			}
			return ""
		},
		"pct":   func(v float64) string { return fmt.Sprintf("%.0f%%", v) },
		"lvl":   levelClass,
		"secs":  func(ms int64) string { return fmt.Sprintf("%.1f", float64(ms)/1000) },
		"join":  strings.Join,
		"short": func(s string) string { return strings.Replace(s, "T", " ", 1) },
		"bar": func(p float64) template.CSS {
			return template.CSS(fmt.Sprintf("width:%.0f%%", p))
		},
		"scoreClass": func(s int) string {
			switch {
			case s >= 85:
				return "ok"
			case s >= 65:
				return "warn"
			}
			return "bad"
		},
		"reclvl": func(l opt.Level) string { return [...]string{"na", "warn", "bad"}[l] },
	}
	t, err := template.New("r").Funcs(fm).Parse(reportTpl)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(r.Programs, func(i, j int) bool { return strings.ToLower(r.Programs[i].Name) < strings.ToLower(r.Programs[j].Name) })
	var buf bytes.Buffer
	err = t.Execute(&buf, map[string]any{"R": r, "L": L, "Lang": i18n.Get(), "GOOS": runtime.GOOS})
	return buf.Bytes(), err
}

const reportTpl = `<!doctype html>
<html lang="{{.Lang}}"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>{{.L.title}} — {{.R.Host}}</title>
<style>
:root{--ink:#16161a;--mut:#6b6b74;--line:#e3e3e8;--bg:#fff;--soft:#f5f5f7;--ok:#2f7d4f;--warn:#a86a00;--bad:#b3261e}
*{box-sizing:border-box}body{margin:0;background:var(--soft);color:var(--ink);font:14px/1.5 "Segoe UI",system-ui,sans-serif}
.wrap{max-width:1100px;margin:0 auto;padding:28px 20px 60px}
header{display:flex;justify-content:space-between;align-items:flex-end;gap:16px;flex-wrap:wrap;border-bottom:2px solid var(--ink);padding-bottom:14px;margin-bottom:22px}
h1{margin:0;font-size:26px;letter-spacing:.02em}h1 small{display:block;font-size:13px;color:var(--mut);font-weight:400;letter-spacing:0}
h2{font-size:15px;text-transform:uppercase;letter-spacing:.08em;margin:0 0 12px}
section{background:var(--bg);border:1px solid var(--line);border-radius:10px;padding:18px 20px;margin-bottom:16px;break-inside:avoid}
.grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(250px,1fr));gap:16px}
.kv{display:grid;grid-template-columns:auto 1fr;gap:4px 16px}.kv span:nth-child(odd){color:var(--mut)}
table{width:100%;border-collapse:collapse;font-size:13px}th,td{text-align:left;padding:6px 8px;border-bottom:1px solid var(--line);vertical-align:top}
th{color:var(--mut);font-weight:600;font-size:12px;text-transform:uppercase;letter-spacing:.04em}td.n{text-align:right;white-space:nowrap}
.score{font-size:46px;font-weight:700;line-height:1}.ok{color:var(--ok)}.warn{color:var(--warn)}.bad{color:var(--bad)}.na{color:var(--mut)}
.dot{display:inline-block;width:9px;height:9px;border-radius:50%;background:currentColor;margin-right:7px}
.bar{height:6px;background:var(--line);border-radius:3px;overflow:hidden;margin-top:4px}.bar i{display:block;height:100%;background:var(--ink)}
.mut{color:var(--mut)}.mono{font-family:Consolas,monospace;font-size:12px;word-break:break-all}
.pill{display:inline-block;border:1px solid var(--line);border-radius:20px;padding:1px 9px;font-size:12px;margin:2px 4px 2px 0}
footer{color:var(--mut);font-size:12px;text-align:center;margin-top:24px}
@media print{body{background:#fff}section{border-color:#ccc}.wrap{padding:0}}
</style></head><body><div class="wrap">
<header><h1>{{.L.title}} · {{.R.Host}}<small>{{.R.OS}}</small></h1><div class="mut">{{.L.generated}}: {{.R.Generated}} · HealCoreOps {{.R.Version}}</div></header>

<div class="grid">
<section><h2>{{.L.summary}}</h2>
<div style="display:flex;gap:18px;align-items:center;margin-bottom:12px"><div class="score {{scoreClass .R.Score}}">{{.R.Score}}</div><div class="mut">{{.L.score}}<br>{{.L.cpuload}} {{pct .R.CPU}} · {{.L.memload}} {{pct .R.MemPct}}</div></div>
<div class="kv"><span>{{.L.user}}</span><span>{{.R.User}}{{if .R.Admin}} ({{.L.admin}}){{end}}</span>
<span>{{.L.uptime}}</span><span>{{.R.Uptime}}</span><span>{{.L.lastboot}}</span><span>{{.R.LastBoot}}</span>
<span>{{.L.reboot}}</span><span>{{if .R.Reboot.Pending}}<b class="warn">{{.L.yes}}</b> — {{join .R.Reboot.Reasons "; "}}{{else}}{{.L.no}}{{end}}</span></div></section>
<section><h2>{{.L.hw}}</h2><div class="kv">
{{with .R.Hardware}}{{if .Board}}<span>{{$.L.board}}</span><span>{{.Board}}</span>{{end}}
<span>{{$.L.cpu}}</span><span>{{.CPU}}</span>{{if .GPU}}<span>{{$.L.gpu}}</span><span>{{.GPU}}</span>{{end}}
<span>{{$.L.ram}}</span><span>{{.RAM}}</span><span>{{$.L.os}}</span><span>{{.OS}}</span>{{end}}
</div></section>
</div>

{{if .R.Recs}}<section><h2>{{.L.recs}}</h2><table>{{range .R.Recs}}<tr><td class="{{reclvl .Level}}" style="width:24px"><span class="dot"></span></td><td><b>{{.Title}}</b><br><span class="mut">{{.Detail}}</span></td></tr>{{end}}</table></section>{{end}}

{{if .R.Security}}<section><h2>{{.L.security}}</h2><table>{{range .R.Security}}<tr><td style="width:34%"><span class="{{lvl .Level}}"><span class="dot"></span></span>{{.Title}}</td><td>{{.State}}{{if .Detail}}<br><span class="mut">{{.Detail}}</span>{{end}}</td></tr>{{end}}</table></section>{{end}}

<div class="grid">
<section><h2>{{.L.disks}}</h2>{{range .R.Disks}}<div style="margin-bottom:10px"><b>{{.Mount}}</b> <span class="mut">{{bytes .Used}} / {{bytes .Total}} · {{pct .Pct}}</span><div class="bar"><i style="{{bar .Pct}}"></i></div></div>{{end}}</section>
{{if .R.Health.Disks}}<section><h2>{{.L.smart}}</h2><table>{{range .R.Health.Disks}}<tr><td>{{.Name}}<br><span class="mut">{{.Media}}</span></td><td class="n">{{.Health}}{{if .TempC}} · {{.TempC}}°C{{end}}{{if .WearPct}} · {{$.L.wear}} {{.WearPct}}%{{end}}</td></tr>{{end}}</table></section>{{end}}
</div>

{{with .R.Crashes}}<section><h2>{{$.L.crashes}}</h2>
{{if .BSOD}}<h3 style="font-size:13px;margin:6px 0">{{$.L.bsod}}: {{len .BSOD}}</h3><table><tr><th>{{$.L.date}}</th><th>{{$.L.code}}</th><th>{{$.L.hint}}</th></tr>{{range .BSOD}}<tr><td class="n">{{short .Time}}</td><td class="mono">{{.Bug.Hex}}<br>{{.Bug.Name}}</td><td>{{.Bug.Hint}}</td></tr>{{end}}</table>{{else}}<p>{{$.L.bsod}}: {{$.L.none}}</p>{{end}}
{{if .Unexpected}}<p>{{$.L.unexp}}: <b>{{len .Unexpected}}</b></p>{{end}}
{{if .Apps}}<h3 style="font-size:13px;margin:14px 0 6px">{{$.L.apps}}</h3><table><tr><th>{{$.L.name}}</th><th>{{$.L.module}}</th><th class="n">{{$.L.count}}</th><th class="n">{{$.L.last}}</th></tr>{{range .Apps}}<tr><td>{{.App}}</td><td class="mono">{{.Module}} {{.Exception}}</td><td class="n">{{.Count}}</td><td class="n">{{short .Last}}</td></tr>{{end}}</table>{{end}}
{{if .Events}}<h3 style="font-size:13px;margin:14px 0 6px">{{$.L.events}}</h3><table>{{range .Events}}<tr><td class="n">{{.Count}}×</td><td><b>{{.Provider}}</b> <span class="mut">#{{.ID}} · {{.Log}}</span><br><span class="mut">{{.Message}}</span></td></tr>{{end}}</table>{{end}}
</section>{{end}}

{{with .R.Boot}}{{if .Boots}}<section><h2>{{$.L.boot}}</h2><table><tr><th>{{$.L.date}}</th><th class="n">{{$.L.total}}, {{$.L.sec}}</th></tr>{{range .Boots}}<tr><td>{{short .Time}}</td><td class="n">{{secs .Total}}</td></tr>{{end}}</table>
{{if .Slow}}<h3 style="font-size:13px;margin:14px 0 6px">{{$.L.slow}}</h3><table>{{range .Slow}}<tr><td>{{if .Friendly}}{{.Friendly}}{{else}}{{.Name}}{{end}} <span class="mut">({{.Kind}})</span></td><td class="n">+{{secs .Degradation}} {{$.L.sec}}</td><td class="n">{{.Count}}×</td></tr>{{end}}</table>{{end}}</section>{{end}}{{end}}

{{if .R.Drivers}}<section><h2>{{.L.drivers}}</h2><table><tr><th>{{.L.name}}</th><th>{{.L.pub}}</th><th>{{.L.ver}}</th><th>{{.L.date}}</th></tr>{{range .R.Drivers}}<tr><td>{{.Name}}{{if .NeedsDriver}} <b class="bad">!</b>{{end}}<br><span class="mut">{{.Category}}</span></td><td>{{.Manu}}</td><td class="mono">{{.Version}}</td><td>{{.Date}}</td></tr>{{end}}</table></section>{{end}}

{{if .R.Adapters}}<section><h2>{{.L.net}}</h2><table><tr><th>{{.L.name}}</th><th>{{.L.ip}}</th><th>{{.L.gw}} / {{.L.dns}}</th><th>{{.L.mac}}</th></tr>{{range .R.Adapters}}{{if eq .Status "Up"}}<tr><td>{{.Name}}<br><span class="mut">{{.Desc}} {{.Speed}}</span></td><td class="mono">{{join .IPv4 ", "}}</td><td class="mono">{{join .Gateway ", "}}<br>{{join .DNS ", "}}</td><td class="mono">{{.MAC}}</td></tr>{{end}}{{end}}</table></section>{{end}}

{{if .R.Autostart}}<section><h2>{{.L.autostart}}</h2><table>{{range .R.Autostart}}<tr><td style="width:30%">{{.Name}}</td><td class="mono">{{.Command}}</td><td class="n">{{if .Enabled}}{{$.L.on}}{{else}}<span class="mut">{{$.L.off}}</span>{{end}}</td></tr>{{end}}</table></section>{{end}}

{{if .R.Services}}<section><h2>{{.L.services}}</h2><table>{{range .R.Services}}<tr><td>{{.Display}}<br><span class="mut mono">{{.Name}}</span></td><td>{{.State}}</td><td>{{.Start}}</td></tr>{{end}}</table></section>{{end}}

{{if .R.Programs}}<section><h2>{{.L.programs}} ({{len .R.Programs}})</h2><table><tr><th>{{.L.name}}</th><th>{{.L.ver}}</th><th>{{.L.pub}}</th><th class="n">{{.L.date}}</th></tr>{{range .R.Programs}}<tr><td>{{.Name}}</td><td class="mono">{{.Version}}</td><td>{{.Publisher}}</td><td class="n">{{.InstallDate}}</td></tr>{{end}}</table></section>{{end}}

{{if .R.Changes}}<section><h2>{{.L.changes}}</h2><table><tr><th>{{.L.date}}</th><th>{{.L.what}}</th><th>{{.L.who}}</th></tr>{{range .R.Changes}}<tr><td class="n">{{.Time.Format "2006-01-02 15:04"}}</td><td>{{.Title}}{{if .UndoneAt}} <span class="mut">({{$.L.undone}})</span>{{end}}<br><span class="mut">{{.Detail}}</span></td><td>{{.User}} · {{.Source}}</td></tr>{{end}}</table></section>{{end}}

{{if .R.Policy.Managed}}<section><h2>{{.L.policy}}</h2><p class="mono">{{.R.Policy.Source}}</p></section>{{end}}
<footer>{{.L.footer}} · github.com/healcore</footer>
</div></body></html>`
