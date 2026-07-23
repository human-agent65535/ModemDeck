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
	"syscall"
	"time"

	"github.com/human-agent65535/modemdeck/internal/agentclient"
	"github.com/human-agent65535/modemdeck/internal/httpapi"
	"github.com/human-agent65535/modemdeck/internal/platform/database"
	"github.com/human-agent65535/modemdeck/internal/store"
	"github.com/human-agent65535/modemdeck/internal/webapp"
)

func main() {
	listenAddress := flag.String("listen", ":8080", "HTTP listen address")
	databasePath := flag.String("database", database.DefaultPath, "ModemDeck SQLite database path")
	legacyDatabasePath := flag.String("legacy-database", database.LegacyPath, "legacy VoHive SQLite database path")
	agentSocketPath := flag.String("agent-socket", environmentOrDefault("MODEMDECK_AGENT_SOCKET", "/run/modemdeck/agent.sock"), "ModemDeck host agent Unix socket")
	flag.Parse()

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger, *listenAddress, *databasePath, *legacyDatabasePath, *agentSocketPath); err != nil {
		logger.Error("ModemDeck stopped", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger, listenAddress, databasePath, legacyDatabasePath, agentSocketPath string) error {
	ctx := context.Background()
	db, openResult, err := database.Open(ctx, database.Config{
		TargetPath: databasePath,
		LegacyPath: legacyDatabasePath,
	})
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	if openResult.Migration.Migrated {
		logger.Info("migrated legacy database", "source", openResult.Migration.Source, "target", openResult.Migration.Target)
	}

	repository, err := store.New(db)
	if err != nil {
		_ = db.Close()
		return fmt.Errorf("create store: %w", err)
	}
	agent, err := agentclient.New(agentSocketPath, 2*time.Second)
	if err != nil {
		_ = db.Close()
		return fmt.Errorf("create host agent client: %w", err)
	}
	defer agent.CloseIdleConnections()
	api, err := httpapi.New(repository, httpapi.Options{
		Capabilities: agentCapabilitySource{client: agent},
		Logger:       logger,
		Web:          webapp.Embedded(),
	})
	if err != nil {
		_ = db.Close()
		return fmt.Errorf("create HTTP API: %w", err)
	}

	server := &http.Server{
		Addr:              listenAddress,
		Handler:           api,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       90 * time.Second,
	}
	serverErrors := make(chan error, 1)
	go func() {
		logger.Info("ModemDeck HTTP server started", "address", listenAddress, "database", openResult.Path)
		serverErrors <- server.ListenAndServe()
	}()

	signals, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	select {
	case <-signals.Done():
		shutdownContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		shutdownErr := server.Shutdown(shutdownContext)
		cancel()
		if shutdownErr != nil {
			_ = server.Close()
			_ = database.CloseWithTimeout(db, 5*time.Second)
			return fmt.Errorf("shut down HTTP server: %w", shutdownErr)
		}
		serveErr := <-serverErrors
		if !errors.Is(serveErr, http.ErrServerClosed) {
			_ = database.CloseWithTimeout(db, 5*time.Second)
			return fmt.Errorf("serve HTTP: %w", serveErr)
		}
	case serveErr := <-serverErrors:
		if !errors.Is(serveErr, http.ErrServerClosed) {
			_ = database.CloseWithTimeout(db, 5*time.Second)
			return fmt.Errorf("serve HTTP: %w", serveErr)
		}
	}
	if err := database.CloseWithTimeout(db, 5*time.Second); err != nil {
		return err
	}
	return nil
}

type agentCapabilitySource struct {
	client *agentclient.Client
}

func (source agentCapabilitySource) Capabilities(ctx context.Context) (httpapi.Capabilities, error) {
	health, err := source.client.Health(ctx)
	if err != nil {
		return httpapi.Capabilities{}, err
	}
	capabilities := httpapi.Capabilities{
		AgentConnected: true,
		Dial:           health.Provider.Available && health.Provider.Capabilities.Dial,
		Message:        health.Provider.Available && health.Provider.Capabilities.SendMessage,
	}
	capabilities.UnavailableReasons = make(map[string]string, 2)
	if !health.Provider.Available {
		capabilities.UnavailableReasons["dial"] = "ModemManager is unavailable"
		capabilities.UnavailableReasons["message"] = "ModemManager is unavailable"
		return capabilities, nil
	}
	if !capabilities.Dial {
		capabilities.UnavailableReasons["dial"] = "The host agent does not support dialing"
	}
	if !capabilities.Message {
		capabilities.UnavailableReasons["message"] = "The host agent does not support sending messages"
	}
	return capabilities, nil
}

func environmentOrDefault(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
