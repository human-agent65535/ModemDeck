package applepush

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	developmentEndpoint = "https://api.sandbox.push.apple.com"
	productionEndpoint  = "https://api.push.apple.com"
	providerTokenAge    = 50 * time.Minute
	maximumPayloadBytes = 4096
)

type PushType string

const (
	PushTypeAlert PushType = "alert"
	PushTypeVoIP  PushType = "voip"
)

type Notification struct {
	DeviceToken string
	Environment string
	PushType    PushType
	APNSID      string
	CollapseID  string
	Expiration  time.Time
	Payload     any
}

type ClientOptions struct {
	HTTPClient          *http.Client
	DevelopmentEndpoint string
	ProductionEndpoint  string
	Now                 func() time.Time
	Random              io.Reader
}

type Client struct {
	config              Config
	key                 *ecdsa.PrivateKey
	httpClient          *http.Client
	developmentEndpoint string
	productionEndpoint  string
	now                 func() time.Time
	random              io.Reader

	tokenMu         sync.Mutex
	providerToken   string
	providerTokenAt time.Time
}

type ResponseError struct {
	StatusCode int
	Reason     string
	APNSID     string
}

type transportError struct {
	cause error
}

func (err *transportError) Error() string {
	return "APNs transport request failed"
}

func (err *transportError) Unwrap() error {
	return err.cause
}

func (err *ResponseError) Error() string {
	if err.Reason == "" {
		return fmt.Sprintf("APNs returned HTTP %d", err.StatusCode)
	}
	return fmt.Sprintf("APNs returned HTTP %d: %s", err.StatusCode, err.Reason)
}

func NewClient(config Config, options ClientOptions) (*Client, error) {
	config.normalize()
	if err := config.validate(); err != nil {
		return nil, err
	}
	keyContents, err := readBoundedFile(config.PrivateKeyFile, 16<<10)
	if err != nil {
		return nil, fmt.Errorf("read APNs private key: %w", err)
	}
	key, err := parsePrivateKey(keyContents)
	if err != nil {
		return nil, err
	}
	developmentURL := endpointOrDefault(options.DevelopmentEndpoint, developmentEndpoint)
	productionURL := endpointOrDefault(options.ProductionEndpoint, productionEndpoint)
	if err := validateEndpoint(developmentURL); err != nil {
		return nil, fmt.Errorf("invalid APNs development endpoint: %w", err)
	}
	if err := validateEndpoint(productionURL); err != nil {
		return nil, fmt.Errorf("invalid APNs production endpoint: %w", err)
	}
	httpClient := options.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{
			Transport: &http.Transport{ForceAttemptHTTP2: true},
			Timeout:   10 * time.Second,
		}
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	randomSource := options.Random
	if randomSource == nil {
		randomSource = rand.Reader
	}
	return &Client{
		config:              config,
		key:                 key,
		httpClient:          httpClient,
		developmentEndpoint: developmentURL,
		productionEndpoint:  productionURL,
		now:                 now,
		random:              randomSource,
	}, nil
}

func (client *Client) BundleID() string {
	return client.config.BundleID
}

func (client *Client) Send(ctx context.Context, notification Notification) error {
	token := strings.ToLower(strings.TrimSpace(notification.DeviceToken))
	decodedToken, err := hex.DecodeString(token)
	if err != nil || len(decodedToken) != 32 {
		return errors.New("invalid APNs device token")
	}
	endpoint, err := client.endpoint(notification.Environment)
	if err != nil {
		return err
	}
	topic := client.config.BundleID
	switch notification.PushType {
	case PushTypeAlert:
	case PushTypeVoIP:
		topic += ".voip"
	default:
		return errors.New("invalid APNs push type")
	}
	payload, err := json.Marshal(notification.Payload)
	if err != nil {
		return fmt.Errorf("encode APNs payload: %w", err)
	}
	if len(payload) == 0 || len(payload) > maximumPayloadBytes {
		return fmt.Errorf("APNs payload has invalid size %d", len(payload))
	}
	providerToken, err := client.authorizationToken()
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		endpoint+"/3/device/"+token,
		strings.NewReader(string(payload)),
	)
	if err != nil {
		return errors.New("create APNs request failed")
	}
	request.Header.Set("authorization", "bearer "+providerToken)
	request.Header.Set("content-type", "application/json")
	request.Header.Set("apns-topic", topic)
	request.Header.Set("apns-push-type", string(notification.PushType))
	request.Header.Set("apns-priority", "10")
	if !notification.Expiration.IsZero() {
		request.Header.Set("apns-expiration", strconv.FormatInt(notification.Expiration.UTC().Unix(), 10))
	}
	if notification.APNSID != "" {
		if !validUUID(notification.APNSID) {
			return errors.New("invalid APNs notification ID")
		}
		request.Header.Set("apns-id", strings.ToLower(notification.APNSID))
	}
	if notification.CollapseID != "" {
		if len(notification.CollapseID) > 64 || strings.ContainsAny(notification.CollapseID, "\r\n") {
			return errors.New("invalid APNs collapse ID")
		}
		request.Header.Set("apns-collapse-id", notification.CollapseID)
	}

	response, err := client.httpClient.Do(request)
	if err != nil {
		return &transportError{cause: err}
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return nil
	}
	responseBody, readErr := io.ReadAll(io.LimitReader(response.Body, 4097))
	if readErr != nil {
		return fmt.Errorf("read APNs error response: %w", readErr)
	}
	var result struct {
		Reason string `json:"reason"`
	}
	if len(responseBody) <= 4096 {
		_ = json.Unmarshal(responseBody, &result)
	}
	return &ResponseError{
		StatusCode: response.StatusCode,
		Reason:     strings.TrimSpace(result.Reason),
		APNSID:     strings.TrimSpace(response.Header.Get("apns-id")),
	}
}

