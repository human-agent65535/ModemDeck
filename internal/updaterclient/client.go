package updaterclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/human-agent65535/modemdeck/internal/updatecheck"
)

const (
	defaultTimeout       = 8 * time.Second
	maximumResponseBytes = 1 << 20
)

var (
	ErrHardwareConfirmationRequired = errors.New("hardware update confirmation is required")
	ErrOperationRunning             = errors.New("a software update is already running")
	ErrUnavailable                  = errors.New("software updater is unavailable")
)

type HTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}

type Options struct {
	BaseURL        string
	CurrentVersion string
	Token          string
	Client         HTTPClient
}

type Client struct {
	baseURL        *url.URL
	currentVersion string
	token          string
	client         HTTPClient
}

func New(options Options) (*Client, error) {
	baseURL, err := url.Parse(strings.TrimSpace(options.BaseURL))
	if err != nil || baseURL.Scheme != "http" || baseURL.Host == "" {
		return nil, fmt.Errorf("invalid updater URL")
	}
	baseURL.Path = strings.TrimRight(baseURL.Path, "/")
	client := options.Client
	if client == nil {
		client = &http.Client{Timeout: defaultTimeout}
	}
	currentVersion := strings.TrimSpace(options.CurrentVersion)
	if currentVersion == "" {
		currentVersion = "dev"
	}
	return &Client{
		baseURL:        baseURL,
		currentVersion: currentVersion,
		token:          strings.TrimSpace(options.Token),
		client:         client,
	}, nil
}

func (client *Client) Check(ctx context.Context) updatecheck.Result {
	var result updatecheck.Result
	if err := client.request(ctx, http.MethodGet, "/v1/updates/check", nil, &result); err != nil {
		return updatecheck.Result{
			Status:         updatecheck.StatusUnavailable,
			CurrentVersion: client.currentVersion,
			CheckedAt:      time.Now().UTC().Format(time.RFC3339),
			ErrorCode:      "updater_unavailable",
		}
	}
	return result
}

func (client *Client) Apply(
	ctx context.Context,
	request updatecheck.ApplyRequest,
) (updatecheck.Operation, error) {
	var operation updatecheck.Operation
	err := client.request(ctx, http.MethodPost, "/v1/updates/apply", request, &operation)
	return operation, err
}

func (client *Client) Status(ctx context.Context) (updatecheck.Operation, error) {
	var operation updatecheck.Operation
	err := client.request(ctx, http.MethodGet, "/v1/updates/status", nil, &operation)
	return operation, err
}

func (client *Client) request(
	ctx context.Context,
	method, path string,
	input, output any,
) error {
	var body io.Reader
	if input != nil {
		encoded, err := json.Marshal(input)
		if err != nil {
			return err
		}
		body = bytes.NewReader(encoded)
	}
	requestURL := *client.baseURL
	requestURL.Path += path
	request, err := http.NewRequestWithContext(ctx, method, requestURL.String(), body)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/json")
	if client.token != "" {
		request.Header.Set("Authorization", "Bearer "+client.token)
	}
	if input != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.client.Do(request)
	if err != nil {
		return ErrUnavailable
	}
	defer response.Body.Close()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var remote struct {
			Code string `json:"code"`
		}
		_ = json.NewDecoder(io.LimitReader(response.Body, maximumResponseBytes)).Decode(&remote)
		switch remote.Code {
		case "hardware_confirmation_required":
			return ErrHardwareConfirmationRequired
		case "update_in_progress":
			return ErrOperationRunning
		default:
			return ErrUnavailable
		}
	}
	if output == nil {
		return nil
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, maximumResponseBytes)).Decode(output); err != nil {
		return ErrUnavailable
	}
	return nil
}
