// Package web содержит встроенный интерфейс (HTML/CSS/JS).
package web

import (
	"embed"
	"io/fs"
)

//go:embed static
var files embed.FS

// FS — файлы интерфейса.
func FS() fs.FS {
	sub, _ := fs.Sub(files, "static")
	return sub
}
