package networking

import (
	"bufio"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/human-agent65535/modemdeck/agent/internal/domain"
)

func TestHTTPProxyForwardsRequestsAndRemovesHopHeaders(t *testing.T) {
	var upstreamConnectionHeader string
	var upstreamRemovedHeader string
	upstream := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter,
		request *http.Request,
	) {
		upstreamConnectionHeader = request.Header.Get("Connection")
		upstreamRemovedHeader = request.Header.Get("X-Remove")
		writer.Header().Set("Connection", "X-Upstream-Hop")
		writer.Header().Set("X-Upstream-Hop", "remove")
		writer.Header().Set("X-Result", "ok")
		_, _ = io.WriteString(writer, "forwarded")
	}))
	defer upstream.Close()
	upstreamURL, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatalf("parse upstream URL: %v", err)
	}

	configuration := testProxyConfiguration()
	configuration.Mode = domain.ProxyModeHTTP
	handler := newHTTPProxyHandler(
		configuration,
		func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, upstreamURL.Host)
		},
	)
	t.Cleanup(handler.close)

	request := httptest.NewRequest(
		http.MethodGet,
		"http://destination.invalid/resource",
		nil,
	)
	request.Header.Set("Connection", "X-Remove")
	request.Header.Set("X-Remove", "remove")
	request.Header.Set("Proxy-Connection", "keep-alive")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK ||
		recorder.Body.String() != "forwarded" ||
		recorder.Header().Get("X-Result") != "ok" {
		t.Fatalf("response = %d %q %+v", recorder.Code, recorder.Body.String(), recorder.Header())
	}
	if upstreamConnectionHeader != "" || upstreamRemovedHeader != "" {
		t.Fatalf(
			"hop headers reached upstream: Connection=%q X-Remove=%q",
			upstreamConnectionHeader,
			upstreamRemovedHeader,
		)
	}
	if recorder.Header().Get("Connection") != "" ||
		recorder.Header().Get("X-Upstream-Hop") != "" {
		t.Fatalf("hop headers reached client: %+v", recorder.Header())
	}
}

func TestHTTPProxyRequiresConfiguredAuthentication(t *testing.T) {
	configuration := testProxyConfiguration()
	configuration.Mode = domain.ProxyModeHTTP
	configuration.AuthEnabled = true
	configuration.Username = "proxy-user"
	configuration.Password = "proxy-password"
	dialCalls := 0
	handler := newHTTPProxyHandler(
		configuration,
		func(context.Context, string, string) (net.Conn, error) {
			dialCalls++
			return nil, errors.New("expected test dial failure")
		},
	)
	t.Cleanup(handler.close)

	for _, credentials := range []string{"", "wrong:credentials"} {
		request := httptest.NewRequest(
			http.MethodGet,
			"http://destination.invalid/",
			nil,
		)
		if credentials != "" {
			request.Header.Set(
				"Proxy-Authorization",
				"Basic "+base64.StdEncoding.EncodeToString([]byte(credentials)),
			)
		}
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusProxyAuthRequired ||
			recorder.Header().Get("Proxy-Authenticate") == "" {
			t.Fatalf("credentials %q response = %d %+v", credentials, recorder.Code, recorder.Header())
		}
	}
	if dialCalls != 0 {
		t.Fatalf("unauthorized requests made %d outbound calls", dialCalls)
	}

	authorized := httptest.NewRequest(
		http.MethodGet,
		"http://destination.invalid/",
		nil,
	)
	authorized.Header.Set(
		"Proxy-Authorization",
		"Basic "+base64.StdEncoding.EncodeToString(
			[]byte("proxy-user:proxy-password"),
		),
	)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, authorized)
	if recorder.Code != http.StatusBadGateway || dialCalls != 1 {
		t.Fatalf("authorized response = %d, dial calls = %d", recorder.Code, dialCalls)
	}
}

