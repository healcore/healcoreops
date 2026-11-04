package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/healcore/healcoreops/internal/core"
	"github.com/healcore/healcoreops/internal/i18n"
	"github.com/healcore/healcoreops/internal/maintenance"
	"github.com/healcore/healcoreops/internal/policy"
	"github.com/healcore/healcoreops/internal/sys"
	"github.com/healcore/healcoreops/internal/tweaks"
)

// Коды возврата командной строки.
const (
	exitOK      = 0
	exitError   = 1
	exitPartial = 2 // выполнено с ошибками / найдены проблемы
	exitPolicy  = 3 // запрещено политикой администратора
	exitUsage   = 64
)

var cliCommands = []string{"--help", "-h", "/?", "--apply", "--export-profile", "--report", "--checkup", "--clean",
	"--maintenance", "--list-tweaks", "--tweak", "--list-maintenance", "--version"}

// isCLI — запущены ли с командой (без окна).
func isCLI(args []string) bool {
	for _, a := range args {
		for _, c := range cliCommands {
			if strings.EqualFold(a, c) || strings.HasPrefix(strings.ToLower(a), c+"=") {
				return true
			}
		}
	}
	return false
}

type cliOut struct {
	w     io.Writer
	log   *os.File
	quiet bool
}

func (o *cliOut) printf(format string, a ...any) {
	s := fmt.Sprintf(format, a...)
	if !o.quiet && o.w != nil {
		fmt.Fprint(o.w, s)
	}
	if o.log != nil {
		fmt.Fprint(o.log, s)
	}
}

// argVal возвращает значение флага: «--flag value» или «--flag=value».
func argVal(args []string, name string) (string, bool) {
	for i, a := range args {
		if strings.EqualFold(a, name) {
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "--") {
				return args[i+1], true
			}
			return "", true
		}
		if strings.HasPrefix(strings.ToLower(a), name+"=") {
			return a[len(name)+1:], true
		}
	}
	return "", false
}

func argVals(args []string, name string) []string {
	var out []string
	for i, a := range args {
		if strings.EqualFold(a, name) && i+1 < len(args) {
			out = append(out, args[i+1])
		} else if strings.HasPrefix(strings.ToLower(a), name+"=") {
			out = append(out, a[len(name)+1:])
		}
	}
	return out
}

func usage() string {
	return i18n.Pick(`HealCoreOps `+core.Version+` — командная строка

  HealCoreOps.exe --apply profile.json [--prefs]   применить профиль (твики и правила; --prefs — и настройки)
  HealCoreOps.exe --export-profile profile.json    сохранить текущий профиль
  HealCoreOps.exe --report [отчёт.html|.json]      отчёт о компьютере (по умолчанию — в «Документы\HealCoreOps»)
  HealCoreOps.exe --checkup [--json итог.json]     проверка ПК; код 0 — всё хорошо, 2 — есть проблемы
  HealCoreOps.exe --clean                          быстрая очистка (временные файлы, отчёты, эскизы)
  HealCoreOps.exe --maintenance sfc                операция обслуживания (список: --list-maintenance)
  HealCoreOps.exe --tweak id=on --tweak id2=off    включить/выключить твики (список: --list-tweaks)

  Общие ключи: --log файл.txt (дописывать вывод в файл), --quiet (без вывода)
  Коды возврата: 0 — успех, 1 — ошибка, 2 — частично/найдены проблемы, 3 — запрещено политикой, 64 — неверные ключи.
  Окно не открывается. В cmd используйте «start /wait», в PowerShell — «Start-Process -Wait».
`, `HealCoreOps `+core.Version+` — command line

  HealCoreOps.exe --apply profile.json [--prefs]   apply a profile (tweaks and rules; --prefs also app settings)
  HealCoreOps.exe --export-profile profile.json    save the current profile
  HealCoreOps.exe --report [report.html|.json]     computer report (default: Documents\HealCoreOps)
  HealCoreOps.exe --checkup [--json result.json]   PC checkup; exit 0 — all good, 2 — problems found
  HealCoreOps.exe --clean                          quick cleanup (temp files, error reports, thumbnails)
  HealCoreOps.exe --maintenance sfc                maintenance action (list: --list-maintenance)
  HealCoreOps.exe --tweak id=on --tweak id2=off    turn tweaks on/off (list: --list-tweaks)

  Common: --log file.txt (append output to a file), --quiet (no output)
  Exit codes: 0 success, 1 error, 2 partial/problems found, 3 blocked by policy, 64 bad arguments.
  No window opens. In cmd use “start /wait”, in PowerShell “Start-Process -Wait”.
`)
}

