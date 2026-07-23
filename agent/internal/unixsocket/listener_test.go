package unixsocket_test

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/human-agent65535/modemdeck/agent/internal/domain"
	"github.com/human-agent65535/modemdeck/agent/internal/httpapi"
	"github.com/human-agent65535/modemdeck/agent/internal/unixsocket"
)

type providerStub struct{}

func (providerStub) Health(context.Context) (domain.ProviderHealth, error) {
	return domain.ProviderHealth{
		Name:      "org.freedesktop.ModemManager1",
		Available: true,
		Capabilities: domain.AgentCapabilities{
			Discovery: true,
		},
	}, nil
}

func (providerStub) Lines(context.Context) ([]domain.Line, error) {
	return []domain.Line{}, nil
}

func (providerStub) StartCall(context.Context, domain.StartCallRequest) (domain.Call, error) {
	return domain.Call{}, domain.NotSupported("start_call")
}

func (providerStub) AnswerCall(context.Context, string) (domain.Call, error) {
	return domain.Call{}, domain.NotSupported("answer_call")
}

func (providerStub) HangupCall(context.Context, string) (domain.Call, error) {
	return domain.Call{}, domain.NotSupported("hangup_call")
}

func (providerStub) SendMessage(context.Context, domain.SendMessageRequest) (domain.Message, error) {
	return domain.Message{}, domain.NotSupported("send_message")
}

func TestHTTPOverUnixSocket(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "agent.sock")
	listener, err := unixsocket.Listen(unixsocket.Config{
		Path: socketPath,
		Mode: 0o660,
		UID:  -1,
		GID:  -1,
	})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	info, err := os.Stat(socketPath)
	if err != nil {
		t.Fatalf("stat socket: %v", err)
	}
	if info.Mode()&os.ModeSocket == 0 || info.Mode().Perm() != 0o660 {
		t.Fatalf("socket mode = %v", info.Mode())
	}

	server := &http.Server{Handler: httpapi.New(providerStub{}, "unix-test")}
	serveResult := make(chan error, 1)
	go func() {
		serveResult <- server.Serve(listener)
	}()

	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
		},
	}
	client := &http.Client{Transport: transport}
	response, err := client.Get("http://modemdeck/v1/health")
	if err != nil {
		t.Fatalf("get health over unix socket: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("health status = %d", response.StatusCode)
	}
	var body struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode health: %v", err)
	}
	if body.Status != "ok" {
		t.Fatalf("health status body = %q", body.Status)
	}

	transport.CloseIdleConnections()
	if err := server.Close(); err != nil {
		t.Fatalf("close server: %v", err)
	}
	if err := <-serveResult; err != nil && !errors.Is(err, http.ErrServerClosed) {
		t.Fatalf("serve result: %v", err)
	}
}

func TestListenRefusesNonSocketPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent.sock")
	if err := os.WriteFile(path, []byte("do not remove"), 0o600); err != nil {
		t.Fatalf("write sentinel: %v", err)
	}
	if _, err := unixsocket.Listen(unixsocket.Config{Path: path, Mode: 0o660, UID: -1, GID: -1}); err == nil {
		t.Fatal("expected non-socket path error")
	}
	contents, err := os.ReadFile(path)
	if err != nil || string(contents) != "do not remove" {
		t.Fatalf("sentinel was changed: %q, %v", contents, err)
	}
}
