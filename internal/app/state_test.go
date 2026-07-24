package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"
)

func TestFrecencyMatchesZoxideRecencyBands(t *testing.T) {
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	entry := hostActivity{Score: 8}
	tests := []struct {
		age  time.Duration
		want float64
	}{
		{30 * time.Minute, 32},
		{2 * time.Hour, 16},
		{48 * time.Hour, 4},
		{8 * 24 * time.Hour, 2},
	}
	for _, tc := range tests {
		entry.LastUsed = now.Add(-tc.age)
		if got := frecency(entry, now); got != tc.want {
			t.Fatalf("age=%s got=%v want=%v", tc.age, got, tc.want)
		}
	}
}

func TestRecordAndSortHostsByFrecency(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	if err := recordHostSuccess(path, "a@old", now.Add(-2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := recordHostSuccess(path, "b@recent:2222", now); err != nil {
			t.Fatal(err)
		}
	}
	state, err := loadState(path)
	if err != nil {
		t.Fatal(err)
	}
	got := sortHostsByFrecency([]string{"a@old", "b@recent:2222", "c@never"}, state, now)
	want := []string{"b@recent:2222", "a@old", "c@never"}
	if !slices.Equal(got, want) {
		t.Fatalf("got=%v want=%v", got, want)
	}
}

func TestConcurrentActivityUpdatesDoNotLoseScores(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	now := time.Now()
	const updates = 24
	var group sync.WaitGroup
	for range updates {
		group.Add(1)
		go func() {
			defer group.Done()
			if err := recordHostSuccess(path, "alice@example.com:2222", now); err != nil {
				t.Errorf("recordHostSuccess: %v", err)
			}
		}()
	}
	group.Wait()
	state, err := loadState(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := state.Hosts["alice@example.com:2222"].Score; got != updates {
		t.Fatalf("score=%v, want %d", got, updates)
	}
}

func TestRecordActionUsePersistsCounts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	for range 3 {
		if err := recordActionUse(path, string(actionStorage)); err != nil {
			t.Fatal(err)
		}
	}
	state, err := loadState(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := state.Actions[string(actionStorage)]; got != 3 {
		t.Fatalf("storage action count=%d, want 3", got)
	}
}

func TestLoadStateKeepsBackwardCompatibilityAndStructuredHardware(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	legacy := `{"version":1,"hosts":{"alice@example.com":{"score":2,"last_used":"2026-07-23T12:00:00Z","memory":"16 GiB","disk":"5 / 20 GiB"}}}`
	if err := os.WriteFile(path, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	state, err := loadState(path)
	if err != nil {
		t.Fatal(err)
	}
	entry := state.Hosts["alice@example.com"]
	if entry.Memory != "16 GiB" || entry.Disk != "5 / 20 GiB" || state.Actions == nil {
		t.Fatalf("legacy state changed during load: %#v", state)
	}

	entry.Memory = "17.2 GB"
	entry.GPUs = []string{"Example GPU"}
	entry.Disks = []diskUsage{{
		Filesystem: "/dev/root", Mountpoint: "/",
		UsedBytes: 5_000_000_000, AvailableBytes: 15_000_000_000, TotalBytes: 20_000_000_000,
	}}
	state.Hosts["alice@example.com"] = entry
	if err := saveState(path, state); err != nil {
		t.Fatal(err)
	}
	reloaded, err := loadState(path)
	if err != nil {
		t.Fatal(err)
	}
	got := reloaded.Hosts["alice@example.com"]
	if got.Memory != "17.2 GB" || !slices.Equal(got.GPUs, []string{"Example GPU"}) ||
		len(got.Disks) != 1 || got.Disks[0].Mountpoint != "/" {
		t.Fatalf("structured hardware did not round-trip: %#v", got)
	}
}

func TestLoadStateFiltersLegacySystemMountNoise(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	state := nexusState{Version: 1, Hosts: map[string]hostActivity{
		"alice@example.com": {
			Disks: []diskUsage{
				{Filesystem: "tmpfs", Mountpoint: "/run", TotalBytes: 10},
				{Filesystem: "/dev/nvme0n1p2", Mountpoint: "/", UsedBytes: 50_000_000_000, TotalBytes: 100_000_000_000},
				{Filesystem: "/dev/loop0", Mountpoint: "/snap/core/1", TotalBytes: 10},
			},
		},
	}}
	if err := saveState(path, state); err != nil {
		t.Fatal(err)
	}
	got, err := loadState(path)
	if err != nil {
		t.Fatal(err)
	}
	entry := got.Hosts["alice@example.com"]
	if len(entry.Disks) != 1 || entry.Disks[0].Mountpoint != "/" || entry.Disk != "50 GB / 100 GB (50%)" {
		t.Fatalf("filtered state=%#v", entry)
	}
}

func TestLatestOperationPersistsSummaryWithoutSessionOutput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	started := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	want := operationSummary{
		Action:     string(actionStorage),
		Label:      "Storage",
		Host:       "alice@example.com:2222",
		Status:     "success",
		Summary:    "12 filesystems inspected",
		StartedAt:  started,
		FinishedAt: started.Add(750 * time.Millisecond),
		Duration:   750 * time.Millisecond,
	}
	if err := recordLatestOperation(path, want); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	latest, ok := document["latest_operation"].(map[string]any)
	if !ok {
		t.Fatalf("latest operation missing from state: %s", raw)
	}
	if _, exists := latest["output"]; exists {
		t.Fatalf("remote output must remain session-only: %s", raw)
	}
	state, err := loadState(path)
	if err != nil {
		t.Fatal(err)
	}
	if state.LatestOperation == nil || *state.LatestOperation != want {
		t.Fatalf("latest operation did not round-trip: %#v", state.LatestOperation)
	}
}

func TestRunningOperationIsNotPersisted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := recordLatestOperation(path, operationSummary{Label: "Check host", Status: "running"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("running operation unexpectedly created state file: %v", err)
	}
}

func TestOlderAsyncOperationCannotReplaceNewerSummary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	now := time.Now()
	newer := operationSummary{
		Label: "Storage", Status: "success", StartedAt: now,
		FinishedAt: now.Add(time.Second),
	}
	older := operationSummary{
		Label: "Refresh info", Status: "success", StartedAt: now.Add(-time.Minute),
		FinishedAt: now.Add(2 * time.Second),
	}
	if err := recordLatestOperation(path, newer); err != nil {
		t.Fatal(err)
	}
	if err := recordLatestOperation(path, older); err != nil {
		t.Fatal(err)
	}
	state, err := loadState(path)
	if err != nil {
		t.Fatal(err)
	}
	if state.LatestOperation == nil || state.LatestOperation.Label != newer.Label {
		t.Fatalf("older completion replaced newer operation: %#v", state.LatestOperation)
	}
}
