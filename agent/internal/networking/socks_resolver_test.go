package networking

import (
	"context"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"github.com/human-agent65535/modemdeck/agent/internal/domain"
)

func TestSOCKSResolverUsesInjectedBearerLookup(t *testing.T) {
	lookup := &fakeIPLookup{
		addresses: []net.IP{
			net.ParseIP("2001:db8::10"),
			net.ParseIP("203.0.113.10"),
		},
	}
	ctx := context.WithValue(context.Background(), resolverContextKey{}, "value")
	returnedContext, address, err := (socksNameResolver{
		lookup:    lookup,
		lifecycle: context.Background(),
	}).Resolve(
		ctx,
		"destination.test",
	)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if returnedContext.Value(resolverContextKey{}) != "value" ||
		address.String() != "2001:db8::10" ||
		lookup.network != "ip" ||
		lookup.host != "destination.test" {
		t.Fatalf(
			"resolve result context=%v address=%v lookup=%+v",
			returnedContext,
			address,
			lookup,
		)
	}
	candidates := socksResolvedAddresses(returnedContext, nil)
	if len(candidates) != 2 ||
		candidates[0].String() != "2001:db8::10" ||
		candidates[1].String() != "203.0.113.10" {
		t.Fatalf("resolver candidates = %v", candidates)
	}
}

func TestSOCKSResolverDoesNotFallbackWhenBearerLookupFails(t *testing.T) {
	expected := errors.New("bearer DNS unavailable")
	_, address, err := (socksNameResolver{
		lookup:    &fakeIPLookup{err: expected},
		lifecycle: context.Background(),
	}).Resolve(context.Background(), "localhost")
	if !errors.Is(err, expected) || address != nil {
		t.Fatalf("resolve error/address = %v/%v", err, address)
	}
}

func TestSOCKSServerResolvesDomainsThroughBearerLookup(t *testing.T) {
	lookup := &fakeIPLookup{
		addresses: []net.IP{
			net.ParseIP("2001:db8::10"),
			net.ParseIP("203.0.113.10"),
		},
	}
	var dialNetwork string
	var dialAddresses []string
	upstream, upstreamPeer := net.Pipe()
	defer upstreamPeer.Close()
	server := newSOCKSServer(
		domain.ProxyConfiguration{Mode: domain.ProxyModeSOCKS5},
		func(_ context.Context, network, address string) (net.Conn, error) {
			dialNetwork = network
			dialAddresses = append(dialAddresses, address)
			if len(dialAddresses) == 1 {
				return nil, errors.New("network is unreachable")
			}
			return tcpAddressConnection{Conn: upstream}, nil
		},
		lookup,
		context.Background(),
	)

	serverConnection, clientConnection := net.Pipe()
	_ = clientConnection.SetDeadline(time.Now().Add(2 * time.Second))
	serveResult := make(chan error, 1)
	go func() {
		serveResult <- server.ServeConn(serverConnection)
	}()

	if _, err := clientConnection.Write([]byte{0x05, 0x01, 0x00}); err != nil {
		t.Fatalf("write SOCKS greeting: %v", err)
	}
	greeting := make([]byte, 2)
	if _, err := io.ReadFull(clientConnection, greeting); err != nil {
		t.Fatalf("read SOCKS greeting: %v", err)
	}
	if greeting[0] != 0x05 || greeting[1] != 0x00 {
		t.Fatalf("SOCKS greeting response = %v", greeting)
	}

	host := []byte("destination.test")
	request := []byte{0x05, 0x01, 0x00, 0x03, byte(len(host))}
	request = append(request, host...)
	request = append(request, 0x00, 0x50)
	if _, err := clientConnection.Write(request); err != nil {
		t.Fatalf("write SOCKS request: %v", err)
	}
	response := make([]byte, 10)
	if _, err := io.ReadFull(clientConnection, response); err != nil {
		t.Fatalf("read SOCKS response: %v", err)
	}
	if response[1] != 0x00 {
		t.Fatalf("SOCKS response = %v", response)
	}
	if lookup.host != "destination.test" ||
		dialNetwork != "tcp" ||
		len(dialAddresses) != 2 ||
		dialAddresses[0] != "[2001:db8::10]:80" ||
		dialAddresses[1] != "203.0.113.10:80" {
		t.Fatalf(
			"lookup host/dial network/addresses = %q/%q/%q",
			lookup.host,
			dialNetwork,
			dialAddresses,
		)
	}

	_ = clientConnection.Close()
	_ = upstreamPeer.Close()
	select {
	case <-serveResult:
	case <-time.After(2 * time.Second):
		t.Fatal("SOCKS ServeConn did not stop")
	}
}

