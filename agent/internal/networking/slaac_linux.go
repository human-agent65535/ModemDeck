//go:build linux

package networking

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"sync"
	"time"

	"github.com/mdlayher/ndp"
	"golang.org/x/net/ipv6"
	"golang.org/x/sys/unix"
)

const (
	routerSolicitationAttempts = 3
	routerSolicitationWait     = 4 * time.Second
)

func (acquirer *productionDynamicAcquirer) AcquireIPv6(
	ctx context.Context,
	request dynamicRequest,
) (dynamicLeaseSession, error) {
	if !request.LinkLocal.IsValid() || !request.LinkLocal.Is6() ||
		!request.LinkLocal.IsLinkLocalUnicast() {
		return nil, errors.New("host SLAAC requires a link-local IPv6 source address")
	}
	session := &slaacSession{
		request: request,
		now:     acquirer.now,
	}
	lease, err := session.solicit(ctx)
	if err != nil {
		return nil, err
	}
	session.lease = lease
	return session, nil
}

type slaacSession struct {
	mu      sync.Mutex
	request dynamicRequest
	now     func() time.Time
	lease   dynamicLease
}

func (session *slaacSession) Lease() dynamicLease {
	session.mu.Lock()
	defer session.mu.Unlock()
	return session.lease
}

func (session *slaacSession) Refresh(ctx context.Context) (dynamicLease, error) {
	session.mu.Lock()
	defer session.mu.Unlock()
	lease, err := session.solicit(ctx)
	if err != nil {
		return dynamicLease{}, err
	}
	session.lease = lease
	return lease, nil
}

func (session *slaacSession) Release(context.Context) error {
	return nil
}

func (session *slaacSession) Close() error {
	return nil
}

func (session *slaacSession) solicit(ctx context.Context) (dynamicLease, error) {
	iface, err := net.InterfaceByName(session.request.Interface)
	if err != nil {
		return dynamicLease{}, fmt.Errorf(
			"find SLAAC interface %q: %w",
			session.request.Interface,
			err,
		)
	}
	connection, _, err := ndp.Listen(
		iface,
		ndp.Addr(session.request.LinkLocal.String()),
	)
	if err != nil {
		return dynamicLease{}, fmt.Errorf(
			"open SLAAC socket on %q using %s: %w",
			session.request.Interface,
			session.request.LinkLocal,
			err,
		)
	}
	defer connection.Close()
	if err := connection.SetControlMessage(ipv6.FlagHopLimit, true); err != nil {
		return dynamicLease{}, fmt.Errorf("enable SLAAC hop-limit validation: %w", err)
	}
	cancelDeadline := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = connection.SetDeadline(time.Now())
		case <-cancelDeadline:
		}
	}()
	defer close(cancelDeadline)

	options := []ndp.Option{}
	if len(session.request.Hardware) > 0 {
		options = append(options, &ndp.LinkLayerAddress{
			Direction: ndp.Source,
			Addr:      append(net.HardwareAddr(nil), session.request.Hardware...),
		})
	}
	solicitation := &ndp.RouterSolicitation{Options: options}
	destination := netip.MustParseAddr("ff02::2").
		WithZone(session.request.Interface)
	for attempt := 0; attempt < routerSolicitationAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return dynamicLease{}, err
		}
		deadline := session.now().Add(routerSolicitationWait)
		if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
			deadline = contextDeadline
		}
		if err := connection.SetDeadline(deadline); err != nil {
			return dynamicLease{}, err
		}
		if err := connection.WriteTo(solicitation, nil, destination); err != nil {
			return dynamicLease{}, fmt.Errorf("send IPv6 router solicitation: %w", err)
		}
		for {
			message, control, source, err := connection.ReadFrom()
			if err != nil {
				if ctx.Err() != nil {
					return dynamicLease{}, ctx.Err()
				}
				if networkError, ok := err.(net.Error); ok && networkError.Timeout() {
					break
				}
				return dynamicLease{}, fmt.Errorf("receive IPv6 router advertisement: %w", err)
			}
			advertisement, ok := message.(*ndp.RouterAdvertisement)
			if !ok {
				continue
			}
			if control == nil || control.HopLimit != ndp.HopLimit ||
				!source.Is6() || !source.IsLinkLocalUnicast() {
				continue
			}
			return parseRouterAdvertisement(
				advertisement,
				source.WithZone(""),
				session.request,
				session.now(),
			)
		}
	}
	return dynamicLease{}, fmt.Errorf(
		"host SLAAC timed out on %q after %d router solicitations",
		session.request.Interface,
		routerSolicitationAttempts,
	)
}

