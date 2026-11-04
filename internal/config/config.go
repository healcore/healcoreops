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

	// Внешний вид окна
	Opacity int  `json:"opacity"` // прозрачность окна, % (40–100)
	Acrylic bool `json:"acrylic"` // системный фон окна (как в Windows 11)
	// Backdrop — вид системного фона: mica (как Параметры и Проводник Windows 11)
	// или acrylic (полупрозрачное размытое стекло, сквозь него видны окна).
	Backdrop string `json:"backdrop"`

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
		Opacity:       100,
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
	if c.Backdrop != "mica" && c.Backdrop != "acrylic" {
		c.Backdrop = "acrylic"
	}
	if c.Opacity < 40 || c.Opacity > 100 {
		c.Opacity = 100
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
