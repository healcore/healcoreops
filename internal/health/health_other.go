//go:build !windows

package health

// Supported — мониторинг железа реализован только для Windows.
func Supported() bool { return false }

// Start ничего не делает вне Windows.
func Start() {}

// CollectNow вне Windows возвращает пустой снимок.
func CollectNow() Snapshot { return Get() }
