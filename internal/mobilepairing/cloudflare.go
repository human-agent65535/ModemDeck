package mobilepairing

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

const (
	cloudflareStatusTimeout       = 6 * time.Second
	cloudflareRequestTimeout      = 3 * time.Second
	cloudflareProbeBytes          = 32
	cloudflareConfigResponseBytes = 256 * 1024

	cloudflareAPIOrigin = "http://modemdeck:7575"
	cloudflareWebOrigin = "https://modemdeck:7577"

	CloudflareProbePath            = "/api/v1/mobile/tunnel/verify"
	CloudflareProbeChallengeHeader = "X-ModemDeck-Tunnel-Challenge"
	CloudflareProbeProofHeader     = "X-ModemDeck-Tunnel-Proof"
)

var ErrInvalidCloudflareConfiguration = errors.New(
	"invalid Cloudflare Tunnel configuration",
)

type CloudflareStatus struct {
	Enabled            bool     `json:"enabled"`
	ConnectorConnected bool     `json:"connector_connected"`
	Connected          bool     `json:"connected"`
	PublicURL          string   `json:"public_url"`
	APIURLs            []string `json:"api_urls,omitempty"`
	WebURLs            []string `json:"web_urls,omitempty"`
}

type Availability interface {
	Status(context.Context) CloudflareStatus
}

type ProbeResponder interface {
	CloudflareProbeProof(*http.Request) (string, bool)
}

type CloudflareGateway struct {
	status    CloudflareStatus
	readyURL  string
	configURL string
	apiOrigin string
	webOrigin string
	probeKey  []byte
	client    *http.Client
}

func NewCloudflareGateway(readyURL string) (*CloudflareGateway, error) {
	readyURL = strings.TrimSpace(readyURL)
	if readyURL == "" {
		return &CloudflareGateway{}, nil
	}
	normalizedReadyURL, err := normalizeReadyURL(readyURL)
	if err != nil {
		return nil, err
	}
	ready, err := url.Parse(normalizedReadyURL)
	if err != nil {
		return nil, fmt.Errorf(
			"%w: parse normalized readiness URL",
			ErrInvalidCloudflareConfiguration,
		)
	}
	config := *ready
	config.Path = "/config"
	config.RawPath = ""
	config.RawQuery = ""
	config.Fragment = ""
	normalizedAPIOrigin, err := normalizeOriginURL(cloudflareAPIOrigin)
	if err != nil {
		return nil, fmt.Errorf(
			"%w: invalid built-in API origin",
			ErrInvalidCloudflareConfiguration,
		)
	}
	normalizedWebOrigin, err := normalizeOriginURL(cloudflareWebOrigin)
	if err != nil {
		return nil, fmt.Errorf(
			"%w: invalid built-in Web origin",
			ErrInvalidCloudflareConfiguration,
		)
	}
	probeKey := make([]byte, cloudflareProbeBytes)
	if _, err := io.ReadFull(rand.Reader, probeKey); err != nil {
		return nil, fmt.Errorf("generate Cloudflare route probe key: %w", err)
	}
	return &CloudflareGateway{
		status: CloudflareStatus{
			Enabled: true,
		},
		readyURL:  normalizedReadyURL,
		configURL: config.String(),
		apiOrigin: normalizedAPIOrigin,
		webOrigin: normalizedWebOrigin,
		probeKey:  probeKey,
		client: &http.Client{
			Timeout: cloudflareRequestTimeout,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}, nil
}

func (gateway *CloudflareGateway) Status(
	ctx context.Context,
) CloudflareStatus {
	if gateway == nil || !gateway.status.Enabled {
		return CloudflareStatus{}
	}
	status := gateway.status
	ctx, cancel := context.WithTimeout(ctx, cloudflareStatusTimeout)
	defer cancel()
	routes, discovered := gateway.discoverRoutes(ctx)
	if discovered {
		status.APIURLs = routes.APIURLs
		status.WebURLs = routes.WebURLs
	}
	if len(status.APIURLs) == 1 {
		status.PublicURL = status.APIURLs[0]
	}
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		gateway.readyURL,
		nil,
	)
	if err != nil {
		return status
	}
	request.Header.Set("Accept", "application/json")
	response, err := gateway.client.Do(request)
	if err != nil {
		return status
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	_ = response.Body.Close()
	status.ConnectorConnected = response.StatusCode == http.StatusOK
	if !status.ConnectorConnected || status.PublicURL == "" {
		return status
	}

	challenge := make([]byte, cloudflareProbeBytes)
	if _, err := io.ReadFull(rand.Reader, challenge); err != nil {
		return status
	}
	request, err = http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		status.PublicURL+CloudflareProbePath,
		nil,
	)
	if err != nil {
		return status
	}
	request.Header.Set(
		CloudflareProbeChallengeHeader,
		base64.RawURLEncoding.EncodeToString(challenge),
	)
	response, err = gateway.client.Do(request)
	if err != nil {
		return status
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	_ = response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		return status
	}
	expectedProof := cloudflareProbeProof(gateway.probeKey, challenge)
	actualProof, err := base64.RawURLEncoding.DecodeString(
		strings.TrimSpace(response.Header.Get(CloudflareProbeProofHeader)),
	)
	if err != nil || len(actualProof) != len(expectedProof) {
		return status
	}
	status.Connected = subtle.ConstantTimeCompare(actualProof, expectedProof) == 1
	return status
}

