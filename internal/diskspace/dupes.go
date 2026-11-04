package diskspace

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Поиск дубликатов: файлы группируются по размеру, затем сравниваются
// по хэшу первых 64 КБ и, при совпадении, по SHA-256 всего содержимого.
// Удалять можно только в Корзину и не в защищённых системных папках.

// DupFile — файл из группы дубликатов.
type DupFile struct {
	Path      string `json:"path"`
	Mod       int64  `json:"mod"`
	Protected bool   `json:"protected"`
}

// DupGroup — одинаковые файлы.
type DupGroup struct {
	Size   int64     `json:"size"`
	Hash   string    `json:"hash"`
	Files  []DupFile `json:"files"`
	Wasted int64     `json:"wasted"` // size × (копий − 1)
}

// DupScan — состояние поиска.
type DupScan struct {
	Running  bool       `json:"running"`
	Done     bool       `json:"done"`
	Root     string     `json:"root"`
	MinSize  int64      `json:"minSize"`
	Phase    string     `json:"phase"` // walk | hash
	Files    int64      `json:"files"`
	ToHash   int64      `json:"toHash"`
	Hashed   int64      `json:"hashed"`
	Groups   []DupGroup `json:"groups"`
	Wasted   int64      `json:"wasted"`
	Elapsed  int64      `json:"elapsed"`
	Err      string     `json:"err,omitempty"`
	Canceled bool       `json:"canceled,omitempty"`
}

var (
	dmu     sync.Mutex
	dcur    DupScan
	dcancel atomic.Bool
	dfiles  atomic.Int64
	dhashed atomic.Int64
	dtohash atomic.Int64
	dphase  atomic.Value
)

// DupStatus — состояние поиска дубликатов.
func DupStatus() DupScan {
	dmu.Lock()
	defer dmu.Unlock()
	s := dcur
	if s.Running {
		s.Files, s.Hashed, s.ToHash = dfiles.Load(), dhashed.Load(), dtohash.Load()
		if p, ok := dphase.Load().(string); ok {
			s.Phase = p
		}
	}
	if s.Groups == nil {
		s.Groups = []DupGroup{}
	}
	return s
}

// DupCancel прерывает поиск.
func DupCancel() { dcancel.Store(true) }

// DupStart запускает поиск дубликатов в папке (minSize — байт).
func DupStart(root string, minSize int64) error {
	root = filepath.Clean(strings.TrimSpace(root))
	st, err := os.Stat(root)
	if err != nil || !st.IsDir() {
		return errors.New("folder not found")
	}
	if minSize < 1 {
		minSize = 1 << 20
	}
	dmu.Lock()
	if dcur.Running {
		dmu.Unlock()
		return errors.New("busy")
	}
	dcur = DupScan{Running: true, Root: root, MinSize: minSize, Phase: "walk"}
	dmu.Unlock()
	dcancel.Store(false)
	dfiles.Store(0)
	dhashed.Store(0)
	dtohash.Store(0)
	dphase.Store("walk")
	go func() {
		res := findDupes(root, minSize, dcancel.Load)
		dmu.Lock()
		dcur = res
		dmu.Unlock()
	}()
	return nil
}

type fent struct {
	path string
	size int64
	mod  int64
}

func findDupes(root string, minSize int64, canceled func() bool) DupScan {
	t0 := time.Now()
	res := DupScan{Root: root, MinSize: minSize, Done: true}
	bySize := map[int64][]fent{}
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if canceled() {
			return filepath.SkipAll
		}
		if err != nil {
			if d != nil && d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() || !d.Type().IsRegular() {
			return nil // ссылки и точки соединения не обходим
		}
		info, err := d.Info()
		if err != nil || info.Size() < minSize {
			return nil
		}
		dfiles.Add(1)
		bySize[info.Size()] = append(bySize[info.Size()], fent{p, info.Size(), info.ModTime().UnixMilli()})
		return nil
	})
	res.Files = dfiles.Load()

	var cands [][]fent
	var total int64
	for _, g := range bySize {
		if len(g) > 1 {
			cands = append(cands, g)
			total += int64(len(g))
		}
	}
	dtohash.Store(total)
	dphase.Store("hash")
	// Крупные группы первыми: так при отмене уже найдено самое полезное.
	sort.Slice(cands, func(i, j int) bool { return cands[i][0].size > cands[j][0].size })

	for _, g := range cands {
		if canceled() {
			break
		}
		// этап 1: первые 64 КБ
		head := map[string][]fent{}
		for _, f := range g {
			if h, err := hashFile(f.path, 64<<10); err == nil {
				head[h] = append(head[h], f)
			}
			dhashed.Add(1)
		}
		for h, hg := range head {
			if len(hg) < 2 {
				continue
			}
			if hg[0].size <= 64<<10 { // файл целиком уже прочитан
				res.Groups = append(res.Groups, mkGroup(hg, h))
				continue
			}
			// этап 2: всё содержимое
			byFull := map[string][]fent{}
			for _, f := range hg {
				if canceled() {
					break
				}
				if fh, err := hashFile(f.path, -1); err == nil {
					byFull[fh] = append(byFull[fh], f)
				}
			}
			for fh, fg := range byFull {
				if len(fg) > 1 {
					res.Groups = append(res.Groups, mkGroup(fg, fh))
				}
			}
		}
	}
	sort.Slice(res.Groups, func(i, j int) bool { return res.Groups[i].Wasted > res.Groups[j].Wasted })
	if len(res.Groups) > 500 {
		res.Groups = res.Groups[:500]
	}
	for _, g := range res.Groups {
		res.Wasted += g.Wasted
	}
	res.ToHash, res.Hashed = total, dhashed.Load()
	res.Elapsed = time.Since(t0).Milliseconds()
	res.Canceled = canceled()
	if res.Groups == nil {
		res.Groups = []DupGroup{}
	}
	return res
}

func mkGroup(fs []fent, hash string) DupGroup {
	g := DupGroup{Size: fs[0].size, Hash: hash}
	sort.Slice(fs, func(i, j int) bool { return fs[i].mod < fs[j].mod }) // самый старый — первым (обычно оригинал)
	for _, f := range fs {
		g.Files = append(g.Files, DupFile{Path: f.path, Mod: f.mod, Protected: IsProtected(f.path)})
	}
	g.Wasted = g.Size * int64(len(fs)-1)
	return g
}

// hashFile — SHA-256 первых limit байт (limit < 0 — всего файла).
func hashFile(p string, limit int64) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	var r io.Reader = f
	if limit > 0 {
		r = io.LimitReader(f, limit)
	}
	if _, err := io.CopyBuffer(h, r, make([]byte, 256<<10)); err != nil {
		return "", err
	}
	return hexs(h.Sum(nil)), nil
}

func hexs(b []byte) string {
	const d = "0123456789abcdef"
	var buf bytes.Buffer
	for _, c := range b {
		buf.WriteByte(d[c>>4])
		buf.WriteByte(d[c&15])
	}
	return buf.String()
}
