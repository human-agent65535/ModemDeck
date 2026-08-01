package ota

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	"github.com/human-agent65535/modemdeck/internal/updatecheck"
)

const workerContainerName = "modemdeck-update-worker"

type CommandRunner interface {
	Run(context.Context, string, ...string) ([]byte, error)
}

type ExecCommandRunner struct{}

func (ExecCommandRunner) Run(
	ctx context.Context,
	name string,
	arguments ...string,
) ([]byte, error) {
	command := exec.CommandContext(ctx, name, arguments...)
	output, err := command.CombinedOutput()
	if err != nil {
		return output, fmt.Errorf("%s: %w: %s", name, err, strings.TrimSpace(string(output)))
	}
	return output, nil
}

type DockerRuntimeOptions struct {
	Runner            CommandRunner
	DeploymentDir     string
	HostDeploymentDir string
	StateDir          string
	StateVolume       string
	SelfImage         string
	Cloudflared       bool
}

type DockerRuntime struct {
	runner            CommandRunner
	deploymentDir     string
	hostDeploymentDir string
	stateDir          string
	stateVolume       string
	selfImage         string
	cloudflared       bool
}

type ApplyError struct {
	Cause          error
	RollbackFailed bool
}

func (err *ApplyError) Error() string {
	if err.RollbackFailed {
		return "software update and rollback failed"
	}
	return "software update failed"
}

func (err *ApplyError) Unwrap() error {
	return err.Cause
}

func NewDockerRuntime(options DockerRuntimeOptions) (*DockerRuntime, error) {
	runner := options.Runner
	if runner == nil {
		runner = ExecCommandRunner{}
	}
	for _, directory := range []string{options.DeploymentDir, options.HostDeploymentDir, options.StateDir} {
		if !filepath.IsAbs(directory) {
			return nil, errors.New("updater directories must be absolute")
		}
	}
	if strings.TrimSpace(options.StateVolume) == "" || strings.TrimSpace(options.SelfImage) == "" {
		return nil, errors.New("updater state volume and image are required")
	}
	return &DockerRuntime{
		runner:            runner,
		deploymentDir:     filepath.Clean(options.DeploymentDir),
		hostDeploymentDir: filepath.Clean(options.HostDeploymentDir),
		stateDir:          filepath.Clean(options.StateDir),
		stateVolume:       options.StateVolume,
		selfImage:         options.SelfImage,
		cloudflared:       options.Cloudflared,
	}, nil
}

func (runtime *DockerRuntime) Managed(service string) bool {
	return service != "cloudflared" || runtime.cloudflared
}

func (runtime *DockerRuntime) CurrentImage(
	ctx context.Context,
	service string,
) (string, bool, error) {
	output, err := runtime.runner.Run(
		ctx,
		"docker",
		"ps", "-aq",
		"--filter", "label=com.docker.compose.project=modemdeck",
		"--filter", "label=com.docker.compose.service="+service,
	)
	if err != nil {
		return "", false, err
	}
	containerID := strings.TrimSpace(strings.SplitN(string(output), "\n", 2)[0])
	if containerID == "" {
		return "", false, nil
	}
	output, err = runtime.runner.Run(
		ctx,
		"docker", "inspect", "--format", "{{.Config.Image}}", containerID,
	)
	if err != nil {
		return "", false, err
	}
	return strings.TrimSpace(string(output)), true, nil
}

