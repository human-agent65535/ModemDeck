package networking

import (
	"context"
	"net"
	"syscall"
	"testing"
)

func TestBoundSocketControlUsesResolvedInterface(t *testing.T) {
	var gotFD uintptr
	var gotInterface string
	control := boundSocketControl(
		"wwan0",
		func(fileDescriptor uintptr, interfaceName string) error {
			gotFD = fileDescriptor
			gotInterface = interfaceName
			return nil
		},
	)
	if err := control("tcp", "example.test:443", fakeRawConnection{fd: 42}); err != nil {
		t.Fatalf("control: %v", err)
	}
	if gotFD != 42 || gotInterface != "wwan0" {
		t.Fatalf("bound fd/interface = %d/%q", gotFD, gotInterface)
	}
}

func TestBearerResolverBindsDNSAndNeverUsesHostResolverAddress(t *testing.T) {
	var addresses []string
	var boundInterfaces []string
	var peers []net.Conn
	dialer, err := buildBoundDialer(
		bearer{
			Interface: "wwan0",
			DNS:       []string{"8.8.8.8", "2001:4860:4860::8888"},
		},
		func(_ uintptr, interfaceName string) error {
			boundInterfaces = append(boundInterfaces, interfaceName)
			return nil
		},
		func(
			_ context.Context,
			socket *net.Dialer,
			network string,
			address string,
		) (net.Conn, error) {
			addresses = append(addresses, network+" "+address)
			if err := socket.Control(
				network,
				address,
				fakeRawConnection{fd: uintptr(len(addresses))},
			); err != nil {
				return nil, err
			}
			local, peer := net.Pipe()
			peers = append(peers, peer)
			return local, nil
		},
	)
	if err != nil {
		t.Fatalf("build bound dialer: %v", err)
	}
	for range 2 {
		connection, err := dialer.Resolver.Dial(
			context.Background(),
			"udp",
			"host-resolver.invalid:53",
		)
		if err != nil {
			t.Fatalf("dial bearer DNS: %v", err)
		}
		_ = connection.Close()
	}
	for _, peer := range peers {
		_ = peer.Close()
	}

	if len(addresses) != 2 ||
		addresses[0] != "udp 8.8.8.8:53" ||
		addresses[1] != "udp [2001:4860:4860::8888]:53" {
		t.Fatalf("DNS addresses = %v", addresses)
	}
	if len(boundInterfaces) != 2 ||
		boundInterfaces[0] != "wwan0" ||
		boundInterfaces[1] != "wwan0" {
		t.Fatalf("DNS bindings = %v", boundInterfaces)
	}
}

type fakeRawConnection struct {
	fd uintptr
}

func (connection fakeRawConnection) Control(run func(uintptr)) error {
	run(connection.fd)
	return nil
}

func (fakeRawConnection) Read(func(uintptr) bool) error {
	return nil
}

func (fakeRawConnection) Write(func(uintptr) bool) error {
	return nil
}

var _ syscall.RawConn = fakeRawConnection{}
