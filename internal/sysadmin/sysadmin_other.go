//go:build !windows

package sysadmin

import "github.com/healcore/healcoreops/internal/ctl"

// Supported — службы и задачи есть только в Windows.
func Supported() bool { return false }

func Services() ([]Service, error)         { return nil, ctl.ErrUnsupported }
func ServiceAction(string, string) error   { return ctl.ErrUnsupported }
func ServiceStart(string) (string, error)  { return "", ctl.ErrUnsupported }
func SetServiceStart(string, string) error { return ctl.ErrUnsupported }
func Tasks() ([]Task, error)               { return nil, ctl.ErrUnsupported }
func SetTaskEnabled(string, bool) error    { return ctl.ErrUnsupported }
func RunTask(string) error                 { return ctl.ErrUnsupported }
