//go:build !windows

package policy

import (
	"encoding/json"
	"os"
)

// FilePath — файл политик в Linux/macOS (изменяет только root).
const FilePath = "/etc/healcoreops/policy.json"

func load() Policy {
	b, err := os.ReadFile(FilePath)
	if err != nil {
		return FromValues(Values{}, "")
	}
	v := Values{}
	if json.Unmarshal(b, &v) != nil {
		return FromValues(Values{}, "")
	}
	return FromValues(v, FilePath)
}
