package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/human-agent65535/modemdeck/internal/httpapi"
	"github.com/human-agent65535/modemdeck/internal/tlsmanager"
)

type fakeCloudflareOriginCertificateManager struct {
	status       tlsmanager.CloudflareOriginStatus
	installError error
	disableError error
	disableCalls int
}

func (manager *fakeCloudflareOriginCertificateManager) Status() tlsmanager.CloudflareOriginStatus {
	return manager.status
}

func (manager *fakeCloudflareOriginCertificateManager) Install(
	_ []byte,
	_ []byte,
	_ []string,
) (tlsmanager.CloudflareOriginStatus, error) {
	if manager.installError != nil {
		return tlsmanager.CloudflareOriginStatus{}, manager.installError
	}
	manager.status.Enabled = true
	return manager.status, nil
}

func (manager *fakeCloudflareOriginCertificateManager) Disable() error {
	manager.disableCalls++
	if manager.disableError != nil {
		return manager.disableError
	}
	manager.status = tlsmanager.CloudflareOriginStatus{}
	return nil
}

func (*fakeCloudflareOriginCertificateManager) CoversDNSNames([]string) bool {
	return true
}

func TestCloudflareOriginTLSInstallWaitsForActivation(t *testing.T) {
	t.Parallel()
	manager := &fakeCloudflareOriginCertificateManager{
		status: testOriginManagerStatus("AA:BB"),
	}
	activatedFingerprint := ""
	service := &cloudflareOriginTLSService{
		manager: manager,
		activate: func(_ context.Context, fingerprint string) error {
			activatedFingerprint = fingerprint
			return nil
		},
	}
	status, err := service.Install(context.Background(), []byte("certificate"), []byte("key"))
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	if !status.Enabled || activatedFingerprint != "AA:BB" || manager.disableCalls != 0 {
		t.Fatalf(
			"status = %+v, fingerprint = %q, disable calls = %d",
			status,
			activatedFingerprint,
			manager.disableCalls,
		)
	}
}

func TestCloudflareOriginTLSInstallRollsBackActivationFailure(t *testing.T) {
	t.Parallel()
	manager := &fakeCloudflareOriginCertificateManager{
		status: testOriginManagerStatus("AA:BB"),
	}
	service := &cloudflareOriginTLSService{
		manager: manager,
		activate: func(context.Context, string) error {
			return errors.New("HTTP/2 unavailable")
		},
	}
	status, err := service.Install(context.Background(), []byte("certificate"), []byte("key"))
	if !errors.Is(err, httpapi.ErrCloudflareOriginTLSActivation) {
		t.Fatalf("Install() error = %v, want activation failure", err)
	}
	if status.Enabled || manager.status.Enabled || manager.disableCalls != 1 {
		t.Fatalf(
			"status = %+v, manager status = %+v, disable calls = %d",
			status,
			manager.status,
			manager.disableCalls,
		)
	}
}

func TestCloudflareOriginTLSActivatorRequiresPinnedHTTP2(t *testing.T) {
	t.Parallel()
	http2Server := httptest.NewUnstartedServer(http.HandlerFunc(
		func(response http.ResponseWriter, _ *http.Request) {
			response.WriteHeader(http.StatusNoContent)
		},
	))
	http2Server.EnableHTTP2 = true
	http2Server.StartTLS()
	t.Cleanup(http2Server.Close)
	fingerprint := sha256.Sum256(http2Server.Certificate().Raw)
	encodedFingerprint := hex.EncodeToString(fingerprint[:])
	activator, err := newCloudflareOriginTLSActivator(http2Server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if err := activator(context.Background(), encodedFingerprint); err != nil {
		t.Fatalf("HTTP/2 activation error = %v", err)
	}

	for _, test := range []struct {
		name        string
		server      *httptest.Server
		fingerprint string
	}{
		{
			name:        "wrong certificate",
			server:      http2Server,
			fingerprint: hex.EncodeToString(make([]byte, sha256.Size)),
		},
		{
			name: "HTTP/1.1",
			server: func() *httptest.Server {
				server := httptest.NewTLSServer(http.HandlerFunc(
					func(response http.ResponseWriter, _ *http.Request) {
						response.WriteHeader(http.StatusNoContent)
					},
				))
				t.Cleanup(server.Close)
				return server
			}(),
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			probeFingerprint := test.fingerprint
			if probeFingerprint == "" {
				sum := sha256.Sum256(test.server.Certificate().Raw)
				probeFingerprint = hex.EncodeToString(sum[:])
			}
			probe, probeErr := newCloudflareOriginTLSActivator(test.server.URL)
			if probeErr != nil {
				t.Fatal(probeErr)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
			defer cancel()
			if err := probe(ctx, probeFingerprint); err == nil {
				t.Fatal("activation unexpectedly succeeded")
			}
		})
	}
}

func testOriginManagerStatus(fingerprint string) tlsmanager.CloudflareOriginStatus {
	return tlsmanager.CloudflareOriginStatus{
		Certificate: tlsmanager.Status{
			Subject:           "CN=example.com",
			Issuer:            "Cloudflare Origin CA",
			DNSNames:          []string{"example.com"},
			NotBefore:         time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
			NotAfter:          time.Date(2031, 8, 1, 0, 0, 0, 0, time.UTC),
			FingerprintSHA256: fingerprint,
		},
	}
}