func (runtime *DockerRuntime) StartWorker(
	ctx context.Context,
	request WorkerRequest,
) error {
	arguments := []string{
		"run", "--detach", "--rm",
		"--name", workerContainerName,
		"--mount", "type=bind,source=/var/run/docker.sock,target=/var/run/docker.sock",
		"--mount", "type=bind,source=" + runtime.hostDeploymentDir + ",target=" + runtime.deploymentDir,
		"--mount", "type=volume,source=" + runtime.stateVolume + ",target=" + runtime.stateDir,
		"--read-only",
		"--tmpfs", "/tmp:rw,noexec,nosuid,nodev,size=32m,mode=1777",
		"--cap-drop", "ALL",
		"--security-opt", "no-new-privileges:true",
		"--pids-limit", "128",
		"--env", "MODEMDECK_DEPLOYMENT_DIR=" + runtime.deploymentDir,
		"--env", "MODEMDECK_HOST_DEPLOYMENT_DIR=" + runtime.hostDeploymentDir,
		"--env", "MODEMDECK_UPDATER_STATE_VOLUME=" + runtime.stateVolume,
		"--env", "MODEMDECK_UPDATER_SELF_IMAGE=" + runtime.selfImage,
		"--env", "MODEMDECK_CLOUDFLARE_ENABLED=" + fmt.Sprintf("%t", runtime.cloudflared),
		runtime.selfImage,
		"worker",
		"--expected-version", request.ExpectedVersion,
		"--operation-id", request.Operation.ID,
		"--started-at", request.Operation.StartedAt,
	}
	if request.ConfirmHardware {
		arguments = append(arguments, "--confirm-hardware")
	}
	_, err := runtime.runner.Run(ctx, "docker", arguments...)
	return err
}

func (runtime *DockerRuntime) Apply(
	ctx context.Context,
	plan Plan,
	report ProgressReporter,
) error {
	for _, target := range plan.Targets {
		if !target.Changed {
			continue
		}
		if err := reportProgress(report, target.Name, updatecheck.OperationComponentPulling); err != nil {
			return err
		}
		if _, err := runtime.runner.Run(ctx, "docker", "pull", target.Reference); err != nil {
			_ = reportProgress(report, target.Name, updatecheck.OperationComponentFailed)
			return &ApplyError{Cause: err}
		}
		if err := reportProgress(report, target.Name, updatecheck.OperationComponentStaged); err != nil {
			return err
		}
	}

	environmentPath := filepath.Join(runtime.deploymentDir, ".env")
	previous, err := os.ReadFile(environmentPath)
	if err != nil {
		return &ApplyError{Cause: fmt.Errorf("read Compose environment: %w", err)}
	}
	values := map[string]string{"MODEMDECK_VERSION": plan.Result.LatestVersion}
	for _, target := range plan.Targets {
		if !target.Changed {
			continue
		}
		if target.ImageEnv != "" {
			values[target.ImageEnv] = target.Image
		}
		if target.RefEnv != "" {
			values[target.RefEnv] = target.Reference
		}
		values[target.VersionEnv] = target.Version
		values[target.DigestEnv] = target.Digest
		if target.Name == "updater" {
			values["MODEMDECK_UPDATER_SELF_IMAGE"] = target.Reference
		}
	}
	next, err := upsertEnvironment(previous, values)
	if err != nil {
		return &ApplyError{Cause: err}
	}
	if err := replaceFile(environmentPath, next); err != nil {
		return &ApplyError{Cause: err}
	}
	if err := reportChangedTargets(
		plan.Targets,
		report,
		updatecheck.OperationComponentRestarting,
	); err != nil {
		return err
	}
	if err := runtime.composeUp(ctx, next); err == nil {
		return reportChangedTargets(
			plan.Targets,
			report,
			updatecheck.OperationComponentReady,
		)
	} else {
		applyErr := err
		if reportErr := reportChangedTargets(
			plan.Targets,
			report,
			updatecheck.OperationComponentRollingBack,
		); reportErr != nil {
			return errors.Join(&ApplyError{Cause: applyErr}, reportErr)
		}
		if restoreErr := replaceFile(environmentPath, previous); restoreErr != nil {
			return &ApplyError{Cause: errors.Join(applyErr, restoreErr), RollbackFailed: true}
		}
		if rollbackErr := runtime.composeUp(ctx, previous); rollbackErr != nil {
			return &ApplyError{Cause: errors.Join(applyErr, rollbackErr), RollbackFailed: true}
		}
		if reportErr := reportChangedTargets(
			plan.Targets,
			report,
			updatecheck.OperationComponentRolledBack,
		); reportErr != nil {
			return errors.Join(&ApplyError{Cause: applyErr}, reportErr)
		}
		return &ApplyError{Cause: applyErr}
	}
}

