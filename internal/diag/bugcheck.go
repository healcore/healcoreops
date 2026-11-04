// Package diag — диагностика: синие экраны и падения программ, ошибки
// в журналах событий, время загрузки, ожидающая перезагрузка, безопасность, сеть.
package diag

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
)

// BugCheck — расшифровка кода синего экрана.
type BugCheck struct {
	Code uint32 `json:"code"`
	Hex  string `json:"hex"`
	Name string `json:"name"`
	Hint string `json:"hint"` // наиболее частая причина и что делать
}

type bcDef struct{ name, ru, en string }

var bugchecks = map[uint32]bcDef{
	0x0A:       {"IRQL_NOT_LESS_OR_EQUAL", "Чаще всего драйвер обращается к неверной памяти. Обновите драйверы (сеть, видео, чипсет), проверьте разгон.", "Usually a driver accessing invalid memory. Update drivers (network, video, chipset), check overclocking."},
	0xD1:       {"DRIVER_IRQL_NOT_LESS_OR_EQUAL", "Ошибка драйвера — чаще сетевого, Wi-Fi или антивирусного. Обновите или переустановите драйвер.", "A driver bug — often network, Wi-Fi or antivirus. Update or reinstall the driver."},
	0x1A:       {"MEMORY_MANAGEMENT", "Проблема с оперативной памятью или драйвером. Проверьте ОЗУ (mdsched), отключите XMP/EXPO-разгон.", "RAM or driver problem. Test RAM (mdsched), disable XMP/EXPO overclock."},
	0x50:       {"PAGE_FAULT_IN_NONPAGED_AREA", "Повреждённая память, драйвер или антивирус. Проверьте ОЗУ и обновите драйверы.", "Faulty RAM, a driver or antivirus. Test RAM and update drivers."},
	0x3B:       {"SYSTEM_SERVICE_EXCEPTION", "Сбой системного вызова — часто видеодрайвер или антивирус. Обновите драйвер видеокарты, запустите SFC.", "A system call crashed — often the GPU driver or antivirus. Update the GPU driver, run SFC."},
	0x7E:       {"SYSTEM_THREAD_EXCEPTION_NOT_HANDLED", "Необработанное исключение в драйвере. Обновите драйверы, особенно недавно установленные.", "Unhandled exception in a driver. Update drivers, especially recently installed ones."},
	0x1E:       {"KMODE_EXCEPTION_NOT_HANDLED", "Исключение в драйвере ядра. Обновите драйверы, проверьте ОЗУ.", "Kernel-mode driver exception. Update drivers, test RAM."},
	0x7F:       {"UNEXPECTED_KERNEL_MODE_TRAP", "Обычно аппаратная проблема или разгон: ОЗУ, перегрев, нестабильный разгон ЦП.", "Usually hardware or overclocking: RAM, overheating, unstable CPU overclock."},
	0x9F:       {"DRIVER_POWER_STATE_FAILURE", "Драйвер не справился со сном/пробуждением. Обновите драйверы чипсета, сети и видео.", "A driver failed during sleep/wake. Update chipset, network and video drivers."},
	0xA0:       {"INTERNAL_POWER_ERROR", "Ошибка управления питанием. Обновите BIOS и драйвер чипсета.", "Power management error. Update BIOS and chipset driver."},
	0x101:      {"CLOCK_WATCHDOG_TIMEOUT", "Ядро процессора перестало отвечать — разгон, перегрев или нужен новый BIOS.", "A CPU core stopped responding — overclock, overheating, or a BIOS update is needed."},
	0x116:      {"VIDEO_TDR_FAILURE", "Сбой видеодрайвера (зависание видеокарты). Переустановите драйвер начисто, проверьте температуру и разгон видеокарты.", "GPU driver crash (GPU hang). Clean-reinstall the driver, check GPU temperature and overclock."},
	0x117:      {"VIDEO_TDR_TIMEOUT_DETECTED", "Видеокарта перестала отвечать. Обновите драйвер, проверьте питание и температуру.", "The GPU stopped responding. Update the driver, check power and temperature."},
	0x119:      {"VIDEO_SCHEDULER_INTERNAL_ERROR", "Ошибка видеодрайвера. Обновите или переустановите драйвер видеокарты.", "GPU driver error. Update or reinstall the graphics driver."},
	0x124:      {"WHEA_UNCORRECTABLE_ERROR", "Аппаратная ошибка: ЦП, ОЗУ или питание. Снимите разгон (включая XMP/EXPO), проверьте температуры и блок питания, обновите BIOS.", "Hardware error: CPU, RAM or power. Remove overclocks (incl. XMP/EXPO), check temperatures and PSU, update BIOS."},
	0x133:      {"DPC_WATCHDOG_VIOLATION", "Драйвер (чаще накопителя SATA/NVMe) завис. Обновите драйвер чипсета/накопителя и прошивку SSD.", "A driver (often SATA/NVMe storage) hung. Update chipset/storage drivers and SSD firmware."},
	0x139:      {"KERNEL_SECURITY_CHECK_FAILURE", "Повреждены данные ядра — драйвер или ОЗУ. Обновите драйверы, запустите SFC и DISM.", "Kernel data corruption — a driver or RAM. Update drivers, run SFC and DISM."},
	0x13A:      {"KERNEL_MODE_HEAP_CORRUPTION", "Драйвер повредил память ядра. Обновите драйверы, особенно видео и звук.", "A driver corrupted kernel memory. Update drivers, especially video and audio."},
	0x154:      {"UNEXPECTED_STORE_EXCEPTION", "Проблема с накопителем или его драйвером. Проверьте здоровье диска (вкладка «Обзор»), обновите прошивку SSD.", "Storage or its driver problem. Check drive health (Overview), update SSD firmware."},
	0x7A:       {"KERNEL_DATA_INPAGE_ERROR", "Не удалось прочитать данные с диска — неисправный накопитель или кабель. Проверьте SMART.", "Couldn't read data from disk — failing drive or cable. Check SMART."},
	0x77:       {"KERNEL_STACK_INPAGE_ERROR", "Ошибка чтения с диска. Проверьте накопитель и кабель.", "Disk read error. Check the drive and cable."},
	0x24:       {"NTFS_FILE_SYSTEM", "Ошибка файловой системы. Запустите chkdsk /f для системного диска.", "File system error. Run chkdsk /f on the system drive."},
	0xEF:       {"CRITICAL_PROCESS_DIED", "Завершился жизненно важный процесс Windows. Запустите SFC и DISM в «Обслуживании».", "A vital Windows process died. Run SFC and DISM in Maintenance."},
	0xF4:       {"CRITICAL_OBJECT_TERMINATION", "Завершился критический процесс — часто сбой накопителя. Проверьте диск.", "A critical process ended — often a storage failure. Check the drive."},
	0xC2:       {"BAD_POOL_CALLER", "Драйвер неправильно работает с памятью. Обновите драйверы.", "A driver misused memory. Update drivers."},
	0x19:       {"BAD_POOL_HEADER", "Повреждена память ядра — драйвер или антивирус.", "Kernel pool corruption — a driver or antivirus."},
	0xC5:       {"DRIVER_CORRUPTED_EXPOOL", "Драйвер повредил память. Обновите драйверы.", "A driver corrupted memory. Update drivers."},
	0x109:      {"CRITICAL_STRUCTURE_CORRUPTION", "Изменены структуры ядра — драйвер, ОЗУ или вредоносная программа. Проверьте ОЗУ и систему антивирусом.", "Kernel structures modified — driver, RAM or malware. Test RAM and scan for malware."},
	0x3D:       {"INTERRUPT_EXCEPTION_NOT_HANDLED", "Сбой обработки прерываний — драйвер или оборудование.", "Interrupt handling failure — a driver or hardware."},
	0x1000007E: {"SYSTEM_THREAD_EXCEPTION_NOT_HANDLED_M", "Необработанное исключение в драйвере. Обновите драйверы.", "Unhandled exception in a driver. Update drivers."},
	0x1000008E: {"KERNEL_MODE_EXCEPTION_NOT_HANDLED_M", "Исключение в драйвере ядра. Обновите драйверы, проверьте ОЗУ.", "Kernel-mode exception. Update drivers, test RAM."},
	0x10000050: {"PAGE_FAULT_IN_NONPAGED_AREA_M", "Повреждённая память или драйвер. Проверьте ОЗУ.", "Faulty RAM or a driver. Test RAM."},
	0x8E:       {"KERNEL_MODE_EXCEPTION_NOT_HANDLED", "Исключение в драйвере ядра. Обновите драйверы, проверьте ОЗУ.", "Kernel-mode exception. Update drivers, test RAM."},
	0x18B:      {"SECURE_KERNEL_ERROR", "Ошибка защищённого ядра (VBS). Обновите BIOS и драйверы.", "Secure kernel (VBS) error. Update BIOS and drivers."},
	0x9C:       {"MACHINE_CHECK_EXCEPTION", "Аппаратная ошибка процессора. Проверьте охлаждение и разгон.", "CPU hardware error. Check cooling and overclocking."},
	0xFC:       {"ATTEMPTED_EXECUTE_OF_NOEXECUTE_MEMORY", "Драйвер пытался выполнить данные. Обновите драйверы.", "A driver tried to execute data. Update drivers."},
	0xBE:       {"ATTEMPTED_WRITE_TO_READONLY_MEMORY", "Драйвер записал в защищённую память. Обновите драйверы.", "A driver wrote to read-only memory. Update drivers."},
	0x4E:       {"PFN_LIST_CORRUPT", "Повреждение списков памяти — часто неисправная ОЗУ.", "Memory list corruption — often faulty RAM."},
	0x7B:       {"INACCESSIBLE_BOOT_DEVICE", "Windows не видит загрузочный диск — режим SATA (AHCI/RAID) в BIOS или драйвер накопителя.", "Windows can't see the boot drive — SATA mode (AHCI/RAID) in BIOS or the storage driver."},
	0xEA:       {"THREAD_STUCK_IN_DEVICE_DRIVER", "Видеодрайвер завис. Обновите драйвер видеокарты.", "The video driver hung. Update the graphics driver."},
}

