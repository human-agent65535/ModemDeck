package ota

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/human-agent65535/modemdeck/internal/updatecheck"
)

var (
	ErrHardwareConfirmationRequired = errors.New("hardware update confirmation is required")
	ErrNoUpdate                     = errors.New("no applicable software update is available")
	ErrOperationRunning             = errors.New("a software update is already running")
	ErrVersionChanged               = errors.New("the requested release is no longer current")
	ErrNoOperation                  = errors.New("no software update operation has been recorded")
)

type ReleaseChecker interface {
	Check(context.Context) updatecheck.Result
}

type Runtime interface {
	CurrentImage(context.Context, string) (string, bool, error)
	Managed(string) bool
	StartWorker(context.Context, WorkerRequest) error
	Apply(context.Context, Plan, ProgressReporter) error
}

type ProgressReporter func(string, updatecheck.OperationComponentState) error

type OperationStore interface {
	Load() (*updatecheck.Operation, error)
	Save(updatecheck.Operation) error
}

type Options struct {
	Checker    ReleaseChecker
	Manifests  ManifestLoader
	Digests    DigestResolver
	Runtime    Runtime
	Operations OperationStore
	Now        func() time.Time
}

type Controller struct {
	checker    ReleaseChecker
	manifests  ManifestLoader
	digests    DigestResolver
	runtime    Runtime
	operations OperationStore
	now        func() time.Time
	mu         sync.Mutex
}

type Target struct {
	Name       string
	Service    string
	Image      string
	Version    string
	Digest     string
	Reference  string
	ImageEnv   string
	RefEnv     string
	VersionEnv string
	DigestEnv  string
	CurrentRef string
	Current    string
	Changed    bool
}

type Plan struct {
	Result  updatecheck.Result
	Targets []Target
}

type WorkerRequest struct {
	ExpectedVersion string
	ConfirmHardware bool
	Operation       updatecheck.Operation
}

