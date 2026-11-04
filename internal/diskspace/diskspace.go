// Package diskspace — анализ места на диске: самые большие папки и файлы,
// известные кэши и мусор, очистка и безопасное удаление в Корзину.
package diskspace

import (
	"container/heap"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Item — папка или файл с размером.
type Item struct {
	Path      string `json:"path"`
	Name      string `json:"name"`
	Size      int64  `json:"size"`
	Files     int64  `json:"files"`
	Dir       bool   `json:"dir"`
	Mod       int64  `json:"mod"`       // время изменения, unix ms
	Protected bool   `json:"protected"` // системный путь — удалять нельзя
}

// Scan — состояние анализа папки или диска.
type Scan struct {
	Running  bool   `json:"running"`
	Done     bool   `json:"done"`
	Root     string `json:"root"`
	Files    int64  `json:"files"`
	Bytes    int64  `json:"bytes"`
	Skipped  int64  `json:"skipped"` // папок без доступа
	Elapsed  int64  `json:"elapsed"` // мс
	Folders  []Item `json:"folders"` // непосредственные подпапки, по размеру
	Largest  []Item `json:"largest"` // самые большие файлы
	Total    uint64 `json:"total"`   // объём раздела
	Free     uint64 `json:"free"`
	Err      string `json:"err,omitempty"`
	Canceled bool   `json:"canceled,omitempty"`
}

// Category — известная категория мусора/кэша.
type Category struct {
	ID    string   `json:"id"`
	Title string   `json:"title"`
	Desc  string   `json:"desc"`
	Size  int64    `json:"size"`
	Files int64    `json:"files"`
	Paths []string `json:"paths"`
	Clean bool     `json:"clean"` // можно очистить кнопкой
	Admin bool     `json:"admin"` // для очистки нужны права администратора
	Note  string   `json:"note,omitempty"`
	Page  string   `json:"page,omitempty"` // куда вести вместо очистки (страница интерфейса)
}

// CleanResult — итог очистки.
type CleanResult struct {
	Removed int64 `json:"removed"`
	Freed   int64 `json:"freed"`
	Failed  int64 `json:"failed"`
}

// ErrProtected — попытка удалить системный путь.
var ErrProtected = errors.New("protected")

// ---------- Полный анализ ----------

type fileHeap []Item

func (h fileHeap) Len() int           { return len(h) }
func (h fileHeap) Less(i, j int) bool { return h[i].Size < h[j].Size }
func (h fileHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *fileHeap) Push(x any)        { *h = append(*h, x.(Item)) }
func (h *fileHeap) Pop() any {
	old := *h
	x := old[len(old)-1]
	*h = old[:len(old)-1]
	return x
}

var (
	smu    sync.Mutex
	cur    Scan
	cancel atomic.Bool
	files  atomic.Int64
	nbytes atomic.Int64
)

// Status возвращает состояние анализа (с текущим прогрессом).
func Status() Scan {
	smu.Lock()
	defer smu.Unlock()
	s := cur
	if s.Running {
		s.Files, s.Bytes = files.Load(), nbytes.Load()
	}
	return s
}

// Cancel прерывает анализ.
func Cancel() { cancel.Store(true) }

// Start запускает анализ папки в фоне.
func Start(root string) error {
	root = filepath.Clean(strings.TrimSpace(root))
	st, err := os.Stat(root)
	if err != nil || !st.IsDir() {
		return errors.New("folder not found")
	}
	smu.Lock()
	if cur.Running {
		smu.Unlock()
		return errors.New("busy")
	}
	cur = Scan{Running: true, Root: root}
	smu.Unlock()
	cancel.Store(false)
	files.Store(0)
	nbytes.Store(0)
	go func() {
		res := scanDir(root)
		smu.Lock()
		cur = res
		smu.Unlock()
	}()
	return nil
}

// scanDir обходит папку: размеры подпапок первого уровня (параллельно) и топ файлов.
func scanDir(root string) Scan {
	t0 := time.Now()
	res := Scan{Root: root, Done: true}
	res.Total, res.Free = volumeSpace(root)
	entries, err := os.ReadDir(root)
	if err != nil {
		res.Err = err.Error()
		return res
	}
	var (
		mu      sync.Mutex
		big     = &fileHeap{}
		skipped atomic.Int64
	)
	const keep = 60
	addFile := func(it Item) {
		mu.Lock()
		if big.Len() < keep {
			heap.Push(big, it)
		} else if it.Size > (*big)[0].Size {
			(*big)[0] = it
			heap.Fix(big, 0)
		}
		mu.Unlock()
	}
	folders := make([]Item, 0, len(entries))
	type job struct{ idx int }
	jobs := make(chan job)
	var wg sync.WaitGroup
	for _, e := range entries {
		full := filepath.Join(root, e.Name())
		if e.IsDir() {
			folders = append(folders, Item{Path: full, Name: e.Name(), Dir: true, Protected: IsProtected(full)})
			continue
		}
		if !e.Type().IsRegular() {
			continue
		}
		if info, err := e.Info(); err == nil {
			it := Item{Path: full, Name: e.Name(), Size: info.Size(), Files: 1, Mod: info.ModTime().UnixMilli(), Protected: IsProtected(full)}
			files.Add(1)
			nbytes.Add(it.Size)
			addFile(it)
			folders = append(folders, it) // файлы в корне тоже показываем рядом с папками
		}
	}
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				f := &folders[j.idx]
				var size, n int64
				_ = filepath.WalkDir(f.Path, func(p string, d fs.DirEntry, err error) error {
					if cancel.Load() {
						return filepath.SkipAll
					}
					if err != nil {
						if d != nil && d.IsDir() {
							skipped.Add(1)
							return filepath.SkipDir
						}
						return nil
					}
					if d.IsDir() || !d.Type().IsRegular() {
						return nil // ссылки и точки соединения не обходим — без двойного счёта
					}
					info, err := d.Info()
					if err != nil {
						return nil
					}
					s := info.Size()
					size += s
					n++
					files.Add(1)
					nbytes.Add(s)
					addFile(Item{Path: p, Name: d.Name(), Size: s, Files: 1, Mod: info.ModTime().UnixMilli(), Protected: IsProtected(p)})
					return nil
				})
				f.Size, f.Files = size, n
			}
		}()
	}
	for i := range folders {
		if folders[i].Dir {
			jobs <- job{i}
		}
	}
	close(jobs)
	wg.Wait()

	sort.Slice(folders, func(i, j int) bool { return folders[i].Size > folders[j].Size })
	if len(folders) > 200 {
		folders = folders[:200]
	}
	largest := make([]Item, big.Len())
	copy(largest, *big)
	sort.Slice(largest, func(i, j int) bool { return largest[i].Size > largest[j].Size })

	res.Folders, res.Largest = folders, largest
	res.Files, res.Bytes, res.Skipped = files.Load(), nbytes.Load(), skipped.Load()
	res.Elapsed = time.Since(t0).Milliseconds()
	res.Canceled = cancel.Load()
	return res
}

