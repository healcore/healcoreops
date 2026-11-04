package diskspace

import (
	"path/filepath"
	"strings"
)

// protEnv — системные папки, которые нельзя удалять (заполняется под ОС).
type protEnv struct {
	windows      bool
	systemRoot   string // C:\Windows
	programFiles []string
	programData  string
	users        string // C:\Users
	profile      string // C:\Users\имя
	appData      string // ...\AppData
	self         string // папка самой программы
}

// protectedNames — системные файлы в корне диска.
var protectedNames = map[string]bool{
	"pagefile.sys": true, "hiberfil.sys": true, "swapfile.sys": true, "bootmgr": true,
	"$recycle.bin": true, "system volume information": true, "recovery": true,
	"boot": true, "efi": true, "$windows.~bt": true, "$windows.~ws": true, "$winreagent": true,
}

func (e protEnv) protected(p string) bool {
	if p == "" {
		return true
	}
	if e.windows {
		n := norm(p)
		if len(n) <= 3 { // «C:», «C:\»
			return true
		}
		if protectedNames[strings.ToLower(filepath.Base(strings.ReplaceAll(p, `\`, "/")))] && strings.Count(n, `\`) <= 1 {
			return true
		}
		for _, r := range append([]string{e.systemRoot, e.programData, e.self}, e.programFiles...) {
			if under(p, r) {
				return true
			}
		}
		// сами папки пользователей и профиля, AppData и папки Microsoft внутри неё
		for _, r := range []string{e.users, e.profile, e.appData,
			e.appData + `\Local`, e.appData + `\Roaming`, e.appData + `\LocalLow`,
			e.appData + `\Local\Microsoft`, e.appData + `\Roaming\Microsoft`, e.appData + `\Local\Packages`} {
			if same(p, r) {
				return true
			}
		}
		for _, r := range []string{e.appData + `\Local\Microsoft\Windows`, e.appData + `\Roaming\Microsoft\Windows`, e.appData + `\Local\Packages`} {
			if under(p, r) {
				return true
			}
		}
		return false
	}
	// Linux / macOS
	n := strings.TrimRight(filepath.Clean(p), "/")
	if n == "" || n == "/" {
		return true
	}
	for _, r := range []string{"/bin", "/boot", "/dev", "/etc", "/lib", "/lib32", "/lib64", "/opt", "/proc", "/root", "/run",
		"/sbin", "/snap", "/srv", "/sys", "/usr", "/var", "/System", "/Library", "/Applications", "/private"} {
		if n == r || strings.HasPrefix(n, r+"/") {
			return true
		}
	}
	if n == "/home" || n == "/Users" || (e.profile != "" && n == strings.TrimRight(e.profile, "/")) {
		return true
	}
	return e.self != "" && (n == e.self || strings.HasPrefix(n, e.self+"/"))
}

// IsProtected — путь системный: удалять и перемещать в Корзину нельзя.
func IsProtected(p string) bool { return sysEnv.protected(p) }

// IsProtectedRoot — для очистки категорий: запрещён только сам защищённый корень,
// а не его содержимое (например, C:\Windows\Temp чистить можно, C:\Windows — нет).
func IsProtectedRoot(p string) bool {
	e := sysEnv
	for _, r := range append([]string{e.systemRoot, e.programData, e.users, e.profile, e.appData}, e.programFiles...) {
		if same(p, r) {
			return true
		}
	}
	n := norm(p)
	return len(n) <= 3 || n == "/" || n == `\`
}
