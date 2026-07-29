package safetywatchdog

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestWriteHeartbeatPublishesCurrentProcess(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "agent-heartbeat.json")
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)

	if err := WriteHeartbeat(path, 42, now); err != nil {
		t.Fatalf("WriteHeartbeat() error = %v", err)
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	var heartbeat Heartbeat
	if err := json.Unmarshal(payload, &heartbeat); err != nil {
		t.Fatalf("decode heartbeat: %v", err)
	}
	if heartbeat.PID != 42 || heartbeat.UnixNanos != now.UnixNano() {
		t.Fatalf("heartbeat = %+v", heartbeat)
	}
}

func TestWaitForFailureDetectsAgentExitImmediately(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "agent-heartbeat.json")
	now := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	if err := WriteHeartbeat(path, 42, now); err != nil {
		t.Fatalf("WriteHeartbeat() error = %v", err)
	}

	reason, err := WaitForFailure(context.Background(), path, 42, MonitorOptions{
		Timeout:      3 * time.Second,
		PollInterval: time.Millisecond,
		Now:          func() time.Time { return now },
		ProcessAlive: func(int) bool { return false },
	})
	if err != nil {
		t.Fatalf("WaitForFailure() error = %v", err)
	}
	if reason != "agent_process_exited" {
		t.Fatalf("reason = %q", reason)
	}
}

func TestWaitForFailureDetectsExpiredHeartbeat(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "agent-heartbeat.json")
	writtenAt := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	if err := WriteHeartbeat(path, 42, writtenAt); err != nil {
		t.Fatalf("WriteHeartbeat() error = %v", err)
	}

	reason, err := WaitForFailure(context.Background(), path, 42, MonitorOptions{
		Timeout:      3 * time.Second,
		PollInterval: time.Millisecond,
		Now:          func() time.Time { return writtenAt.Add(4 * time.Second) },
		ProcessAlive: func(int) bool { return true },
	})
	if err != nil {
		t.Fatalf("WaitForFailure() error = %v", err)
	}
	if reason != "agent_heartbeat_expired" {
		t.Fatalf("reason = %q", reason)
	}
}

func TestRunEmitterRenewsUntilCancelled(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "agent-heartbeat.json")
	startedAt := time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
	var mu sync.Mutex
	now := startedAt
	nowFunc := func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		now = now.Add(time.Second)
		return now
	}
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		result <- RunEmitter(ctx, path, 42, time.Millisecond, nowFunc)
	}()

	deadline := time.Now().Add(time.Second)
	for {
		payload, err := os.ReadFile(path)
		if err == nil {
			var heartbeat Heartbeat
			if json.Unmarshal(payload, &heartbeat) == nil &&
				heartbeat.UnixNanos >= startedAt.Add(2*time.Second).UnixNano() {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("heartbeat was not renewed")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	if err := <-result; err != nil {
		t.Fatalf("RunEmitter() error = %v", err)
	}
}
