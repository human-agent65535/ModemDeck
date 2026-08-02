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
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/human-agent65535/modemdeck/internal/ota"
	"github.com/human-agent65535/modemdeck/internal/updatecheck"
)

var version = "dev"

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	controller, token, err := buildController()
	if err != nil {
		logger.Error("configure updater", "error", err)
		os.Exit(1)
	}
	if len(os.Args) > 1 && os.Args[1] == "worker" {
		if err := runWorker(ctx, controller, os.Args[2:]); err != nil {
			logger.Error("apply software update", "error", err)
			os.Exit(1)
		}
		return
	}
	if err := serve(ctx, controller, token, logger); err != nil {
		logger.Error("updater stopped", "error", err)
		os.Exit(1)
	}
}

func buildController() (*ota.Controller, string, error) {
	deploymentDir := environmentOrDefault("MODEMDECK_DEPLOYMENT_DIR", "/deployment")
	stateDir := environmentOrDefault("MODEMDECK_UPDATER_STATE_DIR", "/var/lib/modemdeck-updater")
	environment := readEnvironment(filepath.Join(deploymentDir, ".env"))
	cloudflared := firstNonempty(
		os.Getenv("MODEMDECK_CLOUDFLARE_ENABLED"),
		environment["MODEMDECK_CLOUDFLARE_ENABLED"],
	) == "true"
	selfImage := firstNonempty(
		os.Getenv(ota.WorkerSelfImageEnvironment),
		os.Getenv("MODEMDECK_UPDATER_SELF_IMAGE"),
		environment["MODEMDECK_UPDATER_SELF_IMAGE"],
	)
	runtime, err := ota.NewDockerRuntime(ota.DockerRuntimeOptions{
		DeploymentDir:     deploymentDir,
		HostDeploymentDir: environmentOrDefault("MODEMDECK_HOST_DEPLOYMENT_DIR", deploymentDir),
		StateDir:          stateDir,
		StateVolume: firstNonempty(
			os.Getenv("MODEMDECK_UPDATER_STATE_VOLUME"),
			environment["MODEMDECK_UPDATER_STATE_VOLUME"],
			"modemdeck-updater-state",
		),
		SelfImage:   selfImage,
		Cloudflared: cloudflared,
	})
	if err != nil {
		return nil, "", err
	}
	controller, err := ota.New(ota.Options{
		Checker: newDeploymentReleaseChecker(
			filepath.Join(stateDir, ota.InstalledVersionFilename),
			firstNonempty(os.Getenv("MODEMDECK_CURRENT_VERSION"), version),
		),
		Manifests:  ota.NewHTTPManifestLoader(os.Getenv("MODEMDECK_RELEASE_MANIFEST_URL_TEMPLATE"), nil),
		Digests:    ota.NewGHCRResolver("", "", nil),
		Runtime:    runtime,
		Operations: ota.NewFileOperationStore(stateDir),
	})
	if err != nil {
		return nil, "", err
	}
	token := ""
	if tokenFile := strings.TrimSpace(os.Getenv("MODEMDECK_UPDATER_TOKEN_FILE")); tokenFile != "" {
		contents, readErr := os.ReadFile(tokenFile)
		if readErr != nil {
			return nil, "", fmt.Errorf("read updater token: %w", readErr)
		}
		token = strings.TrimSpace(string(contents))
	}
	return controller, token, nil
}

type deploymentReleaseChecker struct {
	installedVersionPath string
	fallbackVersion      string

	mu             sync.Mutex
	currentVersion string
	checker        *updatecheck.Checker
}

func newDeploymentReleaseChecker(
	installedVersionPath string,
	fallbackVersion string,
) *deploymentReleaseChecker {
	return &deploymentReleaseChecker{
		installedVersionPath: installedVersionPath,
		fallbackVersion:      firstNonempty(fallbackVersion, "dev"),
	}
}

func (checker *deploymentReleaseChecker) Check(ctx context.Context) updatecheck.Result {
	currentVersion := firstNonempty(
		readTrimmedFile(checker.installedVersionPath),
		checker.fallbackVersion,
	)
	checker.mu.Lock()
	if checker.checker == nil || checker.currentVersion != currentVersion {
		checker.currentVersion = currentVersion
		checker.checker = updatecheck.New(updatecheck.Options{CurrentVersion: currentVersion})
	}
	releaseChecker := checker.checker
	checker.mu.Unlock()
	return releaseChecker.Check(ctx)
}

func readTrimmedFile(path string) string {
	contents, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(contents))
}

func serve(
	ctx context.Context,
	controller *ota.Controller,
	token string,
	logger *slog.Logger,
) error {
	handler, err := ota.NewHandler(controller, token)
	if err != nil {
		return err
	}
	server := &http.Server{
		Addr:              environmentOrDefault("MODEMDECK_UPDATER_LISTEN_ADDRESS", "0.0.0.0:8081"),
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	errorsChannel := make(chan error, 1)
	go func() {
		logger.Info("ModemDeck updater started", "version", version, "address", server.Addr)
		errorsChannel <- server.ListenAndServe()
	}()
	select {
	case <-ctx.Done():
		shutdownContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return server.Shutdown(shutdownContext)
	case err := <-errorsChannel:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func runWorker(ctx context.Context, controller *ota.Controller, arguments []string) error {
	flags := flag.NewFlagSet("worker", flag.ContinueOnError)
	expectedVersion := flags.String("expected-version", "", "release version to apply")
	operationID := flags.String("operation-id", "", "update operation identifier")
	startedAt := flags.String("started-at", "", "update operation start time")
	confirmHardware := flags.Bool("confirm-hardware", false, "confirm Hardware interruption")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if *expectedVersion == "" || *operationID == "" || *startedAt == "" {
		return errors.New("worker update identity is incomplete")
	}
	return controller.RunWorker(ctx, ota.WorkerRequest{
		ExpectedVersion: *expectedVersion,
		ConfirmHardware: *confirmHardware,
		Operation: updatecheck.Operation{
			ID:            *operationID,
			State:         updatecheck.OperationRunning,
			TargetVersion: *expectedVersion,
			StartedAt:     *startedAt,
		},
	})
}

func readEnvironment(path string) map[string]string {
	contents, err := os.ReadFile(path)
	if err != nil {
		return map[string]string{}
	}
	values := make(map[string]string)
	for _, line := range strings.Split(string(contents), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok || key == "" || strings.HasPrefix(key, "#") {
			continue
		}
		values[key] = strings.Trim(strings.TrimSpace(value), `"'`)
	}
	return values
}

func environmentOrDefault(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func firstNonempty(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}
