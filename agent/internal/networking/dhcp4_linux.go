//go:build linux

package networking

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/insomniacslk/dhcp/dhcpv4"
	"github.com/insomniacslk/dhcp/dhcpv4/nclient4"
	"golang.org/x/sys/unix"
)

const (
	defaultDHCPLease = time.Hour
	dhcpRetryTimeout = 3 * time.Second
	dhcpRetries      = 2
)

type productionDynamicAcquirer struct {
	now func() time.Time
}

func newProductionDynamicAcquirer() dynamicAcquirer {
	return &productionDynamicAcquirer{now: time.Now}
}

func (acquirer *productionDynamicAcquirer) AcquireIPv4(
	ctx context.Context,
	request dynamicRequest,
) (dynamicLeaseSession, error) {
	client, err := nclient4.New(
		request.Interface,
		nclient4.WithRetry(dhcpRetries),
		nclient4.WithTimeout(dhcpRetryTimeout),
	)
	if err != nil {
		return nil, fmt.Errorf("open DHCPv4 client on %q: %w", request.Interface, err)
	}
	defer client.Close()
	lease, err := client.Request(ctx, requestedDHCPOptions)
	if err != nil {
		return nil, fmt.Errorf("acquire DHCPv4 lease on %q: %w", request.Interface, err)
	}
	parsed, err := parseDHCPv4Lease(lease, acquirer.now())
	if err != nil {
		return nil, err
	}
	return &dhcp4Session{
		interfaceName: request.Interface,
		hardware:      append(net.HardwareAddr(nil), request.Hardware...),
		now:           acquirer.now,
		wireLease:     lease,
		lease:         parsed,
	}, nil
}

func requestedDHCPOptions(message *dhcpv4.DHCPv4) {
	dhcpv4.WithRequestedOptions(
		dhcpv4.OptionSubnetMask,
		dhcpv4.OptionRouter,
		dhcpv4.OptionDomainNameServer,
		dhcpv4.OptionInterfaceMTU,
		dhcpv4.OptionClasslessStaticRoute,
		dhcpv4.OptionIPAddressLeaseTime,
		dhcpv4.OptionRenewTimeValue,
		dhcpv4.OptionRebindingTimeValue,
	)(message)
}

type dhcp4Session struct {
	mu            sync.Mutex
	interfaceName string
	hardware      net.HardwareAddr
	now           func() time.Time
	wireLease     *nclient4.Lease
	lease         dynamicLease
}

func (session *dhcp4Session) Lease() dynamicLease {
	session.mu.Lock()
	defer session.mu.Unlock()
	return session.lease
}

func (session *dhcp4Session) Refresh(ctx context.Context) (dynamicLease, error) {
	session.mu.Lock()
	defer session.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return dynamicLease{}, err
	}
	now := session.now()
	var (
		renewed *nclient4.Lease
		err     error
	)
	if !session.lease.RebindAt.IsZero() && !now.Before(session.lease.RebindAt) {
		renewed, err = session.rebind(ctx)
	} else {
		renewed, err = session.renew(ctx)
	}
	if err != nil {
		return dynamicLease{}, err
	}
	parsed, err := parseDHCPv4Lease(renewed, now)
	if err != nil {
		return dynamicLease{}, err
	}
	session.wireLease = renewed
	session.lease = parsed
	return parsed, nil
}

func (session *dhcp4Session) renew(ctx context.Context) (*nclient4.Lease, error) {
	server := net.ParseIP(session.lease.Server).To4()
	source := leaseIPv4(session.lease)
	if server == nil || source == nil {
		return nil, errors.New("DHCPv4 lease is missing its server or client address")
	}
	connection, err := boundDHCPPacketConn(
		ctx,
		session.interfaceName,
		source,
		false,
	)
	if err != nil {
		return nil, err
	}
	client, err := nclient4.NewWithConn(
		connection,
		session.hardware,
		nclient4.WithServerAddr(&net.UDPAddr{IP: server, Port: nclient4.ServerPort}),
		nclient4.WithRetry(dhcpRetries),
		nclient4.WithTimeout(dhcpRetryTimeout),
	)
	if err != nil {
		_ = connection.Close()
		return nil, err
	}
	defer client.Close()
	return client.Renew(ctx, session.wireLease, requestedDHCPOptions)
}

