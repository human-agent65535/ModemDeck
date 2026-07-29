package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/human-agent65535/modemdeck/agent/internal/modemmanager"
	"github.com/human-agent65535/modemdeck/agent/internal/safetywatchdog"
)

var version = "dev"

func main() {
	if err := run(); err != nil {
		slog.Error("call watchdog stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	heartbeatPath := flag.String(
		"heartbeat-file",
		"",
		"absolute path to the ModemDeck agent heartbeat",
	)
	agentPID := flag.Int("agent-pid", 0, "ModemDeck agent process id")
	timeout := flag.Duration(
		"timeout",
		3*time.Second,
		"maximum heartbeat age before emergency call cleanup",
	)
	pollInterval := flag.Duration(
		"poll-interval",
		250*time.Millisecond,
		"agent liveness polling interval",
	)
	cleanupTimeout := flag.Duration(
		"cleanup-timeout",
		15*time.Second,
		"maximum time for emergency call cleanup",
	)
	cleanupOnly := flag.Bool(
		"cleanup-only",
		false,
		"perform emergency call cleanup immediately",
	)
	flag.Parse()

	if *cleanupTimeout <= 0 {
		return errors.New("cleanup timeout must be positive")
	}
	reason := "supervisor_fallback"
	if !*cleanupOnly {
		waitContext, stop := signalContext()
		defer stop()
		detectedReason, err := safetywatchdog.WaitForFailure(
			waitContext,
			*heartbeatPath,
			*agentPID,
			safetywatchdog.MonitorOptions{
				Timeout:      *timeout,
				PollInterval: *pollInterval,
				ProcessAlive: processAlive,
			},
		)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return nil
			}
			return err
		}
		reason = detectedReason
		slog.Error(
			"agent liveness failed; ending modem calls",
			"component", "call_watchdog",
			"reason", reason,
			"agent_pid", *agentPID,
		)
	}

	provider, err := modemmanager.OpenSystemBus()
	if err != nil {
		return fmt.Errorf("open ModemManager for emergency call cleanup: %w", err)
	}
	defer provider.Close()
	cleanupContext, cancel := context.WithTimeout(
		context.Background(),
		*cleanupTimeout,
	)
	defer cancel()
	if err := provider.EmergencyHangupAll(cleanupContext, reason); err != nil {
		return fmt.Errorf("emergency call cleanup: %w", err)
	}
	slog.Warn(
		"emergency call cleanup completed",
		"component", "call_watchdog",
		"reason", reason,
	)
	return nil
}

func signalContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
}

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
