package core

import (
	"encoding/json"
	"errors"
	"os"
	"time"

	"github.com/healcore/healcoreops/internal/config"
	"github.com/healcore/healcoreops/internal/i18n"
	"github.com/healcore/healcoreops/internal/policy"
	"github.com/healcore/healcoreops/internal/rules"
	"github.com/healcore/healcoreops/internal/tweaks"
)

// Profile — переносимый набор настроек: твики, правила и параметры программы.
// Сохраняется в файл и применяется на другом ПК одной кнопкой.
type Profile struct {
	App      string          `json:"app"`
	Version  string          `json:"version"`
	Exported string          `json:"exported"`
	Tweaks   map[string]bool `json:"tweaks,omitempty"`
	Rules    []rules.Rule    `json:"rules"`
	Prefs    Prefs           `json:"prefs"`
}

// Prefs — переносимые параметры программы (без путей и машинозависимых значений).
type Prefs struct {
	Lang          string   `json:"lang"`
	Theme         string   `json:"theme"`
	IntervalSec   int      `json:"interval_sec"`
	AutoRules     bool     `json:"auto_rules"`
	AutoTrim      bool     `json:"auto_trim"`
	TrimThreshold int      `json:"trim_threshold"`
	TempAgeDays   int      `json:"temp_age_days"`
	HogThreshold  int      `json:"hog_threshold"`
	Notify        bool     `json:"notify"`
	CloseToTray   bool     `json:"close_to_tray"`
	GameMode      bool     `json:"game_mode"`
	Games         []string `json:"games"`
}

// ExportProfile собирает текущее состояние в профиль.
func (c *Core) ExportProfile() Profile {
	cfg := c.Config()
	p := Profile{
		App: "HealCoreOps", Version: Version, Exported: time.Now().Format(time.RFC3339),
		Rules: cfg.Rules,
		Prefs: Prefs{
			Lang: cfg.Lang, Theme: cfg.Theme, IntervalSec: cfg.IntervalSec, AutoRules: cfg.AutoRules,
			AutoTrim: cfg.AutoTrim, TrimThreshold: cfg.TrimThreshold, TempAgeDays: cfg.TempAgeDays,
			HogThreshold: cfg.HogThreshold, Notify: cfg.Notify, CloseToTray: cfg.CloseToTray,
			GameMode: cfg.GameMode, Games: cfg.Games,
		},
	}
	if p.Rules == nil {
		p.Rules = []rules.Rule{}
	}
	if tweaks.Supported() {
		p.Tweaks = map[string]bool{}
		for _, s := range tweaks.ListSettings() {
			p.Tweaks[s.ID] = s.Enabled
		}
	}
	return p
}

// ImportReport — итог применения профиля.
type ImportReport struct {
	Changed int      `json:"changed"`
	Skipped int      `json:"skipped"` // запрещено политикой администратора
	Failed  []string `json:"failed"`
}

// ImportProfile применяет профиль: параметры, правила и твики (только отличающиеся).
func (c *Core) ImportProfile(p Profile) (ImportReport, error) { return c.importProfile(p, true) }

// importProfile — prefs=false применяет только правила и твики (профиль из политики).
func (c *Core) importProfile(p Profile, prefs bool) (ImportReport, error) {
	rep := ImportReport{Failed: []string{}}
	if p.App != "HealCoreOps" {
		return rep, errors.New(i18n.T("это не файл профиля HealCoreOps"))
	}
	pol := policy.Get()
	if prefs {
		c.UpdateConfig(func(cfg *config.Config) {
			pr := p.Prefs
			cfg.Lang, cfg.Theme, cfg.IntervalSec = pr.Lang, pr.Theme, pr.IntervalSec
			cfg.AutoRules, cfg.AutoTrim, cfg.TrimThreshold = pr.AutoRules, pr.AutoTrim, pr.TrimThreshold
			cfg.TempAgeDays, cfg.HogThreshold, cfg.Notify = pr.TempAgeDays, pr.HogThreshold, pr.Notify
			cfg.CloseToTray, cfg.GameMode, cfg.Games = pr.CloseToTray, pr.GameMode, pr.Games
			cfg.Normalize()
		})
	}
	if p.Rules != nil {
		for i := range p.Rules {
			if p.Rules[i].ID == "" {
				p.Rules[i].ID = rules.NewID()
			}
		}
		c.SetRules(p.Rules)
	}
	if len(p.Tweaks) > 0 && tweaks.Supported() {
		if pol.Blocks(policy.RuleTweaks) {
			rep.Skipped = len(p.Tweaks)
		} else {
			cur := map[string]bool{}
			titles := map[string]string{}
			for _, s := range tweaks.ListSettings() {
				cur[s.ID], titles[s.ID] = s.Enabled, s.Title
			}
			for id, on := range p.Tweaks {
				now, known := cur[id]
				if !known || now == on {
					continue
				}
				if id == "defender" && on && pol.DisableDefender {
					rep.Skipped++
					continue
				}
				if err := tweaks.SetSetting(id, on); err != nil {
					rep.Failed = append(rep.Failed, titles[id]+": "+err.Error())
					continue
				}
				rep.Changed++
				c.RecordTweak(id, titles[id], on)
			}
		}
	}
	c.Log(LvlOK, i18n.T("Профиль импортирован: изменено твиков %d, ошибок %d"), rep.Changed, len(rep.Failed))
	return rep, nil
}

// RecordTweak заносит переключение твика в историю с возможностью отката.
func (c *Core) RecordTweak(id, title string, on bool) {
	verb := i18n.Pick("Твик применён: ", "Tweak applied: ")
	if !on {
		verb = i18n.Pick("Твик отменён: ", "Tweak reverted: ")
	}
	c.Record("tweaks", verb+title, id, undoArgs("tweak", "id", id, "on", b2s(!on)), "")
}

// LoadProfileFile читает профиль из файла.
func LoadProfileFile(path string) (Profile, error) {
	var p Profile
	b, err := os.ReadFile(path)
	if err != nil {
		return p, err
	}
	if err := json.Unmarshal(b, &p); err != nil {
		return p, errors.New(i18n.T("это не файл профиля HealCoreOps"))
	}
	return p, nil
}

// ApplyProfileFile применяет профиль из файла (командная строка).
func (c *Core) ApplyProfileFile(path string, prefs bool) (ImportReport, error) {
	p, err := LoadProfileFile(path)
	if err != nil {
		return ImportReport{Failed: []string{}}, err
	}
	return c.importProfile(p, prefs)
}

// applyEnforcedProfile применяет профиль из политики администратора при запуске.
func (c *Core) applyEnforcedProfile() {
	path := policy.Get().Profile
	if path == "" {
		return
	}
	prev := c.Source
	c.Source = "policy"
	defer func() { c.Source = prev }()
	rep, err := c.ApplyProfileFile(path, false)
	if err != nil {
		c.Log(LvlErr, i18n.T("Профиль из политики (%s): %v"), path, err)
		return
	}
	if rep.Changed > 0 {
		c.Log(LvlInfo, i18n.T("Профиль из политики администратора: изменено твиков %d"), rep.Changed)
	}
}
