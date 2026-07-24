package agentclient

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"
)

func TestNetworkAndPutProxiesUseTypedContract(t *testing.T) {
	t.Parallel()

	var received struct {
		Proxies []ProxyConfiguration `json:"proxies"`
	}
	handler := http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/v1/network":
		case request.Method == http.MethodPut && request.URL.Path == "/v1/proxies":
			if err := json.NewDecoder(request.Body).Decode(&received); err != nil {
				t.Fatalf("decode desired proxies: %v", err)
			}
		default:
			http.NotFound(response, request)
			return
		}
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{
			"boot_epoch":"boot-1",
			"observed_at":"2026-07-24T00:00:00Z",
			"lines":[{
				"line_id":"line-1",
				"connected":true,
				"interface":"wwan0",
				"dns":["1.1.1.1"],
				"rx_bytes":100,
				"tx_bytes":200,
				"error":""
			}],
			"proxies":[{
				"id":"proxy-1",
				"line_id":"line-1",
				"state":"running",
				"running":true,
				"mode":"socks5",
				"listen_address":"127.0.0.1",
				"listen_port":1080,
				"interface":"wwan0",
				"runtime_epoch":"proxy-epoch-1",
				"started_at":"2026-07-24T00:00:00Z",
				"bytes_up":20,
				"bytes_down":10,
				"connections":2,
				"active_connections":1,
				"last_error":""
			}]
		}`))
	})
	client := newUnixTestClient(t, handler)

	snapshot, err := client.Network(context.Background())
	if err != nil {
		t.Fatalf("Network() error = %v", err)
	}
	if snapshot.BootEpoch != "boot-1" ||
		len(snapshot.Lines) != 1 ||
		snapshot.Lines[0].Interface != "wwan0" ||
		len(snapshot.Proxies) != 1 ||
		!snapshot.Proxies[0].Running {
		t.Fatalf("Network() = %+v", snapshot)
	}

	snapshot, err = client.PutProxies(context.Background(), []ProxyConfiguration{{
		ID:            "proxy-1",
		LineID:        "line-1",
		Enabled:       true,
		Mode:          ProxyModeSOCKS5,
		ListenAddress: "127.0.0.1",
		ListenPort:    1080,
		AuthEnabled:   true,
		Username:      "user",
		Password:      "secret",
	}})
	if err != nil {
		t.Fatalf("PutProxies() error = %v", err)
	}
	if len(received.Proxies) != 1 ||
		received.Proxies[0].Password != "secret" ||
		snapshot.Proxies[0].RuntimeEpoch != "proxy-epoch-1" {
		t.Fatalf("received = %+v snapshot = %+v", received, snapshot)
	}
}

func TestPutProxiesAllowsExplicitEmptyCollection(t *testing.T) {
	t.Parallel()

	var received map[string]json.RawMessage
	client := newUnixTestClient(t, http.HandlerFunc(func(
		response http.ResponseWriter,
		request *http.Request,
	) {
		if request.Method != http.MethodPut || request.URL.Path != "/v1/proxies" {
			http.NotFound(response, request)
			return
		}
		if err := json.NewDecoder(request.Body).Decode(&received); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		_, _ = response.Write([]byte(`{
			"boot_epoch":"boot-empty",
			"observed_at":"2026-07-24T00:00:00Z",
			"lines":[],
			"proxies":[]
		}`))
	}))

	snapshot, err := client.PutProxies(context.Background(), nil)
	if err != nil {
		t.Fatalf("PutProxies(nil) error = %v", err)
	}
	if string(received["proxies"]) != "[]" {
		t.Fatalf("proxies JSON = %s, want []", received["proxies"])
	}
	if snapshot.Lines == nil || snapshot.Proxies == nil {
		t.Fatalf("empty collections must be non-nil: %+v", snapshot)
	}
}

func TestNetworkRejectsIncompleteOrContradictoryProtocol(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
	}{
		{
			name: "missing collection",
			body: `{
				"boot_epoch":"boot-1",
				"observed_at":"2026-07-24T00:00:00Z",
				"lines":[]
			}`,
		},
		{
			name: "connected line missing interface",
			body: `{
				"boot_epoch":"boot-1",
				"observed_at":"2026-07-24T00:00:00Z",
				"lines":[{
					"line_id":"line-1",
					"connected":true,
					"interface":"",
					"dns":[],
					"rx_bytes":0,
					"tx_bytes":0,
					"error":""
				}],
				"proxies":[]
			}`,
		},
		{
			name: "proxy state mismatch",
			body: `{
				"boot_epoch":"boot-1",
				"observed_at":"2026-07-24T00:00:00Z",
				"lines":[],
				"proxies":[{
					"id":"proxy-1",
					"line_id":"line-1",
					"state":"disabled",
					"running":true,
					"mode":"http",
					"listen_address":"127.0.0.1",
					"listen_port":8080,
					"interface":"",
					"runtime_epoch":"",
					"started_at":null,
					"bytes_up":0,
					"bytes_down":0,
					"connections":0,
					"active_connections":0,
					"last_error":""
				}]
			}`,
		},
		{
			name: "unknown field",
			body: `{
				"boot_epoch":"boot-1",
				"observed_at":"2026-07-24T00:00:00Z",
				"lines":[],
				"proxies":[],
				"unexpected":true
			}`,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			client := newUnixTestClient(t, http.HandlerFunc(func(
				response http.ResponseWriter,
				_ *http.Request,
			) {
				_, _ = response.Write([]byte(test.body))
			}))
			_, err := client.Network(context.Background())
			if !errors.Is(err, ErrProtocol) {
				t.Fatalf("Network() error = %v, want ErrProtocol", err)
			}
		})
	}
}

func TestPutProxiesRejectsInvalidDesiredStateBeforeRequest(t *testing.T) {
	t.Parallel()

	client := &Client{}
	_, err := client.PutProxies(context.Background(), []ProxyConfiguration{{
		ID:            "proxy-1",
		LineID:        "line-1",
		Mode:          ProxyModeSOCKS5,
		ListenAddress: "127.0.0.1",
		ListenPort:    1080,
		AuthEnabled:   true,
		Username:      "user",
	}})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("PutProxies() error = %v, want ErrInvalidRequest", err)
	}
}

func TestNetworkNormalizesTimesToUTC(t *testing.T) {
	t.Parallel()

	client := newUnixTestClient(t, http.HandlerFunc(func(
		response http.ResponseWriter,
		_ *http.Request,
	) {
		_, _ = response.Write([]byte(`{
			"boot_epoch":"boot-1",
			"observed_at":"2026-07-24T09:00:00+09:00",
			"lines":[],
			"proxies":[]
		}`))
	}))
	snapshot, err := client.Network(context.Background())
	if err != nil {
		t.Fatalf("Network() error = %v", err)
	}
	if !snapshot.ObservedAt.Equal(time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC)) ||
		snapshot.ObservedAt.Location() != time.UTC {
		t.Fatalf("ObservedAt = %s", snapshot.ObservedAt)
	}
}

func TestNetworkAllowsConnectedLineWithBearerErrorAndNoUsableInterface(t *testing.T) {
	t.Parallel()

	client := newUnixTestClient(t, http.HandlerFunc(func(
		response http.ResponseWriter,
		_ *http.Request,
	) {
		_, _ = response.Write([]byte(`{
			"boot_epoch":"boot-1",
			"observed_at":"2026-07-24T00:00:00Z",
			"lines":[{
				"line_id":"line-1",
				"connected":true,
				"interface":"",
				"dns":[],
				"rx_bytes":0,
				"tx_bytes":0,
				"error":"connected bearer has no usable interface"
			}],
			"proxies":[]
		}`))
	}))
	snapshot, err := client.Network(context.Background())
	if err != nil {
		t.Fatalf("Network() error = %v", err)
	}
	if len(snapshot.Lines) != 1 || snapshot.Lines[0].Error == "" {
		t.Fatalf("snapshot = %+v", snapshot)
	}
}
