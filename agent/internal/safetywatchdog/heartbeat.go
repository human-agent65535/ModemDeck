package safetywatchdog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type Heartbeat struct {
	PID       int   `json:"pid"`
	UnixNanos int64 `json:"unix_nanos"`
}

type MonitorOptions struct {
	Timeout      time.Duration
	PollInterval time.Duration
	Now          func() time.Time
	ProcessAlive func(int) bool
}

func WriteHeartbeat(path string, pid int, now time.Time) error {
	if !filepath.IsAbs(path) {
		return errors.New("heartbeat path must be absolute")
	}
	if pid <= 0 {
		return errors.New("heartbeat pid must be positive")
	}
	payload, err := json.Marshal(Heartbeat{
		PID:       pid,
		UnixNanos: now.UTC().UnixNano(),
	})
	if err != nil {
		return fmt.Errorf("encode heartbeat: %w", err)
	}
	payload = append(payload, '\n')

	directory := filepath.Dir(path)
	file, err := os.CreateTemp(directory, ".agent-heartbeat-*")
	if err != nil {
		return fmt.Errorf("create heartbeat: %w", err)
	}
	tempPath := file.Name()
	defer os.Remove(tempPath)

	if err := file.Chmod(0o640); err != nil {
		_ = file.Close()
		return fmt.Errorf("set heartbeat permissions: %w", err)
	}
	if _, err := file.Write(payload); err != nil {
		_ = file.Close()
		return fmt.Errorf("write heartbeat: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close heartbeat: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("publish heartbeat: %w", err)
	}
	return nil
}

func RunEmitter(
	ctx context.Context,
	path string,
	pid int,
	interval time.Duration,
	now func() time.Time,
) error {
	if interval <= 0 {
		return errors.New("heartbeat interval must be positive")
	}
	if now == nil {
		now = time.Now
	}
	if err := WriteHeartbeat(path, pid, now()); err != nil {
		return err
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := WriteHeartbeat(path, pid, now()); err != nil {
				return err
			}
		}
	}
}

func WaitForFailure(
	ctx context.Context,
	path string,
	pid int,
	options MonitorOptions,
) (string, error) {
	if !filepath.IsAbs(path) {
		return "", errors.New("heartbeat path must be absolute")
	}
	if pid <= 0 {
		return "", errors.New("agent pid must be positive")
	}
	if options.Timeout <= 0 {
		return "", errors.New("heartbeat timeout must be positive")
	}
	if options.PollInterval <= 0 {
		return "", errors.New("heartbeat poll interval must be positive")
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.ProcessAlive == nil {
		return "", errors.New("process liveness check is required")
	}

	ticker := time.NewTicker(options.PollInterval)
	defer ticker.Stop()
	for {
		reason, failed, err := inspectHeartbeat(path, pid, options)
		if err != nil {
			return "", err
		}
		if failed {
			return reason, nil
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-ticker.C:
		}
	}
}

func inspectHeartbeat(
	path string,
	pid int,
	options MonitorOptions,
) (string, bool, error) {
	if !options.ProcessAlive(pid) {
		return "agent_process_exited", true, nil
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "agent_heartbeat_missing", true, nil
		}
		return "", false, fmt.Errorf("read heartbeat: %w", err)
	}
	var heartbeat Heartbeat
	if err := json.Unmarshal(payload, &heartbeat); err != nil {
		return "", false, fmt.Errorf("decode heartbeat: %w", err)
	}
	if heartbeat.PID != pid {
		return "agent_heartbeat_pid_changed", true, nil
	}
	updatedAt := time.Unix(0, heartbeat.UnixNanos)
	if updatedAt.IsZero() || options.Now().UTC().Sub(updatedAt) > options.Timeout {
		return "agent_heartbeat_expired", true, nil
	}
	return "", false, nil
}
