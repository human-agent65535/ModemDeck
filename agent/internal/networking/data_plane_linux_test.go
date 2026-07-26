//go:build linux

package networking

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/human-agent65535/modemdeck/agent/internal/domain"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

type fakeNetlinkController struct {
	link               netlink.Link
	linkUpCalls        int
	mtuValues          []int
	addedAddrs         []string
	deletedAddrs       []string
	addedRoutes        []netlink.Route
	deletedRoutes      []netlink.Route
	addedRules         []netlink.Rule
	deletedRules       []netlink.Rule
	addrAlreadyExists  bool
	routeAlreadyExists bool
	ruleAlreadyExists  bool
}

func (fake *fakeNetlinkController) LinkByName(name string) (netlink.Link, error) {
	if fake.link == nil || fake.link.Attrs().Name != name {
		return nil, unix.ENODEV
	}
	return fake.link, nil
}

func (fake *fakeNetlinkController) LinkSetUp(netlink.Link) error {
	fake.linkUpCalls++
	return nil
}

func (fake *fakeNetlinkController) LinkSetMTU(link netlink.Link, mtu int) error {
	fake.mtuValues = append(fake.mtuValues, mtu)
	link.Attrs().MTU = mtu
	return nil
}

func (fake *fakeNetlinkController) AddrAdd(
	_ netlink.Link,
	address *netlink.Addr,
) error {
	fake.addedAddrs = append(fake.addedAddrs, address.String())
	if fake.addrAlreadyExists {
		return unix.EEXIST
	}
	return nil
}

func (fake *fakeNetlinkController) AddrDel(
	_ netlink.Link,
	address *netlink.Addr,
) error {
	fake.deletedAddrs = append(fake.deletedAddrs, address.String())
	return nil
}

func (fake *fakeNetlinkController) RouteAdd(route *netlink.Route) error {
	fake.addedRoutes = append(fake.addedRoutes, *route)
	if fake.routeAlreadyExists {
		return unix.EEXIST
	}
	return nil
}

func (fake *fakeNetlinkController) RouteDel(route *netlink.Route) error {
	fake.deletedRoutes = append(fake.deletedRoutes, *route)
	return nil
}

func (fake *fakeNetlinkController) RuleAdd(rule *netlink.Rule) error {
	fake.addedRules = append(fake.addedRules, *rule)
	if fake.ruleAlreadyExists {
		return unix.EEXIST
	}
	return nil
}

func (fake *fakeNetlinkController) RuleDel(rule *netlink.Rule) error {
	fake.deletedRules = append(fake.deletedRules, *rule)
	return nil
}

