package mobilepairing

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	cloudflareStatusTimeout = 3 * time.Second
	cloudflareProbeBytes    = 32

	CloudflareProbePath            = "/api/v1/mobile/tunnel/verify"
	CloudflareProbeChallengeHeader = "X-ModemDeck-Tunnel-Challenge"
	CloudflareProbeProofHeader     = "X-ModemDeck-Tunnel-Proof"
)

var ErrInvalidCloudflareConfiguration = errors.New(
	"invalid Cloudflare Tunnel configuration",
)

type CloudflareStatus struct {
	Enabled            bool   `json:"enabled"`
	ConnectorConnected bool   `json:"connector_connected"`
	Connected          bool   `json:"connected"`
	PublicURL          string `json:"public_url"`
}

type Availability interface {
	Status(context.Context) CloudflareStatus
}

type ProbeResponder interface {
	CloudflareProbeProof(*http.Request) (string, bool)
}

type CloudflareGateway struct {
	status         CloudflareStatus
	readyURL       string
	publicProbeURL string
	probeKey       []byte
	client         *http.Client
}

func NewCloudflareGateway(
	publicURL string,
	readyURL string,
) (*CloudflareGateway, error) {
	publicURL = strings.TrimSpace(publicURL)
	readyURL = strings.TrimSpace(readyURL)
	if publicURL == "" && readyURL == "" {
		return &CloudflareGateway{}, nil
	}
	if publicURL == "" || readyURL == "" {
		return nil, fmt.Errorf(
			"%w: public URL and readiness URL must be configured together",
			ErrInvalidCloudflareConfiguration,
		)
	}

	normalizedPublicURL, err := normalizePublicURL(publicURL)
	if err != nil {
		return nil, err
	}
	normalizedReadyURL, err := normalizeReadyURL(readyURL)
	if err != nil {
		return nil, err
	}
	probeKey := make([]byte, cloudflareProbeBytes)
	if _, err := io.ReadFull(rand.Reader, probeKey); err != nil {
		return nil, fmt.Errorf("generate Cloudflare route probe key: %w", err)
	}
	return &CloudflareGateway{
		status: CloudflareStatus{
			Enabled:   true,
			PublicURL: normalizedPublicURL,
		},
		readyURL:       normalizedReadyURL,
		publicProbeURL: normalizedPublicURL + CloudflareProbePath,
		probeKey:       probeKey,
		client: &http.Client{
			Timeout: cloudflareStatusTimeout,
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
	if !status.ConnectorConnected {
		return status
	}

	challenge := make([]byte, cloudflareProbeBytes)
	if _, err := io.ReadFull(rand.Reader, challenge); err != nil {
		return status
	}
	request, err = http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		gateway.publicProbeURL,
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
		parsed.Fragment != "" {
		return "", fmt.Errorf(
			"%w: readiness URL must be HTTP or HTTPS",
			ErrInvalidCloudflareConfiguration,
		)
	}
	return parsed.String(), nil
}
