package main

import (
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
