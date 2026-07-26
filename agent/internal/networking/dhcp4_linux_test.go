//go:build linux

package networking

import (
	"net"
	"testing"
	"time"

	"github.com/insomniacslk/dhcp/dhcpv4"
	"github.com/insomniacslk/dhcp/dhcpv4/nclient4"
)

func TestParseDHCPv4LeasePreservesLeaseMetadata(t *testing.T) {
	t.Parallel()
	ack, err := dhcpv4.New(
		dhcpv4.WithYourIP(net.ParseIP("10.71.0.23")),
		dhcpv4.WithOption(dhcpv4.OptSubnetMask(net.CIDRMask(24, 32))),
		dhcpv4.WithOption(dhcpv4.OptServerIdentifier(net.ParseIP("10.71.0.1"))),
		dhcpv4.WithOption(dhcpv4.OptDNS(
			net.ParseIP("1.1.1.1"),
			net.ParseIP("8.8.8.8"),
		)),
		dhcpv4.WithOption(dhcpv4.OptIPAddressLeaseTime(2*time.Hour)),
		dhcpv4.WithOption(dhcpv4.OptRenewTimeValue(30*time.Minute)),
		dhcpv4.WithOption(dhcpv4.OptRebindingTimeValue(90*time.Minute)),
		dhcpv4.WithOption(dhcpv4.OptClasslessStaticRoute(
			&dhcpv4.Route{
				Dest:   mustIPNet(t, "0.0.0.0/0"),
				Router: net.ParseIP("10.71.0.1"),
			},
			&dhcpv4.Route{
				Dest:   mustIPNet(t, "198.51.100.0/24"),
				Router: net.ParseIP("10.71.0.9"),
			},
		)),
		dhcpv4.WithOption(dhcpv4.OptGeneric(
			dhcpv4.OptionInterfaceMTU,
			[]byte{0x05, 0x78},
		)),
	)
	if err != nil {
		t.Fatalf("build DHCP ACK: %v", err)
	}
	now := time.Date(2026, 7, 26, 15, 0, 0, 0, time.UTC)
	lease, err := parseDHCPv4Lease(&nclient4.Lease{ACK: ack}, now)
	if err != nil {
		t.Fatalf("parseDHCPv4Lease() error = %v", err)
	}
	if lease.Kind != dynamicLeaseDHCPv4 ||
		lease.Server != "10.71.0.1" ||
		lease.Router != "10.71.0.1" ||
		lease.MTU != 1400 {
		t.Fatalf("parsed lease metadata = %+v", lease)
	}
	if !lease.RefreshAt.Equal(now.Add(30*time.Minute)) ||
		!lease.RebindAt.Equal(now.Add(90*time.Minute)) ||
		!lease.ExpiresAt.Equal(now.Add(2*time.Hour)) {
		t.Fatalf(
			"lease times: refresh=%s rebind=%s expires=%s",
			lease.RefreshAt,
			lease.RebindAt,
			lease.ExpiresAt,
		)
	}
	if len(lease.Addresses) != 1 ||
		lease.Addresses[0].Prefix != "10.71.0.23/24" ||
		len(lease.Routes) != 3 ||
		len(lease.DNS) != 2 {
		t.Fatalf("parsed lease = %+v", lease)
	}
}

func TestParseDHCPv4LeaseRejectsClasslessRoutesWithoutDefault(t *testing.T) {
	t.Parallel()
	ack, err := dhcpv4.New(
		dhcpv4.WithYourIP(net.ParseIP("10.72.0.23")),
		dhcpv4.WithOption(dhcpv4.OptSubnetMask(net.CIDRMask(24, 32))),
		dhcpv4.WithOption(dhcpv4.OptServerIdentifier(net.ParseIP("10.72.0.1"))),
		dhcpv4.WithOption(dhcpv4.OptIPAddressLeaseTime(time.Hour)),
		dhcpv4.WithOption(dhcpv4.OptClasslessStaticRoute(
			&dhcpv4.Route{
				Dest:   mustIPNet(t, "198.51.100.0/24"),
				Router: net.ParseIP("10.72.0.9"),
			},
		)),
	)
	if err != nil {
		t.Fatal(err)
	}
	_, err = parseDHCPv4Lease(
		&nclient4.Lease{ACK: ack},
		time.Date(2026, 7, 26, 16, 0, 0, 0, time.UTC),
	)
	if err == nil {
		t.Fatal("parseDHCPv4Lease() accepted classless routes without a default")
	}
}
