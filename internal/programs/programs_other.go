//go:build !windows

package programs

import "github.com/healcore/healcoreops/internal/ctl"

// Supported — менеджер программ есть только в Windows.
func Supported() bool { return false }

// OnRemoved вызывается после удаления программы.
var OnRemoved func(name string)

func List() ([]Program, error)                         { return nil, ctl.ErrUnsupported }
func Uninstall([]string, bool) error                   { return ctl.ErrUnsupported }
func Leftovers(string) ([]Leftover, error)             { return nil, ctl.ErrUnsupported }
func RemoveLeftovers([]string) (int, string, []string) { return 0, "", []string{} }
func WingetAvailable() bool                            { return false }
func Upgrades() ([]Upgrade, error)                     { return nil, ctl.ErrUnsupported }
func UpgradeApps([]string) error                       { return ctl.ErrUnsupported }
