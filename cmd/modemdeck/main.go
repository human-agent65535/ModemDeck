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
	"github.com/human-agent65535/modemdeck/internal/auth"
	"github.com/human-agent65535/modemdeck/internal/communication"
	"github.com/human-agent65535/modemdeck/internal/httpapi"
	"github.com/human-agent65535/modemdeck/internal/platform/database"
	"github.com/human-agent65535/modemdeck/internal/store"
	"github.com/human-agent65535/modemdeck/internal/webapp"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	secureCookiesDefault, err := environmentBool("MODEMDECK_SECURE_COOKIES", false)
	if err != nil {
		logger.Error("invalid authentication configuration", "error", err)
		os.Exit(1)
	}
	listenAddress := flag.String("listen", ":8080", "HTTP listen address")
	databasePath := flag.String("database", database.DefaultPath, "ModemDeck SQLite database path")
	legacyDatabasePath := flag.String("legacy-database", database.LegacyPath, "legacy VoHive SQLite database path")
	agentSocketPath := flag.String("agent-socket", environmentOrDefault("MODEMDECK_AGENT_SOCKET", "/run/modemdeck/agent.sock"), "ModemDeck host agent Unix socket")
	adminUsername := flag.String("admin-username", environmentOrDefault("MODEMDECK_ADMIN_USERNAME", defaultAdminUsername), "administrator username")
	adminPasswordFile := flag.String("admin-password-file", os.Getenv("MODEMDECK_ADMIN_PASSWORD_FILE"), "path to the administrator password secret")
	secureCookies := flag.Bool("secure-cookies", secureCookiesDefault, "require HTTPS for authentication cookies")
	flag.Parse()

	admin, err := loadAdminConfig(*adminUsername, *adminPasswordFile, *secureCookies)
	if err != nil {
		logger.Error("load authentication configuration", "error", err)
		os.Exit(1)
	}
	if err := run(logger, *listenAddress, *databasePath, *legacyDatabasePath, *agentSocketPath, admin); err != nil {
		logger.Error("ModemDeck stopped", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger, listenAddress, databasePath, legacyDatabasePath, agentSocketPath string, admin adminConfig) error {
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
	authenticator, err := auth.NewService(repository)
	if err != nil {
		_ = db.Close()
		return fmt.Errorf("create authentication service: %w", err)
	}
	if err := authenticator.EnsureAdmin(ctx, admin.Password); err != nil {
		_ = db.Close()
		return fmt.Errorf("configure administrator: %w", err)
	}
	admin.Password = ""
	agent, err := agentclient.New(agentSocketPath, 2*time.Second)
	if err != nil {
		_ = db.Close()
		return fmt.Errorf("create host agent client: %w", err)
	}
	defer agent.CloseIdleConnections()
	communications, err := communication.New(agent, repository)
	if err != nil {
		_ = db.Close()
		return fmt.Errorf("create communication service: %w", err)
	}
	api, err := httpapi.New(repository, httpapi.Options{
		Communications: communications,
		Authenticator:  authenticator,
		AdminUsername:  admin.Username,
		SecureCookies:  admin.SecureCookies,
		Logger:         logger,
		Web:            webapp.Embedded(),
	})
	if err != nil {
		_ = db.Close()
		return fmt.Errorf("create HTTP API: %w", err)
	}

	signals, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	syncDone := make(chan struct{})
	go func() {
		defer close(syncDone)
		communications.Run(signals, 3*time.Second, func(err error) {
			logger.Warn("hardware snapshot unavailable", "error", err)
		})
	}()

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
		stop()
		if !errors.Is(serveErr, http.ErrServerClosed) {
			_ = database.CloseWithTimeout(db, 5*time.Second)
			return fmt.Errorf("serve HTTP: %w", serveErr)
		}
	}
	stop()
	<-syncDone
	if err := database.CloseWithTimeout(db, 5*time.Second); err != nil {
		return err
	}
	return nil
}

func environmentOrDefault(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
