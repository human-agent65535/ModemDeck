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

	"github.com/human-agent65535/modemdeck/agent/internal/controllease"
	"github.com/human-agent65535/modemdeck/agent/internal/deviceconfig"
	"github.com/human-agent65535/modemdeck/agent/internal/httpapi"
	"github.com/human-agent65535/modemdeck/agent/internal/media"
	"github.com/human-agent65535/modemdeck/agent/internal/modemmanager"
	"github.com/human-agent65535/modemdeck/agent/internal/networking"
	"github.com/human-agent65535/modemdeck/agent/internal/unixsocket"
	"github.com/human-agent65535/modemdeck/agent/internal/volte"
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
	mediaBindingsFile := flag.String(
		"media-bindings-file",
		os.Getenv("MODEMDECK_MEDIA_BINDINGS_FILE"),
		"absolute path to explicit host audio-port bindings JSON",
	)
	bearerStateFile := flag.String(
		"bearer-state-file",
		envOrDefault("MODEMDECK_BEARER_STATE_FILE", "/run/modemdeck/bearers.json"),
		"persistent file recording only ModemDeck-owned ModemManager bearers",
	)
	networkStateFile := flag.String(
		"network-state-file",
		envOrDefault("MODEMDECK_NETWORK_STATE_FILE", "/run/modemdeck/network.json"),
		"persistent file recording only ModemDeck-owned kernel network state",
	)
	radioStateFile := flag.String(
		"radio-state-file",
		envOrDefault("MODEMDECK_RADIO_STATE_FILE", "/run/modemdeck/radio-state.json"),
		"persistent file recording user-disabled modem radios",
	)
	flag.Parse()

	mode, err := parseSocketMode(*socketMode)
	if err != nil {
		return err
	}

	dataPlane, err := networking.NewDataPlane(networking.DataPlaneOptions{
		StateFile: *networkStateFile,
	})
	if err != nil {
		return fmt.Errorf("create cellular data plane: %w", err)
	}
	provider, err := modemmanager.OpenSystemBusWithOptions(modemmanager.Options{
		DataPlane:       dataPlane,
		BearerStateFile: *bearerStateFile,
		RadioStateFile:  *radioStateFile,
	})
	if err != nil {
		return err
	}
	defer provider.Close()
	controlLease, err := controllease.New(provider, controllease.Options{
		Report: func(err error) {
			slog.Error("enforce application control lease", "error", err)
		},
	})
	if err != nil {
		return fmt.Errorf("create application control lease: %w", err)
	}
	recoveryContext, cancelRecovery := context.WithTimeout(
		context.Background(),
		15*time.Second,
	)
	if err := controlLease.Recover(recoveryContext); err != nil {
		cancelRecovery()
		return fmt.Errorf("recover orphan modem calls: %w", err)
	}
	cancelRecovery()
	defer func() {
		shutdownContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := controlLease.Shutdown(shutdownContext); err != nil {
			slog.Error("end calls during agent shutdown", "error", err)
		}
	}()
	reconcileContext, cancelReconcile := context.WithTimeout(
		context.Background(),
		30*time.Second,
	)
	if err := provider.ReconcileDataPlane(reconcileContext); err != nil {
		cancelReconcile()
		return fmt.Errorf("reconcile cellular data plane: %w", err)
	}
	cancelReconcile()
	bindings, err := loadMediaBindings(*mediaBindingsFile)
	if err != nil {
		return err
	}
	backends, err := configureMediaBackends(bindings)
	if err != nil {
		return err
	}
	mediaManager, err := media.NewManager(
		media.CallSourceFunc(func(ctx context.Context, callID string) (media.Call, error) {
			snapshot, err := provider.Snapshot(ctx)
			if err != nil {
				return media.Call{}, media.NewError(
					media.ErrorBackendUnavailable,
					"resolve_media_call",
					"ModemManager call state is unavailable",
					err,
				)
			}
			for _, call := range snapshot.Calls {
				if call.ID != callID {
					continue
				}
				projected := media.Call{
					ID:             call.ID,
					State:          call.State,
					AudioPort:      call.AudioPort,
					MediaAvailable: call.MediaAvailable,
				}
				if call.AudioFormat != nil {
					projected.AudioFormat = &media.AdvertisedFormat{
						Encoding:   call.AudioFormat.Encoding,
						Resolution: call.AudioFormat.Resolution,
						Rate:       call.AudioFormat.Rate,
					}
				}
				return projected, nil
			}
			return media.Call{}, media.NewError(
				media.ErrorNotFound,
				"resolve_media_call",
				"call was not found",
				nil,
			)
		}),
		bindings,
		backends,
		media.Options{},
	)
	if err != nil {
		return err
	}
	defer mediaManager.Close()
	volteRegistry, err := volte.NewRegistry(volte.QuectelLTEStandardQCFGIMSProfile())
	if err != nil {
		return fmt.Errorf("create VoLTE profile registry: %w", err)
	}
	deviceConfigurations, err := deviceconfig.New(
		provider,
		volteRegistry,
		func(
			_ context.Context,
			lineID string,
			_ volte.Identity,
		) (volte.Transports, error) {
			return volte.Transports{AT: provider.ATTransport(lineID)}, nil
		},
	)
	if err != nil {
		return fmt.Errorf("create device configuration service: %w", err)
	}
	networkManager, err := networking.NewManager(provider, deviceConfigurations)
	if err != nil {
		return fmt.Errorf("create network manager: %w", err)
	}
	defer func() {
		if err := networkManager.Close(); err != nil {
			slog.Error("close network manager", "error", err)
		}
	}()

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

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go runRadioReconciler(ctx, provider)
	go controlLease.Run(ctx)

	server := &http.Server{
		Handler: httpapi.NewWithOptions(provider, version, httpapi.Options{
			Media:                mediaManager,
			ControlLease:         controlLease,
			DeviceConfigurations: deviceConfigurations,
			Network:              networkManager,
			NetworkSelection:     provider,
		}),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    32 << 10,
	}
	serveResult := make(chan error, 1)
	go func() {
		serveResult <- server.Serve(listener)
	}()

	slog.Info("agent listening", "socket", *socketPath, "api_version", "v1", "version", version)

	select {
	case err := <-serveResult:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serve unix socket: %w", err)
	case <-ctx.Done():
		callShutdownContext, callShutdownCancel := context.WithTimeout(
			context.Background(),
			5*time.Second,
		)
		if err := controlLease.Shutdown(callShutdownContext); err != nil {
			slog.Error("end calls before agent server shutdown", "error", err)
		}
		callShutdownCancel()
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

func runRadioReconciler(ctx context.Context, provider *modemmanager.Provider) {
	const (
		interval       = 5 * time.Second
		attemptTimeout = 45 * time.Second
	)
	reconcile := func() {
		attemptContext, cancel := context.WithTimeout(ctx, attemptTimeout)
		defer cancel()
		if err := provider.ReconcileRadioState(attemptContext); err != nil &&
			!errors.Is(err, context.Canceled) {
			slog.Warn("reconcile modem radio state", "error", err)
		}
	}

	reconcile()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			reconcile()
		}
	}
}

func envOrDefault(name string, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func parseSocketMode(value string) (os.FileMode, error) {
	parsed, err := strconv.ParseUint(value, 8, 32)
	if err != nil || parsed > 0o777 {
		return 0, fmt.Errorf("invalid socket mode %q: expected octal permissions between 0000 and 0777", value)
	}
	return os.FileMode(parsed), nil
}
