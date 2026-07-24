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
	"strings"
	"syscall"
	"time"

	"github.com/human-agent65535/modemdeck/internal/agentclient"
	"github.com/human-agent65535/modemdeck/internal/agentmedia"
	"github.com/human-agent65535/modemdeck/internal/auth"
	"github.com/human-agent65535/modemdeck/internal/calllifecycle"
	"github.com/human-agent65535/modemdeck/internal/callmedia"
	"github.com/human-agent65535/modemdeck/internal/communication"
	"github.com/human-agent65535/modemdeck/internal/diagnostics"
	"github.com/human-agent65535/modemdeck/internal/httpapi"
	"github.com/human-agent65535/modemdeck/internal/mediaapp"
	"github.com/human-agent65535/modemdeck/internal/messageevents"
	"github.com/human-agent65535/modemdeck/internal/networkruntime"
	"github.com/human-agent65535/modemdeck/internal/platform/database"
	"github.com/human-agent65535/modemdeck/internal/recording"
	"github.com/human-agent65535/modemdeck/internal/runtimeevents"
	"github.com/human-agent65535/modemdeck/internal/secretbox"
	"github.com/human-agent65535/modemdeck/internal/store"
	"github.com/human-agent65535/modemdeck/internal/telegramruntime"
	"github.com/human-agent65535/modemdeck/internal/telegramsettings"
	"github.com/human-agent65535/modemdeck/internal/tlsmanager"
	"github.com/human-agent65535/modemdeck/internal/webapp"
)

const hostAgentRequestTimeout = 15 * time.Second

func main() {
	logBuffer := diagnostics.NewLogBuffer(diagnostics.DefaultLogCapacity)
	logger := slog.New(logBuffer.Handler(slog.NewJSONHandler(os.Stdout, nil)))
	secureCookiesDefault, err := environmentBool("MODEMDECK_SECURE_COOKIES", true)
	if err != nil {
		logger.Error("invalid authentication configuration", "error", err)
		os.Exit(1)
	}
	listenAddress := flag.String(
		"listen",
		environmentOrDefault("MODEMDECK_LISTEN_ADDRESS", ":8080"),
		"HTTPS listen address",
	)
	databasePath := flag.String("database", database.DefaultPath, "ModemDeck SQLite database path")
	agentSocketPath := flag.String("agent-socket", environmentOrDefault("MODEMDECK_AGENT_SOCKET", "/run/modemdeck/agent.sock"), "ModemDeck host agent Unix socket")
	adminUsername := flag.String("admin-username", environmentOrDefault("MODEMDECK_ADMIN_USERNAME", defaultAdminUsername), "administrator username")
	adminPasswordFile := flag.String("admin-password-file", os.Getenv("MODEMDECK_ADMIN_PASSWORD_FILE"), "path to the administrator password secret")
	settingsKeyFile := flag.String("settings-key-file", os.Getenv("MODEMDECK_SETTINGS_KEY_FILE"), "path to the 32-byte settings encryption key")
	recordingsPath := flag.String("recordings", environmentOrDefault("MODEMDECK_RECORDINGS_PATH", "/data/recordings"), "call recording directory")
	tlsDirectory := flag.String(
		"tls-directory",
		environmentOrDefault("MODEMDECK_TLS_DIRECTORY", "/var/lib/modemdeck/tls"),
		"TLS certificate state directory",
	)
	tlsHosts := flag.String(
		"tls-hosts",
		environmentOrDefault("MODEMDECK_TLS_HOSTS", "localhost,127.0.0.1,::1"),
		"comma-separated DNS names and IP addresses for automatic certificates",
	)
	secureCookies := flag.Bool("secure-cookies", secureCookiesDefault, "require HTTPS for authentication cookies")
	flag.Parse()

	admin, err := loadAdminConfig(*adminUsername, *adminPasswordFile, *secureCookies)
	if err != nil {
		logger.Error("load authentication configuration", "error", err)
		os.Exit(1)
	}
	settingsSecrets, err := secretbox.OpenFile(*settingsKeyFile)
	if err != nil {
		logger.Error("load settings encryption key", "error", err)
		os.Exit(1)
	}
	if err := run(
		logger,
		*listenAddress,
		*databasePath,
		*agentSocketPath,
		*recordingsPath,
		*tlsDirectory,
		parseCommaSeparatedList(*tlsHosts),
		admin,
		settingsSecrets,
		logBuffer,
	); err != nil {
		logger.Error("ModemDeck stopped", "error", err)
		os.Exit(1)
	}
}

