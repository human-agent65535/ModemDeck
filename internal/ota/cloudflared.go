package ota

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/human-agent65535/modemdeck/internal/updatecheck"
)

const tunnelRepository = "cloudflare/cloudflared"

var tunnelTag = regexp.MustCompile(`^[0-9]{4}\.(?:[1-9]|1[0-2])\.(?:0|[1-9][0-9]{0,8})$`)

func fetchTunnelResource(ctx context.Context, client HTTPClient, url, accept, token string) ([]byte, http.Header, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Accept", accept)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("cloudflared release check: HTTP %d", resp.StatusCode)
	}
	const limit = 2 << 20
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if len(body) > limit {
		return nil, nil, ErrManifestUnavailable
	}
	return body, resp.Header, err
}

func latestTunnel(ctx context.Context, client HTTPClient) (Target, error) {
	body, _, err := fetchTunnelResource(ctx, client, "https://api.github.com/repos/cloudflare/cloudflared/releases/latest", "application/json", "")
	if err != nil {
		return Target{}, err
	}
	var release struct {
		Tag        string `json:"tag_name"`
		Draft      *bool  `json:"draft"`
		Prerelease *bool  `json:"prerelease"`
	}
	if json.Unmarshal(body, &release) != nil || release.Draft == nil || *release.Draft || release.Prerelease == nil || *release.Prerelease || !tunnelTag.MatchString(release.Tag) {
		return Target{}, ErrManifestUnavailable
	}
	body, _, err = fetchTunnelResource(ctx, client, "https://auth.docker.io/token?service=registry.docker.io&scope=repository:cloudflare/cloudflared:pull", "application/json", "")
	if err != nil {
		return Target{}, err
	}
	var auth struct {
		Token string `json:"token"`
	}
	if json.Unmarshal(body, &auth) != nil || auth.Token == "" {
		return Target{}, ErrManifestUnavailable
	}
	body, headers, err := fetchTunnelResource(ctx, client, "https://registry-1.docker.io/v2/cloudflare/cloudflared/manifests/"+release.Tag,
		"application/vnd.oci.image.index.v1+json, application/vnd.docker.distribution.manifest.list.v2+json, application/vnd.oci.image.manifest.v1+json, application/vnd.docker.distribution.manifest.v2+json", auth.Token)
	if err != nil {
		return Target{}, err
	}
	digest := fmt.Sprintf("sha256:%x", sha256.Sum256(body))
	if headers.Get("Docker-Content-Digest") != digest || !json.Valid(body) {
		return Target{}, ErrManifestUnavailable
	}
	return Target{
		Name:       "cloudflared",
		Service:    "cloudflared",
		Image:      tunnelRepository,
		Version:    release.Tag,
		Digest:     digest,
		Reference:  tunnelRepository + ":" + release.Tag + "@" + digest,
		VersionEnv: "MODEMDECK_CLOUDFLARED_VERSION",
		DigestEnv:  "MODEMDECK_CLOUDFLARED_DIGEST",
	}, nil
}

func newerTunnel(installed, candidate string) bool {
	if !tunnelTag.MatchString(installed) || !tunnelTag.MatchString(candidate) {
		return false
	}
	left, right := strings.Split(installed, "."), strings.Split(candidate, ".")
	for i := range left {
		a, _ := strconv.Atoi(left[i])
		b, _ := strconv.Atoi(right[i])
		if a != b {
			return a > b
		}
	}
	return false
}

func (controller *Controller) tunnelPlan(ctx context.Context) (Plan, error) {
	result := updatecheck.Result{Status: updatecheck.StatusUnavailable, CheckedAt: controller.now().UTC().Format(time.RFC3339), CurrentVersion: "—", TunnelManaged: controller.runtime.Managed("cloudflared")}
	result.Operation, _ = controller.operations.Load()
	plan := Plan{Result: result, TunnelOnly: true}
	if !result.TunnelManaged {
		return plan, ErrNoUpdate
	}
	ref, present, err := controller.runtime.CurrentImage(ctx, "cloudflared")
	if err != nil {
		return plan, err
	}
	current := imageVersion(ref)
	if !present || !tunnelTag.MatchString(current) {
		return plan, errors.New("installed cloudflared version unavailable")
	}
	plan.Result.CurrentVersion = current
	target, err := latestTunnel(ctx, controller.tunnelClient)
	if err != nil {
		return plan, err
	}
	target.CurrentRef, target.Current = ref, current
	target.Changed = !newerTunnel(current, target.Version) && (current != target.Version || imageDigest(ref) != target.Digest)
	plan.Targets = []Target{target}
	plan.Result.LatestVersion = target.Version
	plan.Result.TunnelDigest = target.Digest
	plan.Result.Status = updatecheck.StatusUpToDate
	if target.Changed {
		plan.Result.Status = updatecheck.StatusUpdateAvailable
	} else if newerTunnel(current, target.Version) {
		plan.Result.Status = updatecheck.StatusDevelopment
	}
	plan.Result.ApplyAvailable = target.Changed
	plan.Result.Components = []updatecheck.Component{{Name: target.Name, CurrentVersion: current, TargetVersion: target.Version, Changed: target.Changed}}
	return plan, nil
}
