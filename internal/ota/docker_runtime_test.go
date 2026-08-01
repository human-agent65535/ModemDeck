package ota

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/human-agent65535/modemdeck/internal/updatecheck"
)

func TestDockerRuntimeApplyRestoresEnvironmentWithOneRollback(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	previous := []byte("MODEMDECK_HARDWARE_MODE=simple\nMODEMDECK_VERSION=v1.9.2\nMODEMDECK_API_VERSION=v1.9.2\n")
	environmentPath := filepath.Join(directory, ".env")
	if err := os.WriteFile(environmentPath, previous, 0o600); err != nil {
		t.Fatalf("write environment: %v", err)
	}
	runner := &recordingRunner{composeErrors: []error{errors.New("apply failed"), nil}}
	runtime := newDockerRuntimeForTest(t, runner, directory)
	plan := Plan{
		Result: updateResult("v1.9.3"),
		Targets: []Target{{
			Name:       "api",
			Image:      "ghcr.io/human-agent65535/modemdeck",
			Version:    "v1.9.3",
			Digest:     testDigest("a"),
			Reference:  "ghcr.io/human-agent65535/modemdeck:v1.9.3@" + testDigest("a"),
			ImageEnv:   "MODEMDECK_IMAGE",
			RefEnv:     "MODEMDECK_API_IMAGE_REF",
			VersionEnv: "MODEMDECK_API_VERSION",
			DigestEnv:  "MODEMDECK_API_DIGEST",
			Changed:    true,
		}},
	}

	var progress []updatecheck.OperationComponentState
	err := runtime.Apply(context.Background(), plan, func(
		name string,
		state updatecheck.OperationComponentState,
	) error {
		if name != "api" {
			t.Fatalf("progress component = %q, want api", name)
		}
		progress = append(progress, state)
		return nil
	})
	var applyErr *ApplyError
	if !errors.As(err, &applyErr) || applyErr.RollbackFailed {
		t.Fatalf("Apply() error = %v", err)
	}
	contents, readErr := os.ReadFile(environmentPath)
	if readErr != nil {
		t.Fatalf("read restored environment: %v", readErr)
	}
	if string(contents) != string(previous) {
		t.Fatalf("restored environment = %q; want %q", contents, previous)
	}
	if runner.composeCalls != 2 {
		t.Fatalf("Compose calls = %d; want apply plus one rollback", runner.composeCalls)
	}
	wantProgress := []updatecheck.OperationComponentState{
		updatecheck.OperationComponentPulling,
		updatecheck.OperationComponentStaged,
		updatecheck.OperationComponentRestarting,
		updatecheck.OperationComponentRollingBack,
		updatecheck.OperationComponentRolledBack,
	}
	if !slices.Equal(progress, wantProgress) {
		t.Fatalf("progress = %v; want %v", progress, wantProgress)
	}
	for _, command := range runner.commands {
		if len(command) > 1 && command[1] == "compose" && !containsSequence(command, "--profile", "ota") {
			t.Fatalf("Compose command did not enable OTA profile: %q", command)
		}
	}
}

func TestDockerRuntimeWorkerUsesRestrictedOneShotContainer(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	runner := &recordingRunner{}
	runtime := newDockerRuntimeForTest(t, runner, directory)
	err := runtime.StartWorker(context.Background(), WorkerRequest{
		ExpectedVersion: "v1.9.3",
		Operation:       operation("operation-1"),
	})
	if err != nil {
		t.Fatalf("StartWorker() error = %v", err)
	}
	if len(runner.commands) != 1 {
		t.Fatalf("commands = %q", runner.commands)
	}
	command := runner.commands[0]
	for _, expected := range [][]string{
		{"--name", workerContainerName},
		{"--cap-drop", "ALL"},
		{"--security-opt", "no-new-privileges:true"},
		{"--pids-limit", "128"},
		{"--env", "MODEMDECK_DEPLOYMENT_DIR=" + directory},
		{"worker", "--expected-version", "v1.9.3"},
	} {
		if !containsSequence(command, expected...) {
			t.Errorf("worker command missing %q: %q", expected, command)
		}
	}
}

type recordingRunner struct {
	commands      [][]string
	composeErrors []error
	composeCalls  int
}

func (runner *recordingRunner) Run(
	_ context.Context,
	name string,
	arguments ...string,
) ([]byte, error) {
	command := append([]string{name}, arguments...)
	runner.commands = append(runner.commands, command)
	if len(arguments) == 0 || arguments[0] != "compose" {
		return nil, nil
	}
	index := runner.composeCalls
	runner.composeCalls++
	if index < len(runner.composeErrors) {
		return nil, runner.composeErrors[index]
	}
	return nil, nil
}

func newDockerRuntimeForTest(
	t *testing.T,
	runner CommandRunner,
	directory string,
) *DockerRuntime {
	t.Helper()
	runtime, err := NewDockerRuntime(DockerRuntimeOptions{
		Runner:            runner,
		DeploymentDir:     directory,
		HostDeploymentDir: directory,
		StateDir:          filepath.Join(directory, "state"),
		StateVolume:       "modemdeck-updater-state",
		SelfImage:         "ghcr.io/human-agent65535/modemdeck-updater:v1.9.2@" + testDigest("f"),
	})
	if err != nil {
		t.Fatalf("NewDockerRuntime() error = %v", err)
	}
	return runtime
}

func containsSequence(values []string, sequence ...string) bool {
	for index := 0; index+len(sequence) <= len(values); index++ {
		if slices.Equal(values[index:index+len(sequence)], sequence) {
			return true
		}
	}
	return false
}

func updateResult(version string) updatecheck.Result {
	return updatecheck.Result{LatestVersion: version}
}

func operation(id string) updatecheck.Operation {
	return updatecheck.Operation{
		ID:        id,
		State:     updatecheck.OperationRunning,
		StartedAt: "2026-08-02T00:00:00Z",
	}
}
