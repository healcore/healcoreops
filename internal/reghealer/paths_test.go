package reghealer

import (
	"strings"
	"testing"
)

func fakeFS(files ...string) Resolver {
	set := map[string]bool{`c:\`: true}
	for _, f := range files {
		set[strings.ToLower(f)] = true
	}
	return Resolver{
		Exists: func(p string) bool { return set[strings.ToLower(strings.ReplaceAll(p, "/", `\`))] },
		Expand: func(s string) string {
			r := strings.NewReplacer("%ProgramFiles%", `C:\Program Files`, "%SystemRoot%", `C:\Windows`,
				"%windir%", `C:\Windows`, "%LOCALAPPDATA%", `C:\Users\u\AppData\Local`)
			return r.Replace(s)
		},
	}
}

func TestMissing(t *testing.T) {
	r := fakeFS(
		`C:\Program Files (x86)\Steam\steam.exe`,
		`C:\Program Files\Realtek\Audio\RtkAudUService64.exe`,
		`C:\Windows\SysWOW64\OneDriveSetup.exe`,
		`C:\Users\u\AppData\Local\Discord\Update.exe`,
		`C:\Tools\app.exe`,
		`C:\Program Files\NVIDIA Corporation\Display.NvContainer\NVDisplay.Container.exe`,
		`C:\Program Files\Some App\uninst.exe`,
	)
	cases := []struct {
		cmd, want string
	}{
		// Раньше давали ложное «не найден» — путь без кавычек с пробелами:
		{`C:\Program Files (x86)\Steam\steam.exe -silent`, ""},
		{`C:\Program Files\Realtek\Audio\RtkAudUService64.exe -background`, ""},
		{`C:\Program Files\NVIDIA Corporation\Display.NvContainer\NVDisplay.Container.exe -s NVDisplay.ContainerLocalSystem`, ""},
		{`C:\Program Files\Some App\uninst.exe /S`, ""},
		// В кавычках, с аргументами:
		{`"C:\Users\u\AppData\Local\Discord\Update.exe" --processStart Discord.exe`, ""},
		// Переменные окружения:
		{`%LOCALAPPDATA%\Discord\Update.exe --processStart Discord.exe`, ""},
		// WOW64: 32-битная запись про System32 на деле лежит в SysWOW64:
		{`C:\Windows\System32\OneDriveSetup.exe /thfirstsetup`, ""},
		// Program Files ↔ Program Files (x86):
		{`"C:\Program Files\Steam\steam.exe"`, ""},
		// Без расширения:
		{`C:\Tools\app`, ""},
		// Префикс \??\ и хвост ,0:
		{`\??\C:\Tools\app.exe`, ""},
		{`"C:\Tools\app.exe",0`, ""},
		// Системные загрузчики и относительные пути не трогаем:
		{`rundll32.exe C:\gone\x.dll,Run`, ""},
		{`MsiExec.exe /X{1234}`, ""},
		{`ctfmon`, ""},
		// Отключённый диск — не трогаем:
		{`"E:\Portable\tool.exe"`, ""},
		// Нераскрытая переменная — не трогаем:
		{`%UNKNOWN%\x.exe`, ""},
		// Действительно отсутствующие:
		{`"C:\Program Files\Gone\gone.exe" /min`, `C:\Program Files\Gone\gone.exe`},
		{`C:\Program Files\Gone App\gone.exe -tray`, `C:\Program Files\Gone App\gone.exe`},
		{`C:\OldTool\old.exe`, `C:\OldTool\old.exe`},
	}
	for _, c := range cases {
		if got := r.Missing(c.cmd); got != c.want {
			t.Errorf("Missing(%q) = %q, want %q", c.cmd, got, c.want)
		}
	}
}
