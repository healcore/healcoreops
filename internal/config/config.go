// Package config хранит настройки приложения в JSON в каталоге пользователя.
package config

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/healcore/healcoreops/internal/i18n"
	"github.com/healcore/healcoreops/internal/rules"
)

// Config — настройки приложения.
type Config struct {
	Lang          string       `json:"lang"`  // ru | en
	Theme         string       `json:"theme"` // dark | light
	IntervalSec   int          `json:"interval_sec"`
	AutoRules     bool         `json:"auto_rules"`
	AutoTrim      bool         `json:"auto_trim"`
	TrimThreshold int          `json:"trim_threshold"` // % занятой ОЗУ
	TempAgeDays   int          `json:"temp_age_days"`
	HogThreshold  int          `json:"hog_threshold"` // % одного ядра
	Rules         []rules.Rule `json:"rules"`

	// Фоновая работа
	CloseToTray  bool `json:"close_to_tray"`  // при закрытии окна — сворачивать в трей
	RunAtStartup bool `json:"run_at_startup"` // запускать вместе с Windows (в трее)
	Notify       bool `json:"notify"`         // уведомления о нагрузке

	// Игровой режим
	GameMode bool     `json:"game_mode"`
	Games    []string `json:"games"` // имена процессов игр

	// Обновления
	AutoUpdateCheck bool   `json:"auto_update_check"`
	SkipVersion     string `json:"skip_version"`

	// Драйверы: не использовать пакеты производителей из каталога Windows Update
	DriverNoWU bool `json:"driver_no_wu"`

	// Обслуживание по расписанию (пока программа работает, в т. ч. в трее)
	Schedule Schedule `json:"schedule"`

	// Оповещения по порогам (уведомление в трее и запись в журнал Windows)
	Alerts Alerts `json:"alerts"`

	// Режим интерфейса: simple — только главные разделы, admin — все
	Mode string `json:"mode"`

	// История нагрузки: сколько дней хранить (0 — не вести)
	LoadHistoryDays int `json:"load_history_days"`
}

// Alerts — пороги оповещений. 0 — проверка выключена.
type Alerts struct {
	Enabled     bool     `json:"enabled"`
	DiskFreePct int      `json:"disk_free_pct"` // мало места на разделе, % свободного
	DiskHealth  bool     `json:"disk_health"`   // SMART: ошибки, износ, перегрев
	DiskTemp    int      `json:"disk_temp"`     // °C
	GPUTemp     int      `json:"gpu_temp"`      // °C
	MemPct      int      `json:"mem_pct"`       // память занята дольше 10 минут, %
	Battery     bool     `json:"battery"`       // сильный износ батареи
	Reboot      bool     `json:"reboot"`        // Windows давно ждёт перезагрузки
	Services    []string `json:"services"`      // службы, которые должны работать
}

// Schedule — регулярная автоматическая очистка.
type Schedule struct {
	Enabled bool     `json:"enabled"`
	Day     int      `json:"day"`  // 0 — воскресенье … 6 — суббота, 7 — каждый день
	Hour    int      `json:"hour"` // 0–23
	Cats    []string `json:"cats"` // категории очистки (diskspace)
	LastRun string   `json:"last_run"`
}

// Default — настройки по умолчанию.
func Default() *Config {
	return &Config{
		Theme:         "dark",
		IntervalSec:   2,
		AutoRules:     true,
		TrimThreshold: 85,
		TempAgeDays:   2,
		HogThreshold:  50,
		CloseToTray:   true,
		Notify:        true,
		GameMode:      true,

		AutoUpdateCheck: true,
		Schedule:        Schedule{Day: 0, Hour: 19, Cats: []string{"user_temp", "wer"}},
		Alerts:          Alerts{Enabled: true, DiskFreePct: 10, DiskHealth: true, DiskTemp: 65, GPUTemp: 88, MemPct: 92, Battery: true, Reboot: true, Services: []string{}},
		LoadHistoryDays: 7,
	}
}

// Path — путь к файлу настроек.
func Path() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = "."
	}
	return filepath.Join(dir, "HealCoreOps", "config.json")
}

// Load читает настройки; при ошибке возвращает значения по умолчанию.
func Load() *Config {
	c := Default()
	b, err := os.ReadFile(Path())
	if err == nil {
		_ = json.Unmarshal(b, c)
		if c.Mode == "" {
			c.Mode = "admin" // обновление со старой версии — оставляем все разделы, как было
		}
	} else {
		c.Mode = "simple" // первый запуск — простой режим
	}
	c.Normalize()
	return c
}

func (c *Config) Normalize() {
	if c.Lang != "ru" && c.Lang != "en" {
		c.Lang = i18n.SystemLang()
	}
	if c.Theme != "light" {
		c.Theme = "dark"
	}
	if c.IntervalSec < 1 || c.IntervalSec > 60 {
		c.IntervalSec = 2
	}
	if c.TrimThreshold < 50 || c.TrimThreshold > 98 {
		c.TrimThreshold = 85
	}
	if c.TempAgeDays < 0 || c.TempAgeDays > 365 {
		c.TempAgeDays = 2
	}
	if c.HogThreshold < 10 || c.HogThreshold > 100 {
		c.HogThreshold = 50
	}
	if c.Schedule.Day < 0 || c.Schedule.Day > 7 {
		c.Schedule.Day = 0
	}
	if c.Schedule.Hour < 0 || c.Schedule.Hour > 23 {
		c.Schedule.Hour = 19
	}
	if c.Schedule.Cats == nil {
		c.Schedule.Cats = []string{}
	}
	a := &c.Alerts
	clamp := func(v *int, lo, hi int) {
		if *v != 0 && (*v < lo || *v > hi) {
			*v = 0
		}
	}
	clamp(&a.DiskFreePct, 1, 50)
	clamp(&a.DiskTemp, 40, 90)
	clamp(&a.GPUTemp, 60, 105)
	clamp(&a.MemPct, 50, 99)
	if a.Services == nil {
		a.Services = []string{}
	}
	if len(a.Services) > 30 {
		a.Services = a.Services[:30]
	}
	if c.Mode != "simple" && c.Mode != "admin" {
		c.Mode = "admin"
	}
	if c.LoadHistoryDays < 0 || c.LoadHistoryDays > 31 {
		c.LoadHistoryDays = 7
	}
}

// Save записывает настройки на диск.
func (c *Config) Save() error {
	p := Path()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}
