package main

import (
	"context"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

type reachabilityStatus string

const (
	reachUnknown reachabilityStatus = "unknown"
	reachOnline  reachabilityStatus = "online"
	reachRefused reachabilityStatus = "refused"
	reachTimeout reachabilityStatus = "timeout"
	reachError   reachabilityStatus = "error"
)

type reachabilityResult struct {
	Target  string
	Status  reachabilityStatus
	Latency time.Duration
	Error   string
}

func probeTarget(ctx context.Context, raw string, timeout time.Duration) reachabilityResult {
	result := reachabilityResult{Target: raw, Status: reachError}
	target, err := parseConnectionTarget(raw)
	if err != nil {
		result.Error = sanitizeTerminalText(err.Error())
		return result
	}
	address := net.JoinHostPort(target.Host, strconv.Itoa(target.Port))
	start := time.Now()
	conn, err := (&net.Dialer{Timeout: timeout}).DialContext(ctx, "tcp", address)
	result.Latency = time.Since(start)
	if err == nil {
		_ = conn.Close()
		result.Status = reachOnline
		return result
	}
	if ctx.Err() != nil {
		result.Status = reachTimeout
		result.Error = "cancelled"
		return result
	}
	if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
		result.Status = reachTimeout
	} else if isConnectionRefused(err) {
		result.Status = reachRefused
	} else {
		result.Status = reachError
	}
	result.Error = sanitizeTerminalText(err.Error())
	return result
}

func probeTargets(ctx context.Context, targets []string, timeout time.Duration, concurrency int) []reachabilityResult {
	if concurrency < 1 {
		concurrency = 1
	}
	concurrency = min(concurrency, max(1, len(targets)))
	results := make([]reachabilityResult, len(targets))
	type job struct {
		index  int
		target string
	}
	jobs := make(chan job)
	var wg sync.WaitGroup
	for range concurrency {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for task := range jobs {
				if ctx.Err() != nil {
					results[task.index] = reachabilityResult{Target: task.target, Status: reachTimeout, Error: "cancelled"}
					continue
				}
				results[task.index] = probeTarget(ctx, task.target, timeout)
			}
		}()
	}
	for index, target := range targets {
		jobs <- job{index: index, target: target}
	}
	close(jobs)
	wg.Wait()
	return results
}

func isConnectionRefused(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "connection refused") || strings.Contains(message, "actively refused")
}
