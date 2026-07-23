package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/human-agent65535/modemdeck/agent/internal/httpapi"
	"github.com/human-agent65535/modemdeck/agent/internal/modemmanager"
	"github.com/human-agent65535/modemdeck/agent/internal/unixsocket"
)

var version = "dev"

func main() {
	if err := run(); err != nil {
		slog.Error("agent stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	socketPath := flag.String("socket", "/run/modemdeck/agent.sock", "absolute unix socket path")
	socketMode := flag.String("socket-mode", "0660", "unix socket permission mode in octal")
	socketUID := flag.Int("socket-uid", -1, "unix socket owner uid; -1 keeps the process uid")
	socketGID := flag.Int("socket-gid", -1, "unix socket owner gid; -1 keeps the process gid")
	flag.Parse()

	mode, err := parseSocketMode(*socketMode)
	if err != nil {
		return err
	}

	provider, err := modemmanager.OpenSystemBus()
	if err != nil {
		return err
	}
	defer provider.Close()

	listener, err := unixsocket.Listen(unixsocket.Config{
		Path: *socketPath,
		Mode: mode,
		UID:  *socketUID,
		GID:  *socketGID,
	})
	if err != nil {
		return err
	}
	defer listener.Close()

	server := &http.Server{
		Handler:           httpapi.New(provider, version),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    32 << 10,
	}
	serveResult := make(chan error, 1)
	go func() {
		serveResult <- server.Serve(listener)
	}()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	slog.Info("agent listening", "socket", *socketPath, "api_version", "v1", "version", version)

	select {
	case err := <-serveResult:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serve unix socket: %w", err)
	case <-ctx.Done():
		shutdownContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownContext); err != nil {
			return fmt.Errorf("shutdown http server: %w", err)
		}
		if err := <-serveResult; err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve unix socket: %w", err)
		}
		return nil
	}
}

func parseSocketMode(value string) (os.FileMode, error) {
	parsed, err := strconv.ParseUint(value, 8, 32)
	if err != nil || parsed > 0o777 {
		return 0, fmt.Errorf("invalid socket mode %q: expected octal permissions between 0000 and 0777", value)
	}
	return os.FileMode(parsed), nil
}
