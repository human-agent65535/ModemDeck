package mobilepairing

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

type cloudflaredTestIngress struct {
	Hostname string  `json:"hostname"`
	Path     *string `json:"path"`
	Service  string  `json:"service"`
}

func newCloudflaredManagementServer(
	t *testing.T,
	readyStatus int,
	ingress []cloudflaredTestIngress,
) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(
		func(response http.ResponseWriter, request *http.Request) {
			switch request.URL.Path {
			case "/ready":
				response.WriteHeader(readyStatus)
			case "/config":
				response.Header().Set("Content-Type", "application/json")
				if err := json.NewEncoder(response).Encode(map[string]any{
					"version": 4,
					"config": map[string]any{
						"ingress": ingress,
					},
				}); err != nil {
					t.Errorf("encode cloudflared config: %v", err)
				}
			default:
				http.NotFound(response, request)
			}
		},
	))
	t.Cleanup(server.Close)
	return server
}

func useTLSTestTransport(
	t *testing.T,
	gateway *CloudflareGateway,
	server *httptest.Server,
) {
	t.Helper()
	gateway.client.Transport = server.Client().Transport
}

func TestCloudflareGatewayDisabledWithoutInstallationConfiguration(
	t *testing.T,
) {
	t.Parallel()

	gateway, err := NewCloudflareGateway("")
	if err != nil {
		t.Fatalf("NewCloudflareGateway() error = %v", err)
	}
	status := gateway.Status(context.Background())
	if status.Enabled ||
		status.ConnectorConnected ||
		status.Connected ||
		status.PublicURL != "" ||
		len(status.APIURLs) != 0 ||
		len(status.WebURLs) != 0 {
		t.Fatalf("status = %+v", status)
	}
}

func TestCloudflareGatewayDiscoversAndVerifiesAPIIngress(t *testing.T) {
	t.Parallel()

	var gateway *CloudflareGateway
	public := httptest.NewTLSServer(http.HandlerFunc(
		func(response http.ResponseWriter, request *http.Request) {
			if request.URL.Path != CloudflareProbePath {
				http.NotFound(response, request)
				return
			}
			proof, ok := gateway.CloudflareProbeProof(request)
			if !ok {
				http.NotFound(response, request)
				return
			}
			response.Header().Set(CloudflareProbeProofHeader, proof)
			response.WriteHeader(http.StatusNoContent)
		},
	))
	t.Cleanup(public.Close)
	publicHostname := strings.TrimPrefix(public.URL, "https://")
	management := newCloudflaredManagementServer(
		t,
		http.StatusOK,
		[]cloudflaredTestIngress{
			{
				Hostname: "web.example.com",
				Service:  "https://modemdeck:7577",
			},
			{
				Hostname: publicHostname,
				Service:  cloudflareAPIOrigin,
			},
		},
	)

	var err error
	gateway, err = NewCloudflareGateway(management.URL + "/ready")
	if err != nil {
		t.Fatalf("NewCloudflareGateway() error = %v", err)
	}
	useTLSTestTransport(t, gateway, public)
	status := gateway.Status(context.Background())
	if !status.Enabled ||
		!status.ConnectorConnected ||
		!status.Connected ||
		status.PublicURL != public.URL ||
		len(status.APIURLs) != 1 ||
		status.APIURLs[0] != public.URL ||
		len(status.WebURLs) != 1 ||
		status.WebURLs[0] != "https://web.example.com" {
		t.Fatalf("status = %+v", status)
	}
}

func TestCloudflareGatewayDoesNotTreatConnectorHealthAsPublicReachability(
	t *testing.T,
) {
	t.Parallel()

	public := httptest.NewTLSServer(http.HandlerFunc(
		func(response http.ResponseWriter, _ *http.Request) {
			response.WriteHeader(http.StatusBadGateway)
		},
	))
	t.Cleanup(public.Close)
	management := newCloudflaredManagementServer(
		t,
		http.StatusOK,
		[]cloudflaredTestIngress{{
			Hostname: strings.TrimPrefix(public.URL, "https://"),
			Service:  cloudflareAPIOrigin,
		}},
	)
	gateway, err := NewCloudflareGateway(management.URL + "/ready")
	if err != nil {
		t.Fatal(err)
	}
	useTLSTestTransport(t, gateway, public)
	status := gateway.Status(context.Background())
	if !status.Enabled ||
		!status.ConnectorConnected ||
		status.Connected ||
		status.PublicURL != public.URL ||
		len(status.APIURLs) != 1 ||
		status.APIURLs[0] != public.URL {
		t.Fatalf("status = %+v", status)
	}
}

func TestCloudflareGatewayDiscoversRouteWhileConnectorIsDown(t *testing.T) {
	t.Parallel()

	management := newCloudflaredManagementServer(
		t,
		http.StatusServiceUnavailable,
		[]cloudflaredTestIngress{{
			Hostname: "phone.example.com",
			Service:  cloudflareAPIOrigin,
		}},
	)
	gateway, err := NewCloudflareGateway(management.URL + "/ready")
	if err != nil {
		t.Fatalf("NewCloudflareGateway() error = %v", err)
	}
	status := gateway.Status(context.Background())
	if !status.Enabled ||
		status.ConnectorConnected ||
		status.Connected ||
		status.PublicURL != "https://phone.example.com" ||
		len(status.APIURLs) != 1 ||
		status.APIURLs[0] != "https://phone.example.com" {
		t.Fatalf("status = %+v", status)
	}
}

