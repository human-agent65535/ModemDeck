package ota

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/human-agent65535/modemdeck/internal/updatecheck"
)

func TestCheckPlansOnlyChangedReleaseComponents(t *testing.T) {
	t.Parallel()
	hardwareDigest := testDigest("1")
	cloudflaredDigest := testDigest("2")
	runtime := &fakeRuntime{
		cloudflared: true,
		images: map[string]string{
			"api":         "ghcr.io/human-agent65535/modemdeck:v1.9.2@" + testDigest("3"),
			"modemdeck":   "ghcr.io/human-agent65535/modemdeck-web:v1.9.2@" + testDigest("b"),
			"hardware":    "ghcr.io/human-agent65535/modemdeck-hardware:v1.9.2@" + hardwareDigest,
			"updater":     "ghcr.io/human-agent65535/modemdeck-updater:v1.9.2@" + testDigest("c"),
			"cloudflared": "cloudflare/cloudflared:2026.7.3@" + cloudflaredDigest,
		},
	}
	manifest := testManifest(cloudflaredDigest)
	controller := newTestController(t, runtime, manifest, hardwareDigest)

	result := controller.Check(context.Background())
	if !result.ApplyAvailable {
		t.Fatalf("apply_available = false; result = %+v", result)
	}
	if result.HardwareConfirmationRequired {
		t.Fatal("unchanged Hardware requested confirmation")
	}
	changed := map[string]bool{}
	for _, component := range result.Components {
		changed[component.Name] = component.Changed
	}
	if !changed["api"] {
		t.Error("API was not planned for update")
	}
	if changed["web"] || changed["hardware"] || changed["updater"] || changed["cloudflared"] {
		t.Fatalf("retained components = %+v", changed)
	}
}

func TestApplyRequiresOnlyExplicitHardwareConfirmation(t *testing.T) {
	t.Parallel()
	runtime := &fakeRuntime{images: map[string]string{
		"api":       "ghcr.io/human-agent65535/modemdeck:v1.9.2@" + testDigest("1"),
		"modemdeck": "ghcr.io/human-agent65535/modemdeck-web:v1.9.2@" + testDigest("2"),
		"hardware":  "ghcr.io/human-agent65535/modemdeck-hardware:v1.8.0@" + testDigest("3"),
		"updater":   "ghcr.io/human-agent65535/modemdeck-updater:v1.9.2@" + testDigest("4"),
	}}
	manifest := testManifest(testDigest("8"))
	controller := newTestController(t, runtime, manifest, testDigest("9"))

	_, err := controller.Apply(context.Background(), updatecheck.ApplyRequest{Version: "v2.0.0"})
	if !errors.Is(err, ErrHardwareConfirmationRequired) {
		t.Fatalf("Apply() error = %v", err)
	}
	if runtime.started != nil {
		t.Fatal("worker started without Hardware confirmation")
	}
	operation, err := controller.Apply(context.Background(), updatecheck.ApplyRequest{
		Version:         "v2.0.0",
		ConfirmHardware: true,
	})
	if err != nil {
		t.Fatalf("Apply() confirmed error = %v", err)
	}
	if operation.State != updatecheck.OperationRunning || runtime.started == nil {
		t.Fatalf("operation = %+v; worker = %+v", operation, runtime.started)
	}
	if len(operation.Components) != 4 {
		t.Fatalf("operation components = %+v; want four changed containers", operation.Components)
	}
	for _, component := range operation.Components {
		if component.State != updatecheck.OperationComponentPending {
			t.Fatalf("initial component = %+v; want pending", component)
		}
	}
	if !runtime.started.ConfirmHardware {
		t.Fatal("worker did not receive Hardware confirmation")
	}
}

func TestCheckCanAdvanceReleaseWithoutChangingAContainer(t *testing.T) {
	t.Parallel()
	manifest := testManifest(testDigest("8"))
	runtime := &fakeRuntime{images: map[string]string{
		"api":       manifest.Images.API + ":v2.0.0@" + testDigest("a"),
		"modemdeck": manifest.Images.Web + ":v2.0.0@" + testDigest("b"),
		"hardware":  manifest.Images.Hardware + ":v2.0.0@" + testDigest("9"),
		"updater":   manifest.Images.Updater + ":v2.0.0@" + testDigest("c"),
	}}
	controller := newTestController(t, runtime, manifest, testDigest("9"))
	result := controller.Check(context.Background())
	if !result.ApplyAvailable {
		t.Fatalf("metadata-only release is not applicable: %+v", result)
	}
	for _, component := range result.Components {
		if component.Changed {
			t.Fatalf("metadata-only release changed %s", component.Name)
		}
	}
	operation, err := controller.Apply(
		context.Background(),
		updatecheck.ApplyRequest{Version: "v2.0.0"},
	)
	if err != nil || len(operation.Components) != 0 || runtime.started == nil {
		t.Fatalf("operation = %+v; worker = %+v; error = %v", operation, runtime.started, err)
	}
}

