package httpapi

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/human-agent65535/modemdeck/agent/internal/domain"
)

type fakeNetworkProvider struct {
	snapshot domain.NetworkSnapshot
	desired  []domain.ProxyDesiredSet
	err      error
}

func (provider *fakeNetworkProvider) ApplyProxySet(
	_ context.Context,
	desired domain.ProxyDesiredSet,
) (domain.NetworkSnapshot, error) {
	provider.desired = append(provider.desired, desired)
	return provider.snapshot, provider.err
}

func (provider *fakeNetworkProvider) NetworkSnapshot(
	context.Context,
) (domain.NetworkSnapshot, error) {
	return provider.snapshot, provider.err
}

func TestNetworkRoutesUseFixedWireContract(t *testing.T) {
	startedAt := time.Date(2026, 7, 24, 1, 2, 3, 0, time.UTC)
	network := &fakeNetworkProvider{snapshot: domain.NetworkSnapshot{
		BootEpoch:  "network_boot_test",
		ObservedAt: startedAt.Add(time.Minute),
		Lines: []domain.LineNetworkStatus{{
			LineID:    "line-main",
			Connected: true,
			Interface: "wwan0",
			DNS:       []string{"8.8.8.8"},
			RXBytes:   100,
			TXBytes:   200,
			Error:     "",
		}},
		Proxies: []domain.ProxyNetworkStatus{{
			ID:                "main-proxy",
			LineID:            "line-main",
			State:             domain.ProxyStateRunning,
			Running:           true,
			Mode:              domain.ProxyModeSOCKS5,
			ListenAddress:     "127.0.0.1",
			ListenPort:        1080,
			Interface:         "wwan0",
			RuntimeEpoch:      "proxy_runtime_test",
			StartedAt:         &startedAt,
			BytesUp:           300,
			BytesDown:         400,
			Connections:       5,
			ActiveConnections: 1,
			LastError:         "",
		}},
	}}
	handler := NewWithOptions(
		&fakeProvider{},
		"test",
		Options{Network: network},
	)

	get := performRequest(handler, http.MethodGet, "/v1/network", nil)
	if get.Code != http.StatusOK {
		t.Fatalf("GET status = %d, body = %s", get.Code, get.Body.String())
	}
	var getSnapshot domain.NetworkSnapshot
	decodeResponse(t, get, &getSnapshot)
	assertNetworkWireSnapshot(t, getSnapshot)

	put := performRequest(
		handler,
		http.MethodPut,
		"/v1/proxies",
		[]byte(`{
			"proxies":[{
				"id":"main-proxy",
				"line_id":"line-main",
				"enabled":true,
				"mode":"socks5",
				"listen_address":"127.0.0.1",
				"listen_port":1080,
				"auth_enabled":true,
				"username":"user",
				"password":"secret"
			}]
		}`),
	)
	if put.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, body = %s", put.Code, put.Body.String())
	}
	var putSnapshot domain.NetworkSnapshot
	decodeResponse(t, put, &putSnapshot)
	assertNetworkWireSnapshot(t, putSnapshot)
	if len(network.desired) != 1 ||
		len(network.desired[0].Proxies) != 1 ||
		network.desired[0].Proxies[0].Password != "secret" {
		t.Fatalf("desired set = %+v", network.desired)
	}
	if content := put.Body.String(); content == "" ||
		strings.Contains(content, `"password"`) ||
		strings.Contains(content, `"username"`) {
		t.Fatalf("PUT response leaked credentials: %s", content)
	}
}

func TestNetworkRoutesValidateBodyAndCapability(t *testing.T) {
	provider := &fakeProvider{health: domain.ProviderHealth{Available: true}}
	network := &fakeNetworkProvider{}
	withNetwork := NewWithOptions(
		provider,
		"test",
		Options{Network: network},
	)
	missing := performRequest(
		withNetwork,
		http.MethodPut,
		"/v1/proxies",
		[]byte(`{}`),
	)
	if missing.Code != http.StatusBadRequest {
		t.Fatalf("missing proxies status = %d, body = %s", missing.Code, missing.Body.String())
	}
	unknown := performRequest(
		withNetwork,
		http.MethodPut,
		"/v1/proxies",
		[]byte(`{"proxies":[],"interface":"wwan0"}`),
	)
	if unknown.Code != http.StatusBadRequest {
		t.Fatalf("unknown interface status = %d, body = %s", unknown.Code, unknown.Body.String())
	}

	health := performRequest(withNetwork, http.MethodGet, "/v1/health", nil)
	var response healthResponse
	decodeResponse(t, health, &response)
	if !response.Provider.Capabilities.Network ||
		!response.Provider.Capabilities.Proxy {
		t.Fatalf("network capabilities missing: %+v", response.Provider.Capabilities)
	}

	withoutNetwork := New(provider, "test")
	unavailable := performRequest(
		withoutNetwork,
		http.MethodGet,
		"/v1/network",
		nil,
	)
	if unavailable.Code != http.StatusNotImplemented {
		t.Fatalf("unavailable status = %d, body = %s", unavailable.Code, unavailable.Body.String())
	}
}

func assertNetworkWireSnapshot(
	t *testing.T,
	snapshot domain.NetworkSnapshot,
) {
	t.Helper()
	if snapshot.BootEpoch != "network_boot_test" ||
		len(snapshot.Lines) != 1 ||
		len(snapshot.Proxies) != 1 {
		t.Fatalf("unexpected snapshot: %+v", snapshot)
	}
	line := snapshot.Lines[0]
	if line.LineID != "line-main" ||
		!line.Connected ||
		line.Interface != "wwan0" ||
		len(line.DNS) != 1 ||
		line.RXBytes != 100 ||
		line.TXBytes != 200 {
		t.Fatalf("unexpected line: %+v", line)
	}
	proxy := snapshot.Proxies[0]
	if proxy.ID != "main-proxy" ||
		proxy.State != domain.ProxyStateRunning ||
		!proxy.Running ||
		proxy.RuntimeEpoch != "proxy_runtime_test" ||
		proxy.StartedAt == nil ||
		proxy.BytesUp != 300 ||
		proxy.BytesDown != 400 ||
		proxy.Connections != 5 ||
		proxy.ActiveConnections != 1 {
		t.Fatalf("unexpected proxy: %+v", proxy)
	}
}

var _ domain.NetworkProvider = (*fakeNetworkProvider)(nil)
