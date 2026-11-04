// Package winupdate — Центр обновлений Windows: ожидающие обновления, журнал
// установок с расшифровкой ошибок, установка выбранных и пауза обновлений.
// Работает через штатный агент Windows Update (COM Microsoft.Update.Session).
package winupdate

import (
	"strings"
	"sync"
	"time"

	"github.com/healcore/healcoreops/internal/i18n"
)

// Update — доступное обновление.
type Update struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	KB       string `json:"kb"`
	Size     int64  `json:"size"`
	Category string `json:"cat"`
	Severity string `json:"sev"`
	Reboot   bool   `json:"reboot"`
	Driver   bool   `json:"driver"`
	Optional bool   `json:"optional"` // не выбирается автоматически
}

// HistoryEntry — запись журнала установки.
type HistoryEntry struct {
	Date    string `json:"date"`
	Title   string `json:"title"`
	Result  int    `json:"result"` // 2 успешно, 3 с ошибками, 4 сбой, 5 прервано
	HResult string `json:"hr"`
	Op      int    `json:"op"` // 1 установка, 2 удаление
	Hint    string `json:"hint,omitempty"`
}

// PauseState — состояние паузы.
type PauseState struct {
	Paused bool   `json:"paused"`
	Until  string `json:"until,omitempty"` // ISO 8601
}

// SearchState — состояние поиска обновлений.
type SearchState struct {
	Running bool     `json:"running"`
	Done    bool     `json:"done"`
	Checked int64    `json:"checked"` // unix-мс
	Items   []Update `json:"items"`
	Err     string   `json:"err,omitempty"`
}

var (
	smu    sync.Mutex
	search SearchState
)

// Search — последний результат поиска.
func Search() SearchState {
	smu.Lock()
	defer smu.Unlock()
	s := search
	s.Items = append([]Update{}, search.Items...)
	return s
}

// StartSearch запускает поиск ожидающих обновлений в фоне (1–5 минут).
func StartSearch() error {
	smu.Lock()
	if search.Running {
		smu.Unlock()
		return nil
	}
	search.Running, search.Err = true, ""
	smu.Unlock()
	go func() {
		items, err := searchUpdates()
		smu.Lock()
		defer smu.Unlock()
		search.Running, search.Done, search.Checked = false, true, time.Now().UnixMilli()
		if err != nil {
			search.Err = err.Error()
			return
		}
		search.Items = items
	}()
	return nil
}

// dropInstalled убирает из последнего результата установленные обновления.
func dropInstalled(ids map[string]bool) {
	smu.Lock()
	defer smu.Unlock()
	keep := search.Items[:0]
	for _, u := range search.Items {
		if !ids[u.ID] {
			keep = append(keep, u)
		}
	}
	search.Items = keep
}

// errorHints — частые коды ошибок Windows Update и что с ними делать.
var errorHints = map[string][2]string{
	"0x80070643": {"Ошибка установки (часто — обновление среды восстановления или .NET). Запустите «Обслуживание → Сброс Центра обновлений» и повторите.", "Install failure (often a recovery-environment or .NET update). Run Maintenance → Reset Windows Update and retry."},
	"0x800F081F": {"Не найдены исходные файлы компонентов. Помогает «Обслуживание → DISM (восстановление образа)».", "Component source files not found. Run Maintenance → DISM (repair image)."},
	"0x800F0922": {"Мало места в разделе «Зарезервировано системой» или нет связи с серверами обновлений (VPN).", "Not enough space on the System Reserved partition, or no connection to update servers (VPN)."},
	"0x80073712": {"Повреждено хранилище компонентов. Запустите DISM, затем SFC.", "Component store is corrupted. Run DISM, then SFC."},
	"0x8024402C": {"Нет связи с серверами обновлений: проверьте интернет, прокси и DNS.", "Can't reach update servers: check internet, proxy and DNS."},
	"0x80240034": {"Не удалось скачать обновление. Сбросьте Центр обновлений и повторите.", "Download failed. Reset Windows Update and retry."},
	"0x80070005": {"Отказано в доступе — нужны права администратора.", "Access denied — administrator rights required."},
	"0x80070070": {"Недостаточно места на диске.", "Not enough disk space."},
	"0x80070002": {"Файл обновления не найден — сбросьте Центр обновлений.", "Update file not found — reset Windows Update."},
	"0x800705B4": {"Превышено время ожидания. Повторите позже или сбросьте Центр обновлений.", "Operation timed out. Retry later or reset Windows Update."},
	"0x80240022": {"Все обновления в наборе завершились ошибкой — смотрите отдельные записи.", "All updates in the batch failed — see individual entries."},
	"0x8024200D": {"Нужна докачка обновления — повторите установку.", "The update needs to be downloaded again — retry."},
	"0x80246007": {"Обновление не было скачано. Повторите поиск и установку.", "The update was not downloaded. Search and install again."},
	"0x800F0988": {"Не удалось очистить хранилище компонентов. Запустите DISM (восстановление образа).", "Component cleanup failed. Run DISM (repair image)."},
}

// Hint — подсказка по коду ошибки.
func Hint(hr string) string {
	if h, ok := errorHints[normHR(hr)]; ok {
		return i18n.Pick(h[0], h[1])
	}
	return ""
}

func normHR(hr string) string {
	hr = strings.TrimSpace(hr)
	if len(hr) > 2 && (hr[:2] == "0x" || hr[:2] == "0X") {
		return "0x" + strings.ToUpper(hr[2:])
	}
	return hr
}

// PauseTimes — значения реестра для паузы на days дней (как «Приостановить» в Параметрах).
func PauseTimes(now time.Time, days int) map[string]string {
	if days < 1 {
		days = 7
	}
	if days > 35 {
		days = 35 // больше Windows не позволяет
	}
	start := now.UTC().Format("2006-01-02T15:04:05Z")
	end := now.UTC().Add(time.Duration(days) * 24 * time.Hour).Format("2006-01-02T15:04:05Z")
	return map[string]string{
		"PauseUpdatesStartTime":        start,
		"PauseUpdatesExpiryTime":       end,
		"PauseFeatureUpdatesStartTime": start,
		"PauseFeatureUpdatesEndTime":   end,
		"PauseQualityUpdatesStartTime": start,
		"PauseQualityUpdatesEndTime":   end,
	}
}

// PauseKeys — все значения паузы (для снятия и отката).
var PauseKeys = []string{"PauseUpdatesStartTime", "PauseUpdatesExpiryTime", "PauseFeatureUpdatesStartTime",
	"PauseFeatureUpdatesEndTime", "PauseQualityUpdatesStartTime", "PauseQualityUpdatesEndTime"}

// pausedUntil разбирает срок паузы.
func pausedUntil(v string, now time.Time) PauseState {
	t, err := time.Parse(time.RFC3339, strings.TrimSpace(v))
	if err != nil || !t.After(now) {
		return PauseState{}
	}
	return PauseState{Paused: true, Until: t.UTC().Format(time.RFC3339)}
}
