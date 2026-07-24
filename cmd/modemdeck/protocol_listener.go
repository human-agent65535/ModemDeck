package main

import (
	"bufio"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"net/url"
	"slices"
	"sync"
	"time"
)

const protocolClassificationTimeout = 5 * time.Second

type acceptResult struct {
	connection net.Conn
	err        error
}

type tlsAndPlainListener struct {
	net.Listener

	tlsConfig *tls.Config
	results   chan acceptResult
	closed    chan struct{}
	closeOnce sync.Once
}

type bufferedConn struct {
	net.Conn
	reader *bufio.Reader
}

func (connection *bufferedConn) Read(buffer []byte) (int, error) {
	return connection.reader.Read(buffer)
}

func prepareServerTLSConfig(source *tls.Config) *tls.Config {
	config := source.Clone()
	for _, protocol := range []string{"h2", "http/1.1"} {
		if !slices.Contains(config.NextProtos, protocol) {
			config.NextProtos = append(config.NextProtos, protocol)
		}
	}
	return config
}

func serveTLSAndPlainHTTP(server *http.Server) error {
	listener, err := net.Listen("tcp", server.Addr)
	if err != nil {
		return err
	}
	return server.Serve(newTLSAndPlainListener(listener, server.TLSConfig))
}

func newTLSAndPlainListener(
	listener net.Listener,
	tlsConfig *tls.Config,
) *tlsAndPlainListener {
	mixed := &tlsAndPlainListener{
		Listener:  listener,
		tlsConfig: tlsConfig,
		results:   make(chan acceptResult),
		closed:    make(chan struct{}),
	}
	go mixed.acceptConnections()
	return mixed
}

func (listener *tlsAndPlainListener) Accept() (net.Conn, error) {
	select {
	case result := <-listener.results:
		return result.connection, result.err
	case <-listener.closed:
		return nil, net.ErrClosed
	}
}

func (listener *tlsAndPlainListener) Close() error {
	var closeErr error
	listener.closeOnce.Do(func() {
		close(listener.closed)
		closeErr = listener.Listener.Close()
	})
	return closeErr
}

func (listener *tlsAndPlainListener) acceptConnections() {
	for {
		connection, err := listener.Listener.Accept()
		if err != nil {
			if !listener.deliver(acceptResult{err: err}) {
				return
			}
			if !isTemporaryNetworkError(err) {
				return
			}
			continue
		}
		go listener.classify(connection)
	}
}

func (listener *tlsAndPlainListener) classify(connection net.Conn) {
	if err := connection.SetReadDeadline(
		time.Now().Add(protocolClassificationTimeout),
	); err != nil {
		_ = connection.Close()
		return
	}
	reader := bufio.NewReader(connection)
	firstByte, err := reader.Peek(1)
	if err != nil {
		_ = connection.Close()
		return
	}
	if err := connection.SetReadDeadline(time.Time{}); err != nil {
		_ = connection.Close()
		return
	}

	buffered := &bufferedConn{Conn: connection, reader: reader}
	classified := net.Conn(buffered)
	if firstByte[0] == 0x16 {
		classified = tls.Server(buffered, listener.tlsConfig)
	}
	if !listener.deliver(acceptResult{connection: classified}) {
		_ = classified.Close()
	}
}

func (listener *tlsAndPlainListener) deliver(result acceptResult) bool {
	select {
	case listener.results <- result:
		return true
	case <-listener.closed:
		return false
	}
}

func isTemporaryNetworkError(err error) bool {
	var networkError net.Error
	return errors.As(err, &networkError) && networkError.Temporary()
}

func redirectPlainHTTPToHTTPS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.TLS != nil {
			next.ServeHTTP(response, request)
			return
		}
		if request.Host == "" {
			http.Error(response, "HTTPS is required", http.StatusBadRequest)
			return
		}

		target := &url.URL{
			Scheme:   "https",
			Host:     request.Host,
			Path:     request.URL.Path,
			RawPath:  request.URL.RawPath,
			RawQuery: request.URL.RawQuery,
		}
		http.Redirect(response, request, target.String(), http.StatusPermanentRedirect)
	})
}
