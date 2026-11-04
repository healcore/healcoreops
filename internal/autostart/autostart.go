// Package autostart — управление программами, запускаемыми вместе с Windows:
// ключи реестра Run (HKCU/HKLM, в т.ч. 32-битная ветвь) и папки «Автозагрузка».
package autostart

// Item — одна запись автозагрузки.
type Item struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Command  string `json:"command"`
	Location string `json:"location"` // откуда запускается (для подсказки)
	Scope    string `json:"scope"`    // user | machine
	Enabled  bool   `json:"enabled"`
	Admin    bool   `json:"admin"` // для переключения нужны права администратора
}
