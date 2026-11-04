// Package policy — политики администратора. В Windows задаются в
// HKLM\SOFTWARE\Policies\HealCoreOps (обычный пользователь изменить не может),
// в Linux/macOS — файлом /etc/healcoreops/policy.json. Позволяют развернуть
// программу в организации и ограничить, что пользователи могут менять.
package policy

import (
	"strings"
	"sync"
)

// Policy — действующие ограничения.
type Policy struct {
	Managed            bool     `json:"managed"`            // задана хотя бы одна политика
	ReadOnly           bool     `json:"readOnly"`           // только просмотр и мониторинг
	DisableTweaks      bool     `json:"disableTweaks"`      // твики и удаление встроенных приложений
	DisableDefender    bool     `json:"disableDefender"`    // запрет отключать Microsoft Defender
	DisableRegistry    bool     `json:"disableRegistry"`    // исправления реестра
	DisablePrograms    bool     `json:"disablePrograms"`    // удаление и обновление программ
	DisableServices    bool     `json:"disableServices"`    // службы и задачи планировщика
	DisableDisk        bool     `json:"disableDisk"`        // очистка и удаление файлов
	DisableDrivers     bool     `json:"disableDrivers"`     // установка драйверов
	DisableUpdateCheck bool     `json:"disableUpdateCheck"` // проверка новых версий на GitHub
	LockSettings       bool     `json:"lockSettings"`       // настройки программы и импорт профиля
	HideSections       []string `json:"hideSections"`       // скрытые разделы интерфейса
	Profile            string   `json:"profile"`            // профиль, применяемый при каждом запуске
	Source             string   `json:"source"`
}

// Values — сырые значения (имя → число/строка), из реестра или файла.
type Values map[string]any

func b(v Values, k string) bool {
	switch x := v[k].(type) {
	case uint64:
		return x != 0
	case int:
		return x != 0
	case float64:
		return x != 0
	case bool:
		return x
	case string:
		return x == "1" || strings.EqualFold(x, "true")
	}
	return false
}

func s(v Values, k string) string {
	if x, ok := v[k].(string); ok {
		return strings.TrimSpace(x)
	}
	return ""
}

// FromValues строит политику из значений. Имена — как в реестре.
func FromValues(v Values, source string) Policy {
	p := Policy{
		ReadOnly: b(v, "ReadOnly"), DisableTweaks: b(v, "DisableTweaks"), DisableDefender: b(v, "DisableDefenderTweak"),
		DisableRegistry: b(v, "DisableRegistryHealer"), DisablePrograms: b(v, "DisableProgramChanges"),
		DisableServices: b(v, "DisableServiceChanges"), DisableDisk: b(v, "DisableDiskCleanup"),
		DisableDrivers: b(v, "DisableDriverInstall"), DisableUpdateCheck: b(v, "DisableUpdateCheck"),
		LockSettings: b(v, "LockSettings"), Profile: s(v, "EnforcedProfile"), Source: source,
		HideSections: []string{},
	}
	switch x := v["HideSections"].(type) {
	case []string:
		p.HideSections = x
	case []any:
		for _, e := range x {
			if str, ok := e.(string); ok {
				p.HideSections = append(p.HideSections, str)
			}
		}
	case string:
		for _, e := range strings.FieldsFunc(x, func(r rune) bool { return r == ',' || r == ';' || r == ' ' }) {
			p.HideSections = append(p.HideSections, e)
		}
	}
	p.Managed = len(v) > 0
	return p
}

var (
	mu  sync.Mutex
	cur *Policy
)

// Get возвращает действующую политику (читается один раз и кешируется).
func Get() Policy {
	mu.Lock()
	defer mu.Unlock()
	if cur == nil {
		p := load()
		cur = &p
	}
	return *cur
}

// Reload перечитывает политику.
func Reload() Policy {
	mu.Lock()
	cur = nil
	mu.Unlock()
	return Get()
}

