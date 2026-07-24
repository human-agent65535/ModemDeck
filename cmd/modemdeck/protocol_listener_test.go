package main

import (
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRedirectPlainHTTPToHTTPS(t *testing.T) {
	request := httptest.NewRequest(
		http.MethodGet,
		"http://192.0.2.10:7577/messages?line=main",
		nil,
	)
	response := httptest.NewRecorder()

	redirectPlainHTTPToHTTPS(http.NotFoundHandler()).ServeHTTP(response, request)

	if response.Code != http.StatusPermanentRedirect {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusPermanentRedirect)
	}
	const expected = "https://192.0.2.10:7577/messages?line=main"
	if location := response.Header().Get("Location"); location != expected {
		t.Fatalf("Location = %q, want %q", location, expected)
	}
}

func TestRedirectPlainHTTPPassesTLSRequestsToApplication(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "https://modemdeck.test/", nil)
	request.TLS = &tls.ConnectionState{}
	response := httptest.NewRecorder()

	redirectPlainHTTPToHTTPS(http.HandlerFunc(
		func(response http.ResponseWriter, _ *http.Request) {
			response.WriteHeader(http.StatusNoContent)
		},
	)).ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNoContent)
	}
}

func TestTLSAndPlainHTTPShareOneListener(t *testing.T) {
	certificateSource := httptest.NewTLSServer(http.NotFoundHandler())
	certificate := certificateSource.TLS.Certificates[0]
	certificateSource.Close()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	tlsConfig := prepareServerTLSConfig(&tls.Config{
		Certificates: []tls.Certificate{certificate},
		MinVersion:   tls.VersionTLS12,
	})
	server := &http.Server{
		Handler: redirectPlainHTTPToHTTPS(http.HandlerFunc(
			func(response http.ResponseWriter, _ *http.Request) {
				_, _ = io.WriteString(response, "secure")
			},
		)),
		ReadHeaderTimeout: time.Second,
		TLSConfig:         tlsConfig,
	}
	serverDone := make(chan error, 1)
	go func() {
		serverDone <- server.Serve(newTLSAndPlainListener(listener, tlsConfig))
	}()
	t.Cleanup(func() {
		_ = server.Close()
		if serveErr := <-serverDone; serveErr != nil &&
			serveErr != http.ErrServerClosed {
			t.Errorf("serve: %v", serveErr)
		}
	})

	plainClient := &http.Client{
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Timeout: 2 * time.Second,
	}
	plainResponse, err := plainClient.Get("http://" + listener.Addr().String() + "/calls")
	if err != nil {
		t.Fatalf("plain HTTP request: %v", err)
	}
	_ = plainResponse.Body.Close()
	if plainResponse.StatusCode != http.StatusPermanentRedirect {
		t.Fatalf(
			"plain HTTP status = %d, want %d",
			plainResponse.StatusCode,
			http.StatusPermanentRedirect,
		)
	}

	secureClient := &http.Client{
		Transport: &http.Transport{
			ForceAttemptHTTP2: true,
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true, // Test-only certificate.
				MinVersion:         tls.VersionTLS12,
			},
		},
		Timeout: 2 * time.Second,
	}
	secureResponse, err := secureClient.Get("https://" + listener.Addr().String() + "/calls")
	if err != nil {
		t.Fatalf("HTTPS request: %v", err)
	}
	body, err := io.ReadAll(secureResponse.Body)
	_ = secureResponse.Body.Close()
	if err != nil {
		t.Fatalf("read HTTPS response: %v", err)
	}
	if string(body) != "secure" {
		t.Fatalf("HTTPS body = %q, want %q", body, "secure")
	}
	if secureResponse.ProtoMajor != 2 {
		t.Fatalf("HTTPS protocol = %q, want HTTP/2", secureResponse.Proto)
	}
}
