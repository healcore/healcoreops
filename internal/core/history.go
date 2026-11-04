package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/healcore/healcoreops/internal/autostart"
	"github.com/healcore/healcoreops/internal/config"
	"github.com/healcore/healcoreops/internal/ctl"
	"github.com/healcore/healcoreops/internal/diskspace"
	"github.com/healcore/healcoreops/internal/history"
	"github.com/healcore/healcoreops/internal/i18n"
	"github.com/healcore/healcoreops/internal/reghealer"
	"github.com/healcore/healcoreops/internal/sysadmin"
	"github.com/healcore/healcoreops/internal/tweaks"
	"github.com/healcore/healcoreops/internal/winupdate"
)

// DataDir — папка данных программы (рядом с настройками).
func DataDir() string { return filepath.Dir(config.Path()) }

var (
	histOnce sync.Once
	histDB   *history.Store
)

// History — журнал изменений (общий для окна и командной строки).
func (c *Core) History() *history.Store {
	histOnce.Do(func() {
		histDB = history.Open(filepath.Join(DataDir(), "history.json"))
		histDB.OnAdd = writeSystemEvent // журнал событий Windows (Application, источник HealCoreOps)
	})
	return histDB
}

func currentUser() string {
	if u, err := user.Current(); err == nil {
		return u.Username
	}
	return os.Getenv("USERNAME")
}

// Record заносит изменение в историю и журнал. undo == nil — откат невозможен
// (тогда note объясняет, как вернуть вручную).
func (c *Core) Record(area, title, detail string, undo *history.Undo, note string) {
	src := c.Source
	if src == "" {
		src = "ui"
	}
	c.History().Add(history.Entry{
		Area: area, Title: title, Detail: detail, Undo: undo, UndoNote: note, OK: true,
		Source: src, User: currentUser(), Admin: ctl.IsElevated(),
	})
}

func undoArgs(kind string, kv ...string) *history.Undo {
	u := &history.Undo{Kind: kind, Args: map[string]string{}}
	for i := 0; i+1 < len(kv); i += 2 {
		u.Args[kv[i]] = kv[i+1]
	}
	return u
}

func b2s(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

// Undo откатывает изменение из истории.
func (c *Core) Undo(id int64) error {
	e, ok := c.History().Get(id)
	if !ok {
		return errors.New(i18n.Pick("запись не найдена", "entry not found"))
	}
	if e.UndoneAt != nil {
		return errors.New(i18n.Pick("изменение уже отменено", "already undone"))
	}
	if e.Undo == nil {
		return errors.New(i18n.Pick("это изменение нельзя отменить автоматически", "this change can't be undone automatically"))
	}
	a := e.Undo.Args
	var err error
	switch e.Undo.Kind {
	case "tweak":
		err = tweaks.SetSetting(a["id"], a["on"] == "1")
	case "autostart":
		err = autostart.SetEnabled(a["id"], a["on"] == "1")
	case "service_start":
		err = sysadmin.SetServiceStart(a["name"], a["start"])
	case "service_state":
		act := "stop"
		if a["state"] == "running" {
			act = "start"
		}
		err = sysadmin.ServiceAction(a["name"], act)
	case "task":
		err = sysadmin.SetTaskEnabled(a["path"], a["on"] == "1")
	case "power":
		n, _ := strconv.Atoi(a["profile"])
		err = ctl.SetPowerProfile(ctl.PowerProfile(n))
	case "reg_import":
		err = reghealer.Restore(a["dir"])
	case "recycle":
		err = diskspace.Restore(a["path"])
	case "wu_pause":
		prev := map[string]string{}
		if e := json.Unmarshal([]byte(a["prev"]), &prev); e != nil {
			err = e
		} else {
			err = winupdate.WritePause(prev)
		}
	default:
		err = errors.New("unknown undo kind")
	}
	if err != nil {
		c.Log(LvlErr, i18n.T("Отмена «%s»: %v"), e.Title, err)
		return err
	}
	c.History().MarkUndone(id)
	c.Record(e.Area, i18n.Pick("Отменено: ", "Undone: ")+e.Title, e.Detail, nil, "")
	c.Log(LvlOK, i18n.T("Отменено: %s"), e.Title)
	return nil
}

// ---------- Журнал в файл ----------

type fileLog struct {
	mu    sync.Mutex
	month string
	f     *os.File
}

var flog fileLog

// LogDir — папка журналов.
func LogDir() string { return filepath.Join(DataDir(), "logs") }

func (l *fileLog) write(t time.Time, lvl int, msg string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	m := t.Format("2006-01")
	if l.f == nil || l.month != m {
		if l.f != nil {
			l.f.Close()
		}
		_ = os.MkdirAll(LogDir(), 0o755)
		p := filepath.Join(LogDir(), "healcoreops-"+m+".log")
		if st, err := os.Stat(p); err == nil && st.Size() > 20<<20 { // не больше 20 МБ в месяц
			_ = os.Rename(p, p+".1")
		}
		f, err := os.OpenFile(p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return
		}
		l.f, l.month = f, m
	}
	lv := [...]string{"INFO", "OK", "WARN", "ERROR"}[max(0, min(lvl, 3))]
	fmt.Fprintf(l.f, "%s %-5s %s\n", t.Format("2006-01-02 15:04:05"), lv, strings.ReplaceAll(msg, "\n", " "))
}
