package winupdate

import (
	"testing"
	"time"
)

func TestPauseTimes(t *testing.T) {
	now := time.Date(2026, 11, 4, 20, 0, 0, 0, time.UTC)
	m := PauseTimes(now, 99)
	if m["PauseUpdatesExpiryTime"] != "2026-12-09T20:00:00Z" { // не больше 35 дней
		t.Fatal(m["PauseUpdatesExpiryTime"])
	}
	if len(m) != len(PauseKeys) {
		t.Fatal(len(m))
	}
	if !pausedUntil(m["PauseUpdatesExpiryTime"], now).Paused || pausedUntil("2020-01-01T00:00:00Z", now).Paused || pausedUntil("junk", now).Paused {
		t.Fatal("pausedUntil")
	}
}

func TestHint(t *testing.T) {
	if Hint("0x80070643") == "" || Hint("0X800f081f") == "" || Hint("0x12345678") != "" {
		t.Fatal("hint")
	}
}
