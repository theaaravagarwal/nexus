package main

import (
	"context"
	"net"
	"strconv"
	"testing"
	"time"
)

func TestProbeTargetUsesSavedPort(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := listener.Close(); err != nil {
			t.Errorf("close listener: %v", err)
		}
	})
	port := listener.Addr().(*net.TCPAddr).Port
	target := "test@127.0.0.1:" + strconv.Itoa(port)
	result := probeTarget(context.Background(), target, time.Second)
	if result.Status != reachOnline {
		t.Fatalf("result=%#v", result)
	}
}

func TestProbeTargetRejectsInvalidTarget(t *testing.T) {
	result := probeTarget(context.Background(), "not-a-target", 10*time.Millisecond)
	if result.Status != reachError || result.Error == "" {
		t.Fatalf("result=%#v", result)
	}
}

func TestProbeTargetsCoversOnlineRefusedInvalidAndCancelled(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := listener.Close(); err != nil {
			t.Errorf("close listener: %v", err)
		}
	})
	openPort := listener.Addr().(*net.TCPAddr).Port

	closedListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closedPort := closedListener.Addr().(*net.TCPAddr).Port
	if err := closedListener.Close(); err != nil {
		t.Fatal(err)
	}

	targets := []string{
		"test@127.0.0.1:" + strconv.Itoa(openPort),
		"test@127.0.0.1:" + strconv.Itoa(closedPort),
		"invalid",
	}
	results := probeTargets(context.Background(), targets, time.Second, 8)
	if len(results) != len(targets) {
		t.Fatalf("results=%d want=%d", len(results), len(targets))
	}
	if results[0].Target != targets[0] || results[0].Status != reachOnline {
		t.Fatalf("online result=%+v", results[0])
	}
	if results[1].Target != targets[1] || results[1].Status != reachRefused {
		t.Fatalf("refused result=%+v", results[1])
	}
	if results[2].Target != targets[2] || results[2].Status != reachError {
		t.Fatalf("invalid result=%+v", results[2])
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	results = probeTargets(cancelled, targets[:1], time.Second, 0)
	if results[0].Status != reachTimeout || results[0].Error != "cancelled" {
		t.Fatalf("cancelled result=%+v", results[0])
	}
}
