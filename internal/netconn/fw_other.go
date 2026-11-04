//go:build !windows

package netconn

import "errors"

// Firewall — проверка по правилам брандмауэра доступна только в Windows.
func Firewall([]Port) ([]FwVerdict, error) { return nil, errors.New("windows only") }
