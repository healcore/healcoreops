// Package drivers — список устройств и обновление драйверов через
// Центр обновления Windows (каталог Microsoft, драйверы с подписью WHQL).
// Сторонние сайты и установщики не используются.
package drivers

import (
	"errors"
	"net/url"
	"sync"

	"github.com/healcore/healcoreops/internal/ctl"
	"github.com/healcore/healcoreops/internal/i18n"
)

// OpenVendor открывает официальную страницу драйверов производителя в браузере.
// Разрешены только проверенные хосты из allowedHosts.
func OpenVendor(u string) error {
	p, err := url.Parse(u)
	if err != nil || p.Scheme != "https" || !allowedHosts[p.Host] {
		return errors.New(i18n.T("ссылка не разрешена"))
	}
	return ctl.OpenURL(u)
}

// Device — устройство и его текущий драйвер.
type Device struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Class       string `json:"class"`
	Category    string `json:"category"` // понятная группа
	Manu        string `json:"manu"`
	Version     string `json:"version"`
	Date        string `json:"date"`
	Status      string `json:"status"`
	NeedsDriver bool   `json:"needsDriver"` // нет драйвера или ошибка
	Vendor      string `json:"vendor"`      // производитель для страницы драйверов
	VendorURL   string `json:"vendorURL"`   // официальная страница драйверов
	Winget      string `json:"winget"`      // id официального средства установки драйверов (если есть)
}

// SysInfo — краткие сведения о системе (помогают найти драйверы).
type SysInfo struct {
	Board string `json:"board"`
	CPU   string `json:"cpu"`
	GPU   string `json:"gpu"`
	OS    string `json:"os"`
	RAM   string `json:"ram"`
}

var (
	sysMu   sync.Mutex
	sysInfo SysInfo
)

// GetSysInfo возвращает последние собранные сведения о системе.
func GetSysInfo() SysInfo {
	sysMu.Lock()
	defer sysMu.Unlock()
	return sysInfo
}

func setSysInfo(s SysInfo) {
	sysMu.Lock()
	sysInfo = s
	sysMu.Unlock()
}

// vendorSite — производитель, его официальная страница драйверов и, если есть,
// id официального средства автоустановки драйверов для winget. Где winget-id
// задан, приложение ставит драйвер автоматически из официального источника
// (winget проверяет издателя и контрольную сумму); иначе открывает страницу.
func vendorSite(text string) (vendor, url, winget string) {
	t := toLowerASCII(text)
	switch {
	case has(t, "nvidia"), has(t, "geforce"):
		// Официальное приложение NVIDIA ставит и обновляет драйверы видеокарты.
		return "NVIDIA", "https://www.nvidia.com/Download/index.aspx", "Nvidia.GeForceExperience"
	case has(t, "advanced micro"), has(t, "amd "), t == "amd", has(t, "radeon"), has(t, "ryzen"):
		return "AMD", "https://www.amd.com/en/support/download/drivers.html", ""
	case has(t, "intel"):
		// Intel Driver & Support Assistant — официально находит и ставит драйверы Intel.
		return "Intel", "https://www.intel.com/content/www/us/en/download-center/home.html", "Intel.IntelDriverAndSupportAssistant"
	case has(t, "realtek"):
		return "Realtek", "https://www.realtek.com/Download/List?cate_id=584", ""
	case has(t, "asustek"), has(t, "asus"):
		return "ASUS", "https://www.asus.com/support/download-center/", ""
	case has(t, "micro-star"), has(t, "msi"):
		return "MSI", "https://www.msi.com/support/download", ""
	case has(t, "gigabyte"):
		return "GIGABYTE", "https://www.gigabyte.com/Support", ""
	case has(t, "asrock"):
		return "ASRock", "https://www.asrock.com/support/index.asp", ""
	case has(t, "biostar"):
		return "Biostar", "https://www.biostar.com.tw/app/en/support/download.php", ""
	case has(t, "dell"):
		return "Dell", "https://www.dell.com/support/home/en-us?app=drivers", "Dell.CommandUpdate"
	case has(t, "hewlett"), has(t, "hp "), t == "hp":
		return "HP", "https://support.hp.com/us-en/drivers", ""
	case has(t, "lenovo"):
		return "Lenovo", "https://support.lenovo.com/us/en/solutions/ht003029", "Lenovo.SystemUpdate"
	case has(t, "acer"):
		return "Acer", "https://www.acer.com/us-en/support/drivers-and-manuals", ""
	case has(t, "qualcomm"), has(t, "atheros"):
		return "Qualcomm", "https://www.qualcomm.com/support", ""
	case has(t, "mediatek"):
		return "MediaTek", "https://www.mediatek.com/products/connectivity-and-networking", ""
	}
	return "", "", ""
}

// allowedWinget — разрешённые id для автоустановки (официальные средства вендоров).
var allowedWinget = map[string]bool{
	"Nvidia.GeForceExperience":             true,
	"Intel.IntelDriverAndSupportAssistant": true,
	"Dell.CommandUpdate":                   true,
	"Lenovo.SystemUpdate":                  true,
}

// WingetAllowed проверяет, что id входит в список официальных средств.
func WingetAllowed(id string) bool { return allowedWinget[id] }

// allowedHosts — куда разрешено открывать ссылки из раздела драйверов.
var allowedHosts = map[string]bool{
	"www.nvidia.com": true, "www.amd.com": true, "www.intel.com": true,
	"www.realtek.com": true, "www.asus.com": true, "www.msi.com": true,
	"www.gigabyte.com": true, "www.asrock.com": true, "www.biostar.com.tw": true,
	"www.dell.com": true, "support.hp.com": true, "support.lenovo.com": true,
	"www.acer.com": true, "www.qualcomm.com": true, "www.mediatek.com": true,
}

// AllowedHost сообщает, можно ли открывать ссылку на этот хост.
func AllowedHost(host string) bool { return allowedHosts[host] }

func has(s, sub string) bool {
	return len(sub) <= len(s) && indexOf(s, sub) >= 0
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func toLowerASCII(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 32
		}
	}
	return string(b)
}

// Update — доступное обновление драйвера из Windows Update.
type Update struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Date  string `json:"date"`
}

// Job — состояние фоновой операции (поиск или установка).
type Job struct {
	Running bool     `json:"running"`
	Phase   string   `json:"phase"`
	Lines   []string `json:"lines"`
	Done    bool     `json:"done"`
	OK      bool     `json:"ok"`
}

var (
	jmu     sync.Mutex
	job     Job
	updates []Update // результат последнего поиска
	scanned bool
)

// Status возвращает состояние операции и найденные обновления.
func Status() (Job, []Update, bool) {
	jmu.Lock()
	defer jmu.Unlock()
	cj := job
	cj.Lines = append([]string(nil), job.Lines...)
	return cj, append([]Update(nil), updates...), scanned
}

func emit(line string) {
	jmu.Lock()
	job.Lines = append(job.Lines, line)
	if len(job.Lines) > 300 {
		job.Lines = job.Lines[len(job.Lines)-300:]
	}
	jmu.Unlock()
}

func runJob(phase string, fn func(emit func(string)) error) error {
	jmu.Lock()
	if job.Running {
		jmu.Unlock()
		return errBusy
	}
	job = Job{Running: true, Phase: phase}
	jmu.Unlock()
	go func() {
		err := fn(emit)
		jmu.Lock()
		job.Running, job.Done, job.OK = false, true, err == nil
		if err != nil {
			job.Lines = append(job.Lines, "× "+err.Error())
		}
		jmu.Unlock()
	}()
	return nil
}