func TestCloudflareGatewayRefreshesDiscoveredIngress(t *testing.T) {
	t.Parallel()

	var (
		lock    sync.RWMutex
		ingress = []cloudflaredTestIngress{
			{
				Hostname: "old-api.example.com",
				Service:  cloudflareAPIOrigin,
			},
			{
				Hostname: "old-web.example.com",
				Service:  cloudflareWebOrigin,
			},
		}
	)
	management := httptest.NewServer(http.HandlerFunc(
		func(response http.ResponseWriter, request *http.Request) {
			switch request.URL.Path {
			case "/ready":
				response.WriteHeader(http.StatusServiceUnavailable)
			case "/config":
				lock.RLock()
				current := append([]cloudflaredTestIngress(nil), ingress...)
				lock.RUnlock()
				if err := json.NewEncoder(response).Encode(map[string]any{
					"config": map[string]any{"ingress": current},
				}); err != nil {
					t.Errorf("encode cloudflared config: %v", err)
				}
			default:
				http.NotFound(response, request)
			}
		},
	))
	t.Cleanup(management.Close)

	gateway, err := NewCloudflareGateway(management.URL + "/ready")
	if err != nil {
		t.Fatalf("NewCloudflareGateway() error = %v", err)
	}
	first := gateway.Status(context.Background())
	if first.PublicURL != "https://old-api.example.com" ||
		len(first.WebURLs) != 1 ||
		first.WebURLs[0] != "https://old-web.example.com" {
		t.Fatalf("first status = %+v", first)
	}

	lock.Lock()
	ingress = []cloudflaredTestIngress{
		{
			Hostname: "new-api.example.com",
			Service:  cloudflareAPIOrigin,
		},
		{
			Hostname: "new-web.example.com",
			Service:  cloudflareWebOrigin,
		},
	}
	lock.Unlock()

	second := gateway.Status(context.Background())
	if second.PublicURL != "https://new-api.example.com" ||
		len(second.APIURLs) != 1 ||
		second.APIURLs[0] != "https://new-api.example.com" ||
		len(second.WebURLs) != 1 ||
		second.WebURLs[0] != "https://new-web.example.com" {
		t.Fatalf("second status = %+v", second)
	}
}

func TestCloudflareGatewayRejectsAmbiguousAPIIngress(t *testing.T) {
	t.Parallel()

	management := newCloudflaredManagementServer(
		t,
		http.StatusOK,
		[]cloudflaredTestIngress{
			{
				Hostname: "phone-a.example.com",
				Service:  cloudflareAPIOrigin,
			},
			{
				Hostname: "phone-b.example.com",
				Service:  cloudflareAPIOrigin + "/",
			},
		},
	)
	gateway, err := NewCloudflareGateway(management.URL + "/ready")
	if err != nil {
		t.Fatalf("NewCloudflareGateway() error = %v", err)
	}
	status := gateway.Status(context.Background())
	if !status.Enabled ||
		!status.ConnectorConnected ||
		status.Connected ||
		status.PublicURL != "" ||
		len(status.APIURLs) != 2 ||
		status.APIURLs[0] != "https://phone-a.example.com" ||
		status.APIURLs[1] != "https://phone-b.example.com" {
		t.Fatalf("status = %+v", status)
	}
}

func TestCloudflareGatewayIgnoresPathScopedAndWildcardIngress(t *testing.T) {
	t.Parallel()

	path := "/api/v1/calls"
	management := newCloudflaredManagementServer(
		t,
		http.StatusOK,
		[]cloudflaredTestIngress{
			{
				Hostname: "path.example.com",
				Path:     &path,
				Service:  cloudflareAPIOrigin,
			},
			{
				Hostname: "*.example.com",
				Service:  cloudflareAPIOrigin,
			},
			{
				Hostname: "wrong.example.com",
				Service:  "http://api:8080",
			},
		},
	)
	gateway, err := NewCloudflareGateway(management.URL + "/ready")
	if err != nil {
		t.Fatalf("NewCloudflareGateway() error = %v", err)
	}
	status := gateway.Status(context.Background())
	if !status.Enabled ||
		!status.ConnectorConnected ||
		status.Connected ||
		status.PublicURL != "" ||
		len(status.APIURLs) != 0 ||
		len(status.WebURLs) != 0 {
		t.Fatalf("status = %+v", status)
	}
}

func TestCloudflareGatewayRejectsUnsafeConfiguration(t *testing.T) {
	t.Parallel()

	for _, readyURL := range []string{
		"http://cloudflared:2000",
		"http://cloudflared:2000/ready?token=value",
		"file:///run/cloudflared.ready",
	} {
		if _, err := NewCloudflareGateway(readyURL); err == nil {
			t.Fatalf("NewCloudflareGateway(%q) error = nil", readyURL)
		}
	}
}
