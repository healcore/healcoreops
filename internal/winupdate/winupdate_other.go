//go:build !windows

package winupdate

import "errors"

var errNo = errors.New("Windows only")

// Supported — только Windows.
func Supported() bool { return false }

func searchUpdates() ([]Update, error) { return nil, errNo }

// History — только Windows.
func History(int) ([]HistoryEntry, error) { return nil, errNo }

// Install — только Windows.
func Install([]string, func(string)) (bool, error) { return false, errNo }

// Pause — только Windows.
func Pause() PauseState { return PauseState{} }

// ReadPause — только Windows.
func ReadPause() map[string]string { return map[string]string{} }

// WritePause — только Windows.
func WritePause(map[string]string) error { return errNo }

// SetPause — только Windows.
func SetPause(int) error { return errNo }
