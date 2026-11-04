//go:build !windows

package diskspace

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"time"

	"golang.org/x/sys/unix"

	"github.com/healcore/healcoreops/internal/ctl"
	"github.com/healcore/healcoreops/internal/i18n"
)

// Supported — анализ работает везде; очистка категорий и Корзина — по возможностям ОС.
func Supported() bool { return true }

var sysEnv = func() protEnv {
	home, _ := os.UserHomeDir()
	self := ""
	if exe, err := os.Executable(); err == nil {
		self = filepath.Dir(exe)
	}
	return protEnv{profile: home, self: self}
}()

func volumeSpace(p string) (total, free uint64) {
	var st unix.Statfs_t
	if unix.Statfs(p, &st) != nil {
		return 0, 0
	}
	return st.Blocks * uint64(st.Bsize), st.Bavail * uint64(st.Bsize)
}

// Drives — домашняя папка и корень.
func Drives() []Item {
	var out []Item
	home, _ := os.UserHomeDir()
	for _, r := range []string{"/", home} {
		if r == "" {
			continue
		}
		tot, free := volumeSpace(r)
		out = append(out, Item{Path: r, Name: r, Size: int64(tot - free), Files: int64(tot), Dir: true})
	}
	return out
}

func catPaths() map[string][]string {
	home, _ := os.UserHomeDir()
	cache := filepath.Join(home, ".cache")
	if runtime.GOOS == "darwin" {
		cache = filepath.Join(home, "Library", "Caches")
	}
	return map[string][]string{
		"user_temp":     {os.TempDir()},
		"browser_cache": {filepath.Join(cache, "google-chrome"), filepath.Join(cache, "chromium"), filepath.Join(cache, "mozilla"), filepath.Join(cache, "thumbnails")},
		"recycle":       {filepath.Join(home, ".local", "share", "Trash", "files"), filepath.Join(home, ".local", "share", "Trash", "info"), filepath.Join(home, ".Trash")},
		"downloads":     {filepath.Join(home, "Downloads")},
	}
}

// Categories измеряет известные категории.
func Categories() []Category {
	p := catPaths()
	defs := []Category{
		{ID: "user_temp", Title: i18n.Pick("Временные файлы", "Temp files"), Desc: os.TempDir(), Clean: true},
		{ID: "browser_cache", Title: i18n.Pick("Кэш браузеров и эскизов", "Browser & thumbnail caches"), Desc: "~/.cache", Clean: true},
		{ID: "recycle", Title: i18n.Pick("Корзина", "Trash"), Desc: "~/.local/share/Trash", Clean: true},
		{ID: "downloads", Title: i18n.Pick("Папка «Загрузки»", "Downloads folder"), Desc: "~/Downloads", Page: "open"},
	}
	for i := range defs {
		older := time.Duration(0)
		if defs[i].ID == "user_temp" {
			older = 24 * time.Hour
		}
		defs[i].Size, defs[i].Files, defs[i].Paths = measure(p[defs[i].ID], older)
		if defs[i].Paths == nil {
			defs[i].Paths = []string{}
		}
	}
	sort.SliceStable(defs, func(i, j int) bool { return defs[i].Size > defs[j].Size })
	return defs
}

// Clean очищает категорию.
func Clean(id string) (CleanResult, error) {
	paths, ok := catPaths()[id]
	if !ok || id == "downloads" {
		return CleanResult{}, ctl.ErrUnsupported
	}
	older := time.Duration(0)
	if id == "user_temp" {
		older = 24 * time.Hour
	}
	return cleanPaths(paths, older), nil
}

// Recycle перемещает в Корзину через gio/trash, если есть.
func Recycle(path string) error {
	if IsProtected(path) {
		return ErrProtected
	}
	if p, err := exec.LookPath("gio"); err == nil {
		return exec.Command(p, "trash", path).Run()
	}
	return ctl.ErrUnsupported
}

// RecycleLeftover — то же, что Recycle (остатки программ есть только в Windows).
func RecycleLeftover(path string) error { return Recycle(path) }

// Restore вне Windows не поддерживается.
func Restore(string) error { return ctl.ErrUnsupported }

// Reveal открывает папку в файловом менеджере.
func Reveal(path string) error {
	if st, err := os.Stat(path); err == nil && !st.IsDir() {
		path = filepath.Dir(path)
	}
	if runtime.GOOS == "darwin" {
		return exec.Command("open", path).Start()
	}
	return exec.Command("xdg-open", path).Start()
}

// OpenCleanmgr вне Windows не поддерживается.
func OpenCleanmgr() error { return ctl.ErrUnsupported }