func parseRouterAdvertisement(
	advertisement *ndp.RouterAdvertisement,
	router netip.Addr,
	request dynamicRequest,
	now time.Time,
) (dynamicLease, error) {
	if advertisement == nil {
		return dynamicLease{}, errors.New("empty IPv6 router advertisement")
	}
	if !router.Is6() || !router.IsLinkLocalUnicast() {
		return dynamicLease{}, fmt.Errorf("invalid IPv6 router address %s", router)
	}
	if advertisement.RouterLifetime <= 0 {
		return dynamicLease{}, errors.New("IPv6 router advertisement has no usable default-router lifetime")
	}
	if now.IsZero() {
		now = time.Now()
	}
	iid := request.LinkLocal.As16()
	addresses := []leasedAddress{}
	routes := []appliedRoute{}
	dns := []string{}
	mtu := 0
	refreshAfter := advertisement.RouterLifetime / 2
	expiresAfter := advertisement.RouterLifetime
	source := ""
	for _, option := range advertisement.Options {
		switch value := option.(type) {
		case *ndp.PrefixInformation:
			if !value.AutonomousAddressConfiguration ||
				value.ValidLifetime <= 0 ||
				value.PreferredLifetime <= 0 ||
				value.PreferredLifetime > value.ValidLifetime {
				continue
			}
			if value.PrefixLength != 64 {
				continue
			}
			prefixBytes := value.Prefix.As16()
			copy(prefixBytes[8:], iid[8:])
			address := netip.AddrFrom16(prefixBytes)
			prefix := netip.PrefixFrom(address, int(value.PrefixLength))
			if source == "" {
				source = address.String()
			}
			addresses = append(addresses, leasedAddress{
				Prefix:         prefix.String(),
				PreferredUntil: now.Add(value.PreferredLifetime),
				ValidUntil:     now.Add(value.ValidLifetime),
				NoDAD:          true,
			})
			if value.OnLink {
				routes = append(routes, appliedRoute{
					Family: unix.AF_INET6,
					Dst: netip.PrefixFrom(
						value.Prefix,
						int(value.PrefixLength),
					).Masked().String(),
					Source: address.String(),
				})
			}
			refreshAfter = shortestPositive(
				refreshAfter,
				value.PreferredLifetime/2,
			)
			expiresAfter = shortestPositive(
				expiresAfter,
				value.ValidLifetime,
			)
		case *ndp.MTU:
			if value.MTU >= 1280 {
				mtu = int(value.MTU)
			}
		case *ndp.RouteInformation:
			if value.RouteLifetime <= 0 || value.PrefixLength > 128 {
				continue
			}
			routes = append(routes, appliedRoute{
				Family: unix.AF_INET6,
				Dst: netip.PrefixFrom(
					value.Prefix,
					int(value.PrefixLength),
				).Masked().String(),
				Gateway: router.String(),
			})
		case *ndp.RecursiveDNSServer:
			for _, address := range value.Servers {
				dns = append(dns, address.String())
			}
		}
	}
	if len(addresses) == 0 {
		return dynamicLease{}, errors.New(
			"IPv6 router advertisement did not contain an autonomous /64 prefix",
		)
	}
	if source == "" {
		return dynamicLease{}, errors.New("host SLAAC did not form a global IPv6 address")
	}
	for index := range routes {
		if routes[index].Source == "" {
			routes[index].Source = source
		}
	}
	routes = append(routes, appliedRoute{
		Family:  unix.AF_INET6,
		Gateway: router.String(),
		Source:  source,
	})
	if request.RequestedMTU >= 1280 &&
		(mtu == 0 || int(request.RequestedMTU) < mtu) {
		mtu = int(request.RequestedMTU)
	}
	if refreshAfter <= 0 {
		refreshAfter = time.Minute
	}
	if refreshAfter > 30*time.Minute {
		refreshAfter = 30 * time.Minute
	}
	return dynamicLease{
		Kind:       dynamicLeaseSLAAC,
		AcquiredAt: now,
		RefreshAt:  now.Add(refreshAfter),
		ExpiresAt:  now.Add(expiresAfter),
		Router:     router.String(),
		LinkLocal:  request.LinkLocal.String(),
		DNS:        dns,
		MTU:        mtu,
		Addresses:  addresses,
		Routes:     routes,
	}, nil
}

func shortestPositive(left time.Duration, right time.Duration) time.Duration {
	switch {
	case left <= 0:
		return right
	case right <= 0:
		return left
	case right < left:
		return right
	default:
		return left
	}
}
