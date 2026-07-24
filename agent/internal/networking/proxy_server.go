package networking

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/human-agent65535/modemdeck/agent/internal/domain"
	socks5 "github.com/things-go/go-socks5"
	"github.com/things-go/go-socks5/statute"
)

const (
	proxyShutdownTimeout          = 2 * time.Second
	socksHandshakeTimeout         = 10 * time.Second
	maxConcurrentProxyConnections = 128
)

var globalProxyConnectionSlots = make(
	chan struct{},
	maxConcurrentProxyConnections,
)

type proxyRunner interface {
	Start() error
	Close() error
	Running() bool
	LastError() string
	Counters() ProxyCounters
}

type proxyRunnerFactory func(
	domain.ProxyConfiguration,
	bearer,
) (proxyRunner, error)

type proxyServer struct {
	configuration   domain.ProxyConfiguration
	selected        bearer
	dial            DialContextFunc
	counters        trafficCounters
	socks           *socks5.Server
	httpHandler     *httpProxyHandler
	httpServer      *http.Server
	socksContext    context.Context
	cancelSOCKS     context.CancelFunc
	connectionSlots chan struct{}
	handshakeTime   time.Duration
	socksWG         sync.WaitGroup

	mu          sync.Mutex
	listener    net.Listener
	clients     map[*trackedClientConnection]struct{}
	serveDone   chan struct{}
	started     bool
	closing     bool
	running     bool
	lastError   string
	closeResult error
	closeOnce   sync.Once
}

func newProxyServer(
	configuration domain.ProxyConfiguration,
	selected bearer,
) (proxyRunner, error) {
	if err := validateProxyProtocolConfiguration(configuration); err != nil {
		return nil, err
	}
	route, err := newBoundRoute(selected)
	if err != nil {
		return nil, fmt.Errorf("create bound outbound dialer: %w", err)
	}
	socksContext, cancelSOCKS := context.WithCancel(context.Background())
	server := &proxyServer{
		configuration:   configuration,
		selected:        selected,
		clients:         make(map[*trackedClientConnection]struct{}),
		serveDone:       make(chan struct{}),
		socksContext:    socksContext,
		cancelSOCKS:     cancelSOCKS,
		connectionSlots: globalProxyConnectionSlots,
		handshakeTime:   socksHandshakeTimeout,
	}
	server.dial = func(
		ctx context.Context,
		network string,
		address string,
	) (net.Conn, error) {
		connection, err := route.Dial(ctx, network, address)
		if err != nil {
			return nil, err
		}
		return server.counters.wrap(connection), nil
	}

	switch configuration.Mode {
	case domain.ProxyModeSOCKS5:
		server.socks = newSOCKSServer(
			configuration,
			server.dial,
			route.Resolver,
			server.socksContext,
		)
	case domain.ProxyModeHTTP:
		server.httpHandler = newHTTPProxyHandler(configuration, server.dial)
		server.httpServer = &http.Server{
			Handler:           server.httpHandler,
			ReadHeaderTimeout: 10 * time.Second,
			IdleTimeout:       60 * time.Second,
			MaxHeaderBytes:    32 << 10,
		}
	default:
		cancelSOCKS()
		return nil, fmt.Errorf("unsupported proxy mode %q", configuration.Mode)
	}
	return server, nil
}

func validateProxyProtocolConfiguration(
	configuration domain.ProxyConfiguration,
) error {
	if !configuration.AuthEnabled {
		return nil
	}
	switch configuration.Mode {
	case domain.ProxyModeSOCKS5:
		if len(configuration.Username) > 255 ||
			len(configuration.Password) > 255 {
			return fmt.Errorf(
				"SOCKS5 username and password must each be at most 255 bytes",
			)
		}
	case domain.ProxyModeHTTP:
		if strings.Contains(configuration.Username, ":") {
			return fmt.Errorf("HTTP Basic username must not contain a colon")
		}
	}
	return nil
}

func newSOCKSServer(
	configuration domain.ProxyConfiguration,
	dial DialContextFunc,
	lookup ipLookup,
	lifecycle context.Context,
) *socks5.Server {
	handler := socksConnectHandler{
		dial:      dial,
		lifecycle: lifecycle,
	}
	options := []socks5.Option{
		socks5.WithDial(dial),
		socks5.WithResolver(socksNameResolver{
			lookup:    lookup,
			lifecycle: lifecycle,
		}),
		socks5.WithRule(&socks5.PermitCommand{EnableConnect: true}),
		socks5.WithConnectHandle(handler.connect),
	}
	if configuration.AuthEnabled {
		options = append(options, socks5.WithAuthMethods([]socks5.Authenticator{
			socks5.UserPassAuthenticator{
				Credentials: socks5.StaticCredentials{
					configuration.Username: configuration.Password,
				},
			},
		}))
	}
	return socks5.NewServer(options...)
}

