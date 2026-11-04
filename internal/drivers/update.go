package drivers

import (
	"encoding/json"
	"encoding/xml"
	"errors"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// Источники драйверов:
//   - nvidia — официальный сервис NVIDIA: последняя версия для конкретной модели,
//     загрузка с download.nvidia.com, проверка цифровой подписи NVIDIA, тихая установка;
//   - wu — пакет производителя (подписан им самим), найденный по идентификатору
//     оборудования в каталоге Windows Update: так Realtek, MediaTek, AMD, Intel и
//     производители плат публикуют драйверы для автоматической установки.
// Сайты, требующие ручного выбора или капчи, программа не «обходит» и
// непроверенные установщики не запускает.

// Avail — доступное обновление для устройства.
type Avail struct {
	ID      string `json:"id"`      // id устройства
	Source  string `json:"source"`  // nvidia | wu | current | none
	Version string `json:"version"` // новая версия (если известна)
	Current string `json:"current"` // установленная версия
	Title   string `json:"title"`   // что будет установлено
	Size    int64  `json:"size"`    // примерный размер загрузки, байт
	Note    string `json:"note,omitempty"`

	url   string   // nvidia: ссылка на установщик
	wuIDs []string // wu: идентификаторы обновлений
}

// HasUpdate — есть что устанавливать.
func (a Avail) HasUpdate() bool { return a.Source == "nvidia" || a.Source == "wu" }

var (
	amu     sync.Mutex
	avail   = map[string]Avail{}
	checked bool
	lastDev []Device // последний список устройств (с идентификаторами оборудования)
)

// Avails возвращает результат последней проверки.
func Avails() (map[string]Avail, bool) {
	amu.Lock()
	defer amu.Unlock()
	cp := make(map[string]Avail, len(avail))
	for k, v := range avail {
		cp[k] = v
	}
	return cp, checked
}

func setLastDevices(d []Device) {
	amu.Lock()
	lastDev = d
	amu.Unlock()
}

func lastDevices() []Device {
	amu.Lock()
	defer amu.Unlock()
	return append([]Device(nil), lastDev...)
}

func setAvails(m map[string]Avail) {
	amu.Lock()
	avail, checked = m, true
	amu.Unlock()
}

// ---------- Версии ----------

// NvidiaVersion переводит версию драйвера Windows в версию NVIDIA:
// «32.0.15.6636» → «566.36», «31.0.15.5161» → «551.61».
func NvidiaVersion(win string) string {
	p := strings.Split(strings.TrimSpace(win), ".")
	if len(p) < 4 {
		return ""
	}
	last := p[3]
	for len(last) < 4 {
		last = "0" + last
	}
	d := p[2] + last
	if len(d) < 5 {
		return ""
	}
	d = d[len(d)-5:]
	n, err := strconv.Atoi(d)
	if err != nil {
		return ""
	}
	return strconv.Itoa(n/100) + "." + d[3:]
}

// CompareVersions сравнивает версии вида «566.36» / «10.0.19041.1»: -1, 0, 1.
func CompareVersions(a, b string) int {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(pa) || i < len(pb); i++ {
		var x, y int
		if i < len(pa) {
			x, _ = strconv.Atoi(strings.TrimSpace(pa[i]))
		}
		if i < len(pb) {
			y, _ = strconv.Atoi(strings.TrimSpace(pb[i]))
		}
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}

// ---------- NVIDIA ----------

// NvFamily — модель видеокарты в справочнике NVIDIA.
type NvFamily struct {
	Series string // psid
	ID     string // pfid
	Name   string
}

// ParseNvLookup разбирает справочник моделей NVIDIA (lookupValueSearch, TypeID=3).
func ParseNvLookup(b []byte) ([]NvFamily, error) {
	var doc struct {
		Values []struct {
			Parent string `xml:"ParentID,attr"`
			Name   string `xml:"Name"`
			Value  string `xml:"Value"`
		} `xml:"LookupValues>LookupValue"`
	}
	if err := xml.Unmarshal(b, &doc); err != nil {
		return nil, err
	}
	out := make([]NvFamily, 0, len(doc.Values))
	for _, v := range doc.Values {
		if v.Name != "" && v.Value != "" {
			out = append(out, NvFamily{Series: v.Parent, ID: v.Value, Name: strings.TrimSpace(v.Name)})
		}
	}
	if len(out) == 0 {
		return nil, errors.New("empty NVIDIA lookup")
	}
	return out, nil
}

func normGPU(s string) string {
	s = strings.ToLower(s)
	for _, r := range []string{"nvidia", "(r)", "(tm)", "®", "™"} {
		s = strings.ReplaceAll(s, r, " ")
	}
	return strings.Join(strings.Fields(s), " ")
}

// MatchNvFamily находит модель NVIDIA по имени устройства Windows
// («NVIDIA GeForce RTX 4060 Ti» → «GeForce RTX 4060 Ti»). Сначала точное
// совпадение, затем самое длинное имя модели, целиком входящее в имя устройства.
func MatchNvFamily(device string, fams []NvFamily) (NvFamily, bool) {
	d := normGPU(device)
	for _, f := range fams {
		if normGPU(f.Name) == d {
			return f, true
		}
	}
	var best NvFamily
	for _, f := range fams {
		n := normGPU(f.Name)
		if n == "" || !strings.Contains(" "+d+" ", " "+n+" ") {
			continue
		}
		if len(n) > len(normGPU(best.Name)) {
			best = f
		}
	}
	return best, best.ID != ""
}

// ParseNvDriver разбирает ответ DriverManualLookup: версия и ссылка на загрузку.
func ParseNvDriver(b []byte) (version, link string, size int64, err error) {
	var r struct {
		Success string `json:"Success"`
		IDS     []struct {
			Info struct {
				Version     string `json:"Version"`
				DownloadURL string `json:"DownloadURL"`
				Size        string `json:"DownloadURLFileSize"`
			} `json:"downloadInfo"`
		} `json:"IDS"`
	}
	if err = json.Unmarshal(b, &r); err != nil {
		return
	}
	if len(r.IDS) == 0 || r.IDS[0].Info.DownloadURL == "" {
		return "", "", 0, errors.New("NVIDIA: driver not found")
	}
	in := r.IDS[0].Info
	link = in.DownloadURL
	if strings.Contains(link, "%3A") || strings.Contains(link, "%2F") {
		if u, e := url.QueryUnescape(link); e == nil {
			link = u
		}
	}
	if !NvidiaHostOK(link) {
		return "", "", 0, errors.New("NVIDIA: unexpected download host")
	}
	version = strings.TrimSpace(in.Version)
	sz := strings.TrimSpace(strings.TrimSuffix(strings.TrimSuffix(in.Size, " MB"), "MB"))
	if f, e := strconv.ParseFloat(sz, 64); e == nil {
		size = int64(f * 1024 * 1024)
	}
	return version, link, size, nil
}

// NvidiaHostOK — загрузка только по HTTPS с серверов NVIDIA.
func NvidiaHostOK(link string) bool {
	u, err := url.Parse(link)
	if err != nil || u.Scheme != "https" {
		return false
	}
	h := strings.ToLower(u.Hostname())
	return h == "nvidia.com" || strings.HasSuffix(h, ".nvidia.com")
}

// IsNvidiaGPU — видеокарта NVIDIA.
func IsNvidiaGPU(d Device) bool {
	if d.Class != "Display" {
		return false
	}
	t := strings.ToLower(d.Name + " " + d.Manu)
	return strings.Contains(t, "nvidia") || strings.Contains(t, "geforce") || strings.Contains(t, "quadro")
}

// ---------- Windows Update ----------

// WUDriver — драйвер из каталога Windows Update.
type WUDriver struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Date  string `json:"date"`
	Manu  string `json:"manu"`
	Class string `json:"cls"`
	HW    string `json:"hw"`
	Model string `json:"model"`
	Size  int64  `json:"size"`
}

// классы драйверов чипсета платы; прошивки (Firmware) не ставим автоматически —
// обновление BIOS без участия пользователя слишком рискованно.
var boardClasses = map[string]bool{"system": true, "hdc": true, "scsiadapter": true, "usb": true, "sbp2": true, "smbus": true, "sensor": true, "securitydevices": true}

// MatchWU раскладывает драйверы Windows Update по устройствам: по точному
// идентификатору оборудования; оставшиеся драйверы чипсета — к ЦП (по
// производителю процессора) или к материнской плате.
func MatchWU(devs []Device, ups []WUDriver) map[string][]WUDriver {
	res := map[string][]WUDriver{}
	used := map[string]bool{}
	for _, d := range devs {
		ids := map[string]bool{}
		for _, h := range d.HW {
			ids[strings.ToUpper(h)] = true
		}
		for _, u := range ups {
			if u.HW != "" && ids[strings.ToUpper(u.HW)] {
				res[d.ID] = append(res[d.ID], u)
				used[u.ID] = true
			}
		}
	}
	var cpuVendor string
	for _, d := range devs {
		if d.ID == "cpu" {
			t := strings.ToLower(d.Name + " " + d.Manu)
			switch {
			case strings.Contains(t, "amd"):
				cpuVendor = "amd"
			case strings.Contains(t, "intel"):
				cpuVendor = "intel"
			}
		}
	}
	hasBoard, hasCPU := false, false
	for _, d := range devs {
		hasBoard = hasBoard || d.ID == "board"
		hasCPU = hasCPU || d.ID == "cpu"
	}
	for _, u := range ups {
		if used[u.ID] {
			continue
		}
		cls := strings.ToLower(u.Class)
		if cls == "processor" && hasCPU {
			res["cpu"] = append(res["cpu"], u)
			continue
		}
		if !boardClasses[cls] {
			continue
		}
		manu := strings.ToLower(u.Manu + " " + u.Title)
		isCPUVendor := cpuVendor != "" && (strings.Contains(manu, cpuVendor) ||
			(cpuVendor == "amd" && strings.Contains(manu, "advanced micro devices")))
		if hasCPU && isCPUVendor {
			res["cpu"] = append(res["cpu"], u)
		} else if hasBoard {
			res["board"] = append(res["board"], u)
		}
	}
	for k := range res {
		sort.SliceStable(res[k], func(i, j int) bool { return res[k][i].Date > res[k][j].Date })
	}
	return res
}
