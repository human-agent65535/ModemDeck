package networking

import (
	"net"
	"strings"
	"testing"
	"time"

	"github.com/human-agent65535/modemdeck/agent/internal/domain"
)

func TestProxyServerStartAndCloseAreBounded(t *testing.T) {
	for _, mode := range []domain.ProxyMode{
		domain.ProxyModeSOCKS5,
		domain.ProxyModeHTTP,
	} {
		t.Run(string(mode), func(t *testing.T) {
			runner, err := newProxyServer(
				domain.ProxyConfiguration{
					ID:            "test-proxy",
					LineID:        "line-test",
					Enabled:       true,
					Mode:          mode,
					ListenAddress: "127.0.0.1",
					// Port zero is used only to allocate an isolated test listener.
					ListenPort: 0,
				},
				bearer{
					Interface: "lo",
					DNS:       []string{"127.0.0.1"},
				},
			)
			if err != nil {
				t.Fatalf("new proxy server: %v", err)
			}
			if err := runner.Start(); err != nil {
				t.Fatalf("start proxy server: %v", err)
			}
			if !runner.Running() || runner.LastError() != "" {
				t.Fatalf("running/error = %v/%q", runner.Running(), runner.LastError())
			}
			if err := runner.Close(); err != nil {
				t.Fatalf("close proxy server: %v", err)
			}
			if runner.Running() {
				t.Fatal("proxy server remains running after Close")
			}
			if err := runner.Close(); err != nil {
				t.Fatalf("close proxy server twice: %v", err)
			}
		})
	}
}

func TestSOCKSHandshakeTimeoutAndConnectionBudget(t *testing.T) {
	runner, err := newProxyServer(
		domain.ProxyConfiguration{
			ID:            "limited-socks",
			LineID:        "line-test",
			Enabled:       true,
			Mode:          domain.ProxyModeSOCKS5,
			ListenAddress: "127.0.0.1",
			ListenPort:    0,
		},
		bearer{
			Interface: "lo",
			DNS:       []string{"127.0.0.1"},
		},
	)
	if err != nil {
		t.Fatalf("new proxy server: %v", err)
	}
	server := runner.(*proxyServer)
	server.connectionSlots = make(chan struct{}, 1)
	server.handshakeTime = 250 * time.Millisecond
	if err := server.Start(); err != nil {
		t.Fatalf("start proxy server: %v", err)
	}
	t.Cleanup(func() {
		_ = server.Close()
	})

	first, err := net.Dial("tcp", server.listener.Addr().String())
	if err != nil {
		t.Fatalf("dial first empty client: %v", err)
	}
	defer first.Close()
	waitForCondition(t, time.Second, func() bool {
		return len(server.connectionSlots) == 1
	})

	second, err := net.Dial("tcp", server.listener.Addr().String())
	if err != nil {
		t.Fatalf("dial over-budget client: %v", err)
	}
	defer second.Close()
	_ = second.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := second.Read(make([]byte, 1)); err == nil {
		t.Fatal("over-budget empty connection remained open")
	}

	_ = first.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := first.Read(make([]byte, 1)); err == nil {
		t.Fatal("empty SOCKS connection exceeded handshake deadline")
	}
	waitForCondition(t, time.Second, func() bool {
		return len(server.connectionSlots) == 0
	})
}

func TestSOCKSCloseCancelsHandshakeAndWaitsForConnection(t *testing.T) {
	runner, err := newProxyServer(
		domain.ProxyConfiguration{
			ID:            "cancel-socks",
			LineID:        "line-test",
			Enabled:       true,
			Mode:          domain.ProxyModeSOCKS5,
			ListenAddress: "127.0.0.1",
			ListenPort:    0,
		},
		bearer{
			Interface: "lo",
			DNS:       []string{"127.0.0.1"},
		},
	)
	if err != nil {
		t.Fatalf("new proxy server: %v", err)
	}
	server := runner.(*proxyServer)
	server.connectionSlots = make(chan struct{}, 1)
	server.handshakeTime = time.Hour
	if err := server.Start(); err != nil {
		t.Fatalf("start proxy server: %v", err)
	}

	client, err := net.Dial("tcp", server.listener.Addr().String())
	if err != nil {
		t.Fatalf("dial empty client: %v", err)
	}
	defer client.Close()
	waitForCondition(t, time.Second, func() bool {
		return len(server.connectionSlots) == 1
	})

	started := time.Now()
	if err := server.Close(); err != nil {
		t.Fatalf("close proxy server: %v", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("canceled SOCKS shutdown took %s", elapsed)
	}
	if len(server.connectionSlots) != 0 {
		t.Fatalf("SOCKS slot was not released: %d", len(server.connectionSlots))
	}
}

func TestProxyServersShareGlobalConnectionBudget(t *testing.T) {
	first := newTestProxyServer(t, "first", domain.ProxyModeSOCKS5)
	second := newTestProxyServer(t, "second", domain.ProxyModeHTTP)
	if first.connectionSlots != globalProxyConnectionSlots ||
		second.connectionSlots != globalProxyConnectionSlots ||
		first.connectionSlots != second.connectionSlots {
		t.Fatal("proxy servers do not share the process-wide connection budget")
	}
}

func TestHTTPConnectionBudgetRejectsExcessClients(t *testing.T) {
	server := newTestProxyServer(t, "limited-http", domain.ProxyModeHTTP)
	server.connectionSlots = make(chan struct{}, 1)
	if err := server.Start(); err != nil {
		t.Fatalf("start HTTP proxy: %v", err)
	}
	t.Cleanup(func() {
		_ = server.Close()
	})

	first, err := net.Dial("tcp", server.listener.Addr().String())
	if err != nil {
		t.Fatalf("dial first HTTP client: %v", err)
	}
	defer first.Close()
	waitForCondition(t, time.Second, func() bool {
		return len(server.connectionSlots) == 1
	})

	second, err := net.Dial("tcp", server.listener.Addr().String())
	if err != nil {
		t.Fatalf("dial over-budget HTTP client: %v", err)
	}
	defer second.Close()
	_ = second.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := second.Read(make([]byte, 1)); err == nil {
		t.Fatal("over-budget HTTP connection remained open")
	}

	_ = first.Close()
	waitForCondition(t, time.Second, func() bool {
		return len(server.connectionSlots) == 0
	})
}

func TestProxyServerProtocolCredentialLimits(t *testing.T) {
	testCases := []struct {
		name          string
		mode          domain.ProxyMode
		username      string
		password      string
		errorContains string
	}{
		{
			name:          "socks password length",
			mode:          domain.ProxyModeSOCKS5,
			username:      "user",
			password:      strings.Repeat("p", 256),
			errorContains: "255 bytes",
		},
		{
			name:          "http username colon",
			mode:          domain.ProxyModeHTTP,
			username:      "invalid:user",
			password:      "password",
			errorContains: "must not contain a colon",
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := newProxyServer(
				domain.ProxyConfiguration{
					ID:            "invalid-auth",
					LineID:        "line-test",
					Enabled:       true,
					Mode:          testCase.mode,
					ListenAddress: "127.0.0.1",
					ListenPort:    1080,
					AuthEnabled:   true,
					Username:      testCase.username,
					Password:      testCase.password,
				},
				bearer{
					Interface: "lo",
					DNS:       []string{"127.0.0.1"},
				},
			)
			if err == nil || !strings.Contains(err.Error(), testCase.errorContains) {
				t.Fatalf("protocol validation error = %v", err)
			}
		})
	}
}