func (server *proxyServer) Start() error {
	server.mu.Lock()
	if server.started {
		server.mu.Unlock()
		return fmt.Errorf("proxy server has already been started")
	}
	server.started = true
	server.mu.Unlock()

	address := net.JoinHostPort(
		server.configuration.ListenAddress,
		strconv.Itoa(int(server.configuration.ListenPort)),
	)
	listener, err := net.Listen("tcp", address)
	if err != nil {
		server.mu.Lock()
		server.lastError = err.Error()
		server.mu.Unlock()
		close(server.serveDone)
		return fmt.Errorf("listen on %s: %w", address, err)
	}
	tracked := &trackedListener{Listener: listener, owner: server}

	server.mu.Lock()
	server.listener = tracked
	server.running = true
	server.mu.Unlock()

	go server.serve(tracked)
	return nil
}

func (server *proxyServer) serve(listener net.Listener) {
	var err error
	switch server.configuration.Mode {
	case domain.ProxyModeSOCKS5:
		err = server.serveSOCKS(listener)
	case domain.ProxyModeHTTP:
		err = server.httpServer.Serve(listener)
	}

	server.mu.Lock()
	server.running = false
	if !server.closing &&
		err != nil &&
		!errors.Is(err, net.ErrClosed) &&
		!errors.Is(err, http.ErrServerClosed) {
		server.lastError = err.Error()
	}
	server.mu.Unlock()
	close(server.serveDone)
}

func (server *proxyServer) serveSOCKS(listener net.Listener) error {
	defer server.socksWG.Wait()
	for {
		connection, err := listener.Accept()
		if err != nil {
			server.cancelSOCKS()
			return err
		}
		server.socksWG.Add(1)
		go server.serveSOCKSConnection(connection)
	}
}

func (server *proxyServer) serveSOCKSConnection(connection net.Conn) {
	defer server.socksWG.Done()

	if server.handshakeTime > 0 {
		if err := connection.SetDeadline(
			time.Now().Add(server.handshakeTime),
		); err != nil {
			_ = connection.Close()
			return
		}
	}
	stopCancellation := context.AfterFunc(server.socksContext, func() {
		_ = connection.Close()
	})
	defer stopCancellation()
	_ = server.socks.ServeConn(connection)
}

func (server *proxyServer) Close() error {
	server.closeOnce.Do(func() {
		server.closeResult = server.close()
	})
	return server.closeResult
}

func (server *proxyServer) close() error {
	server.mu.Lock()
	server.closing = true
	listener := server.listener
	started := server.started
	clients := make([]*trackedClientConnection, 0, len(server.clients))
	for connection := range server.clients {
		clients = append(clients, connection)
	}
	server.mu.Unlock()
	server.cancelSOCKS()

	var result error
	if server.httpHandler != nil {
		server.httpHandler.close()
	}
	if server.httpServer != nil {
		if err := server.httpServer.Close(); err != nil &&
			!errors.Is(err, http.ErrServerClosed) {
			result = errors.Join(result, err)
		}
	}
	if listener != nil {
		if err := listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			result = errors.Join(result, err)
		}
	}
	for _, connection := range clients {
		if err := connection.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			result = errors.Join(result, err)
		}
	}
	if started {
		timer := time.NewTimer(proxyShutdownTimeout)
		defer timer.Stop()
		select {
		case <-server.serveDone:
		case <-timer.C:
			result = errors.Join(result, fmt.Errorf("proxy server shutdown timed out"))
		}
	}
	return result
}

func (server *proxyServer) Running() bool {
	server.mu.Lock()
	defer server.mu.Unlock()
	return server.running && server.lastError == ""
}

func (server *proxyServer) LastError() string {
	server.mu.Lock()
	defer server.mu.Unlock()
	return server.lastError
}

func (server *proxyServer) Counters() ProxyCounters {
	return server.counters.snapshot()
}

func (server *proxyServer) register(connection *trackedClientConnection) bool {
	server.mu.Lock()
	defer server.mu.Unlock()
	if server.closing {
		return false
	}
	server.clients[connection] = struct{}{}
	return true
}

func (server *proxyServer) unregister(connection *trackedClientConnection) {
	server.mu.Lock()
	delete(server.clients, connection)
	server.mu.Unlock()
}

type trackedListener struct {
	net.Listener
	owner *proxyServer
}

