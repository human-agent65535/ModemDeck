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
	"sync"
	"time"
)

const (
	cloudflareStatusTimeout       = 6 * time.Second
	cloudflareRequestTimeout      = 3 * time.Second
	cloudflareHealthyRefresh      = 60 * time.Second
	cloudflareUnavailableRefresh  = 15 * time.Second
	cloudflareProbeBytes          = 32
	cloudflareConfigResponseBytes = 256 * 1024

	cloudflareAPIOrigin    = "http://modemdeck:7575"
	cloudflareAPIOriginTLS = "https://modemdeck:7575"
	cloudflareWebOrigin    = "http://modemdeck:7576"
	cloudflareWebOriginTLS = "https://modemdeck:7576"

	CloudflareProbePath            = "/api/v1/mobile/tunnel/verify"
	CloudflareProbeChallengeHeader = "X-ModemDeck-Tunnel-Challenge"
	CloudflareProbeProofHeader     = "X-ModemDeck-Tunnel-Proof"
)

var ErrInvalidCloudflareConfiguration = errors.New(
	"invalid Cloudflare Tunnel configuration",
)

type CloudflareStatus struct {
	Enabled            bool                          `json:"enabled"`
	ConnectorConnected bool                          `json:"connector_connected"`
	Connected          bool                          `json:"connected"`
	PublicURL          string                        `json:"public_url"`
	APIURLs            []string                      `json:"api_urls,omitempty"`
	VerifiedAPIURLs    []string                      `json:"verified_api_urls,omitempty"`
	WebURLs            []string                      `json:"web_urls,omitempty"`
	OriginRoutes       []CloudflareOriginRouteStatus `json:"origin_routes,omitempty"`
}

type CloudflareOriginRouteStatus struct {
	Kind              string `json:"kind"`
	PublicURL         string `json:"public_url"`
	ServiceURL        string `json:"service_url"`
	HTTPS             bool   `json:"https"`
	HTTP2             bool   `json:"http2"`
	TLSNameConfigured bool   `json:"tls_name_configured"`
	TLSVerification   bool   `json:"tls_verification"`
}

type Availability interface {
	Status(context.Context) CloudflareStatus
}

type WebIngressMatcher interface {
	IsWebIngress(context.Context, string) bool
}

type ProbeResponder interface {
	CloudflareProbeProof(*http.Request) (string, bool)
}

type Refresher interface {
	Refresh(context.Context) CloudflareStatus
}