func New(options Options) (*Controller, error) {
	if options.Checker == nil || options.Manifests == nil || options.Digests == nil ||
		options.Runtime == nil || options.Operations == nil {
		return nil, errors.New("complete OTA controller dependencies are required")
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	return &Controller{
		checker:    options.Checker,
		manifests:  options.Manifests,
		digests:    options.Digests,
		runtime:    options.Runtime,
		operations: options.Operations,
		now:        now,
	}, nil
}

func (controller *Controller) Check(ctx context.Context) updatecheck.Result {
	plan, err := controller.plan(ctx)
	if err == nil {
		return plan.Result
	}
	result := controller.checker.Check(ctx)
	result.ApplyAvailable = false
	if errors.Is(err, ErrManifestUnavailable) {
		result.ErrorCode = "ota_manifest_unavailable"
	} else {
		result.ErrorCode = "release_images_unavailable"
	}
	if operation, loadErr := controller.operations.Load(); loadErr == nil {
		result.Operation = operation
	}
	return result
}

func (controller *Controller) Apply(
	ctx context.Context,
	request updatecheck.ApplyRequest,
) (updatecheck.Operation, error) {
	controller.mu.Lock()
	defer controller.mu.Unlock()

	if operation, err := controller.operations.Load(); err == nil &&
		operation != nil && operation.State == updatecheck.OperationRunning {
		return updatecheck.Operation{}, ErrOperationRunning
	}
	plan, err := controller.plan(ctx)
	if err != nil {
		return updatecheck.Operation{}, err
	}
	if !plan.Result.ApplyAvailable {
		return updatecheck.Operation{}, ErrNoUpdate
	}
	if strings.TrimSpace(request.Version) != plan.Result.LatestVersion {
		return updatecheck.Operation{}, ErrVersionChanged
	}
	if plan.Result.HardwareConfirmationRequired && !request.ConfirmHardware {
		return updatecheck.Operation{}, ErrHardwareConfirmationRequired
	}
	operation := updatecheck.Operation{
		ID:            operationID(),
		State:         updatecheck.OperationRunning,
		TargetVersion: plan.Result.LatestVersion,
		StartedAt:     controller.now().UTC().Format(time.RFC3339),
		Components:    operationComponents(plan.Targets),
	}
	if err := controller.operations.Save(operation); err != nil {
		return updatecheck.Operation{}, err
	}
	if err := controller.runtime.StartWorker(ctx, WorkerRequest{
		ExpectedVersion: request.Version,
		ConfirmHardware: request.ConfirmHardware,
		Operation:       operation,
	}); err != nil {
		operation.State = updatecheck.OperationFailed
		operation.FinishedAt = controller.now().UTC().Format(time.RFC3339)
		operation.ErrorCode = "worker_start_failed"
		_ = controller.operations.Save(operation)
		return updatecheck.Operation{}, err
	}
	return operation, nil
}

func (controller *Controller) Status() (updatecheck.Operation, error) {
	operation, err := controller.operations.Load()
	if err != nil {
		return updatecheck.Operation{}, err
	}
	if operation == nil {
		return updatecheck.Operation{}, ErrNoOperation
	}
	return *operation, nil
}

func (controller *Controller) RunWorker(
	ctx context.Context,
	request WorkerRequest,
) error {
	plan, err := controller.plan(ctx)
	if err == nil && plan.Result.LatestVersion != request.ExpectedVersion {
		err = ErrVersionChanged
	}
	if err == nil && plan.Result.HardwareConfirmationRequired && !request.ConfirmHardware {
		err = ErrHardwareConfirmationRequired
	}
	if err == nil && !plan.Result.ApplyAvailable {
		err = ErrNoUpdate
	}
	operation := request.Operation
	if err == nil {
		operation.Components = operationComponents(plan.Targets)
		err = controller.operations.Save(operation)
	}
	if err == nil {
		err = controller.runtime.Apply(ctx, plan, func(
			name string,
			state updatecheck.OperationComponentState,
		) error {
			setOperationComponentState(&operation, name, state)
			return controller.operations.Save(operation)
		})
	}
	operation.FinishedAt = controller.now().UTC().Format(time.RFC3339)
	if err != nil {
		operation.State = updatecheck.OperationFailed
		operation.ErrorCode = applyErrorCode(err)
		for index := range operation.Components {
			if operation.Components[index].State != updatecheck.OperationComponentReady &&
				operation.Components[index].State != updatecheck.OperationComponentRolledBack {
				operation.Components[index].State = updatecheck.OperationComponentFailed
			}
		}
	} else {
		operation.State = updatecheck.OperationSucceeded
		operation.ErrorCode = ""
		for index := range operation.Components {
			operation.Components[index].State = updatecheck.OperationComponentReady
		}
	}
	if saveErr := controller.operations.Save(operation); saveErr != nil {
		return errors.Join(err, saveErr)
	}
	return err
}

func operationComponents(targets []Target) []updatecheck.OperationComponent {
	components := make([]updatecheck.OperationComponent, 0, len(targets))
	for _, target := range targets {
		if !target.Changed {
			continue
		}
		components = append(components, updatecheck.OperationComponent{
			Name:  target.Name,
			State: updatecheck.OperationComponentPending,
		})
	}
	return components
}

func setOperationComponentState(
	operation *updatecheck.Operation,
	name string,
	state updatecheck.OperationComponentState,
) {
	for index := range operation.Components {
		if operation.Components[index].Name == name {
			operation.Components[index].State = state
			return
		}
	}
}

func (controller *Controller) plan(ctx context.Context) (Plan, error) {
	result := controller.checker.Check(ctx)
	result.ApplyAvailable = false
	result.HardwareConfirmationRequired = false
	result.Components = nil
	if operation, err := controller.operations.Load(); err == nil {
		result.Operation = operation
	}
	if result.Status != updatecheck.StatusUpdateAvailable {
		return Plan{Result: result}, nil
	}
	manifest, err := controller.manifests.Load(ctx, result.LatestVersion)
	if err != nil {
		return Plan{}, err
	}
	targets := []Target{
		{Name: "api", Service: "api", Image: manifest.Images.API, Version: manifest.APIVersion, ImageEnv: "MODEMDECK_IMAGE", RefEnv: "MODEMDECK_API_IMAGE_REF", VersionEnv: "MODEMDECK_API_VERSION", DigestEnv: "MODEMDECK_API_DIGEST"},
		{Name: "web", Service: "modemdeck", Image: manifest.Images.Web, Version: manifest.WebVersion, ImageEnv: "MODEMDECK_WEB_IMAGE", RefEnv: "MODEMDECK_WEB_IMAGE_REF", VersionEnv: "MODEMDECK_WEB_VERSION", DigestEnv: "MODEMDECK_WEB_DIGEST"},
		{Name: "hardware", Service: "hardware", Image: manifest.Images.Hardware, Version: manifest.HardwareVersion, ImageEnv: "MODEMDECK_HARDWARE_IMAGE", RefEnv: "MODEMDECK_HARDWARE_IMAGE_REF", VersionEnv: "MODEMDECK_HARDWARE_VERSION", DigestEnv: "MODEMDECK_HARDWARE_DIGEST"},
		{Name: "updater", Service: "updater", Image: manifest.Images.Updater, Version: manifest.UpdaterVersion, ImageEnv: "MODEMDECK_UPDATER_IMAGE", RefEnv: "MODEMDECK_UPDATER_IMAGE_REF", VersionEnv: "MODEMDECK_UPDATER_VERSION", DigestEnv: "MODEMDECK_UPDATER_DIGEST"},
	}
	if controller.runtime.Managed("cloudflared") {
		targets = append(targets, Target{
			Name:       "cloudflared",
			Service:    "cloudflared",
			Image:      manifest.Cloudflared.Image,
			Version:    manifest.Cloudflared.Version,
			Digest:     manifest.Cloudflared.Digest,
			VersionEnv: "MODEMDECK_CLOUDFLARED_VERSION",
			DigestEnv:  "MODEMDECK_CLOUDFLARED_DIGEST",
		})
	}
	type digestResult struct {
		index  int
		digest string
		exists bool
		err    error
	}
	digestResults := make(chan digestResult, len(targets))
	digestRequests := 0
	for index := range targets {
		if targets[index].Digest != "" {
			continue
		}
		digestRequests++
		go func(index int) {
			digest, exists, err := controller.digests.Resolve(
				ctx,
				targets[index].Image,
				targets[index].Version,
			)
			digestResults <- digestResult{
				index: index, digest: digest, exists: exists, err: err,
			}
		}(index)
	}
	for range digestRequests {
		resolved := <-digestResults
		if resolved.err != nil {
			return Plan{}, resolved.err
		}
		if !resolved.exists {
			return Plan{}, ErrRegistryUnavailable
		}
		targets[resolved.index].Digest = resolved.digest
	}

	components := make([]updatecheck.Component, 0, len(targets))
	for index := range targets {
		target := &targets[index]
		currentRef, present, currentErr := controller.runtime.CurrentImage(ctx, target.Service)
		if currentErr != nil {
			return Plan{}, currentErr
		}
		target.CurrentRef = currentRef
		target.Current = imageVersion(currentRef)
		if target.Current == "" {
			target.Current = strings.TrimSpace(result.CurrentVersion)
			if target.Current == "" {
				target.Current = "dev"
			}
		}
		target.Reference = target.Image + ":" + target.Version + "@" + target.Digest
		target.Changed = !present || imageDigest(currentRef) != target.Digest
		if target.Name == "hardware" && target.Changed {
			result.HardwareConfirmationRequired = true
		}
		components = append(components, updatecheck.Component{
			Name:           target.Name,
			CurrentVersion: target.Current,
			TargetVersion:  target.Version,
			Changed:        target.Changed,
		})
	}
	result.Components = components
	result.ApplyAvailable = true
	return Plan{Result: result, Targets: targets}, nil
}

func imageDigest(reference string) string {
	if _, digest, ok := strings.Cut(reference, "@"); ok && digestPattern.MatchString(digest) {
		return digest
	}
	return ""
}

func imageVersion(reference string) string {
	name := reference
	if before, _, ok := strings.Cut(name, "@"); ok {
		name = before
	}
	slash := strings.LastIndexByte(name, '/')
	colon := strings.LastIndexByte(name, ':')
	if colon <= slash || colon == len(name)-1 {
		return ""
	}
	return name[colon+1:]
}

func operationID() string {
	buffer := make([]byte, 12)
	if _, err := rand.Read(buffer); err != nil {
		return time.Now().UTC().Format("20060102T150405.000000000")
	}
	return hex.EncodeToString(buffer)
}

func applyErrorCode(err error) string {
	var applyErr *ApplyError
	if errors.As(err, &applyErr) && applyErr.RollbackFailed {
		return "apply_and_rollback_failed"
	}
	if errors.Is(err, ErrVersionChanged) {
		return "release_changed"
	}
	if errors.Is(err, ErrHardwareConfirmationRequired) {
		return "hardware_confirmation_required"
	}
	return "apply_failed"
}
