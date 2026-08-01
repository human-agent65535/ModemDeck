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
	"github.com/human-agent65535/modemdeck/internal/calllease"
	"github.com/human-agent65535/modemdeck/internal/calllifecycle"
	"github.com/human-agent65535/modemdeck/internal/callmedia"
	"github.com/human-agent65535/modemdeck/internal/communication"
	"github.com/human-agent65535/modemdeck/internal/diagnostics"
	"github.com/human-agent65535/modemdeck/internal/httpapi"
	"github.com/human-agent65535/modemdeck/internal/mediaapp"
	"github.com/human-agent65535/modemdeck/internal/messageevents"
	"github.com/human-agent65535/modemdeck/internal/mobilepairing"
	"github.com/human-agent65535/modemdeck/internal/networkruntime"
	"github.com/human-agent65535/modemdeck/internal/platform/database"
	"github.com/human-agent65535/modemdeck/internal/recording"
	"github.com/human-agent65535/modemdeck/internal/runtimeevents"
	"github.com/human-agent65535/modemdeck/internal/secretbox"
	"github.com/human-agent65535/modemdeck/internal/store"
	"github.com/human-agent65535/modemdeck/internal/telegramruntime"
	"github.com/human-agent65535/modemdeck/internal/telegramsettings"
	"github.com/human-agent65535/modemdeck/internal/tlsmanager"
	"github.com/human-agent65535/modemdeck/internal/updatecheck"
)

const hostAgentRequestTimeout = 15 * time.Second

