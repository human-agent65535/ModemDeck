package mobilepairing

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCloudflareGatewayDisabledWithoutInstallationConfiguration(
	t *testing.T,
) {
	t.Parallel()

	gateway, err := NewCloudflareGateway("", "")
	if err != nil {
		t.Fatalf("NewCloudflareGateway() error = %v", err)
	}
	if status := gateway.Status(context.Background()); status != (CloudflareStatus{}) {
		t.Fatalf("status = %+v", status)
	}
}

func TestCloudflareGatewayReportsLiveConnector(t *testing.T) {
	t.Parallel()

	ready := httptest.NewServer(http.HandlerFunc(
		func(response http.ResponseWriter, request *http.Request) {
			if request.URL.Path != "/ready" {
				http.NotFound(response, request)
				return
			}
			response.WriteHeader(http.StatusOK)
		},
	))
	t.Cleanup(ready.Close)

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

	gateway, err := NewCloudflareGateway(public.URL+"/", ready.URL+"/ready")
	if err != nil {
		t.Fatalf("NewCloudflareGateway() error = %v", err)
	}
	gateway.client = public.Client()
	status := gateway.Status(context.Background())
	if !status.Enabled ||
		!status.ConnectorConnected ||
		!status.Connected ||
		status.PublicURL != public.URL {
		t.Fatalf("status = %+v", status)
	}
}

func TestCloudflareGatewayDoesNotTreatConnectorHealthAsPublicReachability(
	t *testing.T,
) {
	t.Parallel()

	ready := httptest.NewServer(http.HandlerFunc(
		func(response http.ResponseWriter, _ *http.Request) {
			response.WriteHeader(http.StatusOK)
		},
	))
	t.Cleanup(ready.Close)
	public := httptest.NewTLSServer(http.HandlerFunc(
		func(response http.ResponseWriter, _ *http.Request) {
			response.WriteHeader(http.StatusBadGateway)
		},
	))
	t.Cleanup(public.Close)

	gateway, err := NewCloudflareGateway(public.URL, ready.URL)
	if err != nil {
		t.Fatal(err)
	}
	gateway.client = public.Client()
	status := gateway.Status(context.Background())
	if !status.Enabled || !status.ConnectorConnected || status.Connected {
		t.Fatalf("status = %+v", status)
	}
}

func TestCloudflareGatewayKeepsInstallationStateWhenConnectorIsDown(
	t *testing.T,
) {
	t.Parallel()

	ready := httptest.NewServer(http.HandlerFunc(
		func(response http.ResponseWriter, _ *http.Request) {
			http.Error(response, "not ready", http.StatusServiceUnavailable)
		},
	))
	ready.Close()

	gateway, err := NewCloudflareGateway(
		"https://phone.example.com",
		ready.URL+"/ready",
	)
	if err != nil {
		t.Fatalf("NewCloudflareGateway() error = %v", err)
	}
	status := gateway.Status(context.Background())
	if !status.Enabled ||
		status.ConnectorConnected ||
		status.Connected ||
		status.PublicURL != "https://phone.example.com" {
		t.Fatalf("status = %+v", status)
	}
}

func TestCloudflareGatewayRejectsIncompleteOrUnsafeConfiguration(
	t *testing.T,
) {
	t.Parallel()

	for _, input := range []struct {
		publicURL string
		readyURL  string
	}{
		{publicURL: "https://phone.example.com"},
		{readyURL: "http://cloudflared:2000/ready"},
		{
			publicURL: "http://phone.example.com",
			readyURL:  "http://cloudflared:2000/ready",
		},
		{
			publicURL: "https://phone.example.com/api",
			readyURL:  "http://cloudflared:2000/ready",
		},
		{
			publicURL: "https://phone.example.com",
			readyURL:  "file:///run/cloudflared.ready",
		},
	} {
		if _, err := NewCloudflareGateway(
			input.publicURL,
			input.readyURL,
		); err == nil {
			t.Fatalf(
				"NewCloudflareGateway(%q, %q) error = nil",
				input.publicURL,
				input.readyURL,
			)
		}
	}
}