func (session *dhcp4Session) rebind(ctx context.Context) (*nclient4.Lease, error) {
	source := leaseIPv4(session.lease)
	if source == nil {
		return nil, errors.New("DHCPv4 lease is missing its client address")
	}
	connection, err := boundDHCPPacketConn(
		ctx,
		session.interfaceName,
		source,
		true,
	)
	if err != nil {
		return nil, err
	}
	client, err := nclient4.NewWithConn(
		connection,
		session.hardware,
		nclient4.WithServerAddr(nclient4.DefaultServers),
		nclient4.WithRetry(dhcpRetries),
		nclient4.WithTimeout(dhcpRetryTimeout),
	)
	if err != nil {
		_ = connection.Close()
		return nil, err
	}
	defer client.Close()
	request, err := dhcpv4.NewRenewFromAck(
		session.wireLease.ACK,
		requestedDHCPOptions,
		dhcpv4.WithBroadcast(true),
	)
	if err != nil {
		return nil, err
	}
	response, err := client.SendAndRead(
		ctx,
		nclient4.DefaultServers,
		request,
		nclient4.IsMessageType(dhcpv4.MessageTypeAck, dhcpv4.MessageTypeNak),
	)
	if err != nil {
		return nil, fmt.Errorf("rebind DHCPv4 lease: %w", err)
	}
	if response.MessageType() == dhcpv4.MessageTypeNak {
		return nil, &nclient4.ErrNak{
			Offer: session.wireLease.Offer,
			Nak:   response,
		}
	}
	return &nclient4.Lease{
		Offer:        session.wireLease.Offer,
		ACK:          response,
		CreationTime: session.now(),
	}, nil
}

func (session *dhcp4Session) Release(ctx context.Context) error {
	session.mu.Lock()
	defer session.mu.Unlock()
	if session.wireLease == nil {
		return nil
	}
	source := leaseIPv4(session.lease)
	server := net.ParseIP(session.lease.Server).To4()
	if source == nil || server == nil {
		return nil
	}
	connection, err := boundDHCPPacketConn(
		ctx,
		session.interfaceName,
		source,
		false,
	)
	if err != nil {
		return err
	}
	client, err := nclient4.NewWithConn(
		connection,
		session.hardware,
		nclient4.WithServerAddr(&net.UDPAddr{IP: server, Port: nclient4.ServerPort}),
	)
	if err != nil {
		_ = connection.Close()
		return err
	}
	defer client.Close()
	return client.Release(session.wireLease)
}

func (session *dhcp4Session) Close() error {
	return nil
}

func boundDHCPPacketConn(
	ctx context.Context,
	interfaceName string,
	source net.IP,
	broadcast bool,
) (net.PacketConn, error) {
	listenConfig := net.ListenConfig{
		Control: func(_ string, _ string, raw syscall.RawConn) error {
			var controlErr error
			if err := raw.Control(func(fd uintptr) {
				controlErr = unix.SetsockoptString(
					int(fd),
					unix.SOL_SOCKET,
					unix.SO_BINDTODEVICE,
					interfaceName,
				)
				if controlErr == nil && broadcast {
					controlErr = unix.SetsockoptInt(
						int(fd),
						unix.SOL_SOCKET,
						unix.SO_BROADCAST,
						1,
					)
				}
			}); err != nil {
				return err
			}
			return controlErr
		},
	}
	address := net.JoinHostPort(source.String(), strconv.Itoa(nclient4.ClientPort))
	connection, err := listenConfig.ListenPacket(ctx, "udp4", address)
	if err != nil {
		return nil, fmt.Errorf(
			"open DHCPv4 socket on %s (%s): %w",
			interfaceName,
			source,
			err,
		)
	}
	return connection, nil
}

