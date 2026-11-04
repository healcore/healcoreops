//go:build !windows

package health

// Supported — мониторинг железа реализован только для Windows.
func Supported() bool { return false }

// Start ничего не делает вне Windows.
func Start() {}