type CloudflareGateway struct {
	statusMu sync.RWMutex
	status   CloudflareStatus

	refreshMu   sync.Mutex
	refreshDone chan struct{}

	readyURL           string
	configURL          string
	apiOrigin          string
	apiOriginTLS       string
	webOrigin          string
	webOriginTLS       string
	probeKey           []byte
	client             *http.Client
	healthyRefresh     time.Duration
	unavailableRefresh time.Duration
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
	normalizedAPIOriginTLS, err := normalizeOriginURL(cloudflareAPIOriginTLS)
	if err != nil {
		return nil, fmt.Errorf(
			"%w: invalid built-in HTTPS API origin",
			ErrInvalidCloudflareConfiguration,
		)
	}
	normalizedWebOriginTLS, err := normalizeOriginURL(cloudflareWebOriginTLS)
	if err != nil {
		return nil, fmt.Errorf(
			"%w: invalid built-in HTTPS Web origin",
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
		readyURL:     normalizedReadyURL,
		configURL:    config.String(),
		apiOrigin:    normalizedAPIOrigin,
		apiOriginTLS: normalizedAPIOriginTLS,
		webOrigin:    normalizedWebOrigin,
		webOriginTLS: normalizedWebOriginTLS,
		probeKey:     probeKey,
		client: &http.Client{
			Timeout: cloudflareRequestTimeout,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		healthyRefresh:     cloudflareHealthyRefresh,
		unavailableRefresh: cloudflareUnavailableRefresh,
	}, nil
}

func (gateway *CloudflareGateway) Status(
	ctx context.Context,
) CloudflareStatus {
	_ = ctx
	return gateway.statusSnapshot()
}

func (gateway *CloudflareGateway) Refresh(
	ctx context.Context,
) CloudflareStatus {
	if gateway == nil {
		return CloudflareStatus{}
	}
	current := gateway.statusSnapshot()
	if !current.Enabled {
		return CloudflareStatus{}
	}
	if ctx == nil {
		ctx = context.Background()
	}

	gateway.refreshMu.Lock()
	if gateway.refreshDone != nil {
		done := gateway.refreshDone
		gateway.refreshMu.Unlock()
		select {
		case <-done:
		case <-ctx.Done():
		}
		return gateway.statusSnapshot()
	}
	done := make(chan struct{})
	gateway.refreshDone = done
	gateway.refreshMu.Unlock()

	status := gateway.scan(ctx, current)
	if ctx.Err() == nil {
		gateway.statusMu.Lock()
		gateway.status = cloneCloudflareStatus(status)
		gateway.statusMu.Unlock()
	}

	gateway.refreshMu.Lock()
	gateway.refreshDone = nil
	close(done)
	gateway.refreshMu.Unlock()
	return gateway.statusSnapshot()
}

func (gateway *CloudflareGateway) Run(ctx context.Context) {
	if gateway == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if !gateway.statusSnapshot().Enabled {
		return
	}
	for {
		status := gateway.Refresh(ctx)
		delay := gateway.unavailableRefresh
		if status.Connected {
			delay = gateway.healthyRefresh
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return
		case <-timer.C:
		}
	}
}

func (gateway *CloudflareGateway) scan(
	ctx context.Context,
	previous CloudflareStatus,
) CloudflareStatus {
	status := cloneCloudflareStatus(previous)
	status.ConnectorConnected = false
	status.Connected = false
	status.PublicURL = ""
	status.VerifiedAPIURLs = []string{}
	ctx, cancel := context.WithTimeout(ctx, cloudflareStatusTimeout)
	defer cancel()
	routes, discovered := gateway.discoverRoutes(ctx)
	if discovered {
		status.APIURLs = routes.APIURLs
		status.WebURLs = routes.WebURLs
		status.OriginRoutes = routes.OriginRoutes
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
	if !status.ConnectorConnected || len(status.APIURLs) == 0 {
		return status
	}

	status.VerifiedAPIURLs = gateway.verifyAPIIngresses(ctx, status.APIURLs)
	status.Connected = len(status.VerifiedAPIURLs) > 0
	if status.Connected {
		status.PublicURL = status.VerifiedAPIURLs[0]
	}
	return status
}

func (gateway *CloudflareGateway) verifyAPIIngresses(
	ctx context.Context,
	publicURLs []string,
) []string {
	results := make(chan string, len(publicURLs))
	var wait sync.WaitGroup
	for _, candidate := range publicURLs {
		publicURL := candidate
		wait.Add(1)
		go func() {
			defer wait.Done()
			if gateway.verifyAPIIngress(ctx, publicURL) {
				results <- publicURL
			}
		}()
	}
	wait.Wait()
	close(results)
	verified := make([]string, 0, len(publicURLs))
	for publicURL := range results {
		verified = append(verified, publicURL)
	}
	sort.Strings(verified)
	return verified
}

func (gateway *CloudflareGateway) verifyAPIIngress(
	ctx context.Context,
	publicURL string,
) bool {
	challenge := make([]byte, cloudflareProbeBytes)
	if _, err := io.ReadFull(rand.Reader, challenge); err != nil {
		return false
	}
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		publicURL+CloudflareProbePath,
		nil,
	)
	if err != nil {
		return false
	}
	request.Header.Set(
		CloudflareProbeChallengeHeader,
		base64.RawURLEncoding.EncodeToString(challenge),
	)
	response, err := gateway.client.Do(request)
	if err != nil {
		return false
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	_ = response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		return false
	}
	expectedProof := cloudflareProbeProof(gateway.probeKey, challenge)
	actualProof, err := base64.RawURLEncoding.DecodeString(
		strings.TrimSpace(response.Header.Get(CloudflareProbeProofHeader)),
	)
	if err != nil || len(actualProof) != len(expectedProof) {
		return false
	}
	return subtle.ConstantTimeCompare(actualProof, expectedProof) == 1
}

func (gateway *CloudflareGateway) IsWebIngress(
	ctx context.Context,
	requestHost string,
) bool {
	_ = ctx
	status := gateway.statusSnapshot()
	if !status.Enabled {
		return false
	}
	publicURL, err := publicURLFromRequestHost(requestHost)
	if err != nil {
		return false
	}
	for _, candidate := range status.WebURLs {
		if candidate == publicURL {
			return true
		}
	}
	return false
}

func (gateway *CloudflareGateway) statusSnapshot() CloudflareStatus {
	if gateway == nil {
		return CloudflareStatus{}
	}
	gateway.statusMu.RLock()
	defer gateway.statusMu.RUnlock()
	return cloneCloudflareStatus(gateway.status)
}

func cloneCloudflareStatus(status CloudflareStatus) CloudflareStatus {
	status.APIURLs = append([]string(nil), status.APIURLs...)
	status.VerifiedAPIURLs = append([]string(nil), status.VerifiedAPIURLs...)
	status.WebURLs = append([]string(nil), status.WebURLs...)
	status.OriginRoutes = append(
		[]CloudflareOriginRouteStatus(nil),
		status.OriginRoutes...,
	)
	if status.APIURLs == nil {
		status.APIURLs = []string{}
	}
	if status.VerifiedAPIURLs == nil {
		status.VerifiedAPIURLs = []string{}
	}
	if status.WebURLs == nil {
		status.WebURLs = []string{}
	}
	if status.OriginRoutes == nil {
		status.OriginRoutes = []CloudflareOriginRouteStatus{}
	}
	return status
}

type cloudflaredRuntimeOriginRequest struct {
	OriginServerName string `json:"originServerName"`
	MatchSNIToHost   bool   `json:"matchSNItoHost"`
	NoTLSVerify      bool   `json:"noTLSVerify"`
	HTTP2Origin      bool   `json:"http2Origin"`
}

type cloudflaredRuntimeConfiguration struct {
	Config struct {
		Ingress []struct {
			Hostname      string                          `json:"hostname"`
			Path          *string                         `json:"path"`
			Service       string                          `json:"service"`
			OriginRequest cloudflaredRuntimeOriginRequest `json:"originRequest"`
		} `json:"ingress"`
	} `json:"config"`
}

type discoveredCloudflareRoutes struct {
	APIURLs      []string
	WebURLs      []string
	OriginRoutes []CloudflareOriginRouteStatus
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
	originRoutes := make(map[string]CloudflareOriginRouteStatus)
	for _, ingress := range runtimeConfig.Config.Ingress {
		if ingress.Path != nil && strings.TrimSpace(*ingress.Path) != "" {
			continue
		}
		origin, err := normalizeOriginURL(ingress.Service)
		if err != nil {
			continue
		}
		isAPI := origin == gateway.apiOrigin || origin == gateway.apiOriginTLS
		isWeb := origin == gateway.webOrigin || origin == gateway.webOriginTLS
		if !isAPI && !isWeb {
			continue
		}
		publicURL, err := publicURLFromHostname(ingress.Hostname)
		if err != nil {
			continue
		}
		kind := "web"
		if isAPI {
			kind = "api"
			apiCandidates[publicURL] = struct{}{}
		} else {
			webCandidates[publicURL] = struct{}{}
		}
		originRoutes[kind+"\x00"+publicURL] = CloudflareOriginRouteStatus{
			Kind:       kind,
			PublicURL:  publicURL,
			ServiceURL: origin,
			HTTPS: origin == gateway.apiOriginTLS ||
				origin == gateway.webOriginTLS,
			HTTP2: ingress.OriginRequest.HTTP2Origin,
			TLSNameConfigured: ingress.OriginRequest.MatchSNIToHost ||
				strings.TrimSpace(ingress.OriginRequest.OriginServerName) != "",
			TLSVerification: !ingress.OriginRequest.NoTLSVerify,
		}
	}
	return discoveredCloudflareRoutes{
		APIURLs:      sortedStringSet(apiCandidates),
		WebURLs:      sortedStringSet(webCandidates),
		OriginRoutes: sortedCloudflareOriginRoutes(originRoutes),
	}, true
}

func sortedCloudflareOriginRoutes(
	values map[string]CloudflareOriginRouteStatus,
) []CloudflareOriginRouteStatus {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]CloudflareOriginRouteStatus, 0, len(keys))
	for _, key := range keys {
		result = append(result, values[key])
	}
	return result
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
	if gateway == nil || !gateway.statusSnapshot().Enabled || request == nil {
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

func publicURLFromRequestHost(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || strings.ContainsAny(value, ",/\\") {
		return "", fmt.Errorf(
			"%w: request host must be a single hostname",
			ErrInvalidCloudflareConfiguration,
		)
	}
	parsed, err := url.Parse("https://" + value)
	if err != nil ||
		parsed.User != nil ||
		parsed.Hostname() == "" ||
		parsed.RawQuery != "" ||
		parsed.Fragment != "" {
		return "", fmt.Errorf(
			"%w: request host is invalid",
			ErrInvalidCloudflareConfiguration,
		)
	}
	return publicURLFromHostname(strings.ToLower(parsed.Hostname()))
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
