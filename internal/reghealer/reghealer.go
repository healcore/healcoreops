// Package reghealer — осторожная проверка реестра: ищет только заведомо
// «висячие» записи, ссылающиеся на несуществующие файлы (автозапуск,
// пути приложений, записи удаления). Перед удалением всегда делается
// резервная копия (.reg), чтобы изменения можно было откатить.
package reghealer

// Issue — найденная проблемная запись реестра.
type Issue struct {
	ID       string `json:"id"`
	Category string `json:"category"` // startup | apppaths | uninstall
	Title    string `json:"title"`    // что это
	Detail   string `json:"detail"`   // на что ссылается (отсутствует)
	Location string `json:"location"` // путь в реестре
}

// FixReport — итог исправления.
type FixReport struct {
	Removed int      `json:"removed"`
	Failed  []string `json:"failed"`
	Backup  string   `json:"backup"` // путь к папке с резервными копиями
}
