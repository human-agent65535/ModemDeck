package agentclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"time"
)

const (
	APIVersion            = "v1"
	defaultRequestTimeout = 2 * time.Second
	maxHealthBodyBytes    = 64 << 10
)

var ErrInvalidSocketPath = errors.New("host agent socket path must be absolute")

type Capabilities struct {
	Discovery   bool `json:"discovery"`
	Dial        bool `json:"dial"`
	AnswerCall  bool `json:"answer_call"`
	HangupCall  bool `json:"hangup_call"`
	SendMessage bool `json:"send_message"`
}

type ProviderHealth struct {
	Name         string       `json:"name"`
	Available    bool         `json:"available"`
	Capabilities Capabilities `json:"capabilities"`
}

type Health struct {
	Status       string         `json:"status"`
	APIVersion   string         `json:"api_version"`
	AgentVersion string         `json:"agent_version"`
	Provider     ProviderHealth `json:"provider"`
}

type Client struct {
	httpClient *http.Client
}

func New(socketPath string, timeout time.Duration) (*Client, error) {
	socketPath = filepath.Clean(socketPath)
	if !filepath.IsAbs(socketPath) {
		return nil, ErrInvalidSocketPath
	}
	if timeout <= 0 {
		timeout = defaultRequestTimeout
	}
	dialer := &net.Dialer{Timeout: timeout}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, "unix", socketPath)
		},
		MaxIdleConns:        2,
		MaxIdleConnsPerHost: 2,
		IdleConnTimeout:     30 * time.Second,
	}
	return &Client{httpClient: &http.Client{
		Transport: transport,
		Timeout:   timeout,
	}}, nil
}

func (client *Client) Health(ctx context.Context) (Health, error) {
	if client == nil || client.httpClient == nil {
		return Health{}, errors.New("host agent client is not initialized")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://modemdeck-agent/v1/health", nil)
	if err != nil {
		return Health{}, fmt.Errorf("create host agent health request: %w", err)
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		return Health{}, fmt.Errorf("request host agent health: %w", err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(io.LimitReader(response.Body, maxHealthBodyBytes+1))
	if err != nil {
		return Health{}, fmt.Errorf("read host agent health: %w", err)
	}
	if len(body) > maxHealthBodyBytes {
		return Health{}, errors.New("host agent health response is too large")
	}
	if response.StatusCode != http.StatusOK {
		return Health{}, fmt.Errorf("host agent health returned HTTP %d", response.StatusCode)
	}

	var health Health
	if err := json.Unmarshal(body, &health); err != nil {
		return Health{}, fmt.Errorf("decode host agent health: %w", err)
	}
	if health.APIVersion != APIVersion {
		return Health{}, fmt.Errorf("host agent API version %q is incompatible with %q", health.APIVersion, APIVersion)
	}
	return health, nil
}

func (client *Client) CloseIdleConnections() {
	if client == nil || client.httpClient == nil {
		return
	}
	client.httpClient.CloseIdleConnections()
}
