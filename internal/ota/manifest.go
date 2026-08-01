package ota

import (
	"context"
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
	DefaultManifestURLTemplate = "https://raw.githubusercontent.com/human-agent65535/ModemDeck/%s/deploy/release-manifest.json"
	maximumManifestBytes       = 64 << 10
	defaultHTTPTimeout         = 8 * time.Second
)

var (
	ErrManifestUnavailable = errors.New("release manifest is unavailable")
	stableTagPattern       = regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
	digestPattern          = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
)

type ReleaseManifest struct {
	SchemaVersion int `json:"schema_version"`
	Images        struct {
		API      string `json:"api"`
		Web      string `json:"web"`
		Hardware string `json:"hardware"`
		Updater  string `json:"updater"`
	} `json:"images"`
	APIVersion      string `json:"api_version"`
	WebVersion      string `json:"web_version"`
	HardwareVersion string `json:"hardware_version"`
	UpdaterVersion  string `json:"updater_version"`
	Cloudflared     struct {
		Image   string `json:"image"`
		Version string `json:"version"`
		Digest  string `json:"digest"`
	} `json:"cloudflared"`
}

func (manifest ReleaseManifest) Validate() error {
	if manifest.SchemaVersion != 1 {
		return fmt.Errorf("unsupported release manifest schema")
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
	componentVersions := map[string]string{
		"api":      manifest.APIVersion,
		"web":      manifest.WebVersion,
		"hardware": manifest.HardwareVersion,
		"updater":  manifest.UpdaterVersion,
	}
	for component, version := range componentVersions {
		if !stableTagPattern.MatchString(version) {
			return fmt.Errorf("invalid %s release version", component)
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
}

func NewHTTPManifestLoader(urlTemplate string, client HTTPClient) *HTTPManifestLoader {
	if strings.TrimSpace(urlTemplate) == "" {
		urlTemplate = DefaultManifestURLTemplate
	}
	if client == nil {
		client = &http.Client{Timeout: defaultHTTPTimeout}
	}
	return &HTTPManifestLoader{urlTemplate: urlTemplate, client: client}
}

func (loader *HTTPManifestLoader) Load(
	ctx context.Context,
	version string,
) (ReleaseManifest, error) {
	if !stableTagPattern.MatchString(version) {
		return ReleaseManifest{}, ErrManifestUnavailable
	}
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		fmt.Sprintf(loader.urlTemplate, version),
		nil,
	)
	if err != nil {
		return ReleaseManifest{}, ErrManifestUnavailable
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "ModemDeck-Updater")
	response, err := loader.client.Do(request)
	if err != nil {
		return ReleaseManifest{}, ErrManifestUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return ReleaseManifest{}, ErrManifestUnavailable
	}
	var manifest ReleaseManifest
	decoder := json.NewDecoder(io.LimitReader(response.Body, maximumManifestBytes))
	if err := decoder.Decode(&manifest); err != nil {
		return ReleaseManifest{}, ErrManifestUnavailable
	}
	if err := manifest.Validate(); err != nil {
		return ReleaseManifest{}, errors.Join(ErrManifestUnavailable, err)
	}
	return manifest, nil
}
