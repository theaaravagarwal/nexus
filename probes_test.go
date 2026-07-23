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
