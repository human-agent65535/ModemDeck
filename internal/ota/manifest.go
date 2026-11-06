package ota

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

const (
	// The publish workflow signs a manifest that pins every image digest and
	// attaches it, with a detached signature at the same URL plus ".sig", to the
	// GitHub Release of each tag.
	DefaultManifestURLTemplate = "https://github.com/human-agent65535/ModemDeck/releases/download/%s/release-manifest.json"
	signedManifestSchema       = 3
	maximumManifestBytes       = 64 << 10
	defaultHTTPTimeout         = 8 * time.Second
)

var (
	ErrManifestUnavailable = errors.New("release manifest is unavailable")
	stableTagPattern       = regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
	digestPattern          = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
)

type ReleaseManifest struct {
	SchemaVersion int    `json:"schema_version"`
	Release       string `json:"release"`
	Images        struct {
		API      string `json:"api"`
		Web      string `json:"web"`
		Hardware string `json:"hardware"`
		Updater  string `json:"updater"`
	} `json:"images"`
	Digests struct {
		API      string `json:"api"`
		Web      string `json:"web"`
		Hardware string `json:"hardware"`
		Updater  string `json:"updater"`
	} `json:"digests"`
	Cloudflared struct {
		Image   string `json:"image"`
		Version string `json:"version"`
		Digest  string `json:"digest"`
	} `json:"cloudflared"`
}

func (manifest ReleaseManifest) Validate() error {
	if manifest.SchemaVersion != signedManifestSchema {
		return fmt.Errorf("unsupported release manifest schema")
	}
	if !stableTagPattern.MatchString(manifest.Release) {
		return fmt.Errorf("invalid release manifest version")
	}
	for component, digest := range map[string]string{
		"api":      manifest.Digests.API,
		"web":      manifest.Digests.Web,
		"hardware": manifest.Digests.Hardware,
		"updater":  manifest.Digests.Updater,
	} {
		if !digestPattern.MatchString(digest) {
			return fmt.Errorf("invalid %s image digest", component)
		}
	}
	expectedImages := map[string]string{
		"api":      "ghcr.io/human-agent65535/modemdeck",
		"web":      "ghcr.io/human-agent65535/modemdeck-web",
		"hardware": "ghcr.io/human-agent65535/modemdeck-hardware",
		"updater":  "ghcr.io/human-agent65535/modemdeck-updater",
	}
	actualImages := map[string]string{
		"api":      manifest.Images.API,
		"web":      manifest.Images.Web,
		"hardware": manifest.Images.Hardware,
		"updater":  manifest.Images.Updater,
	}
	for component, expected := range expectedImages {
		if strings.TrimSpace(actualImages[component]) != expected {
			return fmt.Errorf("invalid %s image repository", component)
		}
	}
	if manifest.Cloudflared.Image != "cloudflare/cloudflared" {
		return fmt.Errorf("invalid cloudflared image repository")
	}
	if !validImageTag(manifest.Cloudflared.Version) ||
		strings.EqualFold(manifest.Cloudflared.Version, "latest") ||
		!digestPattern.MatchString(manifest.Cloudflared.Digest) {
		return fmt.Errorf("invalid cloudflared image reference")
	}
	return nil
}

func validImageTag(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for index, character := range value {
		if (character >= 'a' && character <= 'z') ||
			(character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') ||
			character == '_' || character == '.' || character == '-' {
			if index == 0 && (character == '.' || character == '-') {
				return false
			}
			continue
		}
		return false
	}
	return true
}

type ManifestLoader interface {
	Load(context.Context, string) (ReleaseManifest, error)
}

type HTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}

type HTTPManifestLoader struct {
	urlTemplate string
	client      HTTPClient
	keys        []ed25519.PublicKey
}

func NewHTTPManifestLoader(urlTemplate string, client HTTPClient) (*HTTPManifestLoader, error) {
	keys, err := parseReleaseSigningKeys(trustedReleaseSigningKeys)
	if err != nil {
		return nil, err
	}
	return newHTTPManifestLoader(urlTemplate, client, keys), nil
}

func newHTTPManifestLoader(
	urlTemplate string,
	client HTTPClient,
	keys []ed25519.PublicKey,
) *HTTPManifestLoader {
	if strings.TrimSpace(urlTemplate) == "" {
		urlTemplate = DefaultManifestURLTemplate
	}
	if client == nil {
		client = &http.Client{Timeout: defaultHTTPTimeout}
	}
	return &HTTPManifestLoader{urlTemplate: urlTemplate, client: client, keys: keys}
}

func (loader *HTTPManifestLoader) Load(
	ctx context.Context,
	version string,
) (ReleaseManifest, error) {
	if !stableTagPattern.MatchString(version) {
		return ReleaseManifest{}, ErrManifestUnavailable
	}
	manifestURL := fmt.Sprintf(loader.urlTemplate, version)
	body, err := loader.fetch(ctx, manifestURL, "application/json", maximumManifestBytes)
	if err != nil {
		return ReleaseManifest{}, ErrManifestUnavailable
	}
	signature, err := loader.fetch(ctx, manifestURL+".sig", "text/plain", maximumSignatureBytes)
	if err != nil {
		return ReleaseManifest{}, ErrManifestUnavailable
	}
	if err := verifyManifestSignature(loader.keys, body, signature); err != nil {
		return ReleaseManifest{}, errors.Join(ErrManifestUnavailable, err)
	}
	var manifest ReleaseManifest
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return ReleaseManifest{}, ErrManifestUnavailable
	}
	if err := manifest.Validate(); err != nil {
		return ReleaseManifest{}, errors.Join(ErrManifestUnavailable, err)
	}
	// A validly signed manifest of another release must not be replayed as
	// this one, e.g. to roll a device back to a vulnerable version.
	if manifest.Release != version {
		return ReleaseManifest{}, errors.Join(
			ErrManifestUnavailable,
			fmt.Errorf("release manifest is for %s, not %s", manifest.Release, version),
		)
	}
	return manifest, nil
}

func (loader *HTTPManifestLoader) fetch(
	ctx context.Context,
	url string,
	accept string,
	limit int64,
) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", accept)
	request.Header.Set("User-Agent", "ModemDeck-Updater")
	response, err := loader.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch %s: status %d", url, response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("fetch %s: response is too large", url)
	}
	return body, nil
}