func TestHTTPServerDoesNotApplyWholeBodyTimeouts(t *testing.T) {
	runner, err := newProxyServer(
		domain.ProxyConfiguration{
			ID:            "http-timeouts",
			LineID:        "line-test",
			Enabled:       true,
			Mode:          domain.ProxyModeHTTP,
			ListenAddress: "127.0.0.1",
			ListenPort:    8080,
		},
		bearer{
			Interface: "lo",
			DNS:       []string{"127.0.0.1"},
		},
	)
	if err != nil {
		t.Fatalf("new proxy server: %v", err)
	}
	server := runner.(*proxyServer)
	if server.httpServer.ReadTimeout != 0 ||
		server.httpServer.WriteTimeout != 0 ||
		server.httpServer.ReadHeaderTimeout <= 0 ||
		server.httpServer.IdleTimeout <= 0 {
		t.Fatalf(
			"HTTP timeouts read=%s write=%s header=%s idle=%s",
			server.httpServer.ReadTimeout,
			server.httpServer.WriteTimeout,
			server.httpServer.ReadHeaderTimeout,
			server.httpServer.IdleTimeout,
		)
	}
}

func newTestSOCKSServer(t *testing.T, id string) *proxyServer {
	return newTestProxyServer(t, id, domain.ProxyModeSOCKS5)
}

func newTestProxyServer(
	t *testing.T,
	id string,
	mode domain.ProxyMode,
) *proxyServer {
	t.Helper()
	runner, err := newProxyServer(
		domain.ProxyConfiguration{
			ID:            id,
			LineID:        "line-test",
			Enabled:       true,
			Mode:          mode,
			ListenAddress: "127.0.0.1",
			ListenPort:    1080,
		},
		bearer{
			Interface: "lo",
			DNS:       []string{"127.0.0.1"},
		},
	)
	if err != nil {
		t.Fatalf("new proxy server: %v", err)
	}
	server := runner.(*proxyServer)
	t.Cleanup(server.cancelSOCKS)
	return server
}

func testProxyConfiguration() domain.ProxyConfiguration {
	return domain.ProxyConfiguration{
		ID:            "test-proxy",
		LineID:        "line-test",
		Enabled:       true,
		Mode:          domain.ProxyModeSOCKS5,
		ListenAddress: "127.0.0.1",
		ListenPort:    1080,
	}
}

func waitForCondition(
	t *testing.T,
	timeout time.Duration,
	condition func() bool,
) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatal("condition did not become true before timeout")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func tcpConnectionPair(t *testing.T) (*net.TCPConn, *net.TCPConn) {
	t.Helper()
	listener, err := net.ListenTCP(
		"tcp",
		&net.TCPAddr{IP: net.ParseIP("127.0.0.1")},
	)
	if err != nil {
		t.Fatalf("listen TCP pair: %v", err)
	}
	defer listener.Close()

	accepted := make(chan *net.TCPConn, 1)
	acceptError := make(chan error, 1)
	go func() {
		connection, err := listener.AcceptTCP()
		if err != nil {
			acceptError <- err
			return
		}
		accepted <- connection
	}()
	dialed, err := net.DialTCP(
		"tcp",
		nil,
		listener.Addr().(*net.TCPAddr),
	)
	if err != nil {
		t.Fatalf("dial TCP pair: %v", err)
	}
	select {
	case connection := <-accepted:
		return dialed, connection
	case err := <-acceptError:
		_ = dialed.Close()
		t.Fatalf("accept TCP pair: %v", err)
	case <-time.After(time.Second):
		_ = dialed.Close()
		t.Fatal("accept TCP pair timed out")
	}
	return nil, nil
}