// DescribeBugCheck возвращает название и подсказку для кода синего экрана.
func DescribeBugCheck(code uint32, en bool) BugCheck {
	b := BugCheck{Code: code, Hex: fmt.Sprintf("0x%08X", code)}
	if d, ok := bugchecks[code]; ok && d.name != "" {
		b.Name = strings.TrimSuffix(d.name, "_M")
		b.Hint = d.ru
		if en {
			b.Hint = d.en
		}
		return b
	}
	b.Name = "BUGCHECK"
	b.Hint = "Найдите код в справочнике Microsoft «Bug Check Code Reference». Начните с обновления драйверов и BIOS."
	if en {
		b.Hint = "Look the code up in Microsoft's Bug Check Code Reference. Start by updating drivers and BIOS."
	}
	return b
}

// ParseDumpHeader читает код синего экрана и параметры из заголовка
// файла дампа памяти (Minidump/MEMORY.DMP, 64- и 32-битные).
func ParseDumpHeader(b []byte) (code uint32, params [4]uint64, err error) {
	if len(b) < 0x60 || string(b[0:4]) != "PAGE" {
		return 0, params, errors.New("not a kernel dump")
	}
	switch string(b[4:8]) {
	case "DU64":
		code = binary.LittleEndian.Uint32(b[0x38:])
		for i := 0; i < 4; i++ {
			params[i] = binary.LittleEndian.Uint64(b[0x40+8*i:])
		}
	case "DUMP":
		code = binary.LittleEndian.Uint32(b[0x28:])
		for i := 0; i < 4; i++ {
			params[i] = uint64(binary.LittleEndian.Uint32(b[0x2C+4*i:]))
		}
	default:
		return 0, params, errors.New("unknown dump format")
	}
	return code, params, nil
}

// ParseBugCheckText извлекает код из текста события 1001 («…0x00000116 (…)…»).
func ParseBugCheckText(s string) (uint32, bool) {
	i := strings.Index(strings.ToLower(s), "0x")
	for i >= 0 {
		j := i + 2
		for j < len(s) && strings.ContainsRune("0123456789abcdefABCDEF", rune(s[j])) {
			j++
		}
		if j-i-2 >= 8 && j-i-2 <= 16 {
			var v uint64
			if _, err := fmt.Sscanf(s[i+2:j], "%x", &v); err == nil {
				return uint32(v), true
			}
		}
		k := strings.Index(strings.ToLower(s[j:]), "0x")
		if k < 0 {
			break
		}
		i = j + k
	}
	return 0, false
}