func (listener *trackedListener) Accept() (net.Conn, error) {
	for {
		connection, err := listener.Listener.Accept()
		if err != nil {
			return nil, err
		}
		select {
		case listener.owner.connectionSlots <- struct{}{}:
		default:
			_ = connection.Close()
			continue
		}
		tracked := &trackedClientConnection{
			Conn:  connection,
			owner: listener.owner,
			slots: listener.owner.connectionSlots,
		}
		if !listener.owner.register(tracked) {
			_ = tracked.Close()
			return nil, net.ErrClosed
		}
		return tracked, nil
	}
}

type trackedClientConnection struct {
	net.Conn
	owner     *proxyServer
	slots     chan struct{}
	closeOnce sync.Once
	closeErr  error
}

func (connection *trackedClientConnection) Close() error {
	connection.closeOnce.Do(func() {
		connection.owner.unregister(connection)
		connection.closeErr = connection.Conn.Close()
		if connection.slots != nil {
			<-connection.slots
		}
	})
	return connection.closeErr
}

func (connection *trackedClientConnection) CloseWrite() error {
	return closeWrite(connection.Conn)
}

type socksConnectHandler struct {
	dial      DialContextFunc
	lifecycle context.Context
}

func (handler socksConnectHandler) connect(
	resolverContext context.Context,
	writer io.Writer,
	request *socks5.Request,
) error {
	client, ok := writer.(net.Conn)
	if !ok {
		return fmt.Errorf("SOCKS5 client connection is unavailable")
	}
	if err := client.SetDeadline(time.Time{}); err != nil {
		return fmt.Errorf("clear SOCKS5 handshake deadline: %w", err)
	}
	if request == nil || request.DestAddr == nil {
		_ = socks5.SendReply(writer, statute.RepServerFailure, nil)
		return fmt.Errorf("SOCKS5 destination is unavailable")
	}

	ctx := handler.lifecycle
	if ctx == nil {
		ctx = context.Background()
	}
	candidates := socksResolvedAddresses(
		resolverContext,
		request.DestAddr.IP,
	)
	if len(candidates) == 0 {
		_ = socks5.SendReply(writer, statute.RepHostUnreachable, nil)
		return fmt.Errorf("SOCKS5 destination has no resolved IP addresses")
	}

	var (
		upstream  net.Conn
		dialError error
	)
	for _, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			return err
		}
		address := net.JoinHostPort(
			candidate.String(),
			strconv.Itoa(request.DestAddr.Port),
		)
		connection, err := handler.dial(ctx, "tcp", address)
		if err == nil && connection != nil {
			upstream = connection
			break
		}
		if err == nil {
			err = fmt.Errorf("dial returned no connection")
		}
		dialError = errors.Join(dialError, err)
	}
	if upstream == nil {
		reply := statute.RepHostUnreachable
		message := dialError.Error()
		switch {
		case strings.Contains(message, "refused"):
			reply = statute.RepConnectionRefused
		case strings.Contains(message, "network is unreachable"):
			reply = statute.RepNetworkUnreachable
		}
		if err := socks5.SendReply(writer, reply, nil); err != nil {
			return errors.Join(dialError, err)
		}
		return dialError
	}
	defer upstream.Close()

	if err := socks5.SendReply(
		writer,
		statute.RepSuccess,
		upstream.LocalAddr(),
	); err != nil {
		return fmt.Errorf("send SOCKS5 success reply: %w", err)
	}
	return relayBidirectional(ctx, client, request.Reader, upstream)
}

func relayBidirectional(
	ctx context.Context,
	client net.Conn,
	clientReader io.Reader,
	upstream net.Conn,
) error {
	results := make(chan error, 2)
	copyStream := func(destination net.Conn, source io.Reader) {
		_, err := io.Copy(destination, source)
		if err == nil {
			err = closeWrite(destination)
		}
		results <- err
	}

	stopCancellation := context.AfterFunc(ctx, func() {
		_ = client.Close()
		_ = upstream.Close()
	})
	defer stopCancellation()

	go copyStream(upstream, clientReader)
	go copyStream(client, upstream)

	var result error
	for range 2 {
		err := <-results
		if err == nil {
			continue
		}
		_ = client.Close()
		_ = upstream.Close()
		if !errors.Is(err, io.EOF) &&
			!errors.Is(err, net.ErrClosed) {
			result = errors.Join(result, err)
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return result
}

func closeWrite(connection net.Conn) error {
	type closeWriter interface {
		CloseWrite() error
	}
	if writer, ok := connection.(closeWriter); ok {
		return writer.CloseWrite()
	}
	return nil
}

var _ proxyRunner = (*proxyServer)(nil)
