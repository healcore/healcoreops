//go:build windows

package policy

import "golang.org/x/sys/windows/registry"

// KeyPath — ключ политик.
const KeyPath = `SOFTWARE\Policies\HealCoreOps`

func load() Policy {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, KeyPath, registry.QUERY_VALUE)
	if err != nil {
		return FromValues(Values{}, "")
	}
	defer k.Close()
	names, _ := k.ReadValueNames(-1)
	v := Values{}
	for _, n := range names {
		if x, _, err := k.GetIntegerValue(n); err == nil {
			v[n] = x
			continue
		}
		if x, _, err := k.GetStringsValue(n); err == nil {
			v[n] = x
			continue
		}
		if x, _, err := k.GetStringValue(n); err == nil {
			if x == "" {
				continue // пустые строки из шаблона — не политика
			}
			v[n] = x
		}
	}
	return FromValues(v, `HKLM\`+KeyPath)
}