// ---------- Категории ----------

// measure считает размер файлов по путям (глоб-шаблоны допускаются).
func measure(patterns []string, olderThan time.Duration) (size, n int64, paths []string) {
	cut := time.Now().Add(-olderThan)
	for _, pat := range patterns {
		if !filepath.IsAbs(pat) { // пустая переменная окружения дала бы относительный путь
			continue
		}
		matches, _ := filepath.Glob(pat)
		for _, m := range matches {
			paths = append(paths, m)
			_ = filepath.WalkDir(m, func(p string, d fs.DirEntry, err error) error {
				if err != nil {
					if d != nil && d.IsDir() {
						return filepath.SkipDir
					}
					return nil
				}
				if d.IsDir() || !d.Type().IsRegular() {
					return nil
				}
				if info, err := d.Info(); err == nil && (olderThan == 0 || info.ModTime().Before(cut)) {
					size += info.Size()
					n++
				}
				return nil
			})
		}
	}
	return
}

// cleanPaths удаляет файлы внутри путей (сами корневые папки остаются).
// Занятые программами файлы пропускаются.
func cleanPaths(patterns []string, olderThan time.Duration) CleanResult {
	var r CleanResult
	cut := time.Now().Add(-olderThan)
	for _, pat := range patterns {
		if !filepath.IsAbs(pat) {
			continue
		}
		matches, _ := filepath.Glob(pat)
		for _, root := range matches {
			if IsProtectedRoot(root) {
				continue
			}
			st, err := os.Lstat(root)
			if err != nil {
				continue
			}
			if !st.IsDir() { // шаблон указывает на файл
				if olderThan == 0 || st.ModTime().Before(cut) {
					if os.Remove(root) == nil {
						r.Removed++
						r.Freed += st.Size()
					} else {
						r.Failed++
					}
				}
				continue
			}
			var dirs []string
			_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
				if err != nil {
					if d != nil && d.IsDir() {
						return filepath.SkipDir
					}
					return nil
				}
				if d.IsDir() {
					if p != root {
						dirs = append(dirs, p)
					}
					return nil
				}
				info, err := d.Info()
				if err != nil {
					return nil
				}
				if olderThan > 0 && !info.ModTime().Before(cut) {
					return nil
				}
				if os.Remove(p) == nil {
					r.Removed++
					r.Freed += info.Size()
				} else {
					r.Failed++
				}
				return nil
			})
			// пустые подпапки — снизу вверх
			for i := len(dirs) - 1; i >= 0; i-- {
				_ = os.Remove(dirs[i])
			}
		}
	}
	return r
}

// ---------- Защита системных путей ----------

func norm(p string) string {
	p = strings.TrimRight(filepath.Clean(p), `\/`)
	return strings.ToLower(strings.ReplaceAll(p, "/", `\`))
}

func under(p, root string) bool {
	if root == "" {
		return false
	}
	p, root = norm(p), norm(root)
	return p == root || strings.HasPrefix(p, root+`\`)
}

func same(p, root string) bool { return root != "" && norm(p) == norm(root) }
