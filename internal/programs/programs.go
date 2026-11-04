// Package programs — установленные программы (Win32): список, удаление
// штатным деинсталлятором, поиск остатков и обновление через winget.
package programs

import (
	"regexp"
	"strings"
)

// Program — установленная программа.
type Program struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Publisher   string `json:"publisher"`
	Version     string `json:"version"`
	InstallDate string `json:"installDate"` // ГГГГ-ММ-ДД
	Size        int64  `json:"size"`        // байт (по данным установщика)
	Location    string `json:"location"`
	Scope       string `json:"scope"` // machine | user
	Arch        string `json:"arch"`  // x64 | x86
	Quiet       bool   `json:"quiet"` // есть тихое удаление
	MSI         bool   `json:"msi"`
	Icon        string `json:"icon,omitempty"`
}

// Leftover — остаток программы после удаления.
type Leftover struct {
	ID   string `json:"id"`
	Kind string `json:"kind"` // folder | registry
	Path string `json:"path"`
	Size int64  `json:"size"`
}

// Upgrade — доступное обновление программы (winget).
type Upgrade struct {
	Name      string `json:"name"`
	ID        string `json:"id"`
	Version   string `json:"version"`
	Available string `json:"available"`
	Source    string `json:"source"`
}

var (
	reParen = regexp.MustCompile(`\s*[\(\[][^\)\]]*[\)\]]`)
	reVer   = regexp.MustCompile(`(?i)\s+v?\d+(?:\.\d+)+[a-z0-9.\-_]*`)
	reArch  = regexp.MustCompile(`(?i)\s+(x64|x86|64-bit|32-bit|64bit|32bit|amd64|arm64|win64|win32)\b`)
	reSpace = regexp.MustCompile(`\s{2,}`)
)

// CleanName убирает из названия версию, разрядность и пометки в скобках:
// «7-Zip 23.01 (x64)» → «7-Zip», «Mozilla Firefox (x64 ru)» → «Mozilla Firefox».
// Диапазоны лет вроде «2015-2022» сохраняются.
func CleanName(s string) string {
	s = reParen.ReplaceAllString(s, " ")
	s = reArch.ReplaceAllString(s, " ")
	s = reVer.ReplaceAllString(s, "")
	s = reSpace.ReplaceAllString(s, " ")
	return strings.Trim(strings.TrimSpace(s), "-–—:,. ")
}

// SplitCommand делит командную строку деинсталлятора на программу и аргументы
// (как CreateProcess: путь в кавычках или самый длинный существующий префикс).
func SplitCommand(cmd string, exists func(string) bool) (exe, args string) {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return "", ""
	}
	if cmd[0] == '"' {
		if j := strings.IndexByte(cmd[1:], '"'); j >= 0 {
			return cmd[1 : 1+j], strings.TrimSpace(cmd[2+j:])
		}
		return strings.Trim(cmd, `"`), ""
	}
	toks := strings.Split(cmd, " ")
	for i := len(toks); i >= 1; i-- {
		p := strings.Join(toks[:i], " ")
		if exists(p) || exists(p+".exe") {
			return p, strings.TrimSpace(strings.Join(toks[i:], " "))
		}
	}
	return toks[0], strings.TrimSpace(strings.Join(toks[1:], " "))
}

var reGUID = regexp.MustCompile(`\{[0-9A-Fa-f\-]{36}\}`)

// MSIProduct извлекает код продукта MSI из строки «MsiExec.exe /I{…}».
func MSIProduct(cmd string) string {
	if !strings.Contains(strings.ToLower(cmd), "msiexec") {
		return ""
	}
	return reGUID.FindString(cmd)
}

