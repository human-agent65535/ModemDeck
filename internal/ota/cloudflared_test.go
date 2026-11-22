package ota

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/human-agent65535/modemdeck/internal/updatecheck"
)

type tunnelTransport func(*http.Request) (*http.Response, error)

func (transport tunnelTransport) Do(req *http.Request) (*http.Response, error) { return transport(req) }

func TestIndependentTunnelUpgrade(t *testing.T) {
	ctx := context.Background()
	body := `{"schemaVersion":2}`
	digest := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(body)))
	current := "cloudflare/cloudflared:2026.10.3@" + testDigest("1")
	runtime := &fakeRuntime{cloudflared: true, images: map[string]string{"cloudflared": current}}
	controller := newTestController(t, runtime, testManifest(testDigest("2")), testDigest("3"))
	registryDigest := digest
	controller.tunnelClient = tunnelTransport(func(req *http.Request) (*http.Response, error) {
		var reply string
		headers := http.Header{}
		switch req.URL.Host {
		case "api.github.com":
			reply = `{"tag_name":"2026.11.1","draft":false,"prerelease":false}`
		case "auth.docker.io":
			reply = `{"token":"mock-token"}`
		case "registry-1.docker.io":
			if req.Header.Get("Authorization") != "Bearer mock-token" || req.URL.Path != "/v2/cloudflare/cloudflared/manifests/2026.11.1" {
				t.Fatal("unexpected registry request")
			}
			reply = body
			headers.Set("Docker-Content-Digest", registryDigest)
		default:
			t.Fatalf("unexpected host %s", req.URL.Host)
		}
		return &http.Response{StatusCode: 200, Header: headers, Body: io.NopCloser(strings.NewReader(reply))}, nil
	})
	request := updatecheck.ApplyRequest{Version: "2026.11.1", TunnelDigest: digest}
	check := controller.Check(updatecheck.WithTunnelCheck(ctx))
	if !check.ApplyAvailable || check.CurrentVersion != "2026.10.3" || check.TunnelDigest != digest {
		t.Fatal(check)
	}
	registryDigest = testDigest("4")
	if _, err := controller.Apply(ctx, request); !errors.Is(err, ErrManifestUnavailable) {
		t.Fatalf("digest mismatch: %v", err)
	}
	registryDigest = digest
	stale := request
	stale.TunnelDigest = testDigest("5")
	if _, err := controller.Apply(ctx, stale); !errors.Is(err, ErrVersionChanged) {
		t.Fatalf("stale preview: %v", err)
	}
	op, err := controller.Apply(ctx, request)
	if err != nil || len(op.Components) != 1 || op.Components[0].Name != "cloudflared" || runtime.started.TunnelDigest != digest {
		t.Fatalf("operation %+v: %v", op, err)
	}
	if err := controller.RunWorker(ctx, *runtime.started); err != nil {
		t.Fatal(err)
	}
	plan, err := controller.applyPlan(ctx, digest)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	env := filepath.Join(dir, ".env")
	if err := os.WriteFile(env, []byte("MODEMDECK_VERSION=v1.2.3\nMODEMDECK_CLOUDFLARE_ENABLED=true\n"), 0600); err != nil {
		t.Fatal(err)
	}
	runner := &recordingRunner{}
	docker := newDockerRuntimeForTest(t, runner, dir)
	if err := docker.Apply(ctx, plan, nil); err != nil {
		t.Fatal(err)
	}
	next, err := os.ReadFile(env)
	if err != nil || parseEnvironment(next)["MODEMDECK_VERSION"] != "v1.2.3" || parseEnvironment(next)["MODEMDECK_CLOUDFLARED_DIGEST"] != digest {
		t.Fatalf("environment %s: %v", next, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "state", InstalledVersionFilename)); !os.IsNotExist(err) {
		t.Fatal("tunnel wrote app version")
	}
	command := runner.commands[len(runner.commands)-1]
	if command[len(command)-1] != "cloudflared" || !containsSequence(command, "--no-deps", "--wait") {
		t.Fatal(command)
	}
	runtime.images["cloudflared"] = plan.Targets[0].Reference
	if checked := controller.Check(updatecheck.WithTunnelCheck(ctx)); checked.ApplyAvailable || checked.Status != updatecheck.StatusUpToDate {
		t.Fatal(checked)
	}
	for _, component := range controller.Check(ctx).Components {
		if component.Name == "cloudflared" && component.Changed {
			t.Fatal("app release would downgrade connector")
		}
	}
	runtime.cloudflared = false
	if _, err := controller.Apply(ctx, request); !errors.Is(err, ErrNoUpdate) {
		t.Fatalf("disabled tunnel: %v", err)
	}
}
