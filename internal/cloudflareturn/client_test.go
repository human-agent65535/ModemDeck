package cloudflareturn

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const (
	testKeyID    = "0123456789abcdef0123456789abcdef"
	testAPIToken = "test-turn-api-token"
)

func TestGenerateReturnsRelayOnlyConfiguration(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(
		func(response http.ResponseWriter, request *http.Request) {
			if request.Method != http.MethodPost ||
				request.URL.Path != "/v1/turn/keys/"+testKeyID+
					"/credentials/generate-ice-servers" {
				http.NotFound(response, request)
				return
			}
			if authorization := request.Header.Get("Authorization"); authorization !=
				"Bearer "+testAPIToken {
				t.Fatalf("Authorization = %q", authorization)
			}
			var input credentialRequest
			if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
				t.Fatalf("decode request: %v", err)
			}
			if input.TTL != 86400 {
				t.Fatalf("TTL = %d", input.TTL)
			}
			response.Header().Set("Content-Type", "application/json")
			response.WriteHeader(http.StatusCreated)
			_, _ = response.Write([]byte(`{
				"iceServers": [
					{"urls": [
						"stun:stun.cloudflare.com:3478",
						"stun:stun.cloudflare.com:53"
					]},
					{
						"urls": [
							"turn:turn.cloudflare.com:3478?transport=udp",
							"turn:turn.cloudflare.com:53?transport=udp",
							"turn:turn.cloudflare.com:3478?transport=tcp",
							"turn:turn.cloudflare.com:80?transport=tcp",
							"turns:turn.cloudflare.com:5349?transport=tcp",
							"turns:turn.cloudflare.com:443?transport=tcp"
						],
						"username": "relay-user",
						"credential": "relay-secret"
					}
				]
			}`))
		},
	))
	t.Cleanup(server.Close)
	now := time.Date(2026, 7, 31, 0, 0, 0, 0, time.UTC)
	client, err := New(Options{
		KeyID:      testKeyID,
		APIToken:   testAPIToken,
		BaseURL:    server.URL,
		HTTPClient: server.Client(),
		Now:        func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	configuration, err := client.Generate(context.Background())
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if !configuration.RelayOnly ||
		!configuration.ExpiresAt.Equal(now.Add(24*time.Hour)) ||
		len(configuration.ICEServers) != 1 {
		t.Fatalf("configuration = %+v", configuration)
	}
	relay := configuration.ICEServers[0]
	if relay.Username != "relay-user" ||
		relay.Credential != "relay-secret" ||
		len(relay.URLs) != 5 {
		t.Fatalf("relay server = %+v", relay)
	}
	for _, relayURL := range relay.URLs {
		if strings.Contains(relayURL, ":53?") ||
			strings.HasPrefix(relayURL, "stun:") {
			t.Fatalf("unexpected relay URL %q", relayURL)
		}
	}
}

func TestGenerateRejectsUpstreamFailuresWithoutLeakingSecrets(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(
		func(response http.ResponseWriter, _ *http.Request) {
			http.Error(
				response,
				"upstream included "+testAPIToken,
				http.StatusUnauthorized,
			)
		},
	))
	t.Cleanup(server.Close)
	client, err := New(Options{
		KeyID:      testKeyID,
		APIToken:   testAPIToken,
		BaseURL:    server.URL,
		HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Generate(context.Background())
	if err == nil ||
		strings.Contains(err.Error(), testAPIToken) ||
		!strings.Contains(err.Error(), "401") {
		t.Fatalf("Generate() error = %v", err)
	}
}

func TestGenerateRejectsResponseWithoutTURNRelay(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(
		func(response http.ResponseWriter, _ *http.Request) {
			response.WriteHeader(http.StatusCreated)
			_, _ = response.Write([]byte(`{
				"iceServers": [
					{"urls": ["stun:stun.cloudflare.com:3478"]}
				]
			}`))
		},
	))
	t.Cleanup(server.Close)
	client, err := New(Options{
		KeyID:      testKeyID,
		APIToken:   testAPIToken,
		BaseURL:    server.URL,
		HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Generate(context.Background()); err == nil {
		t.Fatal("Generate() error = nil")
	}
}

func TestGenerateDoesNotFollowRedirects(t *testing.T) {
	t.Parallel()

	redirectTarget := httptest.NewServer(http.HandlerFunc(
		func(_ http.ResponseWriter, request *http.Request) {
			t.Fatalf(
				"redirect target received Authorization %q",
				request.Header.Get("Authorization"),
			)
		},
	))
	t.Cleanup(redirectTarget.Close)
	server := httptest.NewServer(http.HandlerFunc(
		func(response http.ResponseWriter, _ *http.Request) {
			http.Redirect(
				response,
				&http.Request{},
				redirectTarget.URL,
				http.StatusTemporaryRedirect,
			)
		},
	))
	t.Cleanup(server.Close)
	client, err := New(Options{
		KeyID:      testKeyID,
		APIToken:   testAPIToken,
		BaseURL:    server.URL,
		HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Generate(context.Background()); err == nil ||
		!strings.Contains(err.Error(), "307") {
		t.Fatalf("Generate() error = %v", err)
	}
}

func TestNewRejectsIncompleteConfiguration(t *testing.T) {
	t.Parallel()

	for _, options := range []Options{
		{},
		{KeyID: testKeyID},
		{KeyID: "not-a-key", APIToken: testAPIToken},
		{KeyID: testKeyID, APIToken: "contains space"},
		{
			KeyID:    testKeyID,
			APIToken: testAPIToken,
			BaseURL:  "http://rtc.example.test",
		},
		{
			KeyID:    testKeyID,
			APIToken: testAPIToken,
			TTL:      1500 * time.Millisecond,
		},
		{
			KeyID:    testKeyID,
			APIToken: testAPIToken,
			TTL:      49 * time.Hour,
		},
	} {
		if _, err := New(options); err == nil {
			t.Fatalf("New(%+v) error = nil", options)
		}
	}
}