func TestSOCKSServerRejectsUDPAssociate(t *testing.T) {
	dialCalls := 0
	server := newSOCKSServer(
		domain.ProxyConfiguration{Mode: domain.ProxyModeSOCKS5},
		func(context.Context, string, string) (net.Conn, error) {
			dialCalls++
			return nil, errors.New("unexpected dial")
		},
		&fakeIPLookup{},
		context.Background(),
	)
	serverConnection, clientConnection := net.Pipe()
	_ = clientConnection.SetDeadline(time.Now().Add(2 * time.Second))
	serveResult := make(chan error, 1)
	go func() {
		serveResult <- server.ServeConn(serverConnection)
	}()

	if _, err := clientConnection.Write([]byte{0x05, 0x01, 0x00}); err != nil {
		t.Fatalf("write SOCKS greeting: %v", err)
	}
	greeting := make([]byte, 2)
	if _, err := io.ReadFull(clientConnection, greeting); err != nil {
		t.Fatalf("read SOCKS greeting: %v", err)
	}
	request := []byte{
		0x05, 0x03, 0x00, 0x01,
		127, 0, 0, 1,
		0x00, 0x35,
	}
	if _, err := clientConnection.Write(request); err != nil {
		t.Fatalf("write UDP ASSOCIATE request: %v", err)
	}
	response := make([]byte, 10)
	if _, err := io.ReadFull(clientConnection, response); err != nil {
		t.Fatalf("read UDP ASSOCIATE response: %v", err)
	}
	if response[1] != 0x02 || dialCalls != 0 {
		t.Fatalf("UDP ASSOCIATE response = %v, dial calls = %d", response, dialCalls)
	}
	_ = clientConnection.Close()
	select {
	case <-serveResult:
	case <-time.After(2 * time.Second):
		t.Fatal("SOCKS ServeConn did not stop after rejecting UDP ASSOCIATE")
	}
}

func TestSOCKSConnectPreservesHalfClose(t *testing.T) {
	socksClient, socksServer := tcpConnectionPair(t)
	defer socksClient.Close()
	defer socksServer.Close()
	upstreamProxy, upstreamApplication := tcpConnectionPair(t)
	defer upstreamProxy.Close()
	defer upstreamApplication.Close()

	server := newSOCKSServer(
		domain.ProxyConfiguration{Mode: domain.ProxyModeSOCKS5},
		func(context.Context, string, string) (net.Conn, error) {
			return upstreamProxy, nil
		},
		&fakeIPLookup{},
		context.Background(),
	)
	serveResult := make(chan error, 1)
	go func() {
		serveResult <- server.ServeConn(socksServer)
	}()

	_ = socksClient.SetDeadline(time.Now().Add(2 * time.Second))
	if _, err := socksClient.Write([]byte{0x05, 0x01, 0x00}); err != nil {
		t.Fatalf("write SOCKS greeting: %v", err)
	}
	greeting := make([]byte, 2)
	if _, err := io.ReadFull(socksClient, greeting); err != nil {
		t.Fatalf("read SOCKS greeting: %v", err)
	}
	request := []byte{
		0x05, 0x01, 0x00, 0x01,
		127, 0, 0, 1,
		0x00, 0x50,
	}
	if _, err := socksClient.Write(request); err != nil {
		t.Fatalf("write SOCKS request: %v", err)
	}
	response := make([]byte, 10)
	if _, err := io.ReadFull(socksClient, response); err != nil {
		t.Fatalf("read SOCKS response: %v", err)
	}
	if response[1] != 0x00 {
		t.Fatalf("SOCKS response = %v", response)
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

	if _, err := socksClient.Write([]byte("request-body")); err != nil {
		t.Fatalf("write SOCKS tunnel request: %v", err)
	}
	if err := socksClient.CloseWrite(); err != nil {
		t.Fatalf("half-close SOCKS tunnel request: %v", err)
	}
	tunnelResponse, err := io.ReadAll(socksClient)
	if err != nil {
		t.Fatalf("read SOCKS tunnel response: %v", err)
	}
	if string(tunnelResponse) != "response-after-eof" {
		t.Fatalf("SOCKS tunnel response = %q", tunnelResponse)
	}
	if content := <-requestBody; string(content) != "request-body" {
		t.Fatalf("SOCKS upstream request = %q", content)
	}
	if err := <-upstreamDone; err != nil {
		t.Fatalf("SOCKS upstream half-close flow: %v", err)
	}
	select {
	case err := <-serveResult:
		if err != nil {
			t.Fatalf("serve SOCKS connection: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("SOCKS ServeConn did not finish after both half-closes")
	}
}

func TestSOCKSResolverCancelsBearerLookupWithServerLifecycle(t *testing.T) {
	lifecycle, cancel := context.WithCancel(context.Background())
	lookup := blockingIPLookup{}
	result := make(chan error, 1)
	go func() {
		_, _, err := (socksNameResolver{
			lookup:    lookup,
			lifecycle: lifecycle,
		}).Resolve(context.Background(), "destination.test")
		result <- err
	}()

	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("resolver cancellation error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("resolver did not cancel with server lifecycle")
	}
}

type fakeIPLookup struct {
	addresses []net.IP
	err       error
	network   string
	host      string
}

type blockingIPLookup struct{}

func (blockingIPLookup) LookupIP(
	ctx context.Context,
	_ string,
	_ string,
) ([]net.IP, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

func (lookup *fakeIPLookup) LookupIP(
	_ context.Context,
	network string,
	host string,
) ([]net.IP, error) {
	lookup.network = network
	lookup.host = host
	return lookup.addresses, lookup.err
}

type resolverContextKey struct{}

type tcpAddressConnection struct {
	net.Conn
}

func (tcpAddressConnection) LocalAddr() net.Addr {
	return &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 30000}
}

func (tcpAddressConnection) RemoteAddr() net.Addr {
	return &net.TCPAddr{IP: net.ParseIP("203.0.113.10"), Port: 80}
}