func TestEnvironmentUpdateRetainsUnrelatedConfiguration(t *testing.T) {
	t.Parallel()
	updated, err := upsertEnvironment([]byte("# keep\nMODEMDECK_PORT=7577\nMODEMDECK_API_VERSION=v1.0.0\n"), map[string]string{
		"MODEMDECK_API_VERSION": "v2.0.0",
		"MODEMDECK_API_DIGEST":  testDigest("a"),
	})
	if err != nil {
		t.Fatalf("upsertEnvironment() error = %v", err)
	}
	text := string(updated)
	for _, expected := range []string{
		"# keep\n",
		"MODEMDECK_PORT=7577\n",
		"MODEMDECK_API_VERSION=v2.0.0\n",
		"MODEMDECK_API_DIGEST=" + testDigest("a") + "\n",
	} {
		if !strings.Contains(text, expected) {
			t.Errorf("updated environment missing %q: %s", expected, text)
		}
	}
}

type fakeChecker struct {
	result  updatecheck.Result
	observe func(bool)
}

func (checker fakeChecker) Check(ctx context.Context) updatecheck.Result {
	if checker.observe != nil {
		checker.observe(updatecheck.RefreshRequested(ctx))
	}
	return checker.result
}

type fakeManifestLoader struct {
	manifest ReleaseManifest
}

func (loader fakeManifestLoader) Load(context.Context, string) (ReleaseManifest, error) {
	return loader.manifest, nil
}

type fakeResolver struct {
	digests map[string]string
}

func (resolver fakeResolver) Resolve(_ context.Context, image, tag string) (string, bool, error) {
	digest, ok := resolver.digests[image+":"+tag]
	return digest, ok, nil
}

type fakeRuntime struct {
	cloudflared bool
	images      map[string]string
	started     *WorkerRequest
}

func (runtime *fakeRuntime) CurrentImage(_ context.Context, service string) (string, bool, error) {
	image, ok := runtime.images[service]
	return image, ok, nil
}

func (runtime *fakeRuntime) Managed(service string) bool {
	return service != "cloudflared" || runtime.cloudflared
}

func (runtime *fakeRuntime) StartWorker(_ context.Context, request WorkerRequest) error {
	runtime.started = &request
	return nil
}

func (*fakeRuntime) Apply(context.Context, Plan, ProgressReporter) error {
	return nil
}

type memoryOperationStore struct {
	operation *updatecheck.Operation
}

func (store *memoryOperationStore) Load() (*updatecheck.Operation, error) {
	if store.operation == nil {
		return nil, nil
	}
	copy := *store.operation
	return &copy, nil
}

func (store *memoryOperationStore) Save(operation updatecheck.Operation) error {
	store.operation = &operation
	return nil
}

func newTestController(
	t *testing.T,
	runtime Runtime,
	manifest ReleaseManifest,
	hardwareDigest string,
) *Controller {
	t.Helper()
	digests := map[string]string{
		manifest.Images.API + ":v2.0.0":      testDigest("a"),
		manifest.Images.Web + ":v2.0.0":      testDigest("b"),
		manifest.Images.Hardware + ":v2.0.0": hardwareDigest,
		manifest.Images.Updater + ":v2.0.0":  testDigest("c"),
	}
	controller, err := New(Options{
		Checker: fakeChecker{result: updatecheck.Result{
			Status:         updatecheck.StatusUpdateAvailable,
			CurrentVersion: "v1.9.2",
			LatestVersion:  "v2.0.0",
			CheckedAt:      "2026-08-01T00:00:00Z",
		}},
		Manifests:  fakeManifestLoader{manifest: manifest},
		Digests:    fakeResolver{digests: digests},
		Runtime:    runtime,
		Operations: &memoryOperationStore{},
		Now: func() time.Time {
			return time.Date(2026, 8, 1, 1, 0, 0, 0, time.UTC)
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return controller
}

func testManifest(cloudflaredDigest string) ReleaseManifest {
	var manifest ReleaseManifest
	manifest.SchemaVersion = 2
	manifest.Images.API = "ghcr.io/human-agent65535/modemdeck"
	manifest.Images.Web = "ghcr.io/human-agent65535/modemdeck-web"
	manifest.Images.Hardware = "ghcr.io/human-agent65535/modemdeck-hardware"
	manifest.Images.Updater = "ghcr.io/human-agent65535/modemdeck-updater"
	manifest.Cloudflared.Image = "cloudflare/cloudflared"
	manifest.Cloudflared.Version = "2026.7.3"
	manifest.Cloudflared.Digest = cloudflaredDigest
	return manifest
}

func testDigest(character string) string {
	return "sha256:" + strings.Repeat(character, 64)
}