func reportProgress(
	report ProgressReporter,
	name string,
	state updatecheck.OperationComponentState,
) error {
	if report == nil {
		return nil
	}
	return report(name, state)
}

func reportChangedTargets(
	targets []Target,
	report ProgressReporter,
	state updatecheck.OperationComponentState,
) error {
	for _, target := range targets {
		if !target.Changed {
			continue
		}
		if err := reportProgress(report, target.Name, state); err != nil {
			return err
		}
	}
	return nil
}

func (runtime *DockerRuntime) composeUp(ctx context.Context, environment []byte) error {
	values := parseEnvironment(environment)
	arguments := []string{
		"compose",
		"--project-directory", runtime.deploymentDir,
		"--env-file", filepath.Join(runtime.deploymentDir, ".env"),
		"--profile", "ota",
		"-f", filepath.Join(runtime.deploymentDir, "docker-compose.yml"),
	}
	if values["MODEMDECK_HARDWARE_MODE"] == "advanced" {
		arguments = append(arguments, "-f", filepath.Join(runtime.deploymentDir, "docker-compose.advanced.yml"))
	}
	if values["MODEMDECK_CLOUDFLARE_ENABLED"] == "true" {
		arguments = append(arguments, "-f", filepath.Join(runtime.deploymentDir, "docker-compose.cloudflare.yml"))
		if values["MODEMDECK_CLOUDFLARE_TURN_KEY_ID"] != "" {
			arguments = append(arguments, "-f", filepath.Join(runtime.deploymentDir, "docker-compose.cloudflare-turn.yml"))
		}
	}
	arguments = append(
		arguments,
		"-f", filepath.Join(runtime.deploymentDir, "docker-compose.ota.yml"),
		"up", "--detach", "--no-build", "--remove-orphans",
		"--wait", "--wait-timeout", "180",
	)
	_, err := runtime.runner.Run(ctx, "docker", arguments...)
	return err
}

func upsertEnvironment(contents []byte, values map[string]string) ([]byte, error) {
	for key, value := range values {
		if strings.ContainsAny(key+value, "\r\n") || key == "" || strings.Contains(key, "=") {
			return nil, errors.New("invalid Compose environment value")
		}
	}
	lines := strings.Split(strings.TrimSuffix(string(contents), "\n"), "\n")
	written := make(map[string]bool, len(values))
	for index, line := range lines {
		key, _, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		value, replace := values[key]
		if !replace {
			continue
		}
		if !written[key] {
			lines[index] = key + "=" + value
			written[key] = true
		} else {
			lines[index] = ""
		}
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		if !written[key] {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	for _, key := range keys {
		lines = append(lines, key+"="+values[key])
	}
	result := make([]string, 0, len(lines))
	for _, line := range lines {
		if line != "" {
			result = append(result, line)
		}
	}
	return []byte(strings.Join(result, "\n") + "\n"), nil
}

func parseEnvironment(contents []byte) map[string]string {
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

func replaceFile(path string, contents []byte) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("inspect Compose environment: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".env-*")
	if err != nil {
		return fmt.Errorf("create Compose environment: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(info.Mode().Perm()); err == nil {
		if stat, ok := info.Sys().(*syscall.Stat_t); ok {
			err = temporary.Chown(int(stat.Uid), int(stat.Gid))
		}
	}
	if err == nil {
		_, err = temporary.Write(contents)
	}
	if err == nil {
		err = temporary.Sync()
	}
	closeErr := temporary.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("write Compose environment: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("commit Compose environment: %w", err)
	}
	return nil
}
