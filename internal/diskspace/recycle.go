package diskspace

import (
	"encoding/binary"
	"errors"
	"time"
	"unicode/utf16"
)

// RecycleInfo — содержимое служебного файла $I… из Корзины Windows:
// исходный путь удалённого файла, размер и время удаления.
type RecycleInfo struct {
	Path    string
	Size    int64
	Deleted time.Time
}

// ParseRecycleInfo разбирает файл $I (форматы Windows Vista–8.1 и 10/11).
func ParseRecycleInfo(b []byte) (RecycleInfo, error) {
	var ri RecycleInfo
	if len(b) < 24 {
		return ri, errors.New("short $I file")
	}
	ver := binary.LittleEndian.Uint64(b[0:8])
	ri.Size = int64(binary.LittleEndian.Uint64(b[8:16]))
	ft := int64(binary.LittleEndian.Uint64(b[16:24]))
	// FILETIME: сотни наносекунд с 1601-01-01
	ri.Deleted = time.Unix(0, (ft-116444736000000000)*100)
	var raw []byte
	switch ver {
	case 1:
		if len(b) < 24+520 {
			return ri, errors.New("short v1 $I")
		}
		raw = b[24 : 24+520]
	case 2:
		if len(b) < 28 {
			return ri, errors.New("short v2 $I")
		}
		n := int(binary.LittleEndian.Uint32(b[24:28]))
		if n <= 0 || 28+2*n > len(b) {
			return ri, errors.New("bad v2 length")
		}
		raw = b[28 : 28+2*n]
	default:
		return ri, errors.New("unknown $I version")
	}
	u := make([]uint16, 0, len(raw)/2)
	for i := 0; i+1 < len(raw); i += 2 {
		c := binary.LittleEndian.Uint16(raw[i:])
		if c == 0 {
			break
		}
		u = append(u, c)
	}
	ri.Path = string(utf16.Decode(u))
	if ri.Path == "" {
		return ri, errors.New("empty path")
	}
	return ri, nil
}
