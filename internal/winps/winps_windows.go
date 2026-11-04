//go:build windows

// Package winps запускает скрипты PowerShell скрыто, с тайм-аутом и выводом в UTF-8.
package winps

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os/exec"
	"strings"
	"syscall"
	"time"
	"unicode/utf16"

	"github.com/healcore/healcoreops/internal/i18n"
)

func encoded(script string) string {
	full := "$ProgressPreference='SilentlyContinue'\n$ErrorActionPreference='Continue'\n[Console]::OutputEncoding=[Text.Encoding]::UTF8\n" + script
	u := utf16.Encode([]rune(full))
	b := make([]byte, len(u)*2)
	for i, v := range u {
		b[2*i], b[2*i+1] = byte(v), byte(v>>8)
	}
	return base64.StdEncoding.EncodeToString(b)
}

func command(ctx context.Context, script string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass",
		"-EncodedCommand", encoded(script))
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	return cmd
}

// Stream выполняет скрипт и передаёт строки вывода по мере появления.
// В скрипте для немедленного вывода пишите [Console]::Out.WriteLine(...).
func Stream(script string, timeout time.Duration, line func(string)) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := command(ctx, script)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	sc := bufio.NewScanner(out)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		if l := strings.TrimRight(sc.Text(), "\r "); l != "" {
			line(l)
		}
	}
	err = cmd.Wait()
	if ctx.Err() == context.DeadlineExceeded {
		return errors.New(i18n.T("превышено время ожидания"))
	}
	if err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			if i := strings.IndexByte(msg, '\n'); i > 0 {
				msg = strings.TrimSpace(msg[:i])
			}
			return errors.New(msg)
		}
	}
	return err
}

// Run выполняет скрипт и возвращает stdout.
func Run(script string, timeout time.Duration) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := command(ctx, script)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if ctx.Err() == context.DeadlineExceeded {
		return out, errors.New(i18n.T("превышено время ожидания"))
	}
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if i := strings.IndexByte(msg, '\n'); i > 0 {
			msg = strings.TrimSpace(msg[:i])
		}
		if msg != "" {
			return out, errors.New(msg)
		}
	}
	return out, err
}

// JSON выполняет скрипт, вывод которого — JSON, и разбирает его в v.
// Одиночный объект от ConvertTo-Json тоже принимается там, где ждут массив.
func JSON(script string, timeout time.Duration, v any) error {
	out, err := Run(script, timeout)
	s := strings.TrimSpace(string(out))
	if s == "" {
		if err != nil {
			return err
		}
		return nil
	}
	// берём с первого символа JSON (на случай посторонних строк выше)
	if i := strings.IndexAny(s, "[{"); i > 0 {
		s = s[i:]
	}
	if e := json.Unmarshal([]byte(s), v); e != nil {
		if strings.HasPrefix(s, "{") {
			if e2 := json.Unmarshal([]byte("["+s+"]"), v); e2 == nil {
				return nil
			}
		}
		if err != nil {
			return err
		}
		return e
	}
	return nil
}

// Quote экранирует строку для PowerShell (одинарные кавычки).
func Quote(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
