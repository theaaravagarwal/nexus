package app

import (
	"testing"
	"time"
)

func TestFleetTelemetryPoolSyncReuseIdleSession(t *testing.T) {
	pool := newFleetTelemetryPool()
	defer pool.close()

	// Create initial session
	pool.sync([]fleetTelemetrySpec{
		{Target: "host1", Interval: 5 * time.Second},
	})

	// Get the session pointer
	pool.mu.Lock()
	session1, exists1 := pool.sessions["host1"]
	pool.mu.Unlock()
	if !exists1 {
		t.Fatal("expected active session for host1")
	}
	originalGeneration := session1.generation

	// Remove from desired list to move to idle
	pool.sync([]fleetTelemetrySpec{})

	// Check that session moved to idle
	pool.mu.Lock()
	_, activeExists := pool.sessions["host1"]
	_, idleExists := pool.idleSessions["host1"]
	pool.mu.Unlock()

	if activeExists {
		t.Fatal("session should have moved to idle")
	}
	if !idleExists {
		t.Fatal("session should be in idle pool")
	}

	// Re-add within grace period - should reuse same session
	pool.sync([]fleetTelemetrySpec{
		{Target: "host1", Interval: 5 * time.Second},
	})

	pool.mu.Lock()
	session2, exists2 := pool.sessions["host1"]
	pool.mu.Unlock()

	if !exists2 {
		t.Fatal("expected session to be restored from idle")
	}

	// Verify same generation was reused
	if session2.generation != originalGeneration {
		t.Fatalf("expected generation %d, got %d (session was not reused)", originalGeneration, session2.generation)
	}
}

func TestFleetTelemetryPoolSyncCancelAfterGracePeriod(t *testing.T) {
	pool := newFleetTelemetryPool()
	defer pool.close()

	// Create initial session
	pool.sync([]fleetTelemetrySpec{
		{Target: "host1", Interval: 5 * time.Second},
	})

	pool.mu.Lock()
	_, _ = pool.sessions["host1"]
	pool.mu.Unlock()

	// Remove from desired list to move to idle
	pool.sync([]fleetTelemetrySpec{})

	// Manually expire the grace period by setting it in the past
	pool.mu.Lock()
	if idleSession, ok := pool.idleSessions["host1"]; ok {
		idleSession.graceUntil = time.Now().Add(-1 * time.Second)
	}
	pool.mu.Unlock()

	// Sync again - should cancel the idle session
	pool.sync([]fleetTelemetrySpec{})

	pool.mu.Lock()
	_, idleExists := pool.idleSessions["host1"]
	pool.mu.Unlock()

	if idleExists {
		t.Fatal("idle session should have been cancelled after grace period expired")
	}
}

func TestFleetTelemetryPoolSyncRespectsBoundedSessions(t *testing.T) {
	pool := newFleetTelemetryPool()
	defer pool.close()

	// Create maximum allowed sessions (fleetTelemetryMaxStreams = 12)
	specs := make([]fleetTelemetrySpec, fleetTelemetryMaxStreams)
	for i := 0; i < fleetTelemetryMaxStreams; i++ {
		specs[i] = fleetTelemetrySpec{Target: "host" + string(rune('0'+i)), Interval: 5 * time.Second}
	}
	pool.sync(specs)

	pool.mu.Lock()
	sessionCount := len(pool.sessions)
	pool.mu.Unlock()

	if sessionCount != fleetTelemetryMaxStreams {
		t.Fatalf("expected %d sessions, got %d", fleetTelemetryMaxStreams, sessionCount)
	}

	// Try to add more - should be limited by max + grace slots
	extraSpecs := make([]fleetTelemetrySpec, fleetTelemetryMaxStreams+10)
	for i := 0; i < fleetTelemetryMaxStreams+10; i++ {
		extraSpecs[i] = fleetTelemetrySpec{Target: "extra" + string(rune('a'+rune(i%26))), Interval: 5 * time.Second}
	}
	pool.sync(extraSpecs)

	pool.mu.Lock()
	totalSessions := len(pool.sessions) + len(pool.idleSessions)
	pool.mu.Unlock()

	if totalSessions > fleetTelemetryMaxStreams+4 {
		t.Fatalf("expected at most %d total sessions, got %d", fleetTelemetryMaxStreams+4, totalSessions)
	}
}