type cloudflaredRuntimeConfiguration struct {
	Config struct {
		Ingress []struct {
			Hostname string  `json:"hostname"`
			Path     *string `json:"path"`
			Service  string  `json:"service"`
		} `json:"ingress"`
	} `json:"config"`
}

type discoveredCloudflareRoutes struct {
	APIURLs []string
	WebURLs []string
}

func (gateway *CloudflareGateway) discoverRoutes(
	ctx context.Context,
) (discoveredCloudflareRoutes, bool) {
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		gateway.configURL,
		nil,
	)
	if err != nil {
		return discoveredCloudflareRoutes{}, false
	}
	request.Header.Set("Accept", "application/json")
	response, err := gateway.client.Do(request)
	if err != nil {
		return discoveredCloudflareRoutes{}, false
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return discoveredCloudflareRoutes{}, false
	}
	content, err := io.ReadAll(io.LimitReader(
		response.Body,
		cloudflareConfigResponseBytes+1,
	))
	if err != nil || len(content) > cloudflareConfigResponseBytes {
		return discoveredCloudflareRoutes{}, false
	}
	var runtimeConfig cloudflaredRuntimeConfiguration
	if err := json.Unmarshal(content, &runtimeConfig); err != nil {
		return discoveredCloudflareRoutes{}, false
	}
	apiCandidates := make(map[string]struct{})
	webCandidates := make(map[string]struct{})
	for _, ingress := range runtimeConfig.Config.Ingress {
		if ingress.Path != nil && strings.TrimSpace(*ingress.Path) != "" {
			continue
		}
		origin, err := normalizeOriginURL(ingress.Service)
		if err != nil ||
			(origin != gateway.apiOrigin && origin != gateway.webOrigin) {
			continue
		}
		publicURL, err := publicURLFromHostname(ingress.Hostname)
		if err != nil {
			continue
		}
		if origin == gateway.apiOrigin {
			apiCandidates[publicURL] = struct{}{}
		} else {
			webCandidates[publicURL] = struct{}{}
		}
	}
	return discoveredCloudflareRoutes{
		APIURLs: sortedStringSet(apiCandidates),
		WebURLs: sortedStringSet(webCandidates),
	}, true
}

func sortedStringSet(values map[string]struct{}) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func (gateway *CloudflareGateway) CloudflareProbeProof(
	request *http.Request,
) (string, bool) {
	if gateway == nil || !gateway.status.Enabled || request == nil {
		return "", false
	}
	challenge, err := base64.RawURLEncoding.DecodeString(
		strings.TrimSpace(request.Header.Get(CloudflareProbeChallengeHeader)),
	)
	if err != nil || len(challenge) != cloudflareProbeBytes {
		return "", false
	}
	return base64.RawURLEncoding.EncodeToString(
		cloudflareProbeProof(gateway.probeKey, challenge),
	), true
}

func cloudflareProbeProof(key, challenge []byte) []byte {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(challenge)
	return mac.Sum(nil)
}

func publicURLFromHostname(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || strings.Contains(value, "*") {
		return "", fmt.Errorf(
			"%w: API ingress hostname must be concrete",
			ErrInvalidCloudflareConfiguration,
		)
	}
	return normalizePublicURL("https://" + value)
}

func normalizePublicURL(value string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil ||
		parsed.Scheme != "https" ||
		parsed.Host == "" ||
		parsed.User != nil ||
		parsed.Opaque != "" ||
		parsed.RawQuery != "" ||
		parsed.Fragment != "" ||
		(parsed.Path != "" && parsed.Path != "/") {
		return "", fmt.Errorf(
			"%w: public URL must be an HTTPS origin without a path",
			ErrInvalidCloudflareConfiguration,
		)
	}
	parsed.Path = ""
	parsed.Host = strings.ToLower(parsed.Host)
	return parsed.String(), nil
}

func normalizeOriginURL(value string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil ||
		(parsed.Scheme != "http" && parsed.Scheme != "https") ||
		parsed.Host == "" ||
		parsed.User != nil ||
		parsed.Opaque != "" ||
		parsed.RawQuery != "" ||
		parsed.Fragment != "" ||
		(parsed.Path != "" && parsed.Path != "/") {
		return "", fmt.Errorf(
			"%w: origin must be an HTTP(S) origin without a path",
			ErrInvalidCloudflareConfiguration,
		)
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	parsed.Host = strings.ToLower(parsed.Host)
	parsed.Path = ""
	return parsed.String(), nil
}

func normalizeReadyURL(value string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil ||
		(parsed.Scheme != "http" && parsed.Scheme != "https") ||
		parsed.Host == "" ||
		parsed.User != nil ||
		parsed.Opaque != "" ||
		parsed.RawQuery != "" ||
		parsed.Fragment != "" ||
		parsed.Path != "/ready" {
		return "", fmt.Errorf(
			"%w: readiness URL must be an HTTP(S) /ready endpoint",
			ErrInvalidCloudflareConfiguration,
		)
	}
	return parsed.String(), nil
}
