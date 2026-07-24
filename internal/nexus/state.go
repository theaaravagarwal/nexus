package nexus

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const frecencyMaxAge = 10000.0

const (
	stateLockTimeout = 2 * time.Second
	stateLockStale   = 30 * time.Second
)

type hostActivity struct {
	Score    float64     `json:"score"`
	LastUsed time.Time   `json:"last_used"`
	OS       string      `json:"os,omitempty"`
	CPU      string      `json:"cpu,omitempty"`
	GPUs     []string    `json:"gpus,omitempty"`
	Memory   string      `json:"memory,omitempty"`
	Disk     string      `json:"disk,omitempty"`
	Disks    []diskUsage `json:"disks,omitempty"`
	Tools    []string    `json:"tools,omitempty"`
	Updated  time.Time   `json:"updated,omitempty"`
}

type operationSummary struct {
	Action     string        `json:"action,omitempty"`
	Label      string        `json:"label"`
	Host       string        `json:"host,omitempty"`
	Status     string        `json:"status"`
	Summary    string        `json:"summary,omitempty"`
	StartedAt  time.Time     `json:"started_at,omitempty"`
	FinishedAt time.Time     `json:"finished_at,omitempty"`
	Duration   time.Duration `json:"duration,omitempty"`
}

type nexusState struct {
	Version         int                     `json:"version"`
	Hosts           map[string]hostActivity `json:"hosts"`
	Actions         map[string]int          `json:"actions,omitempty"`
	LatestOperation *operationSummary       `json:"latest_operation,omitempty"`
}

func loadState(path string) (nexusState, error) {
	state := nexusState{Version: 1, Hosts: map[string]hostActivity{}, Actions: map[string]int{}}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return state, nil
	}
	if err != nil {
		return state, err
	}
	if err := json.Unmarshal(raw, &state); err != nil {
		return nexusState{}, fmt.Errorf("invalid state file %s: %w", path, err)
	}
	if state.Hosts == nil {
		state.Hosts = map[string]hostActivity{}
	}
	if state.Actions == nil {
		state.Actions = map[string]int{}
	}
	for target, entry := range state.Hosts {
		if len(entry.Disks) == 0 {
			continue
		}
		entry.Disks = meaningfulStorageDisks(entry.Disks)
		entry.Disk = legacyDiskSummary(entry.Disks)
		state.Hosts[target] = entry
	}
	state.Version = 1
	return state, nil
}

func recordActionUse(path, action string) error {
	action = strings.TrimSpace(action)
	if action == "" {
		return nil
	}
	return updateState(path, func(state *nexusState) {
		if state.Actions == nil {
			state.Actions = map[string]int{}
		}
		state.Actions[action]++
	})
}

func recordLatestOperation(path string, operation operationSummary) error {
	operation.Action = boundedStateText(operation.Action, 64)
	operation.Label = boundedStateText(operation.Label, 128)
	operation.Host = boundedStateText(operation.Host, 512)
	operation.Status = boundedStateText(operation.Status, 16)
	operation.Summary = boundedStateText(operation.Summary, 512)
	if operation.Label == "" {
		return nil
	}
	if operation.Status == "" || operation.Status == "running" {
		return nil
	}
	return updateState(path, func(state *nexusState) {
		if state.LatestOperation != nil &&
			!state.LatestOperation.StartedAt.IsZero() &&
			operation.StartedAt.Before(state.LatestOperation.StartedAt) {
			return
		}
		snapshot := operation
		state.LatestOperation = &snapshot
	})
}

func boundedStateText(value string, limit int) string {
	value = strings.TrimSpace(sanitizeTerminalText(value))
	runes := []rune(value)
	if len(runes) > limit {
		value = string(runes[:limit])
	}
	return value
}

func saveState(path string, state nexusState) error {
	raw, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	return atomicWritePrivate(path, raw)
}

func atomicWritePrivate(path string, raw []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".state-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func recordHostSuccess(path, target string, now time.Time) error {
	return updateState(path, func(state *nexusState) {
		entry := state.Hosts[target]
		entry.Score++
		if entry.Score < 1 {
			entry.Score = 1
		}
		entry.LastUsed = now
		state.Hosts[target] = entry
		ageState(state)
	})
}

func updateState(path string, mutate func(*nexusState)) error {
	unlock, err := acquireStateLock(path)
	if err != nil {
		return err
	}
	defer unlock()
	state, err := loadState(path)
	if err != nil {
		return err
	}
	mutate(&state)
	return saveState(path, state)
}

func acquireStateLock(path string) (func(), error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	lockPath := path + ".lock"
	deadline := time.Now().Add(stateLockTimeout)
	for {
		err := os.Mkdir(lockPath, 0o700)
		if err == nil {
			return func() { _ = os.Remove(lockPath) }, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		if info, statErr := os.Stat(lockPath); statErr == nil && time.Since(info.ModTime()) > stateLockStale {
			_ = os.Remove(lockPath)
			continue
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("state file is busy: %s", path)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func ageState(state *nexusState) {
	total := 0.0
	for _, entry := range state.Hosts {
		total += entry.Score
	}
	if total <= frecencyMaxAge {
		return
	}
	factor := total / (frecencyMaxAge * 0.9)
	for target, entry := range state.Hosts {
		entry.Score /= factor
		if entry.Score < 1 {
			delete(state.Hosts, target)
			continue
		}
		state.Hosts[target] = entry
	}
}

func frecency(entry hostActivity, now time.Time) float64 {
	if entry.Score <= 0 || entry.LastUsed.IsZero() {
		return 0
	}
	age := now.Sub(entry.LastUsed)
	switch {
	case age < 0, age < time.Hour:
		return entry.Score * 4
	case age < 24*time.Hour:
		return entry.Score * 2
	case age < 7*24*time.Hour:
		return entry.Score / 2
	default:
		return entry.Score / 4
	}
}

func sortHostsByFrecency(hosts []string, state nexusState, now time.Time) []string {
	out := append([]string(nil), hosts...)
	order := make(map[string]int, len(hosts))
	for i, host := range hosts {
		order[host] = i
	}
	sort.SliceStable(out, func(i, j int) bool {
		left := frecency(state.Hosts[out[i]], now)
		right := frecency(state.Hosts[out[j]], now)
		if left == right {
			return order[out[i]] < order[out[j]]
		}
		return left > right
	})
	return out
}
