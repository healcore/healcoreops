//go:build !windows

package core

import (
	"os"
	"path/filepath"
)

// documentsDir — ~/Documents, если есть, иначе домашняя папка.
func documentsDir() string {
	home, _ := os.UserHomeDir()
	d := filepath.Join(home, "Documents")
	if st, err := os.Stat(d); err == nil && st.IsDir() {
		return d
	}
	return home
}