func (client *Client) endpoint(environment string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(environment)) {
	case "development":
		return client.developmentEndpoint, nil
	case "production":
		return client.productionEndpoint, nil
	default:
		return "", errors.New("invalid APNs environment")
	}
}

func (client *Client) authorizationToken() (string, error) {
	now := client.now().UTC()
	client.tokenMu.Lock()
	defer client.tokenMu.Unlock()
	if client.providerToken != "" &&
		!now.Before(client.providerTokenAt) &&
		now.Sub(client.providerTokenAt) < providerTokenAge {
		return client.providerToken, nil
	}
	header, err := json.Marshal(map[string]string{
		"alg": "ES256",
		"kid": client.config.KeyID,
	})
	if err != nil {
		return "", fmt.Errorf("encode APNs provider header: %w", err)
	}
	claims, err := json.Marshal(map[string]any{
		"iss": client.config.TeamID,
		"iat": now.Unix(),
	})
	if err != nil {
		return "", fmt.Errorf("encode APNs provider claims: %w", err)
	}
	unsigned := base64.RawURLEncoding.EncodeToString(header) + "." +
		base64.RawURLEncoding.EncodeToString(claims)
	digest := sha256.Sum256([]byte(unsigned))
	r, s, err := ecdsa.Sign(client.random, client.key, digest[:])
	if err != nil {
		return "", fmt.Errorf("sign APNs provider token: %w", err)
	}
	signature := make([]byte, 64)
	r.FillBytes(signature[:32])
	s.FillBytes(signature[32:])
	client.providerToken = unsigned + "." + base64.RawURLEncoding.EncodeToString(signature)
	client.providerTokenAt = now
	return client.providerToken, nil
}

func parsePrivateKey(contents []byte) (*ecdsa.PrivateKey, error) {
	block, trailing := pem.Decode(contents)
	if block == nil || strings.TrimSpace(string(trailing)) != "" || block.Type != "PRIVATE KEY" {
		return nil, errors.New("parse APNs private key: expected one PKCS#8 PRIVATE KEY")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse APNs private key: %w", err)
	}
	key, ok := parsed.(*ecdsa.PrivateKey)
	if !ok || key.Curve != elliptic.P256() {
		return nil, errors.New("parse APNs private key: expected an ECDSA P-256 key")
	}
	return key, nil
}

func IsRetryable(err error) bool {
	var responseError *ResponseError
	if errors.As(err, &responseError) {
		return responseError.StatusCode == http.StatusTooManyRequests || responseError.StatusCode >= 500
	}
	var networkError net.Error
	return errors.As(err, &networkError) &&
		!errors.Is(err, context.Canceled) &&
		!errors.Is(err, context.DeadlineExceeded)
}

func InvalidatesToken(err error) bool {
	var responseError *ResponseError
	if !errors.As(err, &responseError) {
		return false
	}
	switch responseError.Reason {
	case "BadDeviceToken", "DeviceTokenNotForTopic", "Unregistered":
		return true
	default:
		return false
	}
}

func endpointOrDefault(value, fallback string) string {
	value = strings.TrimRight(strings.TrimSpace(value), "/")
	if value == "" {
		return fallback
	}
	return value
}

func validateEndpoint(value string) error {
	parsed, err := url.Parse(value)
	if err != nil {
		return err
	}
	if parsed.Scheme != "https" || parsed.Host == "" || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("endpoint must be an HTTPS origin")
	}
	return nil
}

func validUUID(value string) bool {
	value = strings.ToLower(strings.TrimSpace(value))
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
		return false
	}
	_, err := hex.DecodeString(strings.ReplaceAll(value, "-", ""))
	return err == nil
}
