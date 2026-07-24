package networking

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"sync/atomic"
	"syscall"
	"time"
)

type DialContextFunc func(context.Context, string, string) (net.Conn, error)

type socketBinder func(uintptr, string) error

type socketDialer func(
	context.Context,
	*net.Dialer,
	string,
	string,
) (net.Conn, error)

type outboundRoute struct {
	Dial     DialContextFunc
	Resolver *net.Resolver
}

func newBoundRoute(selected bearer) (outboundRoute, error) {
	dialer, err := buildBoundDialer(
		selected,
		bindSocketToInterface,
		defaultSocketDialer,
	)
	if err != nil {
		return outboundRoute{}, err
	}
	return outboundRoute{
		Dial:     dialer.DialContext,
		Resolver: dialer.Resolver,
	}, nil
}

func buildBoundDialer(
	selected bearer,
	binder socketBinder,
	dialSocket socketDialer,
) (*net.Dialer, error) {
	if err := validateInterfaceName(selected.Interface); err != nil {
		return nil, err
	}
	if len(selected.DNS) == 0 {
		return nil, fmt.Errorf("bearer has no DNS servers")
	}
	for _, server := range selected.DNS {
		if net.ParseIP(server) == nil {
			return nil, fmt.Errorf("invalid bearer DNS server %q", server)
		}
	}
	if binder == nil {
		return nil, fmt.Errorf("socket binder is required")
	}
	if dialSocket == nil {
		dialSocket = defaultSocketDialer
	}

	control := boundSocketControl(selected.Interface, binder)
	var dnsCursor atomic.Uint64
	resolver := &net.Resolver{
		PreferGo:     true,
		StrictErrors: true,
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			index := dnsCursor.Add(1) - 1
			server := selected.DNS[index%uint64(len(selected.DNS))]
			dnsDialer := &net.Dialer{
				Timeout: 5 * time.Second,
				Control: control,
			}
			return dialSocket(
				ctx,
				dnsDialer,
				network,
				net.JoinHostPort(server, strconv.Itoa(53)),
			)
		},
	}
	return &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
		Resolver:  resolver,
		Control:   control,
	}, nil
}

func boundSocketControl(
	interfaceName string,
	binder socketBinder,
) func(string, string, syscall.RawConn) error {
	return func(_, _ string, rawConnection syscall.RawConn) error {
		var bindError error
		if err := rawConnection.Control(func(fileDescriptor uintptr) {
			bindError = binder(fileDescriptor, interfaceName)
		}); err != nil {
			return err
		}
		if bindError != nil {
			return fmt.Errorf(
				"bind socket to interface %s: %w",
				interfaceName,
				bindError,
			)
		}
		return nil
	}
}

func defaultSocketDialer(
	ctx context.Context,
	dialer *net.Dialer,
	network string,
	address string,
) (net.Conn, error) {
	return dialer.DialContext(ctx, network, address)
}