func parseDHCPv4Lease(
	wireLease *nclient4.Lease,
	now time.Time,
) (dynamicLease, error) {
	if wireLease == nil || wireLease.ACK == nil {
		return dynamicLease{}, errors.New("DHCPv4 client returned an empty lease")
	}
	ack := wireLease.ACK
	address := ack.YourIPAddr.To4()
	if address == nil || address.IsUnspecified() {
		return dynamicLease{}, errors.New("DHCPv4 ACK did not include a client address")
	}
	mask := ack.SubnetMask()
	prefix, bits := mask.Size()
	if bits != 32 || prefix <= 0 {
		return dynamicLease{}, errors.New("DHCPv4 ACK did not include a valid subnet mask")
	}
	if now.IsZero() {
		now = time.Now()
	}
	leaseDuration := ack.IPAddressLeaseTime(defaultDHCPLease)
	if leaseDuration <= 0 {
		return dynamicLease{}, errors.New("DHCPv4 ACK reported an invalid lease duration")
	}
	t1 := ack.IPAddressRenewalTime(leaseDuration / 2)
	t2 := ack.IPAddressRebindingTime(leaseDuration * 7 / 8)
	if t1 <= 0 || t1 >= leaseDuration {
		t1 = leaseDuration / 2
	}
	if t2 <= t1 || t2 >= leaseDuration {
		t2 = leaseDuration * 7 / 8
	}

	server := ack.ServerIdentifier().To4()
	if server == nil {
		return dynamicLease{}, errors.New("DHCPv4 ACK did not identify its server")
	}
	source := address.String()
	_, subnet, _ := net.ParseCIDR(fmt.Sprintf("%s/%d", source, prefix))
	routes := []appliedRoute{{
		Family: unix.AF_INET,
		Dst:    subnet.String(),
		Source: source,
	}}
	classless := ack.ClasslessStaticRoute()
	router := ""
	if len(classless) > 0 {
		for _, route := range classless {
			if route == nil || route.Dest == nil || route.Router.To4() == nil {
				return dynamicLease{}, errors.New("DHCPv4 ACK contained an invalid classless route")
			}
			routes = append(routes, appliedRoute{
				Family:  unix.AF_INET,
				Dst:     route.Dest.String(),
				Gateway: route.Router.To4().String(),
				Source:  source,
			})
			if ones, _ := route.Dest.Mask.Size(); ones == 0 {
				router = route.Router.To4().String()
			}
		}
	} else {
		routers := ack.Router()
		if len(routers) == 0 || routers[0].To4() == nil {
			return dynamicLease{}, errors.New("DHCPv4 ACK did not include a default router")
		}
		router = routers[0].To4().String()
		routes = append(routes, appliedRoute{
			Family:  unix.AF_INET,
			Gateway: router,
			Source:  source,
		})
	}
	if router == "" {
		return dynamicLease{}, errors.New("DHCPv4 classless routes did not include a default route")
	}

	dns := make([]string, 0, len(ack.DNS()))
	for _, address := range ack.DNS() {
		if value := address.To4(); value != nil {
			dns = append(dns, value.String())
		}
	}
	mtu := 0
	if value, err := dhcpv4.GetUint16(dhcpv4.OptionInterfaceMTU, ack.Options); err == nil &&
		value >= 576 {
		mtu = int(value)
	}
	expiresAt := now.Add(leaseDuration)
	return dynamicLease{
		Kind:       dynamicLeaseDHCPv4,
		AcquiredAt: now,
		RefreshAt:  now.Add(t1),
		RebindAt:   now.Add(t2),
		ExpiresAt:  expiresAt,
		Server:     server.String(),
		Router:     router,
		DNS:        dns,
		MTU:        mtu,
		Addresses: []leasedAddress{{
			Prefix:     fmt.Sprintf("%s/%d", source, prefix),
			ValidUntil: expiresAt,
		}},
		Routes: routes,
	}, nil
}

func leaseIPv4(lease dynamicLease) net.IP {
	for _, address := range lease.Addresses {
		prefix, err := netip.ParsePrefix(address.Prefix)
		if err == nil && prefix.Addr().Is4() {
			return net.IP(prefix.Addr().AsSlice())
		}
	}
	return nil
}
