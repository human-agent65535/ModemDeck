package cloudflareturn

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/human-agent65535/modemdeck/internal/rtcconfig"
)

const (
	defaultBaseURL      = "https://rtc.live.cloudflare.com"
	defaultRequestTTL   = 24 * time.Hour
	defaultHTTPTimeout  = 10 * time.Second
	maximumResponseBody = 64 << 10
	maximumCredential   = 4096
	maximumICEServers   = 8
	maximumServerURLs   = 16
	maximumURLLength    = 2048
)

var ErrInvalidConfiguration = errors.New(
	"invalid Cloudflare TURN configuration",
)

type Options struct {
	KeyID      string
	APIToken   string
	TTL        time.Duration
	BaseURL    string
	HTTPClient *http.Client
	Now        func() time.Time
}

type Client struct {
	endpoint   string
	apiToken   string
	ttlSeconds int64
	ttl        time.Duration
	httpClient *http.Client
	now        func() time.Time
}

type credentialRequest struct {
	TTL int64 `json:"ttl"`
}

type credentialResponse struct {
	ICEServers []iceServerResponse `json:"iceServers"`
}

type iceServerResponse struct {
	URLs       []string `json:"urls"`
	Username   string   `json:"username"`
	Credential string   `json:"credential"`
}

func New(options Options) (*Client, error) {
	keyID := strings.TrimSpace(options.KeyID)
	if !validKeyID(keyID) {
		return nil, fmt.Errorf("%w: key ID is invalid", ErrInvalidConfiguration)
	}
	apiToken := strings.TrimSpace(options.APIToken)
	if !validSecret(apiToken) {
		return nil, fmt.Errorf("%w: API token is invalid", ErrInvalidConfiguration)
	}
	ttl := options.TTL
	if ttl == 0 {
		ttl = defaultRequestTTL
	}
	if ttl < time.Second || ttl > 48*time.Hour ||
		ttl%time.Second != 0 {
		return nil, fmt.Errorf("%w: TTL is invalid", ErrInvalidConfiguration)
	}
	baseURL := strings.TrimSpace(options.BaseURL)
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	parsedBase, err := url.Parse(baseURL)
	if err != nil ||
		(parsedBase.Scheme != "https" && parsedBase.Scheme != "http") ||
		(parsedBase.Scheme == "http" && options.HTTPClient == nil) ||
		parsedBase.Host == "" ||
		parsedBase.User != nil ||
		parsedBase.RawQuery != "" ||
		parsedBase.Fragment != "" {
		return nil, fmt.Errorf("%w: API base URL is invalid", ErrInvalidConfiguration)
	}
	parsedBase.Path = strings.TrimRight(parsedBase.Path, "/") +
		"/v1/turn/keys/" + url.PathEscape(keyID) +
		"/credentials/generate-ice-servers"
	client := options.HTTPClient
	if client == nil {
		client = &http.Client{
			Timeout: defaultHTTPTimeout,
		}
	} else {
		cloned := *client
		client = &cloned
	}
	if client.Timeout <= 0 {
		client.Timeout = defaultHTTPTimeout
	}
	client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	return &Client{
		endpoint:   parsedBase.String(),
		apiToken:   apiToken,
		ttlSeconds: int64(ttl / time.Second),
		ttl:        ttl,
		httpClient: client,
		now:        now,
	}, nil
}

