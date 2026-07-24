package networking

import (
	"context"
	"fmt"
	"net"
)

type ipLookup interface {
	LookupIP(context.Context, string, string) ([]net.IP, error)
}

type socksNameResolver struct {
	lookup    ipLookup
	lifecycle context.Context
}

func (resolver socksNameResolver) Resolve(
	ctx context.Context,
	name string,
) (context.Context, net.IP, error) {
	if resolver.lookup == nil {
		return ctx, nil, fmt.Errorf("bearer DNS resolver is unavailable")
	}
	lookupContext := ctx
	cancel := func() {}
	stopCancellation := func() bool { return false }
	if resolver.lifecycle != nil {
		lookupContext, cancel = context.WithCancel(ctx)
		stopCancellation = context.AfterFunc(resolver.lifecycle, cancel)
	}
	defer cancel()
	defer stopCancellation()

	addresses, err := resolver.lookup.LookupIP(lookupContext, "ip", name)
	if err != nil {
		return ctx, nil, err
	}
	addresses = canonicalIPCandidates(addresses)
	if len(addresses) == 0 {
		return ctx, nil, fmt.Errorf("bearer DNS returned no addresses for %q", name)
	}
	return context.WithValue(
		ctx,
		socksResolvedAddressesKey{},
		addresses,
	), addresses[0], nil
}

type socksResolvedAddressesKey struct{}

func canonicalIPCandidates(addresses []net.IP) []net.IP {
	seen := make(map[string]struct{}, len(addresses))
	result := make([]net.IP, 0, len(addresses))
	for _, address := range addresses {
		if address == nil {
			continue
		}
		canonical := net.ParseIP(address.String())
		if canonical == nil {
			continue
		}
		key := canonical.String()
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, canonical)
	}
	return result
}

func socksResolvedAddresses(ctx context.Context, fallback net.IP) []net.IP {
	if addresses, ok := ctx.Value(socksResolvedAddressesKey{}).([]net.IP); ok {
		return canonicalIPCandidates(addresses)
	}
	return canonicalIPCandidates([]net.IP{fallback})
}
