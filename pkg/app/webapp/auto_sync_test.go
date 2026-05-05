package webapp

import (
	"testing"
	"time"
)

func TestAutoSyncDueUsesWallClockElapsed(t *testing.T) {
	lastRun := time.Date(2026, 5, 4, 21, 13, 24, 0, time.FixedZone("CST", 8*60*60))

	if autoSyncDue(lastRun, lastRun.Add(23*time.Hour+59*time.Minute), 24*time.Hour) {
		t.Fatal("expected sync not to be due before interval")
	}
	if !autoSyncDue(lastRun, lastRun.Add(24*time.Hour), 24*time.Hour) {
		t.Fatal("expected sync to be due once wall clock reaches interval")
	}
	if !autoSyncDue(lastRun, lastRun.Add(26*time.Hour), 24*time.Hour) {
		t.Fatal("expected sync to be due after missed interval")
	}
}

func TestAutoSyncPollInterval(t *testing.T) {
	if got := autoSyncPollInterval(24 * time.Hour); got != time.Minute {
		t.Fatalf("24h poll interval=%s, want 1m", got)
	}
	if got := autoSyncPollInterval(30 * time.Second); got != 30*time.Second {
		t.Fatalf("30s poll interval=%s, want 30s", got)
	}
}
