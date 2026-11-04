//go:build windows

package sysadmin

import (
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/healcore/healcoreops/internal/i18n"
	"github.com/healcore/healcoreops/internal/winps"
)

const tasksScript = `
$s = New-Object -ComObject Schedule.Service; $s.Connect()
$out = New-Object System.Collections.ArrayList
function T($d){ if($d -and $d.Year -gt 2000){ $d.ToString('yyyy-MM-ddTHH:mm:ss') } else { '' } }
function Walk($f){
  try { $tasks = $f.GetTasks(1) } catch { $tasks = @() }
  foreach($t in $tasks){
    $a = ''; $au = ''; $ds = ''; $tr = @()
    try {
      $d = $t.Definition
      $acts = @(); foreach($x in $d.Actions){ if($x.Type -eq 0){ $acts += ($x.Path + ' ' + $x.Arguments).Trim() } else { $acts += 'COM' } }
      $a = $acts -join '; '
      $au = [string]$d.RegistrationInfo.Author; $ds = [string]$d.RegistrationInfo.Description
      foreach($g in $d.Triggers){ $tr += [int]$g.Type }
    } catch {}
    [void]$out.Add([pscustomobject]@{ p=$t.Path; n=$t.Name; e=[bool]$t.Enabled; s=[int]$t.State; l=(T $t.LastRunTime); x=(T $t.NextRunTime); r=[int64]$t.LastTaskResult; a=$a; au=$au; ds=$ds; tr=$tr })
  }
  try { foreach($sub in $f.GetFolders(0)){ Walk $sub } } catch {}
}
Walk ($s.GetFolder('\'))
ConvertTo-Json -InputObject @($out) -Compress -Depth 3
`

type rawTask struct {
	P  string `json:"p"`
	N  string `json:"n"`
	E  bool   `json:"e"`
	S  int    `json:"s"`
	L  string `json:"l"`
	X  string `json:"x"`
	R  int64  `json:"r"`
	A  string `json:"a"`
	Au string `json:"au"`
	Ds string `json:"ds"`
	Tr []int  `json:"tr"`
}

// Tasks возвращает задачи планировщика.
func Tasks() ([]Task, error) {
	var raw []rawTask
	if err := winps.JSON(tasksScript, 90*time.Second, &raw); err != nil {
		return nil, err
	}
	out := make([]Task, 0, len(raw))
	for _, r := range raw {
		folder := r.P[:strings.LastIndex(r.P, `\`)+1]
		desc := r.Ds
		if strings.HasPrefix(desc, "$(@") || strings.HasPrefix(desc, "@") {
			desc = ""
		}
		au := r.Au
		if strings.HasPrefix(au, "$(@") || strings.HasPrefix(au, "@") {
			au = "Microsoft"
		}
		out = append(out, Task{
			Path: r.P, Name: r.N, Folder: folder, Enabled: r.E, State: r.S, LastRun: r.L, NextRun: r.X, Result: r.R,
			Action: r.A, Author: au, Desc: desc, Triggers: r.Tr,
			Windows: strings.HasPrefix(strings.ToLower(folder), `\microsoft\`),
		})
		if out[len(out)-1].Triggers == nil {
			out[len(out)-1].Triggers = []int{}
		}
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Path) < strings.ToLower(out[j].Path) })
	return out, nil
}

func taskRef(path string) (string, string, error) {
	if !strings.HasPrefix(path, `\`) || strings.ContainsAny(path, "\r\n") {
		return "", "", errors.New("bad task path")
	}
	i := strings.LastIndex(path, `\`)
	folder, name := path[:i], path[i+1:]
	if folder == "" {
		folder = `\`
	}
	return folder, name, nil
}

func friendlyTaskErr(err error) error {
	if err == nil {
		return nil
	}
	if strings.Contains(err.Error(), "0x80070005") || strings.Contains(strings.ToLower(err.Error()), "access") || strings.Contains(err.Error(), "Отказано") {
		return errors.New(i18n.T("нужны права администратора — нажмите «Перезапустить от имени администратора»"))
	}
	return err
}

// SetTaskEnabled включает или отключает задачу.
func SetTaskEnabled(path string, on bool) error {
	folder, name, err := taskRef(path)
	if err != nil {
		return err
	}
	v := "$false"
	if on {
		v = "$true"
	}
	script := `$ErrorActionPreference='Stop'
$s = New-Object -ComObject Schedule.Service; $s.Connect()
$t = $s.GetFolder(` + winps.Quote(folder) + `).GetTask(` + winps.Quote(name) + `)
$t.Enabled = ` + v
	_, err = winps.Run(script, 30*time.Second)
	return friendlyTaskErr(err)
}

// RunTask запускает задачу сейчас.
func RunTask(path string) error {
	folder, name, err := taskRef(path)
	if err != nil {
		return err
	}
	script := `$ErrorActionPreference='Stop'
$s = New-Object -ComObject Schedule.Service; $s.Connect()
[void]$s.GetFolder(` + winps.Quote(folder) + `).GetTask(` + winps.Quote(name) + `).Run($null)`
	_, err = winps.Run(script, 30*time.Second)
	return friendlyTaskErr(err)
}
