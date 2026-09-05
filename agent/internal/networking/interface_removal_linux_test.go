//go:build linux

package networking

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReleaseRemovedSLAACInterface(t *testing.T) {
	now := time.Now()
	fake := newFakeNetlink("wwan-review", 43)
	fake.link.Attrs().HardwareAddr = mustHardwareAddr(t, "02:00:00:00:00:43")
	sysctl := newFakeSysctlController("wwan-review")
	session := &fakeLeaseSession{lease: slaacLease(now, "fe80::ff:fe00:43", "2001:db8:43::43", "fe80::1", time.Minute, time.Hour)}
	dataPlane, err := newLinuxDataPlaneWithDependencies(DataPlaneOptions{}, fake, sysctl, &fakeDynamicAcquirer{ipv6: []acquireResult{{session: session}}}, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if err := dataPlane.Configure(context.Background(), "line-review", slaacConnection("wwan-review", "")); err != nil {
		t.Fatal(err)
	}
	// A USB disconnect removes the netdev and its /proc/sys entries.
	fake.link = nil
	dataPlane.sysctl = procSysctl{root: t.TempDir()}
	err = dataPlane.Release(context.Background(), "line-review")
	if errors.Is(err, os.ErrNotExist) {
		t.Logf("reproduced missing-interface failure: %v; retained=%v", err, dataPlane.OwnedLines())
	}
	if err != nil {
		t.Fatalf("removed hardware must be releasable: %v", err)
	}
}

func TestReleaseDoesNotTouchReusedInterfaceName(t *testing.T) {
	now := time.Now()
	fake := newFakeNetlink("wwan-review", 43)
	fake.link.Attrs().HardwareAddr = mustHardwareAddr(t, "02:00:00:00:00:43")
	sysctl := newFakeSysctlController("wwan-review")
	session := &fakeLeaseSession{lease: slaacLease(now, "fe80::ff:fe00:43", "2001:db8:43::43", "fe80::1", time.Minute, time.Hour)}
	dataPlane, err := newLinuxDataPlaneWithDependencies(DataPlaneOptions{}, fake, sysctl, &fakeDynamicAcquirer{ipv6: []acquireResult{{session: session}}}, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if err := dataPlane.Configure(context.Background(), "line-review", slaacConnection("wwan-review", "")); err != nil {
		t.Fatal(err)
	}
	// Original USB netdev disappears; another owner brings up a newly enumerated netdev with the same name.
	replacement := newFakeNetlink("wwan-review", 99).link
	replacement.Attrs().Flags = net.FlagUp
	fake.link = replacement
	dataPlane.sysctl = newFakeSysctlController("wwan-review")
	if err := dataPlane.Release(context.Background(), "line-review"); err != nil {
		t.Fatal(err)
	}
	if len(fake.deletedAddrs) != 0 {
		t.Fatalf("deleted replacement addresses: %v", fake.deletedAddrs)
	}
	for _, route := range fake.deletedRoutes {
		if route.LinkIndex != 43 {
			t.Fatalf("deleted route on foreign interface: %+v", route)
		}
	}
	if replacement.Attrs().Flags&net.FlagUp == 0 {
		t.Fatalf("released old index 43 shut down foreign replacement index 99; link-down calls=%d", fake.linkDownCalls)
	}
}

func TestRemovedSLAACStateIsClearedAcrossRestart(t *testing.T) {
	now := time.Now()
	fake := newFakeNetlink("wwan-review", 43)
	fake.link.Attrs().HardwareAddr = mustHardwareAddr(t, "02:00:00:00:00:43")
	sysctl := newFakeSysctlController("wwan-review")
	lease := &fakeLeaseSession{lease: slaacLease(now, "fe80::ff:fe00:43", "2001:db8:43::43", "fe80::1", time.Minute, time.Hour)}
	options := DataPlaneOptions{StateFile: filepath.Join(t.TempDir(), "network.json")}
	plane, err := newLinuxDataPlaneWithDependencies(options, fake, sysctl, &fakeDynamicAcquirer{ipv6: []acquireResult{{session: lease}}}, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if err = plane.Configure(context.Background(), "line-review", slaacConnection("wwan-review", "")); err != nil {
		t.Fatal(err)
	}
	fake.link = nil
	missingProc := procSysctl{root: t.TempDir()}
	plane.sysctl = missingProc
	if err = plane.Release(context.Background(), "line-review"); err != nil {
		t.Fatal(err)
	}
	restarted, err := newLinuxDataPlaneWithDependencies(options, fake, missingProc, &fakeDynamicAcquirer{}, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if len(restarted.OwnedLines()) != 0 {
		t.Fatalf("stale ownership after restart: %v", restarted.OwnedLines())
	}
	if err = restarted.Release(context.Background(), "line-review"); err != nil {
		t.Fatalf("restart cannot reconcile removed interface: %v", err)
	}
}