var version = "dev"

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
		"HTTP listen address",
	)
	databasePath := flag.String("database", database.DefaultPath, "ModemDeck SQLite database path")
	agentSocketPath := flag.String("agent-socket", environmentOrDefault("MODEMDECK_AGENT_SOCKET", "/run/modemdeck/agent.sock"), "ModemDeck host agent Unix socket")
	settingsKeyFile := flag.String("settings-key-file", os.Getenv("MODEMDECK_SETTINGS_KEY_FILE"), "path to the 32-byte settings encryption key")
	recordingsPath := flag.String("recordings", environmentOrDefault("MODEMDECK_RECORDINGS_PATH", "/data/recordings"), "call recording directory")
	tlsDirectory := flag.String(
		"tls-directory",
		environmentOrDefault("MODEMDECK_TLS_DIRECTORY", "/var/lib/modemdeck/tls"),
		"Web TLS certificate state directory",
	)
	tlsHosts := flag.String(
		"tls-hosts",
		environmentOrDefault("MODEMDECK_TLS_HOSTS", "localhost,127.0.0.1,::1"),
		"comma-separated DNS names and IP addresses for automatic Web certificates",
	)
	secureCookies := flag.Bool(
		"secure-cookies",
		secureCookiesDefault,
		"mark authentication cookies for the Cloudflare HTTPS client",
	)
	flag.Parse()

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
		*secureCookies,
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
	secureCookies bool,
	settingsSecrets *secretbox.Box,
	logBuffer diagnostics.LogSource,
) error {
	ctx := context.Background()
	tlsCertificates, err := tlsmanager.Open(tlsmanager.Config{
		Directory: tlsDirectory,
		Hosts:     tlsHosts,
	})
	if err != nil {
		return fmt.Errorf("open Web TLS certificate manager: %w", err)
	}
	cloudflareOriginTLS, err := tlsmanager.OpenCloudflareOrigin(
		tlsmanager.CloudflareOriginConfig{Directory: tlsDirectory},
	)
	if err != nil {
		return fmt.Errorf("open Cloudflare origin TLS certificate manager: %w", err)
	}
	cloudflareOriginActivator, err := newCloudflareOriginTLSActivator(
		os.Getenv("MODEMDECK_CLOUDFLARE_ORIGIN_PROBE_URLS"),
	)
	if err != nil {
		return fmt.Errorf("configure Cloudflare origin TLS activation: %w", err)
	}
	cloudflareGateway, err := mobilepairing.NewCloudflareGateway(
		os.Getenv("MODEMDECK_CLOUDFLARE_READY_URL"),
	)
	if err != nil {
		return fmt.Errorf("configure Cloudflare Tunnel: %w", err)
	}
	turnProvider, err := cloudflareTURNProvider()
	if err != nil {
		return fmt.Errorf("configure call relay: %w", err)
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
	callMedia, err := mediaapp.New(
		communications,
		repository,
		mediaCore,
		mediaapp.Options{RTCProvider: turnProvider},
	)
	if err != nil {
		_ = mediaCore.Close(context.Background())
		_ = db.Close()
		return fmt.Errorf("create call media service: %w", err)
	}
	recordings, err := recording.New(repository, mediaCore, recording.Options{
		RootDirectory: recordingsPath,
		OnChange: func() {
			runtimeEvents.Publish(runtimeevents.Event{
				Resources: []runtimeevents.Resource{runtimeevents.ResourceRecordings},
			})
		},
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
	callLeases, err := calllease.New(
		repository,
		communications,
		calllease.Options{
			Report: func(err error) {
				logger.Warn(
					"browser call lease failed",
					"component",
					"calls",
					"error",
					err,
				)
			},
		},
	)
	if err != nil {
		_ = recordings.Close(context.Background())
		_ = mediaCore.Close(context.Background())
		_ = db.Close()
		return fmt.Errorf("create browser call lease manager: %w", err)
	}
	callLifecycle, err := calllifecycle.New(callMedia, recordings, callLeases)
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
		telegramruntime.Options{
			Logger:        logger.With("component", "telegram"),
			RuntimeEvents: runtimeEvents,
		},
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
			RuntimeEvents:      runtimeEvents,
			RuntimeEventSource: runtimeEvents,
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
		MessagePolicies:      communications,
		CallMedia:            callMedia,
		CallLeases:           callLeases,
		Recording:            recordings,
		Network:              networkRuntime,
		TelegramSettings:     telegramSettings,
		TLSSettings:          tlsSettingsService{manager: tlsCertificates},
		CloudflareOriginTLS: &cloudflareOriginTLSService{
			manager:    cloudflareOriginTLS,
			cloudflare: cloudflareGateway,
			activate:   cloudflareOriginActivator,
		},
		MobilePairing:      cloudflareGateway,
		RTCConfiguration:   turnProvider,
		Authenticator:      authenticator,
		SecureCookies:      secureCookies,
		Logger:             logger.With("component", "http"),
		DiagnosticLogs:     logBuffer,
		MessageEvents:      messageEvents,
		RuntimeEvents:      runtimeEvents,
		UpdateChecker:      updatecheck.New(updatecheck.Options{CurrentVersion: version}),
		ApplicationVersion: version,
	})
	if err != nil {
		_ = recordings.Close(context.Background())
		_ = mediaCore.Close(context.Background())
		_ = db.Close()
		return fmt.Errorf("create HTTP API: %w", err)
	}

	signals, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	tlsMaintenanceDone := make(chan struct{})
	go func() {
		defer close(tlsMaintenanceDone)
		maintainTLSCertificates(signals, tlsCertificates, logger)
	}()
	syncDone := make(chan struct{})
	go func() {
		defer close(syncDone)
		communications.Run(signals, 30*time.Second, func(err error) {
			logger.Warn("hardware snapshot unavailable", "component", "communications", "error", err)
		})
	}()
	callLeaseDone := make(chan struct{})
	go func() {
		defer close(callLeaseDone)
		callLeases.Run(signals)
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
		Handler:           api,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       90 * time.Second,
	}
	serverErrors := make(chan error, 1)
	go func() {
		logger.Info(
			"ModemDeck HTTP origin started",
			"component",
			"http",
			"address",
			listenAddress,
		)
		serverErrors <- server.ListenAndServe()
	}()
	cloudflareDone := make(chan struct{})
	go func() {
		defer close(cloudflareDone)
		cloudflareGateway.Run(signals)
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
			runErr = fmt.Errorf("serve HTTP: %w", serveErr)
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
			runErr = errors.Join(runErr, fmt.Errorf("serve HTTP: %w", serveErr))
		}
	}
	<-syncDone
	<-callLeaseDone
	<-tlsMaintenanceDone
	<-cloudflareDone
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

func maintainTLSCertificates(
	ctx context.Context,
	manager *tlsmanager.Manager,
	logger *slog.Logger,
) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := manager.TLSConfig().GetCertificate(nil); err != nil {
				logger.Warn(
					"Web TLS certificate maintenance failed",
					"component",
					"tls",
					"error",
					err,
				)
			}
		}
	}
}
