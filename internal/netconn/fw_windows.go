//go:build windows

package netconn

import (
	"time"

	"golang.org/x/sys/windows/registry"

	"github.com/healcore/healcoreops/internal/winps"
)

const fwScript = `$ErrorActionPreference='SilentlyContinue'
$on = [bool](Get-NetFirewallProfile | Where-Object { $_.Enabled -eq 'True' -or $_.Enabled -eq $true })
$ids = @{}
foreach($r in (Get-NetFirewallRule -Direction Inbound -Enabled True -Action Allow)){ $ids[$r.InstanceID] = $r.DisplayName }
$apps = @{}
foreach($a in (Get-NetFirewallApplicationFilter -All)){ if($ids.ContainsKey($a.InstanceID)){ $apps[$a.InstanceID] = [string]$a.Program } }
$out = foreach($p in (Get-NetFirewallPortFilter -All)){
  if(-not $ids.ContainsKey($p.InstanceID)){ continue }
  $app = $apps[$p.InstanceID]; if(-not $app){ $app = 'Any' }
  [pscustomobject]@{ n = $ids[$p.InstanceID]; proto = [string]$p.Protocol; port = [string](@($p.LocalPort) -join ','); app = $app }
}
@{ on = $on; rules = @($out) } | ConvertTo-Json -Depth 3 -Compress`

// Firewall проверяет порты по правилам брандмауэра Windows (5–30 с на больших наборах правил).
func Firewall(ports []Port) ([]FwVerdict, error) {
	var res struct {
		On    bool     `json:"on"`
		Rules []FwRule `json:"rules"`
	}
	if err := winps.JSON(fwScript, 90*time.Second, &res); err != nil {
		return nil, err
	}
	out := make([]FwVerdict, 0, len(ports))
	for _, p := range ports {
		if p.Exposure == "local" {
			continue
		}
		out = append(out, MatchFirewall(p, res.Rules, res.On, expandWin))
	}
	return out, nil
}

func expandWin(s string) string {
	if v, err := registry.ExpandString(s); err == nil {
		return v
	}
	return s
}
