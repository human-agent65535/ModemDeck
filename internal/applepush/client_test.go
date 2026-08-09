package applepush

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestClientSendsSignedVoIPRequest(t *testing.T) {
	t.Parallel()
	privateKey, keyPath := testPrivateKey(t)
	now := time.Date(2026, time.August, 9, 4, 0, 0, 0, time.UTC)
	type capturedRequest struct {
		Path          string
		Authorization string
		Topic         string
		PushType      string
		Priority      string
		Expiration    string
		Body          []byte
	}
	captured := make(chan capturedRequest, 1)
	server := httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		body, _ := io.ReadAll(request.Body)
		captured <- capturedRequest{
			Path:          request.URL.Path,
			Authorization: request.Header.Get("authorization"),
			Topic:         request.Header.Get("apns-topic"),
			PushType:      request.Header.Get("apns-push-type"),
			Priority:      request.Header.Get("apns-priority"),
			Expiration:    request.Header.Get("apns-expiration"),
			Body:          body,
		}
		response.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client, err := NewClient(Config{
		TeamID:         "ABCDEFGHIJ",
		KeyID:          "KLMNOPQRST",
		BundleID:       "com.example.modemdeck",
		PrivateKeyFile: keyPath,
	}, ClientOptions{
		HTTPClient:          server.Client(),
		DevelopmentEndpoint: server.URL,
		ProductionEndpoint:  server.URL,
		Now:                 func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	token := strings.Repeat("ab", 32)
	if err := client.Send(context.Background(), Notification{
		DeviceToken: token,
		Environment: "production",
		PushType:    PushTypeVoIP,
		APNSID:      "12345678-1234-5678-9234-1234567890ab",
		CollapseID:  "call-example",
		Expiration:  now.Add(30 * time.Second),
		Payload:     map[string]any{"aps": map[string]any{}, "modemdeck_call": map[string]string{"call_id": "call-example"}},
	}); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	request := <-captured
	if request.Path != "/3/device/"+token ||
		request.Topic != "com.example.modemdeck.voip" ||
		request.PushType != "voip" ||
		request.Priority != "10" ||
		request.Expiration != strconv.FormatInt(now.Add(30*time.Second).Unix(), 10) {
		t.Fatalf("request = %+v", request)
	}
	verifyProviderToken(t, strings.TrimPrefix(request.Authorization, "bearer "), privateKey, now)
	if !strings.Contains(string(request.Body), `"call_id":"call-example"`) {
		t.Fatalf("payload = %s", request.Body)
	}
}

func TestClientClassifiesInvalidDeviceToken(t *testing.T) {
	t.Parallel()
	_, keyPath := testPrivateKey(t)
	server := httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("apns-id", "12345678-1234-5678-9234-1234567890ab")
		response.WriteHeader(http.StatusGone)
		_, _ = response.Write([]byte(`{"reason":"Unregistered"}`))
	}))
	defer server.Close()
	client, err := NewClient(Config{
		TeamID:         "ABCDEFGHIJ",
		KeyID:          "KLMNOPQRST",
		BundleID:       "com.example.modemdeck",
		PrivateKeyFile: keyPath,
	}, ClientOptions{
		HTTPClient:          server.Client(),
		DevelopmentEndpoint: server.URL,
		ProductionEndpoint:  server.URL,
	})
	if err != nil {
		t.Fatal(err)
	}
	err = client.Send(context.Background(), Notification{
		DeviceToken: strings.Repeat("ab", 32),
		Environment: "development",
		PushType:    PushTypeAlert,
		Payload:     map[string]any{"aps": map[string]string{"sound": "default"}},
	})
	if !InvalidatesToken(err) || IsRetryable(err) {
		t.Fatalf("error = %v, invalidates=%t retryable=%t", err, InvalidatesToken(err), IsRetryable(err))
	}
}

func TestClientDoesNotExposeDeviceTokenInTransportError(t *testing.T) {
	t.Parallel()
	_, keyPath := testPrivateKey(t)
	client, err := NewClient(Config{
		TeamID:         "ABCDEFGHIJ",
		KeyID:          "KLMNOPQRST",
		BundleID:       "com.example.modemdeck",
		PrivateKeyFile: keyPath,
	}, ClientOptions{
		HTTPClient: &http.Client{Transport: failingRoundTripper{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	token := strings.Repeat("ab", 32)
	err = client.Send(context.Background(), Notification{
		DeviceToken: token,
		Environment: "production",
		PushType:    PushTypeAlert,
		Payload:     map[string]any{"aps": map[string]string{"sound": "default"}},
	})
	if err == nil || strings.Contains(err.Error(), token) || !IsRetryable(err) {
		t.Fatalf("transport error = %q, retryable=%t", err, IsRetryable(err))
	}
}

type failingRoundTripper struct{}

func (failingRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return nil, errors.New("fixture failure for " + request.URL.String())
}

func testPrivateKey(t *testing.T) (*ecdsa.PrivateKey, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "AuthKey_example.p8")
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	return key, path
}

func verifyProviderToken(
	t *testing.T,
	token string,
	key *ecdsa.PrivateKey,
	now time.Time,
) {
	t.Helper()
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("provider token parts = %d", len(parts))
	}
	headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		t.Fatal(err)
	}
	claimsBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatal(err)
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || len(signature) != 64 {
		t.Fatalf("signature length = %d, error = %v", len(signature), err)
	}
	var header map[string]any
	var claims map[string]any
	if err := json.Unmarshal(headerBytes, &header); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(claimsBytes, &claims); err != nil {
		t.Fatal(err)
	}
	if header["alg"] != "ES256" || header["kid"] != "KLMNOPQRST" ||
		claims["iss"] != "ABCDEFGHIJ" || claims["iat"] != float64(now.Unix()) {
		t.Fatalf("header=%v claims=%v", header, claims)
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if !ecdsa.Verify(key.Public().(*ecdsa.PublicKey), digest[:],
		new(big.Int).SetBytes(signature[:32]), new(big.Int).SetBytes(signature[32:])) {
		t.Fatal("provider token signature is invalid")
	}
}
