//go:build linux

package networking

import (
	"net/netip"
	"testing"
	"time"

	"github.com/mdlayher/ndp"
)

func TestParseRouterAdvertisementBuildsOwnedSLAACLease(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 26, 17, 0, 0, 0, time.UTC)
	advertisement := &ndp.RouterAdvertisement{
		RouterLifetime: 30 * time.Minute,
		Options: []ndp.Option{
			&ndp.PrefixInformation{
				PrefixLength:                   64,
				OnLink:                         true,
				AutonomousAddressConfiguration: true,
				ValidLifetime:                  time.Hour,
				PreferredLifetime:              20 * time.Minute,
				Prefix:                         netip.MustParseAddr("2001:db8:73::"),
			},
			&ndp.RouteInformation{
				PrefixLength:  48,
				RouteLifetime: 25 * time.Minute,
				Prefix:        netip.MustParseAddr("2001:db8:9000::"),
			},
			&ndp.MTU{MTU: 1500},
			&ndp.RecursiveDNSServer{
				Lifetime: 20 * time.Minute,
				Servers: []netip.Addr{
					netip.MustParseAddr("2001:4860:4860::8888"),
				},
			},
		},
	}
	lease, err := parseRouterAdvertisement(
		advertisement,
		netip.MustParseAddr("fe80::1"),
		dynamicRequest{
			Interface:    "wwan73",
			LinkLocal:    netip.MustParseAddr("fe80::abcd"),
			RequestedMTU: 1400,
		},
		now,
	)
	if err != nil {
		t.Fatalf("parseRouterAdvertisement() error = %v", err)
	}
	if lease.Kind != dynamicLeaseSLAAC ||
		lease.LinkLocal != "fe80::abcd" ||
		lease.Router != "fe80::1" ||
		lease.MTU != 1400 {
		t.Fatalf("parsed SLAAC lease = %+v", lease)
	}
	if len(lease.Addresses) != 1 ||
		lease.Addresses[0].Prefix != "2001:db8:73::abcd/64" ||
		!lease.Addresses[0].NoDAD {
		t.Fatalf("formed SLAAC addresses = %+v", lease.Addresses)
	}
	if !lease.RefreshAt.Equal(now.Add(10*time.Minute)) ||
		!lease.ExpiresAt.Equal(now.Add(30*time.Minute)) {
		t.Fatalf(
			"SLAAC times: refresh=%s expires=%s",
			lease.RefreshAt,
			lease.ExpiresAt,
		)
	}
	if len(lease.Routes) != 3 ||
		len(lease.DNS) != 1 ||
		lease.DNS[0] != "2001:4860:4860::8888" {
		t.Fatalf("SLAAC routes/DNS = %+v / %v", lease.Routes, lease.DNS)
	}
}

func TestParseRouterAdvertisementRejectsMissingAutonomousPrefix(t *testing.T) {
	t.Parallel()
	_, err := parseRouterAdvertisement(
		&ndp.RouterAdvertisement{
			RouterLifetime: time.Minute,
			Options: []ndp.Option{&ndp.PrefixInformation{
				PrefixLength:      64,
				OnLink:            true,
				ValidLifetime:     time.Hour,
				PreferredLifetime: time.Hour,
				Prefix:            netip.MustParseAddr("2001:db8:74::"),
			}},
		},
		netip.MustParseAddr("fe80::1"),
		dynamicRequest{LinkLocal: netip.MustParseAddr("fe80::74")},
		time.Date(2026, 7, 26, 18, 0, 0, 0, time.UTC),
	)
	if err == nil {
		t.Fatal("parseRouterAdvertisement() accepted a non-autonomous prefix")
	}
}
