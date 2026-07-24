package networking

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/textproto"
	"strings"
	"time"

	"github.com/human-agent65535/modemdeck/agent/internal/domain"
)

type httpProxyHandler struct {
	dial          DialContextFunc
	transport     *http.Transport
	authEnabled   bool
	username      string
	password      string
	authChallenge string
}

func newHTTPProxyHandler(
	configuration domain.ProxyConfiguration,
	dial DialContextFunc,
) *httpProxyHandler {
	handler := &httpProxyHandler{
		dial:          dial,
		authEnabled:   configuration.AuthEnabled,
		username:      configuration.Username,
		password:      configuration.Password,
		authChallenge: `Basic realm="ModemDeck proxy"`,
	}
	handler.transport = &http.Transport{
		Proxy:                 nil,
		DialContext:           handler.dial,
		ForceAttemptHTTP2:     false,
		MaxIdleConns:          32,
		MaxIdleConnsPerHost:   8,
		IdleConnTimeout:       60 * time.Second,
		TLSHandshakeTimeout:   15 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
	}
	return handler
}

func (handler *httpProxyHandler) ServeHTTP(
	writer http.ResponseWriter,
	request *http.Request,
) {
	if handler.authEnabled && !handler.authorized(request) {
		writer.Header().Set("Proxy-Authenticate", handler.authChallenge)
		http.Error(
			writer,
			http.StatusText(http.StatusProxyAuthRequired),
			http.StatusProxyAuthRequired,
		)
		return
	}
	if strings.EqualFold(request.Method, http.MethodConnect) {
		handler.connect(writer, request)
		return
	}
	handler.forward(writer, request)
}

func (handler *httpProxyHandler) forward(
	writer http.ResponseWriter,
	request *http.Request,
) {
	outbound := request.Clone(request.Context())
	outbound.RequestURI = ""
	if outbound.URL == nil {
		http.Error(writer, "request URL is required", http.StatusBadRequest)
		return
	}
	if outbound.URL.Scheme == "" {
		outbound.URL.Scheme = "http"
	}
	if outbound.URL.Host == "" {
		outbound.URL.Host = outbound.Host
	}
	removeHopHeaders(outbound.Header)
	outbound.Header.Del("Proxy-Authorization")

	response, err := handler.transport.RoundTrip(outbound)
	if err != nil {
		http.Error(writer, "proxy upstream unavailable", http.StatusBadGateway)
		return
	}
	defer response.Body.Close()
	removeHopHeaders(response.Header)
	copyHeaders(writer.Header(), response.Header)
	writer.WriteHeader(response.StatusCode)
	_, _ = io.Copy(writer, response.Body)
}

func (handler *httpProxyHandler) connect(
	writer http.ResponseWriter,
	request *http.Request,
) {
	target, err := normalizeConnectTarget(request.Host)
	if err != nil {
		http.Error(writer, err.Error(), http.StatusBadRequest)
		return
	}
	upstream, err := handler.dial(request.Context(), "tcp", target)
	if err != nil {
		http.Error(writer, "proxy upstream unavailable", http.StatusBadGateway)
		return
	}

	hijacker, ok := writer.(http.Hijacker)
	if !ok {
		_ = upstream.Close()
		http.Error(writer, "connection hijacking is unavailable", http.StatusInternalServerError)
		return
	}
	client, buffered, err := hijacker.Hijack()
	if err != nil {
		_ = upstream.Close()
		return
	}
	if _, err := io.WriteString(
		client,
		"HTTP/1.1 200 Connection Established\r\n\r\n",
	); err != nil {
		_ = client.Close()
		_ = upstream.Close()
		return
	}

	go func() {
		defer client.Close()
		defer upstream.Close()
		_ = relayBidirectional(
			context.Background(),
			client,
			buffered,
			upstream,
		)
	}()
}

func (handler *httpProxyHandler) authorized(request *http.Request) bool {
	value := strings.TrimSpace(request.Header.Get("Proxy-Authorization"))
	scheme, encoded, found := strings.Cut(value, " ")
	if !found || !strings.EqualFold(scheme, "Basic") {
		return false
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
	if err != nil {
		return false
	}
	username, password, found := strings.Cut(string(decoded), ":")
	if !found {
		return false
	}
	usernameMatch := subtle.ConstantTimeCompare(
		[]byte(username),
		[]byte(handler.username),
	)
	passwordMatch := subtle.ConstantTimeCompare(
		[]byte(password),
		[]byte(handler.password),
	)
	return usernameMatch&passwordMatch == 1
}

func (handler *httpProxyHandler) close() {
	handler.transport.CloseIdleConnections()
}

func normalizeConnectTarget(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("CONNECT target is required")
	}
	if _, _, err := net.SplitHostPort(value); err == nil {
		return value, nil
	}
	if strings.Contains(value, ":") && net.ParseIP(value) == nil {
		return "", fmt.Errorf("CONNECT target must be host:port")
	}
	return net.JoinHostPort(value, "443"), nil
}

func removeHopHeaders(header http.Header) {
	if header == nil {
		return
	}
	connection := header.Get("Connection")
	for _, name := range hopHeaders {
		header.Del(name)
	}
	for _, name := range strings.Split(connection, ",") {
		if name = textproto.TrimString(name); name != "" {
			header.Del(name)
		}
	}
}

func copyHeaders(destination http.Header, source http.Header) {
	for key, values := range source {
		for _, value := range values {
			destination.Add(key, value)
		}
	}
}

var hopHeaders = []string{
	"Connection",
	"Proxy-Connection",
	"Keep-Alive",
	"Proxy-Authenticate",
	"Proxy-Authorization",
	"Te",
	"Trailer",
	"Transfer-Encoding",
	"Upgrade",
}

var _ http.Handler = (*httpProxyHandler)(nil)