func run(
	logger *slog.Logger,
	listenAddress, databasePath, agentSocketPath, recordingsPath string,
	tlsDirectory string,
	tlsHosts []string,
	admin adminConfig,
	settingsSecrets *secretbox.Box,
	logBuffer diagnostics.LogSource,
) error {
	ctx := context.Background()
	tlsCertificates, err := tlsmanager.Open(tlsmanager.Config{
		Directory: tlsDirectory,
		Hosts:     tlsHosts,
	})
	if err != nil {
		return fmt.Errorf("open TLS certificate manager: %w", err)
	}
	db, err := database.Open(ctx, database.Config{
		TargetPath: databasePath,
	})
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}

	repository, err := store.New(db)
	if err != nil {
		_ = db.Close()
		return fmt.Errorf("create store: %w", err)
	}
	if err := repository.RecoverInterruptedCommunicationOperations(ctx); err != nil {
		_ = db.Close()
		return fmt.Errorf("recover interrupted communication operations: %w", err)
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
	agent, err := agentclient.New(agentSocketPath, hostAgentRequestTimeout)
	if err != nil {
		_ = db.Close()
		return fmt.Errorf("create host agent client: %w", err)
	}
	defer agent.CloseIdleConnections()
	messageEvents := messageevents.NewBuffer(messageevents.DefaultCapacity)
	runtimeEvents := runtimeevents.NewBuffer(runtimeevents.DefaultCapacity)
	communications, err := communication.New(agent, repository, messageEvents)
	if err != nil {
		_ = db.Close()
		return fmt.Errorf("create communication service: %w", err)
	}
	if err := communications.SetRuntimeEventPublisher(runtimeEvents); err != nil {
		_ = db.Close()
		return fmt.Errorf("configure communication runtime events: %w", err)
	}
	mediaOpener, err := agentmedia.New(agentSocketPath, repository, agentmedia.Options{})
	if err != nil {
		_ = db.Close()
		return fmt.Errorf("create host media opener: %w", err)
	}
	mediaCore, err := callmedia.New(callmedia.Options{EndpointOpener: mediaOpener})
	if err != nil {
		_ = db.Close()
		return fmt.Errorf("create WebRTC media core: %w", err)
	}
	callMedia, err := mediaapp.New(communications, repository, mediaCore)
	if err != nil {
		_ = mediaCore.Close(context.Background())
		_ = db.Close()
		return fmt.Errorf("create call media service: %w", err)
	}
	recordings, err := recording.New(repository, mediaCore, recording.Options{
		RootDirectory: recordingsPath,
		Report: func(err error) {
			logger.Warn("call recording worker failed", "component", "recording", "error", err)
		},
	})
	if err != nil {
		_ = mediaCore.Close(context.Background())
		_ = db.Close()
		return fmt.Errorf("create call recording service: %w", err)
	}
	if err := recordings.Recover(ctx); err != nil {
		_ = recordings.Close(context.Background())
		_ = mediaCore.Close(context.Background())
		_ = db.Close()
		return fmt.Errorf("recover call recordings: %w", err)
	}
	callLifecycle, err := calllifecycle.New(callMedia, recordings)
	if err != nil {
		_ = recordings.Close(context.Background())
		_ = mediaCore.Close(context.Background())
		_ = db.Close()
		return fmt.Errorf("create call lifecycle coordinator: %w", err)
	}
	if err := communications.SetCallLifecycleObserver(callLifecycle); err != nil {
		_ = recordings.Close(context.Background())
		_ = mediaCore.Close(context.Background())
		_ = db.Close()
		return fmt.Errorf("configure call media lifecycle: %w", err)
	}
	telegramSettings, err := telegramsettings.New(repository, settingsSecrets)
	if err != nil {
		_ = recordings.Close(context.Background())
		_ = mediaCore.Close(context.Background())
		_ = db.Close()
		return fmt.Errorf("create Telegram settings service: %w", err)
	}
	telegramRuntime, err := telegramruntime.New(
		telegramSettings,
		communications,
		repository,
		telegramruntime.Options{Logger: logger.With("component", "telegram")},
	)
	if err != nil {
		_ = recordings.Close(context.Background())
		_ = mediaCore.Close(context.Background())
		_ = db.Close()
		return fmt.Errorf("create Telegram runtime: %w", err)
	}
	networkRuntime, err := networkruntime.New(
		repository,
		settingsSecrets,
		agent,
		networkruntime.Options{
			RuntimeEvents: runtimeEvents,
			Report: func(err error) {
				logger.Warn("network runtime synchronization failed", "component", "network", "error", err)
			},
		},
	)
	if err != nil {
		_ = recordings.Close(context.Background())
		_ = mediaCore.Close(context.Background())
		_ = db.Close()
		return fmt.Errorf("create network runtime: %w", err)
	}
	api, err := httpapi.New(repository, httpapi.Options{
		Communications:       communications,
		DeviceConfigurations: communications,
		LineServices:         communications,
		CallPolicies:         communications,
		CallMedia:            callMedia,
		Recording:            recordings,
		Network:              networkRuntime,
		TelegramSettings:     telegramSettings,
		TLSSettings:          tlsSettingsService{manager: tlsCertificates},
		Authenticator:        authenticator,
		AdminUsername:        admin.Username,
		SecureCookies:        admin.SecureCookies,
		Logger:               logger.With("component", "http"),
		DiagnosticLogs:       logBuffer,
		MessageEvents:        messageEvents,
		RuntimeEvents:        runtimeEvents,
		Web:                  webapp.Embedded(),
	})
	if err != nil {
		_ = recordings.Close(context.Background())
		_ = mediaCore.Close(context.Background())
		_ = db.Close()
		return fmt.Errorf("create HTTP API: %w", err)
	}

	signals, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	syncDone := make(chan struct{})
	go func() {
		defer close(syncDone)
		communications.Run(signals, 3*time.Second, func(err error) {
			logger.Warn("hardware snapshot unavailable", "component", "communications", "error", err)
		})
	}()
	telegramDone := make(chan error, 1)
	go func() {
		telegramDone <- telegramRuntime.Run(signals)
	}()
	networkDone := make(chan error, 1)
	go func() {
		networkDone <- networkRuntime.Run(signals)
	}()
	server := &http.Server{
		Addr:              listenAddress,
		Handler:           redirectPlainHTTPToHTTPS(api),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       90 * time.Second,
		TLSConfig:         prepareServerTLSConfig(tlsCertificates.TLSConfig()),
	}
	serverErrors := make(chan error, 1)
	go func() {
		logger.Info(
			"ModemDeck HTTPS server started",
			"component",
			"http",
			"address",
			listenAddress,
		)
		serverErrors <- serveTLSAndPlainHTTP(server)
	}()

	var runErr error
	serverStopped := false
	telegramStopped := false
	networkStopped := false
	select {
	case <-signals.Done():
	case serveErr := <-serverErrors:
		serverStopped = true
		if !errors.Is(serveErr, http.ErrServerClosed) {
			runErr = fmt.Errorf("serve HTTPS: %w", serveErr)
		}
	case telegramErr := <-telegramDone:
		telegramStopped = true
		if telegramErr != nil && !errors.Is(telegramErr, context.Canceled) {
			runErr = fmt.Errorf("run Telegram runtime: %w", telegramErr)
		}
	case networkErr := <-networkDone:
		networkStopped = true
		if networkErr != nil && !errors.Is(networkErr, context.Canceled) {
			runErr = fmt.Errorf("run network runtime: %w", networkErr)
		}
	}
	stop()
	if !serverStopped {
		shutdownContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		shutdownErr := server.Shutdown(shutdownContext)
		cancel()
		if shutdownErr != nil {
			_ = server.Close()
			runErr = errors.Join(runErr, fmt.Errorf("shut down HTTP server: %w", shutdownErr))
		}
		serveErr := <-serverErrors
		if !errors.Is(serveErr, http.ErrServerClosed) {
			runErr = errors.Join(runErr, fmt.Errorf("serve HTTPS: %w", serveErr))
		}
	}
	<-syncDone
	if !telegramStopped {
		telegramErr := <-telegramDone
		if telegramErr != nil && !errors.Is(telegramErr, context.Canceled) {
			runErr = errors.Join(runErr, fmt.Errorf("stop Telegram runtime: %w", telegramErr))
		}
	}
	if !networkStopped {
		networkErr := <-networkDone
		if networkErr != nil && !errors.Is(networkErr, context.Canceled) {
			runErr = errors.Join(runErr, fmt.Errorf("stop network runtime: %w", networkErr))
		}
	}
	mediaCloseContext, mediaCloseCancel := context.WithTimeout(context.Background(), 5*time.Second)
	if err := recordings.Close(mediaCloseContext); err != nil {
		runErr = errors.Join(runErr, fmt.Errorf("close call recording service: %w", err))
	}
	if err := callMedia.Close(mediaCloseContext); err != nil {
		runErr = errors.Join(runErr, fmt.Errorf("close call media: %w", err))
	}
	mediaCloseCancel()
	if err := database.CloseWithTimeout(db, 5*time.Second); err != nil {
		runErr = errors.Join(runErr, err)
	}
	return runErr
}

func environmentOrDefault(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func parseCommaSeparatedList(value string) []string {
	parts := strings.Split(value, ",")
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			values = append(values, trimmed)
		}
	}
	return values
}
