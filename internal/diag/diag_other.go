//go:build !windows

package diag

import (
	"bufio"
	"context"
	"errors"
	"os/exec"
	"time"

	"github.com/healcore/healcoreops/internal/ctl"
	"github.com/healcore/healcoreops/internal/maintenance"
)

// Supported — журналы сбоев и безопасность есть только в Windows; сеть — везде.
func Supported() bool { return false }

func Crashes(int) (Report, error) { return Report{}, ctl.ErrUnsupported }

func BootInfo(lastBoot time.Time) (Boot, error) {
	return Boot{LastBoot: lastBoot.Format("2006-01-02T15:04:05"), Boots: []BootTime{}, Slow: []Degrader{}}, nil
}

func PendingReboot() Reboot { return Reboot{Reasons: []string{}} }

func Security() []SecItem { return []SecItem{} }

// Adapters — интерфейсы средствами Go.
func Adapters() []Adapter { return basicAdapters() }

// Traceroute — traceroute/tracepath, если установлены.
func Traceroute(host string) error {
	if !ValidHost(host) {
		return errors.New("bad host")
	}
	bin, args := "", []string{}
	if p, err := exec.LookPath("traceroute"); err == nil {
		bin, args = p, []string{"-n", "-m", "24", "-w", "1", host}
	} else if p, err := exec.LookPath("tracepath"); err == nil {
		bin, args = p, []string{"-n", host}
	} else {
		return errors.New("traceroute not installed")
	}
	return maintenance.StartJob("tracert", "traceroute "+host, func(emit func(string)) error {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		cmd := exec.CommandContext(ctx, bin, args...)
		out, err := cmd.StdoutPipe()
		if err != nil {
			return err
		}
		if err := cmd.Start(); err != nil {
			return err
		}
		sc := bufio.NewScanner(out)
		for sc.Scan() {
			emit(sc.Text())
		}
		return cmd.Wait()
	})
}