// Rule — какое ограничение запрещает действие (для API).
type Rule int

const (
	RuleNone Rule = iota
	RuleWrite
	RuleTweaks
	RuleRegistry
	RulePrograms
	RuleServices
	RuleDisk
	RuleDrivers
	RuleSettings
)

// Blocks — запрещает ли политика действие данного вида.
func (p Policy) Blocks(r Rule) bool {
	if r == RuleNone {
		return false
	}
	if p.ReadOnly {
		return true
	}
	switch r {
	case RuleTweaks:
		return p.DisableTweaks
	case RuleRegistry:
		return p.DisableRegistry
	case RulePrograms:
		return p.DisablePrograms
	case RuleServices:
		return p.DisableServices
	case RuleDisk:
		return p.DisableDisk
	case RuleDrivers:
		return p.DisableDrivers
	case RuleSettings:
		return p.LockSettings
	}
	return false
}

// RuleFor сопоставляет изменяющий запрос API с видом ограничения.
func RuleFor(method, path string) Rule {
	if method != "POST" {
		return RuleNone
	}
	pre := func(p string) bool { return strings.HasPrefix(path, p) }
	switch {
	case path == "/api/tweaks/restore-point", path == "/api/tweaks/restart-explorer":
		return RuleNone
	case pre("/api/tweaks/"):
		return RuleTweaks
	case pre("/api/reg/fix"):
		return RuleRegistry
	case pre("/api/programs/uninstall"), pre("/api/programs/leftovers/remove"), pre("/api/programs/upgrade"):
		return RulePrograms
	case pre("/api/services/"), pre("/api/tasks/"):
		return RuleServices
	case pre("/api/disk/clean"), pre("/api/disk/recycle"), pre("/api/opt/tempclean"):
		return RuleDisk
	case pre("/api/drivers/update"):
		return RuleDrivers
	case pre("/api/profile/import"):
		return RuleSettings
	case pre("/api/autostart"), pre("/api/maintenance/run"), pre("/api/proc/"), pre("/api/power"),
		pre("/api/opt/trim"), pre("/api/opt/lower"), pre("/api/history/undo"), pre("/api/schedule"),
		pre("/api/wu/install"), pre("/api/wu/pause"):
		return RuleWrite
	}
	return RuleNone
}

// Template — шаблон .reg со всеми политиками (по умолчанию выключены).
func Template() string {
	return `Windows Registry Editor Version 5.00

; HealCoreOps — политики администратора.
; Импортируйте на компьютеры (GPO, Intune, SCCM или reg import) и поменяйте значения:
; dword:00000001 — включить ограничение, dword:00000000 — выключить.

[HKEY_LOCAL_MACHINE\SOFTWARE\Policies\HealCoreOps]
; Только просмотр: никаких изменений системы из программы
"ReadOnly"=dword:00000000
; Запретить твики и удаление встроенных приложений
"DisableTweaks"=dword:00000000
; Запретить отключение Microsoft Defender
"DisableDefenderTweak"=dword:00000001
; Запретить исправление реестра
"DisableRegistryHealer"=dword:00000000
; Запретить удаление и обновление программ
"DisableProgramChanges"=dword:00000000
; Запретить изменение служб и задач планировщика
"DisableServiceChanges"=dword:00000000
; Запретить очистку диска и удаление файлов
"DisableDiskCleanup"=dword:00000000
; Запретить установку драйверов
"DisableDriverInstall"=dword:00000000
; Не проверять обновления HealCoreOps на GitHub
"DisableUpdateCheck"=dword:00000000
; Запретить менять настройки программы и импортировать профиль
"LockSettings"=dword:00000000
; Скрыть разделы (через запятую): processes,rules,optimize,disk,programs,tweaks,autostart,services,maintenance,drivers,registry,network,crashes,report
"HideSections"=""
; Профиль (экспорт из «Настройки → Профиль»), применяемый при каждом запуске: твики и правила
"EnforcedProfile"=""
`
}
