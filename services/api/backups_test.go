package main

import (
	"testing"
	"time"
)

func TestNextBackupRunDailyInConfiguredTimezone(t *testing.T) {
	now := time.Date(2026, 9, 11, 20, 0, 0, 0, time.UTC) // 01:30 next day in Colombo
	next, err := nextBackupRun(now, "daily", "02:00", "Asia/Colombo", 0)
	if err != nil { t.Fatal(err) }
	want := time.Date(2026, 9, 11, 20, 30, 0, 0, time.UTC)
	if !next.Equal(want) { t.Fatalf("got %s, want %s", next, want) }
}

func TestNextBackupRunWeeklyMovesToNextWeekAfterCutoff(t *testing.T) {
	location, _ := time.LoadLocation("Asia/Colombo")
	now := time.Date(2026, 9, 13, 3, 0, 0, 0, location) // Sunday after 02:00
	next, err := nextBackupRun(now, "weekly", "02:00", "Asia/Colombo", 0)
	if err != nil { t.Fatal(err) }
	local := next.In(location)
	if local.Weekday() != time.Sunday || local.Hour() != 2 || local.Day() != 20 { t.Fatalf("unexpected next weekly run %s", local) }
}

func TestNextBackupRunRejectsInvalidInput(t *testing.T) {
	if _, err := nextBackupRun(time.Now(), "daily", "25:00", "Asia/Colombo", 0); err == nil { t.Fatal("invalid time accepted") }
	if _, err := nextBackupRun(time.Now(), "daily", "02:00", "Invalid/Zone", 0); err == nil { t.Fatal("invalid timezone accepted") }
}
