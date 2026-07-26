//go:build linux

package networking

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"slices"
	"time"

	"github.com/human-agent65535/modemdeck/agent/internal/domain"
)

const (
	dynamicLeaseDHCPv4 = "dhcp4"
	dynamicLeaseSLAAC  = "slaac6"
)

var errDynamicNotSupported = errors.New("dynamic bearer method is not supported")

type dynamicRequest struct {
	Interface    string
	LinkIndex    int
	Hardware     net.HardwareAddr
	LinkLocal    netip.Addr
	RequestedMTU uint32
}

type leasedAddress struct {
	Prefix         string    `json:"prefix"`
	PreferredUntil time.Time `json:"preferred_until,omitempty"`
	ValidUntil     time.Time `json:"valid_until"`
	NoDAD          bool      `json:"no_dad,omitempty"`
}

type dynamicLease struct {
	Kind       string          `json:"kind"`
	AcquiredAt time.Time       `json:"acquired_at"`
	RefreshAt  time.Time       `json:"refresh_at"`
	RebindAt   time.Time       `json:"rebind_at,omitempty"`
	ExpiresAt  time.Time       `json:"expires_at"`
	Server     string          `json:"server,omitempty"`
	Router     string          `json:"router,omitempty"`
	LinkLocal  string          `json:"link_local,omitempty"`
	DNS        []string        `json:"dns,omitempty"`
	MTU        int             `json:"mtu,omitempty"`
	Addresses  []leasedAddress `json:"addresses"`
	Routes     []appliedRoute  `json:"routes"`
}

type dynamicLeaseSession interface {
	Lease() dynamicLease
	Refresh(context.Context) (dynamicLease, error)
	Release(context.Context) error
	Close() error
}

type dynamicAcquirer interface {
	AcquireIPv4(context.Context, dynamicRequest) (dynamicLeaseSession, error)
	AcquireIPv6(context.Context, dynamicRequest) (dynamicLeaseSession, error)
}

type lineDynamicRuntime struct {
	connection domain.DataConnection
	sessions   []dynamicLeaseSession
	cancel     context.CancelFunc
}

type dynamicRuntimeTimer interface {
	C() <-chan time.Time
	Stop() bool
}

type systemDynamicRuntimeTimer struct {
	timer *time.Timer
}

func (timer systemDynamicRuntimeTimer) C() <-chan time.Time {
	return timer.timer.C
}

func (timer systemDynamicRuntimeTimer) Stop() bool {
	return timer.timer.Stop()
}

func dynamicLeasesEqual(left []dynamicLease, right []dynamicLease) bool {
	return slices.EqualFunc(left, right, func(a, b dynamicLease) bool {
		return a.Kind == b.Kind &&
			a.AcquiredAt.Equal(b.AcquiredAt) &&
			a.RefreshAt.Equal(b.RefreshAt) &&
			a.RebindAt.Equal(b.RebindAt) &&
			a.ExpiresAt.Equal(b.ExpiresAt) &&
			a.Server == b.Server &&
			a.Router == b.Router &&
			a.LinkLocal == b.LinkLocal &&
			a.MTU == b.MTU &&
			slices.Equal(a.DNS, b.DNS) &&
			slices.Equal(a.Addresses, b.Addresses) &&
			slices.Equal(a.Routes, b.Routes)
	})
}

func dynamicMethodError(family string, method string) error {
	return fmt.Errorf(
		"%w: %s method %q cannot be represented by the owned netlink data plane",
		errDynamicNotSupported,
		family,
		method,
	)
}