// ParseWingetTable разбирает таблицу «winget upgrade» (заголовки могут быть
// на любом языке: колонки определяются по строке из дефисов и позициям заголовка).
func ParseWingetTable(out string) []Upgrade {
	// winget рисует прогресс через \r — берём последний сегмент каждой строки.
	var lines []string
	for _, l := range strings.Split(strings.ReplaceAll(out, "\r\n", "\n"), "\n") {
		if i := strings.LastIndex(l, "\r"); i >= 0 {
			l = l[i+1:]
		}
		lines = append(lines, l)
	}
	sep := -1
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if len(t) >= 10 && strings.Trim(t, "-─") == "" {
			sep = i
			break
		}
	}
	if sep < 1 {
		return nil
	}
	head := []rune(lines[sep-1])
	// начала колонок: позиции, где после пробела начинается слово
	var cols []int
	for i, r := range head {
		if r != ' ' && (i == 0 || head[i-1] == ' ') {
			cols = append(cols, i)
		}
	}
	if len(cols) < 4 {
		return nil
	}
	field := func(row []rune, k int) string {
		if cols[k] >= len(row) {
			return ""
		}
		end := len(row)
		if k+1 < len(cols) && cols[k+1] < end {
			end = cols[k+1]
		}
		return strings.TrimSpace(string(row[cols[k]:end]))
	}
	var res []Upgrade
	for _, l := range lines[sep+1:] {
		row := []rune(l)
		if strings.TrimSpace(l) == "" {
			continue
		}
		// итоговая строка («N upgrades available.») не выровнена по колонкам
		u := Upgrade{Name: field(row, 0), ID: field(row, 1), Version: field(row, 2), Available: field(row, 3)}
		if len(cols) > 4 {
			u.Source = field(row, 4)
		}
		if u.ID == "" || u.Available == "" || strings.ContainsAny(u.ID, " ") {
			continue
		}
		res = append(res, u)
	}
	return res
}

// genericNames — имена, по которым остатки искать нельзя (слишком общие).
var genericNames = map[string]bool{
	"microsoft": true, "windows": true, "common files": true, "intel": true, "amd": true, "nvidia": true,
	"nvidia corporation": true, "google": true, "mozilla": true, "adobe": true, "apple": true, "packages": true,
	"temp": true, "programs": true, "microsoft corporation": true, "realtek": true, "app": true, "update": true,
	"steam": false,
}

// LeftoverNames — варианты имени для поиска остатков (без общих слов).
func LeftoverNames(name, publisher string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(s string) {
		s = strings.TrimSpace(s)
		l := strings.ToLower(s)
		if len([]rune(s)) < 3 || genericNames[l] || seen[l] || strings.ContainsAny(s, `\/:*?"<>|`) {
			return
		}
		seen[l] = true
		out = append(out, s)
	}
	add(name)
	add(CleanName(name))
	// «Mozilla Firefox» → «Firefox», если имя начинается с издателя
	pub := CleanPublisher(publisher)
	if n := CleanName(name); pub != "" && strings.HasPrefix(strings.ToLower(n), strings.ToLower(pub)+" ") {
		add(n[len(pub)+1:])
	}
	return out
}

// CleanPublisher убирает юридические хвосты: «Valve Corporation» → «Valve».
func CleanPublisher(p string) string {
	p = strings.TrimSpace(p)
	for _, suf := range []string{", Inc.", " Inc.", " Inc", ", LLC", " LLC", " Ltd.", " Ltd", " Limited", " Corporation", " Corp.", " GmbH", " s.r.o.", " Co.", " AB", " AG", " S.A."} {
		if strings.HasSuffix(p, suf) {
			p = strings.TrimSpace(strings.TrimSuffix(p, suf))
		}
	}
	return strings.TrimRight(p, ",. ")
}

// LocationMatches — папка установки действительно принадлежит программе:
// её имя содержит название программы или издателя и не является общей папкой
// (например, «Common Files»). Иначе её нельзя предлагать к удалению.
func LocationMatches(location, name, publisher string) bool {
	base := strings.ToLower(strings.TrimRight(location, `\/`))
	if i := strings.LastIndexAny(base, `\/`); i >= 0 {
		base = base[i+1:]
	}
	if len(base) < 3 || genericNames[base] {
		return false
	}
	for _, n := range append(LeftoverNames(name, publisher), CleanPublisher(publisher)) {
		w := strings.ToLower(strings.Fields(n + " ")[0])
		if len(w) >= 3 && !genericNames[w] && strings.Contains(base, w) {
			return true
		}
	}
	return false
}
