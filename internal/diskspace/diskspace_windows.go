//go:build windows

package diskspace

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/healcore/healcoreops/internal/ctl"
	"github.com/healcore/healcoreops/internal/i18n"
)

// Supported — полный набор функций доступен в Windows.
func Supported() bool { return true }

var sysEnv = func() protEnv {
	sr := os.Getenv("SystemRoot")
	if sr == "" {
		sr = `C:\Windows`
	}
	prof := os.Getenv("USERPROFILE")
	self := ""
	if exe, err := os.Executable(); err == nil {
		self = filepath.Dir(exe)
	}
	return protEnv{
		windows: true, systemRoot: sr, programData: os.Getenv("ProgramData"),
		programFiles: []string{os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)"), os.Getenv("ProgramW6432")},
		users:        filepath.Dir(prof), profile: prof, appData: filepath.Join(prof, "AppData"), self: self,
	}
}()

var (
	shell32          = windows.NewLazySystemDLL("shell32.dll")
	procFileOp       = shell32.NewProc("SHFileOperationW")
	procQueryRecycle = shell32.NewProc("SHQueryRecycleBinW")
	procEmptyRecycle = shell32.NewProc("SHEmptyRecycleBinW")
)

func volumeSpace(p string) (total, free uint64) {
	vol := filepath.VolumeName(p) + `\`
	ptr, _ := windows.UTF16PtrFromString(vol)
	var avail, tot, tfree uint64
	if windows.GetDiskFreeSpaceEx(ptr, &avail, &tot, &tfree) == nil {
		return tot, avail
	}
	return 0, 0
}

// Drives — подключённые локальные диски.
func Drives() []Item {
	var out []Item
	mask, _ := windows.GetLogicalDrives()
	for i := 0; i < 26; i++ {
		if mask&(1<<uint(i)) == 0 {
			continue
		}
		root := string(rune('A'+i)) + `:\`
		p, _ := windows.UTF16PtrFromString(root)
		t := windows.GetDriveType(p)
		if t != windows.DRIVE_FIXED && t != windows.DRIVE_REMOVABLE {
			continue
		}
		tot, free := volumeSpace(root)
		if tot == 0 {
			continue
		}
		out = append(out, Item{Path: root, Name: root, Size: int64(tot - free), Files: int64(tot), Dir: true})
	}
	return out
}

func env(k string) string { return os.Getenv(k) }

type catDef struct {
	id, ru, en, dru, den string
	paths                []string
	older                time.Duration
	clean, admin         bool
	nru, nen             string
	page                 string
}

func catDefs() []catDef {
	la, sr, pd := env("LOCALAPPDATA"), env("SystemRoot"), env("ProgramData")
	prof := env("USERPROFILE")
	chromium := func(base string) []string {
		return []string{
			filepath.Join(base, `User Data\*\Cache`), filepath.Join(base, `User Data\*\Code Cache`),
			filepath.Join(base, `User Data\*\GPUCache`), filepath.Join(base, `User Data\*\Service Worker\CacheStorage`),
			filepath.Join(base, `User Data\ShaderCache`), filepath.Join(base, `User Data\GrShaderCache`),
		}
	}
	var browsers []string
	for _, b := range []string{`Google\Chrome`, `Microsoft\Edge`, `Yandex\YandexBrowser`, `BraveSoftware\Brave-Browser`, `Vivaldi`, `Chromium`} {
		browsers = append(browsers, chromium(filepath.Join(la, b))...)
	}
	browsers = append(browsers,
		filepath.Join(la, `Mozilla\Firefox\Profiles\*\cache2`),
		filepath.Join(la, `Opera Software\Opera Stable\Cache`), filepath.Join(la, `Opera Software\Opera GX Stable\Cache`))

	return []catDef{
		{id: "user_temp", ru: "Временные файлы пользователя", en: "User temp files",
			dru: "Папка %TEMP%: остатки установщиков и программ", den: "%TEMP% folder: leftovers from installers and apps",
			paths: []string{env("TEMP")}, older: 24 * time.Hour, clean: true},
		{id: "win_temp", ru: "Временные файлы Windows", en: "Windows temp files",
			dru: "C:\\Windows\\Temp", den: "C:\\Windows\\Temp",
			paths: []string{filepath.Join(sr, "Temp")}, older: 24 * time.Hour, clean: true, admin: true},
		{id: "browser_cache", ru: "Кэш браузеров", en: "Browser caches",
			dru: "Chrome, Edge, Яндекс, Firefox, Opera, Brave — только кэш; пароли, вкладки и история не затрагиваются", den: "Chrome, Edge, Yandex, Firefox, Opera, Brave — cache only; passwords, tabs and history are untouched",
			paths: browsers, clean: true, nru: "Закройте браузеры перед очисткой — открытые файлы будут пропущены", nen: "Close browsers first — files in use are skipped"},
		{id: "wu_download", ru: "Загрузки Центра обновления", en: "Windows Update downloads",
			dru: "Скачанные установщики обновлений — Windows перекачает нужное", den: "Downloaded update packages — Windows re-downloads what it needs",
			paths: []string{filepath.Join(sr, `SoftwareDistribution\Download`)}, older: 72 * time.Hour, clean: true, admin: true},
		{id: "delivery_opt", ru: "Кэш оптимизации доставки", en: "Delivery Optimization cache",
			dru: "Обновления, которыми Windows делится с другими ПК", den: "Updates Windows shares with other PCs",
			paths: []string{filepath.Join(sr, `ServiceProfiles\NetworkService\AppData\Local\Microsoft\Windows\DeliveryOptimization\Cache`),
				filepath.Join(sr, `SoftwareDistribution\DeliveryOptimization`)}, clean: true, admin: true},
		{id: "wer", ru: "Отчёты об ошибках", en: "Error reports",
			dru: "Архив и очередь отчётов Windows Error Reporting", den: "Windows Error Reporting archive and queue",
			paths: []string{filepath.Join(pd, `Microsoft\Windows\WER\ReportArchive`), filepath.Join(pd, `Microsoft\Windows\WER\ReportQueue`),
				filepath.Join(la, `Microsoft\Windows\WER\ReportArchive`), filepath.Join(la, `Microsoft\Windows\WER\ReportQueue`)}, clean: true},
		{id: "crash_dumps", ru: "Дампы падений программ", en: "App crash dumps",
			dru: "%LOCALAPPDATA%\\CrashDumps и полный дамп памяти MEMORY.DMP", den: "%LOCALAPPDATA%\\CrashDumps and the full MEMORY.DMP",
			paths: []string{filepath.Join(la, "CrashDumps"), filepath.Join(sr, "MEMORY.DMP")}, clean: true, admin: true,
			nru: "Малые дампы синих экранов (Minidump) не удаляются — они нужны для диагностики", nen: "Small BSOD dumps (Minidump) are kept — they are needed for diagnosis"},
		{id: "thumbs", ru: "Кэш эскизов", en: "Thumbnail cache",
			dru: "Миниатюры картинок и видео в Проводнике — пересоздаются автоматически", den: "Explorer image and video thumbnails — rebuilt automatically",
			paths: []string{filepath.Join(la, `Microsoft\Windows\Explorer\thumbcache_*.db`), filepath.Join(la, `Microsoft\Windows\Explorer\iconcache_*.db`)}, clean: true,
			nru: "Часть файлов занята Проводником — они будут пропущены", nen: "Some files are locked by Explorer and will be skipped"},
		{id: "dx_cache", ru: "Кэш шейдеров", en: "Shader caches",
			dru: "DirectX, NVIDIA, AMD — пересоздаётся при запуске игр (первый запуск может быть медленнее)", den: "DirectX, NVIDIA, AMD — rebuilt when games start (first launch may be slower)",
			paths: []string{filepath.Join(la, "D3DSCache"), filepath.Join(la, `NVIDIA\DXCache`), filepath.Join(la, `NVIDIA\GLCache`),
				filepath.Join(la, `AMD\DxCache`), filepath.Join(la, `AMD\DxcCache`), filepath.Join(la, `AMD\GLCache`)}, clean: true},
		{id: "recycle", ru: "Корзина", en: "Recycle Bin",
			dru: "Удалённые файлы на всех дисках", den: "Deleted files on all drives", clean: true},
		{id: "downloads", ru: "Папка «Загрузки»", en: "Downloads folder",
			dru: "Скачанные файлы — проверьте вручную, что больше не нужно", den: "Downloaded files — review manually what you no longer need",
			paths: []string{filepath.Join(prof, "Downloads")}, page: "open"},
		{id: "windows_old", ru: "Предыдущая версия Windows", en: "Previous Windows installation",
			dru: "C:\\Windows.old после обновления — удаляется через «Очистку диска» Windows", den: "C:\\Windows.old after an upgrade — removed with Windows Disk Cleanup",
			paths: []string{filepath.Join(filepath.VolumeName(sr)+`\`, "Windows.old")}, page: "cleanmgr"},
		{id: "hiberfil", ru: "Файл гибернации", en: "Hibernation file",
			dru: "hiberfil.sys — исчезнет, если отключить гибернацию в «Твиках»", den: "hiberfil.sys — goes away if you disable hibernation in Tweaks",
			paths: []string{filepath.Join(filepath.VolumeName(sr)+`\`, "hiberfil.sys")}, page: "tweaks"},
	}
}

func recycleSize() (size, n int64) {
	type shqrbinfo struct {
		cbSize      uint32
		_           uint32
		i64Size     int64
		i64NumItems int64
	}
	var q shqrbinfo
	q.cbSize = uint32(unsafe.Sizeof(q))
	if r, _, _ := procQueryRecycle.Call(0, uintptr(unsafe.Pointer(&q))); r == 0 {
		return q.i64Size, q.i64NumItems
	}
	return 0, 0
}

// Categories измеряет известные категории мусора и кэша.
func Categories() []Category {
	defs := catDefs()
	out := make([]Category, len(defs))
	done := make(chan struct{}, len(defs))
	for i, d := range defs {
		go func(i int, d catDef) {
			c := Category{ID: d.id, Title: i18n.Pick(d.ru, d.en), Desc: i18n.Pick(d.dru, d.den), Clean: d.clean, Admin: d.admin, Page: d.page}
			if d.nru != "" {
				c.Note = i18n.Pick(d.nru, d.nen)
			}
			if d.id == "recycle" {
				c.Size, c.Files = recycleSize()
			} else {
				c.Size, c.Files, c.Paths = measure(d.paths, d.older)
			}
			if c.Paths == nil {
				c.Paths = []string{}
			}
			out[i] = c
			done <- struct{}{}
		}(i, d)
	}
	for range defs {
		<-done
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Size > out[j].Size })
	return out
}

func psRun(script string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	return cmd.Run()
}

// Clean очищает категорию.
func Clean(id string) (CleanResult, error) {
	for _, d := range catDefs() {
		if d.id != id {
			continue
		}
		if !d.clean {
			return CleanResult{}, errors.New(i18n.Pick("эта категория очищается вручную", "this category is cleaned manually"))
		}
		if d.admin && !ctl.IsElevated() {
			return CleanResult{}, errors.New(i18n.T("нужны права администратора — нажмите «Перезапустить от имени администратора»"))
		}
		switch id {
		case "recycle":
			size, n := recycleSize()
			// SHERB_NOCONFIRMATION|SHERB_NOPROGRESSUI|SHERB_NOSOUND
			if r, _, _ := procEmptyRecycle.Call(0, 0, 0x1|0x2|0x4); r != 0 && size > 0 {
				return CleanResult{}, errors.New(i18n.Pick("не удалось очистить Корзину", "could not empty the Recycle Bin"))
			}
			return CleanResult{Removed: n, Freed: size}, nil
		case "delivery_opt":
			size, n, _ := measure(d.paths, 0)
			if psRun("Delete-DeliveryOptimizationCache -Force -ErrorAction Stop", 2*time.Minute) == nil {
				return CleanResult{Removed: n, Freed: size}, nil
			}
		}
		return cleanPaths(d.paths, d.older), nil
	}
	return CleanResult{}, errors.New("unknown category")
}

// Recycle перемещает файл или папку в Корзину (обратимо).
func Recycle(path string) error {
	if IsProtected(path) {
		return ErrProtected
	}
	return recycleRaw(path)
}

// RecycleLeftover — для остатков удалённых программ: разрешены вложенные папки
// Program Files и ProgramData, но не сами эти папки и не каталог Windows.
func RecycleLeftover(path string) error {
	if IsProtectedRoot(path) || under(path, sysEnv.systemRoot) || under(path, sysEnv.self) || len(norm(path)) <= 3 {
		return ErrProtected
	}
	return recycleRaw(path)
}

func recycleRaw(path string) error {
	if _, err := os.Lstat(path); err != nil {
		return err
	}
	type shFileOp struct {
		hwnd                  uintptr
		wFunc                 uint32
		pFrom, pTo            *uint16
		fFlags                uint16
		fAnyOperationsAborted int32
		hNameMappings         uintptr
		lpszProgressTitle     *uint16
	}
	from := windows.StringToUTF16(path)
	from = append(from, 0) // двойной нуль в конце списка
	op := shFileOp{
		wFunc: 3, // FO_DELETE
		pFrom: &from[0],
		// FOF_SILENT|FOF_NOCONFIRMATION|FOF_ALLOWUNDO|FOF_NOERRORUI|FOF_WANTNUKEWARNING
		fFlags: 0x4 | 0x10 | 0x40 | 0x400 | 0x4000,
	}
	r, _, _ := procFileOp.Call(uintptr(unsafe.Pointer(&op)))
	if r != 0 || op.fAnyOperationsAborted != 0 {
		return errors.New(i18n.Pick("не удалось переместить в Корзину (файл занят или нет доступа)", "could not move to the Recycle Bin (file in use or access denied)"))
	}
	if _, err := os.Lstat(path); err == nil {
		return errors.New(i18n.Pick("файл не удалён", "the file was not removed"))
	}
	return nil
}

// Restore возвращает файл из Корзины на исходное место (для отката).
func Restore(orig string) error {
	tok := windows.GetCurrentProcessToken()
	u, err := tok.GetTokenUser()
	if err != nil {
		return err
	}
	dir := filepath.Join(filepath.VolumeName(orig)+`\`, "$Recycle.Bin", u.User.Sid.String())
	ents, err := os.ReadDir(dir)
	if err != nil {
		return errors.New(i18n.Pick("Корзина недоступна", "Recycle Bin not accessible"))
	}
	var best string
	var bestT time.Time
	for _, e := range ents {
		if !strings.HasPrefix(e.Name(), "$I") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		ri, err := ParseRecycleInfo(b)
		if err != nil || !strings.EqualFold(ri.Path, orig) {
			continue
		}
		if ri.Deleted.After(bestT) {
			best, bestT = e.Name(), ri.Deleted
		}
	}
	if best == "" {
		return errors.New(i18n.Pick("файла нет в Корзине — возможно, её уже очистили", "the file is not in the Recycle Bin — it may have been emptied"))
	}
	if _, err := os.Lstat(orig); err == nil {
		return errors.New(i18n.Pick("на исходном месте уже есть файл с таким именем", "a file with this name already exists at the original location"))
	}
	_ = os.MkdirAll(filepath.Dir(orig), 0o755)
	if err := os.Rename(filepath.Join(dir, "$R"+best[2:]), orig); err != nil {
		return err
	}
	_ = os.Remove(filepath.Join(dir, best))
	return nil
}

// Reveal показывает файл или папку в Проводнике.
func Reveal(path string) error {
	if st, err := os.Stat(path); err == nil && st.IsDir() {
		return exec.Command("explorer.exe", path).Start()
	}
	return exec.Command("explorer.exe", "/select,", path).Start()
}

// OpenCleanmgr запускает «Очистку диска» Windows для системного диска.
func OpenCleanmgr() error {
	return exec.Command("cleanmgr.exe", "/d", filepath.VolumeName(env("SystemRoot"))).Start()
}
