package mobilepairing

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const cloudflareStatusTimeout = 2 * time.Second

var ErrInvalidCloudflareConfiguration = errors.New(
	"invalid Cloudflare Tunnel configuration",
)

type CloudflareStatus struct {
	Enabled   bool   `json:"enabled"`
	Connected bool   `json:"connected"`
	PublicURL string `json:"public_url"`
}

type Availability interface {
	Status(context.Context) CloudflareStatus
}

type CloudflareGateway struct {
	status   CloudflareStatus
	readyURL string
	client   *http.Client
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
	return &CloudflareGateway{
		status: CloudflareStatus{
			Enabled:   true,
			Connected: false,
			PublicURL: normalizedPublicURL,
		},
		readyURL: normalizedReadyURL,
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
	status.Connected = response.StatusCode == http.StatusOK
	return status
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
