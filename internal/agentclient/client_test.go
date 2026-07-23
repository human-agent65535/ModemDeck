package agentclient

import (
	"context"
	"errors"
	"net"
	"net/http"
	"path/filepath"
	"testing"
	"time"
)

func TestHealthOverUnixSocket(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "agent.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/health" {
			http.NotFound(response, request)
			return
		}
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{
			"status":"ok",
			"api_version":"v1",
			"agent_version":"test",
			"provider":{
				"name":"org.freedesktop.ModemManager1",
				"available":true,
				"capabilities":{"discovery":true,"dial":false,"answer_call":false,"hangup_call":false,"send_message":false}
			}
		}`))
	})}
	serveResult := make(chan error, 1)
	go func() {
		serveResult <- server.Serve(listener)
	}()
	t.Cleanup(func() {
		_ = server.Close()
		_ = listener.Close()
		<-serveResult
	})

	client, err := New(socketPath, time.Second)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	health, err := client.Health(context.Background())
	if err != nil {
		t.Fatalf("Health() error = %v", err)
	}
	if health.APIVersion != APIVersion || !health.Provider.Available || !health.Provider.Capabilities.Discovery {
		t.Fatalf("unexpected health: %+v", health)
	}
	if health.Provider.Capabilities.Dial || health.Provider.Capabilities.SendMessage {
		t.Fatalf("unimplemented mutations were advertised: %+v", health.Provider.Capabilities)
	}
}

func TestHealthRejectsIncompatibleVersion(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "agent.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write([]byte(`{"api_version":"v2"}`))
	})}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() {
		_ = server.Close()
		_ = listener.Close()
	})

	client, err := New(socketPath, time.Second)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if _, err := client.Health(context.Background()); err == nil {
		t.Fatal("Health() accepted an incompatible API version")
	}
}

func TestNewRejectsRelativeSocketPath(t *testing.T) {
	if _, err := New("agent.sock", time.Second); !errors.Is(err, ErrInvalidSocketPath) {
		t.Fatalf("New() error = %v, want ErrInvalidSocketPath", err)
	}
}
