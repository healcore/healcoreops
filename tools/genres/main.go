// genres создаёт ресурсы Windows (.syso): иконку, манифест (DPI, права) и сведения о версии.
//
//	go run ./tools/genres -icon Icon.png -out rsrc_windows_amd64.syso -desc "HealCoreOps"
package main

import (
	"flag"
	"image/png"
	"log"
	"os"

	"github.com/tc-hib/winres"
	"github.com/tc-hib/winres/version"
)

func main() {
	iconPath := flag.String("icon", "Icon.png", "PNG-иконка (желательно 256×256 и больше)")
	out := flag.String("out", "rsrc_windows_amd64.syso", "выходной файл")
	desc := flag.String("desc", "HealCoreOps", "описание файла")
	name := flag.String("name", "HealCoreOps.exe", "исходное имя файла")
	ver := flag.String("version", "1.1.0", "версия")
	flag.Parse()

	f, err := os.Open(*iconPath)
	if err != nil {
		log.Fatal(err)
	}
	img, err := png.Decode(f)
	f.Close()
	if err != nil {
		log.Fatal(err)
	}

	rs := winres.ResourceSet{}
	icon, err := winres.NewIconFromResizedImage(img, []int{256, 64, 48, 32, 24, 16})
	if err != nil {
		log.Fatal(err)
	}
	if err := rs.SetIcon(winres.ID(1), icon); err != nil {
		log.Fatal(err)
	}
	rs.SetManifest(winres.AppManifest{
		Description:         *desc,
		ExecutionLevel:      winres.AsInvoker,
		DPIAwareness:        winres.DPIPerMonitorV2,
		UseCommonControlsV6: true,
		LongPathAware:       true,
		Compatibility:       winres.Win10AndAbove,
	})

	var vi version.Info
	vi.SetFileVersion(*ver + ".0")
	vi.SetProductVersion(*ver + ".0")
	for k, v := range map[string]string{
		version.ProductName:      "HealCoreOps",
		version.FileDescription:  *desc,
		version.CompanyName:      "HealCoreOps",
		version.LegalCopyright:   "© 2026 HealCoreOps",
		version.OriginalFilename: *name,
		version.InternalName:     "HealCoreOps",
	} {
		_ = vi.Set(version.LangDefault, k, v)
	}
	rs.SetVersionInfo(vi)

	o, err := os.Create(*out)
	if err != nil {
		log.Fatal(err)
	}
	defer o.Close()
	if err := rs.WriteObject(o, winres.ArchAMD64); err != nil {
		log.Fatal(err)
	}
}
