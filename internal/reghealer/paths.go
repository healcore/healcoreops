package reghealer

import (
	"strings"
)

// Разбор командных строк из реестра. Чистые функции без обращения к системе —
// проверяются тестами на любой ОС. Принцип: при любом сомнении запись НЕ
// считается висячей (лучше пропустить мусор, чем удалить рабочую запись).

// systemLoaders — команды-посредники: по ним нельзя судить, что запись «висячая».
var systemLoaders = map[string]bool{
	"msiexec.exe": true, "rundll32.exe": true, "regsvr32.exe": true, "cmd.exe": true,
	"powershell.exe": true, "pwsh.exe": true, "control.exe": true, "mshta.exe": true,
	"wscript.exe": true, "cscript.exe": true, "dllhost.exe": true, "svchost.exe": true,
	"explorer.exe": true, "conhost.exe": true, "schtasks.exe": true, "sc.exe": true,
	"reg.exe": true, "start": true, "msiexec": true, "rundll32": true, "cmd": true,
}

var execExts = []string{".exe", ".com", ".bat", ".cmd", ".lnk", ".dll", ".ps1", ".vbs", ".js", ".msc", ".cpl", ".scr", ".msi", ".url"}

func hasExecExt(p string) bool {
	l := strings.ToLower(p)
	for _, e := range execExts {
		if strings.HasSuffix(l, e) {
			return true
		}
	}
	return false
}

func baseName(p string) string {
	p = strings.TrimRight(p, `\/`)
	if i := strings.LastIndexAny(p, `\/`); i >= 0 {
		return p[i+1:]
	}
	return p
}

// cleanPath убирает служебные префиксы и хвосты вида ",0" (номер иконки).
func cleanPath(p string) string {
	p = strings.TrimSpace(p)
	p = strings.Trim(p, `"`)
	for _, pre := range []string{`\??\`, `\\?\`} {
		p = strings.TrimPrefix(p, pre)
	}
	if i := strings.LastIndexByte(p, ','); i > 0 && hasExecExt(p[:i]) {
		p = p[:i]
	}
	return strings.TrimSpace(p)
}

// isAbsDrive — абсолютный путь с буквой диска (C:\...).
func isAbsDrive(p string) bool {
	if len(p) < 3 || p[1] != ':' || (p[2] != '\\' && p[2] != '/') {
		return false
	}
	c := p[0] | 0x20
	return c >= 'a' && c <= 'z'
}

func replaceFold(s, old, repl string) (string, bool) {
	i := strings.Index(strings.ToLower(s), strings.ToLower(old))
	if i < 0 {
		return s, false
	}
	return s[:i] + repl + s[i+len(old):], true
}

// variants — все пути, под которыми Windows могла бы найти этот файл:
// с добавленным .exe, с учётом перенаправления WOW64 (System32↔SysWOW64,
// Program Files↔Program Files (x86)) и Sysnative.
func variants(p string) []string {
	base := []string{p}
	if !hasExecExt(p) {
		base = append(base, p+".exe")
	}
	out := append([]string(nil), base...)
	pairs := [][2]string{
		{`\System32\`, `\SysWOW64\`},
		{`\SysWOW64\`, `\System32\`},
		{`\Sysnative\`, `\System32\`},
		{`\Program Files (x86)\`, `\Program Files\`},
		{`\Program Files\`, `\Program Files (x86)\`},
	}
	for _, b := range base {
		for _, pr := range pairs {
			if v, ok := replaceFold(b, pr[0], pr[1]); ok {
				out = append(out, v)
			}
		}
	}
	return out
}

// Resolver проверяет командные строки. Exists и Expand подставляются:
// в Windows — реальная файловая система, в тестах — заглушки.
type Resolver struct {
	Exists func(path string) bool // true также при «нет доступа» — не трогаем
	Expand func(s string) string  // раскрытие %переменных%
}

func (r Resolver) anyExists(p string) bool {
	for _, v := range variants(p) {
		if r.Exists(v) {
			return true
		}
	}
	return false
}

// driveMissing — диск не подключён (флешка, сетевой диск): запись не трогаем.
func (r Resolver) driveMissing(p string) bool {
	return isAbsDrive(p) && !r.Exists(p[:3])
}

// Missing возвращает путь к отсутствующему файлу, если запись действительно
// висячая; пустая строка — файл найден или судить нельзя.
func (r Resolver) Missing(cmd string) string {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return ""
	}
	if r.Expand != nil {
		cmd = r.Expand(cmd)
	}
	if strings.Contains(cmd, "%") { // переменная не раскрылась — судить нельзя
		return ""
	}
	cmd = strings.TrimPrefix(strings.TrimPrefix(cmd, `\??\`), `\\?\`)

	// 1) Путь в кавычках — однозначен.
	if strings.HasPrefix(cmd, `"`) {
		end := strings.IndexByte(cmd[1:], '"')
		var p string
		if end >= 0 {
			p = cmd[1 : 1+end]
		} else {
			p = cmd[1:]
		}
		return r.check(cleanPath(p))
	}

	// 2) Без кавычек: как CreateProcess — пробуем всё более длинные префиксы
	// по пробелам («C:\Program», «C:\Program Files\App\app.exe», …).
	toks := strings.Split(cmd, " ")
	if systemLoaders[strings.ToLower(baseName(toks[0]))] {
		return ""
	}
	var report string
	for i := 1; i <= len(toks); i++ {
		p := cleanPath(strings.Join(toks[:i], " "))
		if p == "" {
			continue
		}
		if !isAbsDrive(p) {
			return "" // относительный путь / команда из PATH
		}
		if systemLoaders[strings.ToLower(baseName(p))] {
			return ""
		}
		if r.anyExists(p) {
			return "" // нашли файл — запись рабочая
		}
		if report == "" && hasExecExt(p) {
			report = p // первый «похожий на программу» префикс
		}
		// дальше аргументы (" /S", " -silent") — продолжать бессмысленно
		if i < len(toks) {
			nx := toks[i]
			if strings.HasPrefix(nx, "/") || strings.HasPrefix(nx, "-") {
				break
			}
		}
	}
	if report == "" {
		// нет ни одного префикса с расширением программы — не уверены
		return ""
	}
	if r.driveMissing(report) {
		return ""
	}
	return report
}

func (r Resolver) check(p string) string {
	if p == "" || !isAbsDrive(p) {
		return ""
	}
	if systemLoaders[strings.ToLower(baseName(p))] {
		return ""
	}
	if r.anyExists(p) || r.driveMissing(p) {
		return ""
	}
	return p
}