func TestHTTPProxyConnectCreatesBidirectionalTunnel(t *testing.T) {
	upstream, upstreamPeer := net.Pipe()
	defer upstreamPeer.Close()
	handler := newHTTPProxyHandler(
		func() domain.ProxyConfiguration {
			configuration := testProxyConfiguration()
			configuration.Mode = domain.ProxyModeHTTP
			return configuration
		}(),
		func(context.Context, string, string) (net.Conn, error) {
			return upstream, nil
		},
	)
	t.Cleanup(handler.close)

	writer := newHijackWriter()
	defer writer.client.Close()
	request := httptest.NewRequest(http.MethodConnect, "http://example.test", nil)
	request.Host = "example.test:443"
	served := make(chan struct{})
	go func() {
		handler.ServeHTTP(writer, request)
		close(served)
	}()

	_ = writer.client.SetDeadline(time.Now().Add(2 * time.Second))
	reader := bufio.NewReader(writer.client)
	status, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("read CONNECT response: %v", err)
	}
	if !strings.Contains(status, "200 Connection Established") {
		t.Fatalf("CONNECT status = %q", status)
	}
	if _, err := reader.ReadString('\n'); err != nil {
		t.Fatalf("read CONNECT response terminator: %v", err)
	}
	<-served

	upstreamReceived := make(chan string, 1)
	go func() {
		content := make([]byte, 4)
		_, _ = io.ReadFull(upstreamPeer, content)
		upstreamReceived <- string(content)
		_, _ = upstreamPeer.Write([]byte("pong"))
	}()
	if _, err := writer.client.Write([]byte("ping")); err != nil {
		t.Fatalf("write tunnel request: %v", err)
	}
	if got := <-upstreamReceived; got != "ping" {
		t.Fatalf("upstream received %q", got)
	}
	response := make([]byte, 4)
	if _, err := io.ReadFull(reader, response); err != nil {
		t.Fatalf("read tunnel response: %v", err)
	}
	if string(response) != "pong" {
		t.Fatalf("tunnel response = %q", response)
	}
}

func TestHTTPProxyConnectPreservesHalfClose(t *testing.T) {
	proxyClient, proxyServer := tcpConnectionPair(t)
	defer proxyClient.Close()
	defer proxyServer.Close()
	upstreamProxy, upstreamApplication := tcpConnectionPair(t)
	defer upstreamProxy.Close()
	defer upstreamApplication.Close()

	handler := newHTTPProxyHandler(
		func() domain.ProxyConfiguration {
			configuration := testProxyConfiguration()
			configuration.Mode = domain.ProxyModeHTTP
			return configuration
		}(),
		func(context.Context, string, string) (net.Conn, error) {
			return upstreamProxy, nil
		},
	)
	t.Cleanup(handler.close)
	writer := &hijackWriter{
		header: make(http.Header),
		server: proxyServer,
		client: proxyClient,
	}
	request := httptest.NewRequest(http.MethodConnect, "http://example.test", nil)
	request.Host = "example.test:443"
	handler.ServeHTTP(writer, request)

	_ = proxyClient.SetDeadline(time.Now().Add(2 * time.Second))
	reader := bufio.NewReader(proxyClient)
	status, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("read CONNECT response: %v", err)
	}
	if !strings.Contains(status, "200 Connection Established") {
		t.Fatalf("CONNECT status = %q", status)
	}
	if _, err := reader.ReadString('\n'); err != nil {
		t.Fatalf("read CONNECT response terminator: %v", err)
	}

	requestBody := make(chan []byte, 1)
	upstreamDone := make(chan error, 1)
	go func() {
		content, err := io.ReadAll(upstreamApplication)
		if err == nil {
			_, err = upstreamApplication.Write([]byte("response-after-eof"))
		}
		if closeErr := upstreamApplication.CloseWrite(); err == nil {
			err = closeErr
		}
		requestBody <- content
		upstreamDone <- err
	}()

	if _, err := proxyClient.Write([]byte("request-body")); err != nil {
		t.Fatalf("write tunnel request: %v", err)
	}
	if err := proxyClient.CloseWrite(); err != nil {
		t.Fatalf("half-close tunnel request: %v", err)
	}
	response, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read tunnel response after half-close: %v", err)
	}
	if string(response) != "response-after-eof" {
		t.Fatalf("tunnel response = %q", response)
	}
	if content := <-requestBody; string(content) != "request-body" {
		t.Fatalf("upstream request = %q", content)
	}
	if err := <-upstreamDone; err != nil {
		t.Fatalf("upstream half-close flow: %v", err)
	}
}

type hijackWriter struct {
	header http.Header
	server net.Conn
	client net.Conn
}

func newHijackWriter() *hijackWriter {
	server, client := net.Pipe()
	return &hijackWriter{
		header: make(http.Header),
		server: server,
		client: client,
	}
}

func (writer *hijackWriter) Header() http.Header {
	return writer.header
}

func (*hijackWriter) Write(content []byte) (int, error) {
	return len(content), nil
}

func (*hijackWriter) WriteHeader(int) {}

func (writer *hijackWriter) Hijack() (
	net.Conn,
	*bufio.ReadWriter,
	error,
) {
	return writer.server, bufio.NewReadWriter(
		bufio.NewReader(writer.server),
		bufio.NewWriter(writer.server),
	), nil
}

var _ http.Hijacker = (*hijackWriter)(nil)