func (client *Client) Generate(
	ctx context.Context,
) (rtcconfig.Configuration, error) {
	if client == nil {
		return rtcconfig.Configuration{}, fmt.Errorf(
			"generate Cloudflare TURN credentials: %w",
			ErrInvalidConfiguration,
		)
	}
	body, err := json.Marshal(credentialRequest{TTL: client.ttlSeconds})
	if err != nil {
		return rtcconfig.Configuration{}, fmt.Errorf(
			"generate Cloudflare TURN credentials: encode request: %w",
			err,
		)
	}
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		client.endpoint,
		bytes.NewReader(body),
	)
	if err != nil {
		return rtcconfig.Configuration{}, fmt.Errorf(
			"generate Cloudflare TURN credentials: create request: %w",
			err,
		)
	}
	request.Header.Set("Authorization", "Bearer "+client.apiToken)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	response, err := client.httpClient.Do(request)
	if err != nil {
		return rtcconfig.Configuration{}, fmt.Errorf(
			"generate Cloudflare TURN credentials: request failed: %w",
			err,
		)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(
		response.Body,
		maximumResponseBody+1,
	))
	if err != nil {
		return rtcconfig.Configuration{}, fmt.Errorf(
			"generate Cloudflare TURN credentials: read response: %w",
			err,
		)
	}
	if len(raw) > maximumResponseBody {
		return rtcconfig.Configuration{}, errors.New(
			"generate Cloudflare TURN credentials: response is too large",
		)
	}
	if response.StatusCode != http.StatusCreated {
		return rtcconfig.Configuration{}, fmt.Errorf(
			"generate Cloudflare TURN credentials: unexpected HTTP status %d",
			response.StatusCode,
		)
	}
	var decoded credentialResponse
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return rtcconfig.Configuration{}, fmt.Errorf(
			"generate Cloudflare TURN credentials: decode response: %w",
			err,
		)
	}
	servers, err := relayServers(decoded.ICEServers)
	if err != nil {
		return rtcconfig.Configuration{}, fmt.Errorf(
			"generate Cloudflare TURN credentials: %w",
			err,
		)
	}
	return rtcconfig.Configuration{
		ICEServers: servers,
		ExpiresAt:  client.now().UTC().Add(client.ttl),
		RelayOnly:  true,
	}, nil
}

func relayServers(
	input []iceServerResponse,
) ([]rtcconfig.ICEServer, error) {
	if len(input) == 0 || len(input) > maximumICEServers {
		return nil, errors.New("response contains an invalid ICE server count")
	}
	result := make([]rtcconfig.ICEServer, 0, len(input))
	for _, server := range input {
		username := strings.TrimSpace(server.Username)
		credential := strings.TrimSpace(server.Credential)
		urls := make([]string, 0, len(server.URLs))
		if len(server.URLs) > maximumServerURLs {
			return nil, errors.New("response contains too many ICE server URLs")
		}
		for _, rawURL := range server.URLs {
			normalized, relay, err := relayURL(rawURL)
			if err != nil {
				return nil, err
			}
			if relay {
				urls = append(urls, normalized)
			}
		}
		if len(urls) == 0 {
			continue
		}
		if !validSecret(username) || !validSecret(credential) {
			return nil, errors.New(
				"response contains invalid TURN credentials",
			)
		}
		result = append(result, rtcconfig.ICEServer{
			URLs:       urls,
			Username:   username,
			Credential: credential,
		})
	}
	if len(result) == 0 {
		return nil, errors.New("response contains no TURN relay server")
	}
	return result, nil
}

func relayURL(value string) (normalized string, relay bool, err error) {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > maximumURLLength ||
		strings.IndexFunc(value, unicode.IsSpace) >= 0 ||
		strings.IndexFunc(value, unicode.IsControl) >= 0 {
		return "", false, errors.New("response contains an invalid ICE server URL")
	}
	lower := strings.ToLower(value)
	if strings.HasPrefix(lower, "stun:") ||
		strings.HasPrefix(lower, "stuns:") {
		return "", false, nil
	}
	var scheme string
	switch {
	case strings.HasPrefix(lower, "turn:"):
		scheme = "turn:"
	case strings.HasPrefix(lower, "turns:"):
		scheme = "turns:"
	default:
		return "", false, errors.New(
			"response contains an unsupported ICE server URL",
		)
	}
	address := value[len(scheme):]
	if query := strings.IndexByte(address, '?'); query >= 0 {
		address = address[:query]
	}
	if colon := strings.LastIndexByte(address, ':'); colon >= 0 {
		if port, parseErr := strconv.Atoi(address[colon+1:]); parseErr == nil &&
			port == 53 {
			return "", false, nil
		}
	}
	return value, true, nil
}

func validKeyID(value string) bool {
	if len(value) != 32 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func validSecret(value string) bool {
	if value == "" || len(value) > maximumCredential {
		return false
	}
	return strings.IndexFunc(value, func(character rune) bool {
		return unicode.IsSpace(character) || unicode.IsControl(character)
	}) < 0
}