func TestLinuxDataPlaneAppliesAndReleasesOnlyRecordedState(t *testing.T) {
	t.Parallel()
	stateFile := filepath.Join(t.TempDir(), "network.json")
	fake := &fakeNetlinkController{
		link: &netlink.Dummy{LinkAttrs: netlink.LinkAttrs{
			Index: 7,
			Name:  "wwan0",
			MTU:   1500,
		}},
	}
	dataPlane, err := newLinuxDataPlane(
		DataPlaneOptions{StateFile: stateFile},
		fake,
	)
	if err != nil {
		t.Fatalf("newLinuxDataPlane() error = %v", err)
	}

	connection := domain.DataConnection{
		Connected: true,
		Interface: "wwan0",
		IPv4: domain.IPConfiguration{
			Method:  "static",
			Address: "10.23.0.2",
			Prefix:  30,
			Gateway: "10.23.0.1",
			MTU:     1420,
		},
		IPv6: domain.IPConfiguration{
			Method:  "static",
			Address: "2001:db8:23::2",
			Prefix:  64,
			Gateway: "2001:db8:23::1",
			MTU:     1420,
		},
	}
	if err := dataPlane.Configure(context.Background(), "line-fixture", connection); err != nil {
		t.Fatalf("Configure() error = %v", err)
	}
	if fake.linkUpCalls != 1 ||
		len(fake.addedAddrs) != 2 ||
		len(fake.addedRoutes) != 4 ||
		len(fake.addedRules) != 6 {
		t.Fatalf(
			"apply calls: up=%d addresses=%d routes=%d rules=%d",
			fake.linkUpCalls,
			len(fake.addedAddrs),
			len(fake.addedRoutes),
			len(fake.addedRules),
		)
	}
	if len(fake.mtuValues) != 1 || fake.mtuValues[0] != 1420 {
		t.Fatalf("MTU changes = %v", fake.mtuValues)
	}
	for _, route := range fake.addedRoutes {
		if route.Table < routeTableBase || route.LinkIndex != 7 {
			t.Fatalf("route escaped owned table/interface: %+v", route)
		}
	}
	for _, rule := range fake.addedRules {
		if rule.Table < routeTableBase || rule.Table >= routeTableBase+routeTableSlots {
			t.Fatalf("rule escaped owned table range: %+v", rule)
		}
	}
	info, err := os.Stat(stateFile)
	if err != nil {
		t.Fatalf("stat state file: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("state file mode = %o", info.Mode().Perm())
	}

	if err := dataPlane.Release(context.Background(), "line-fixture"); err != nil {
		t.Fatalf("Release() error = %v", err)
	}
	if len(fake.deletedRules) != len(fake.addedRules) ||
		len(fake.deletedRoutes) != len(fake.addedRoutes) ||
		len(fake.deletedAddrs) != len(fake.addedAddrs) {
		t.Fatalf(
			"release calls: addresses=%d routes=%d rules=%d",
			len(fake.deletedAddrs),
			len(fake.deletedRoutes),
			len(fake.deletedRules),
		)
	}
	if got := fake.mtuValues[len(fake.mtuValues)-1]; got != 1500 {
		t.Fatalf("restored MTU = %d, want 1500", got)
	}
	content, err := os.ReadFile(stateFile)
	if err != nil {
		t.Fatalf("read state file: %v", err)
	}
	if !strings.Contains(string(content), `"networks": []`) {
		t.Fatalf("released state file = %s", content)
	}
}

func TestLinuxDataPlaneRejectsDynamicBearerWithoutLease(t *testing.T) {
	t.Parallel()
	fake := &fakeNetlinkController{
		link: &netlink.Dummy{LinkAttrs: netlink.LinkAttrs{
			Index: 8,
			Name:  "wwan1",
			MTU:   1500,
		}},
	}
	dataPlane, err := newLinuxDataPlane(DataPlaneOptions{}, fake)
	if err != nil {
		t.Fatalf("newLinuxDataPlane() error = %v", err)
	}
	err = dataPlane.Configure(context.Background(), "line-dhcp", domain.DataConnection{
		Connected: true,
		Interface: "wwan1",
		IPv4: domain.IPConfiguration{
			Method: "dhcp",
		},
	})
	if err == nil || !strings.Contains(err.Error(), "lease client") {
		t.Fatalf("Configure() error = %v, want lease client diagnosis", err)
	}
	if fake.linkUpCalls != 0 ||
		len(fake.addedAddrs) != 0 ||
		len(fake.addedRoutes) != 0 {
		t.Fatalf("dynamic failure mutated netlink state")
	}
}

func TestLinuxDataPlaneDoesNotDeleteUnownedAddressOnConflict(t *testing.T) {
	t.Parallel()
	fake := &fakeNetlinkController{
		link: &netlink.Dummy{LinkAttrs: netlink.LinkAttrs{
			Index: 9,
			Name:  "wwan2",
			MTU:   1500,
		}},
		addrAlreadyExists: true,
	}
	dataPlane, err := newLinuxDataPlane(DataPlaneOptions{}, fake)
	if err != nil {
		t.Fatalf("newLinuxDataPlane() error = %v", err)
	}
	err = dataPlane.Configure(
		context.Background(),
		"line-conflict",
		staticIPv4Connection("wwan2", "10.42.0.2", "10.42.0.1", 1420),
	)
	if err == nil || !strings.Contains(err.Error(), "file exists") {
		t.Fatalf("Configure() error = %v, want existing address conflict", err)
	}
	if len(fake.deletedAddrs) != 0 ||
		len(fake.deletedRoutes) != 0 ||
		len(fake.deletedRules) != 0 {
		t.Fatalf(
			"conflict deleted unowned state: addresses=%d routes=%d rules=%d",
			len(fake.deletedAddrs),
			len(fake.deletedRoutes),
			len(fake.deletedRules),
		)
	}
	if got := fake.link.Attrs().MTU; got != 1500 {
		t.Fatalf("MTU after conflict = %d, want 1500", got)
	}
}

func TestLinuxDataPlaneReplacesChangedOwnedConfiguration(t *testing.T) {
	t.Parallel()
	stateFile := filepath.Join(t.TempDir(), "network.json")
	fake := &fakeNetlinkController{
		link: &netlink.Dummy{LinkAttrs: netlink.LinkAttrs{
			Index: 10,
			Name:  "wwan3",
			MTU:   1500,
		}},
	}
	dataPlane, err := newLinuxDataPlane(
		DataPlaneOptions{StateFile: stateFile},
		fake,
	)
	if err != nil {
		t.Fatalf("newLinuxDataPlane() error = %v", err)
	}
	if err := dataPlane.Configure(
		context.Background(),
		"line-replace",
		staticIPv4Connection("wwan3", "10.51.0.2", "10.51.0.1", 1420),
	); err != nil {
		t.Fatalf("first Configure() error = %v", err)
	}
	if err := dataPlane.Configure(
		context.Background(),
		"line-replace",
		staticIPv4Connection("wwan3", "10.52.0.2", "10.52.0.1", 1400),
	); err != nil {
		t.Fatalf("second Configure() error = %v", err)
	}
	if len(fake.deletedAddrs) != 1 ||
		fake.deletedAddrs[0] != "10.51.0.2/30" {
		t.Fatalf("deleted addresses = %v", fake.deletedAddrs)
	}
	if len(fake.mtuValues) != 3 ||
		fake.mtuValues[0] != 1420 ||
		fake.mtuValues[1] != 1500 ||
		fake.mtuValues[2] != 1400 {
		t.Fatalf("MTU changes = %v", fake.mtuValues)
	}
	content, err := os.ReadFile(stateFile)
	if err != nil {
		t.Fatalf("read state file: %v", err)
	}
	if strings.Contains(string(content), "10.51.0.2") ||
		!strings.Contains(string(content), "10.52.0.2/30") {
		t.Fatalf("replaced state file = %s", content)
	}
}

func TestLinuxDataPlaneAllowsExistingObjectsOnlyForRecordedState(t *testing.T) {
	t.Parallel()
	fake := &fakeNetlinkController{
		link: &netlink.Dummy{LinkAttrs: netlink.LinkAttrs{
			Index: 11,
			Name:  "wwan4",
			MTU:   1500,
		}},
	}
	dataPlane, err := newLinuxDataPlane(DataPlaneOptions{}, fake)
	if err != nil {
		t.Fatalf("newLinuxDataPlane() error = %v", err)
	}
	connection := staticIPv4Connection("wwan4", "10.61.0.2", "10.61.0.1", 1420)
	if err := dataPlane.Configure(context.Background(), "line-reconcile", connection); err != nil {
		t.Fatalf("first Configure() error = %v", err)
	}
	fake.addrAlreadyExists = true
	fake.routeAlreadyExists = true
	fake.ruleAlreadyExists = true
	if err := dataPlane.Configure(context.Background(), "line-reconcile", connection); err != nil {
		t.Fatalf("reconcile Configure() error = %v", err)
	}
	if len(fake.deletedAddrs) != 0 ||
		len(fake.deletedRoutes) != 0 ||
		len(fake.deletedRules) != 0 {
		t.Fatalf("reconcile removed recorded state")
	}
}

func TestLinuxDataPlaneRejectsCorruptPersistentState(t *testing.T) {
	t.Parallel()
	stateFile := filepath.Join(t.TempDir(), "network.json")
	if err := os.WriteFile(stateFile, []byte(`{"version":1,"networks":[{}]}`), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	_, err := newLinuxDataPlane(DataPlaneOptions{StateFile: stateFile}, &fakeNetlinkController{})
	if err == nil || !strings.Contains(err.Error(), "invalid entry") {
		t.Fatalf("newLinuxDataPlane() error = %v", err)
	}
}

func TestLinuxDataPlaneReleaseIsIdempotent(t *testing.T) {
	t.Parallel()
	dataPlane, err := newLinuxDataPlane(
		DataPlaneOptions{},
		&fakeNetlinkController{},
	)
	if err != nil {
		t.Fatalf("newLinuxDataPlane() error = %v", err)
	}
	if err := dataPlane.Release(context.Background(), "line-missing"); err != nil {
		t.Fatalf("Release() error = %v", err)
	}
}

func staticIPv4Connection(
	interfaceName string,
	address string,
	gateway string,
	mtu uint32,
) domain.DataConnection {
	return domain.DataConnection{
		Connected: true,
		Interface: interfaceName,
		IPv4: domain.IPConfiguration{
			Method:  "static",
			Address: address,
			Prefix:  30,
			Gateway: gateway,
			MTU:     mtu,
		},
	}
}
