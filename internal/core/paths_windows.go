//go:build windows

package core

import (
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

// documentsDir — папка «Документы» (учитывает перенос в OneDrive).
func documentsDir() string {
	if p, err := windows.KnownFolderPath(windows.FOLDERID_Documents, 0); err == nil && p != "" {
		return p
	}
	return filepath.Join(os.Getenv("USERPROFILE"), "Documents")
}