// runCLI выполняет команду и возвращает код выхода.
func runCLI(args []string) int {
	out := &cliOut{w: attachConsole(), quiet: hasFlag(args, "--quiet") || hasFlag(args, "-q")}
	if p, ok := argVal(args, "--log"); ok && p != "" {
		if f, err := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
			out.log = f
			defer f.Close()
			fmt.Fprintf(f, "\n=== %s %s\n", time.Now().Format("2006-01-02 15:04:05"), strings.Join(args, " "))
		}
	}
	c := core.New()
	c.Source = "cli"
	c.Quiet = true
	pol := policy.Get()
	blocked := func(r policy.Rule) bool {
		if pol.Blocks(r) {
			out.printf("%s\n", i18n.Pick("Запрещено политикой администратора.", "Blocked by an administrator policy."))
			return true
		}
		return false
	}

	switch {
	case hasFlag(args, "--help") || hasFlag(args, "-h") || hasFlag(args, "/?"):
		out.printf("%s", usage())
		return exitOK

	case hasFlag(args, "--version"):
		out.printf("HealCoreOps %s\n", core.Version)
		return exitOK

	case hasFlag(args, "--list-tweaks"):
		if !tweaks.Supported() {
			out.printf("Windows only\n")
			return exitError
		}
		for _, s := range tweaks.ListSettings() {
			st := "off"
			if s.Enabled {
				st = "ON "
			}
			out.printf("%s  %-28s %s\n", st, s.ID, s.Title)
		}
		return exitOK

	case hasFlag(args, "--list-maintenance"):
		for _, a := range maintenance.Actions() {
			out.printf("%-18s %s\n", a.ID, a.Title)
		}
		return exitOK

	case hasFlag(args, "--apply"):
		path, _ := argVal(args, "--apply")
		if path == "" {
			out.printf("%s", usage())
			return exitUsage
		}
		if blocked(policy.RuleSettings) {
			return exitPolicy
		}
		rep, err := c.ApplyProfileFile(path, hasFlag(args, "--prefs"))
		if err != nil {
			out.printf("%s: %v\n", i18n.Pick("Ошибка", "Error"), err)
			return exitError
		}
		out.printf(i18n.Pick("Профиль применён: изменено твиков %d, пропущено политикой %d, ошибок %d\n",
			"Profile applied: %d tweaks changed, %d skipped by policy, %d errors\n"), rep.Changed, rep.Skipped, len(rep.Failed))
		for _, f := range rep.Failed {
			out.printf("  × %s\n", f)
		}
		if len(rep.Failed) > 0 {
			return exitPartial
		}
		return exitOK

	case hasFlag(args, "--export-profile"):
		path, _ := argVal(args, "--export-profile")
		if path == "" {
			out.printf("%s", usage())
			return exitUsage
		}
		b, _ := json.MarshalIndent(c.ExportProfile(), "", "  ")
		if err := os.WriteFile(path, b, 0o644); err != nil {
			out.printf("%v\n", err)
			return exitError
		}
		out.printf("%s\n", path)
		return exitOK

	case hasFlag(args, "--report"):
		path, _ := argVal(args, "--report")
		c.Prime()
		out.printf("%s\n", i18n.Pick("Сбор сведений… (до минуты)", "Collecting data… (up to a minute)"))
		p, err := c.SaveReport(c.BuildReport(), path)
		if err != nil {
			out.printf("%v\n", err)
			return exitError
		}
		out.printf("%s\n", p)
		return exitOK

	case hasFlag(args, "--checkup"):
		c.Prime()
		res := c.RunCheckup()
		marks := []string{"✓", "!", "×"}
		for _, it := range res.Items {
			out.printf("%s %s\n", marks[it.Level], it.Title)
			if it.Detail != "" && it.Level > 0 {
				out.printf("    %s\n", it.Detail)
			}
		}
		if p, ok := argVal(args, "--json"); ok && p != "" {
			b, _ := json.MarshalIndent(res, "", "  ")
			_ = os.WriteFile(p, b, 0o644)
		}
		if res.Bad > 0 || res.Warn > 0 {
			return exitPartial
		}
		return exitOK

	case hasFlag(args, "--clean"):
		if blocked(policy.RuleDisk) {
			return exitPolicy
		}
		r, _ := c.QuickClean()
		out.printf(i18n.Pick("Удалено файлов: %d, освобождено: %s, пропущено (заняты): %d\n", "Files deleted: %d, freed: %s, skipped (in use): %d\n"),
			r.Removed, sys.Bytes(uint64(r.Freed)), r.Failed)
		return exitOK

	case hasFlag(args, "--maintenance"):
		id, _ := argVal(args, "--maintenance")
		if id == "" {
			out.printf("%s", usage())
			return exitUsage
		}
		if blocked(policy.RuleWrite) {
			return exitPolicy
		}
		if err := maintenance.Run(id); err != nil {
			out.printf("%v\n", err)
			return exitError
		}
		c.Record("maintenance", i18n.Pick("Обслуживание: ", "Maintenance: ")+id, "", nil, "")
		shown := 0
		for {
			st := maintenance.Status()
			for ; shown < len(st.Lines); shown++ {
				out.printf("%s\n", st.Lines[shown])
			}
			if !st.Running {
				if st.OK {
					return exitOK
				}
				return exitError
			}
			time.Sleep(500 * time.Millisecond)
		}

	case len(argVals(args, "--tweak")) > 0:
		if blocked(policy.RuleTweaks) {
			return exitPolicy
		}
		titles := map[string]string{}
		for _, s := range tweaks.ListSettings() {
			titles[s.ID] = s.Title
		}
		failed := 0
		for _, t := range argVals(args, "--tweak") {
			id, val, _ := strings.Cut(t, "=")
			on := val == "" || strings.EqualFold(val, "on") || val == "1" || strings.EqualFold(val, "true")
			if _, ok := titles[id]; !ok {
				out.printf("× %s: %s\n", id, i18n.Pick("неизвестный твик", "unknown tweak"))
				failed++
				continue
			}
			if id == "defender" && on && pol.DisableDefender {
				out.printf("× %s: %s\n", id, i18n.Pick("запрещено политикой", "blocked by policy"))
				failed++
				continue
			}
			if err := tweaks.SetSetting(id, on); err != nil {
				out.printf("× %s: %v\n", id, err)
				failed++
				continue
			}
			c.RecordTweak(id, titles[id], on)
			out.printf("✓ %s = %v\n", id, on)
		}
		if failed > 0 {
			return exitPartial
		}
		return exitOK
	}
	out.printf("%s", usage())
	return exitUsage
}

func hasFlag(args []string, f string) bool {
	for _, a := range args {
		if strings.EqualFold(a, f) || strings.HasPrefix(strings.ToLower(a), strings.ToLower(f)+"=") {
			return true
		}
	}
	return false
}
