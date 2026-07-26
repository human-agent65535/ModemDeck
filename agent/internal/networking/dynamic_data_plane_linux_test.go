//go:build linux

package networking

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/human-agent65535/modemdeck/agent/internal/domain"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

type fakeSysctlController struct {
	mu     sync.Mutex
	values map[string]int
	writes []fakeSysctlWrite
}

type fakeSysctlWrite struct {
	Interface string
	Name      string
	Value     int
}

func newFakeSysctlController(interfaceName string) *fakeSysctlController {
	return &fakeSysctlController{values: map[string]int{
		interfaceName + ".accept_ra": 1,
		interfaceName + ".autoconf":  1,
	}}
}

func (controller *fakeSysctlController) Read(
	interfaceName string,
	name string,
) (int, error) {
	controller.mu.Lock()
	defer controller.mu.Unlock()
	value, found := controller.values[interfaceName+"."+name]
	if !found {
		return 0, errors.New("fake sysctl is not configured")
	}
	return value, nil
}

func (controller *fakeSysctlController) Write(
	interfaceName string,
	name string,
	value int,
) error {
	controller.mu.Lock()
	defer controller.mu.Unlock()
	controller.values[interfaceName+"."+name] = value
	controller.writes = append(controller.writes, fakeSysctlWrite{
		Interface: interfaceName,
		Name:      name,
		Value:     value,
	})
	return nil
}

func (controller *fakeSysctlController) set(
	interfaceName string,
	name string,
	value int,
) {
	controller.mu.Lock()
	defer controller.mu.Unlock()
	controller.values[interfaceName+"."+name] = value
}

func (controller *fakeSysctlController) value(
	interfaceName string,
	name string,
) int {
	controller.mu.Lock()
	defer controller.mu.Unlock()
	return controller.values[interfaceName+"."+name]
}

func (controller *fakeSysctlController) writeCount() int {
	controller.mu.Lock()
	defer controller.mu.Unlock()
	return len(controller.writes)
}

type acquireResult struct {
	session dynamicLeaseSession
	err     error
}

type fakeDynamicAcquirer struct {
	mu          sync.Mutex
	ipv4        []acquireResult
	ipv6        []acquireResult
	acquireIPv4 func(context.Context, dynamicRequest) (dynamicLeaseSession, error)
	acquireIPv6 func(context.Context, dynamicRequest) (dynamicLeaseSession, error)
	ipv4Calls   int
	ipv6Calls   int
	ipv4Request []dynamicRequest
	ipv6Request []dynamicRequest
}

func (acquirer *fakeDynamicAcquirer) AcquireIPv4(
	ctx context.Context,
	request dynamicRequest,
) (dynamicLeaseSession, error) {
	acquirer.mu.Lock()
	acquirer.ipv4Calls++
	acquirer.ipv4Request = append(acquirer.ipv4Request, request)
	callback := acquirer.acquireIPv4
	if callback == nil {
		if len(acquirer.ipv4) == 0 {
			acquirer.mu.Unlock()
			return nil, errors.New("fake DHCPv4 result is not configured")
		}
		result := acquirer.ipv4[0]
		acquirer.ipv4 = acquirer.ipv4[1:]
		acquirer.mu.Unlock()
		return result.session, result.err
	}
	acquirer.mu.Unlock()
	return callback(ctx, request)
}

func (acquirer *fakeDynamicAcquirer) AcquireIPv6(
	ctx context.Context,
	request dynamicRequest,
) (dynamicLeaseSession, error) {
	acquirer.mu.Lock()
	acquirer.ipv6Calls++
	acquirer.ipv6Request = append(acquirer.ipv6Request, request)
	callback := acquirer.acquireIPv6
	if callback == nil {
		if len(acquirer.ipv6) == 0 {
			acquirer.mu.Unlock()
			return nil, errors.New("fake SLAAC result is not configured")
		}
		result := acquirer.ipv6[0]
		acquirer.ipv6 = acquirer.ipv6[1:]
		acquirer.mu.Unlock()
		return result.session, result.err
	}
	acquirer.mu.Unlock()
	return callback(ctx, request)
}

type leaseRefreshResult struct {
	lease dynamicLease
	err   error
}

type fakeLeaseSession struct {
	mu           sync.Mutex
	lease        dynamicLease
	refresh      []leaseRefreshResult
	refreshCalls int
	releaseCalls int
	closeCalls   int
}

func (session *fakeLeaseSession) Lease() dynamicLease {
	session.mu.Lock()
	defer session.mu.Unlock()
	return session.lease
}

func (session *fakeLeaseSession) Refresh(
	ctx context.Context,
) (dynamicLease, error) {
	session.mu.Lock()
	defer session.mu.Unlock()
	session.refreshCalls++
	if err := ctx.Err(); err != nil {
		return dynamicLease{}, err
	}
	if len(session.refresh) == 0 {
		return dynamicLease{}, errors.New("fake refresh result is not configured")
	}
	result := session.refresh[0]
	session.refresh = session.refresh[1:]
	if result.err == nil {
		session.lease = result.lease
	}
	return result.lease, result.err
}

func (session *fakeLeaseSession) Release(context.Context) error {
	session.mu.Lock()
	defer session.mu.Unlock()
	session.releaseCalls++
	return nil
}

func (session *fakeLeaseSession) Close() error {
	session.mu.Lock()
	defer session.mu.Unlock()
	session.closeCalls++
	return nil
}

func (session *fakeLeaseSession) counts() (int, int, int) {
	session.mu.Lock()
	defer session.mu.Unlock()
	return session.refreshCalls, session.releaseCalls, session.closeCalls
}

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (clock *fakeClock) Now() time.Time {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	return clock.now
}

func (clock *fakeClock) Advance(duration time.Duration) time.Time {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	clock.now = clock.now.Add(duration)
	return clock.now
}

type manualRuntimeTimer struct {
	channel chan time.Time
}

func (timer *manualRuntimeTimer) C() <-chan time.Time {
	return timer.channel
}

func (*manualRuntimeTimer) Stop() bool {
	return true
}

type manualTimerRequest struct {
	Duration time.Duration
	Timer    *manualRuntimeTimer
}

type manualTimerFactory struct {
	created chan manualTimerRequest
}

func newManualTimerFactory() *manualTimerFactory {
	return &manualTimerFactory{
		created: make(chan manualTimerRequest, 16),
	}
}

func (factory *manualTimerFactory) New(duration time.Duration) dynamicRuntimeTimer {
	timer := &manualRuntimeTimer{channel: make(chan time.Time, 1)}
	factory.created <- manualTimerRequest{
		Duration: duration,
		Timer:    timer,
	}
	return timer
}

func (factory *manualTimerFactory) next(t *testing.T) manualTimerRequest {
	t.Helper()
	select {
	case request := <-factory.created:
		return request
	case <-time.After(2 * time.Second):
		t.Fatal("dynamic runtime did not schedule a timer")
		return manualTimerRequest{}
	}
}

func TestLinuxDataPlaneDynamicAcquireHonorsContext(t *testing.T) {
	t.Parallel()
	for _, testCase := range []struct {
		name    string
		context func() (context.Context, context.CancelFunc)
		want    error
	}{
		{
			name: "timeout",
			context: func() (context.Context, context.CancelFunc) {
				return context.WithTimeout(context.Background(), 10*time.Millisecond)
			},
			want: context.DeadlineExceeded,
		},
		{
			name: "cancel",
			context: func() (context.Context, context.CancelFunc) {
				ctx, cancel := context.WithCancel(context.Background())
				go cancel()
				return ctx, func() {}
			},
			want: context.Canceled,
		},
	} {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			fake := newFakeNetlink("wwan-context", 31)
			acquirer := &fakeDynamicAcquirer{
				acquireIPv4: func(
					ctx context.Context,
					_ dynamicRequest,
				) (dynamicLeaseSession, error) {
					<-ctx.Done()
					return nil, ctx.Err()
				},
			}
			dataPlane, err := newLinuxDataPlaneWithDependencies(
				DataPlaneOptions{},
				fake,
				newFakeSysctlController("wwan-context"),
				acquirer,
				time.Now,
			)
			if err != nil {
				t.Fatalf("newLinuxDataPlaneWithDependencies() error = %v", err)
			}
			ctx, cancel := testCase.context()
			defer cancel()
			err = dataPlane.Configure(
				ctx,
				"line-context",
				dhcp4Connection("wwan-context"),
			)
			if !errors.Is(err, testCase.want) {
				t.Fatalf("Configure() error = %v, want %v", err, testCase.want)
			}
			if fake.linkUpCalls != 1 ||
				fake.linkDownCalls != 1 ||
				fake.link.Attrs().Flags&net.FlagUp != 0 ||
				len(fake.addedAddrs) != 0 ||
				len(fake.addedRoutes) != 0 ||
				len(fake.addedRules) != 0 {
				t.Fatal("canceled dynamic acquisition did not fully roll back")
			}
		})
	}
}

func TestLinuxDataPlaneRejectsPPPWithoutMutatingNetwork(t *testing.T) {
	t.Parallel()
	fake := newFakeNetlink("wwan-ppp", 32)
	dataPlane, err := newLinuxDataPlaneWithDependencies(
		DataPlaneOptions{},
		fake,
		newFakeSysctlController("wwan-ppp"),
		&fakeDynamicAcquirer{},
		time.Now,
	)
	if err != nil {
		t.Fatalf("newLinuxDataPlaneWithDependencies() error = %v", err)
	}
	err = dataPlane.Configure(context.Background(), "line-ppp", domain.DataConnection{
		Connected: true,
		Interface: "wwan-ppp",
		IPv4:      domain.IPConfiguration{Method: "ppp"},
	})
	if !errors.Is(err, errDynamicNotSupported) ||
		!strings.Contains(err.Error(), "TTY plus LCP/IPCP") {
		t.Fatalf("Configure() error = %v, want explicit PPP NotSupported", err)
	}
	if fake.linkUpCalls != 0 || len(fake.addedAddrs) != 0 {
		t.Fatal("PPP rejection mutated netlink state")
	}
}

func TestLinuxDataPlaneDynamicApplyFailureRollsBack(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 26, 9, 0, 0, 0, time.UTC)
	fake := newFakeNetlink("wwan-rollback", 33)
	fake.routeAddErrors = []error{errors.New("injected route failure")}
	session := &fakeLeaseSession{
		lease: dhcp4Lease(now, "10.33.0.2", "10.33.0.1", time.Minute, time.Hour),
	}
	dataPlane, err := newLinuxDataPlaneWithDependencies(
		DataPlaneOptions{StateFile: filepath.Join(t.TempDir(), "network.json")},
		fake,
		newFakeSysctlController("wwan-rollback"),
		&fakeDynamicAcquirer{ipv4: []acquireResult{{session: session}}},
		func() time.Time { return now },
	)
	if err != nil {
		t.Fatalf("newLinuxDataPlaneWithDependencies() error = %v", err)
	}
	err = dataPlane.Configure(
		context.Background(),
		"line-rollback",
		dhcp4Connection("wwan-rollback"),
	)
	if err == nil || !strings.Contains(err.Error(), "injected route failure") {
		t.Fatalf("Configure() error = %v, want injected failure", err)
	}
	if len(fake.existingAddrs[netlink.FAMILY_V4]) != 0 ||
		countFakeRoutes(fake) != 0 ||
		countFakeRules(fake) != 0 {
		t.Fatalf("failed apply left owned netlink state behind")
	}
	if fake.link.Attrs().MTU != 1500 {
		t.Fatalf("MTU after rollback = %d, want 1500", fake.link.Attrs().MTU)
	}
	if fake.link.Attrs().Flags&net.FlagUp != 0 {
		t.Fatal("failed dynamic apply left the bearer interface up")
	}
	_, releases, closes := session.counts()
	if releases != 1 || closes != 1 {
		t.Fatalf("lease cleanup: release=%d close=%d, want 1/1", releases, closes)
	}
	if len(dataPlane.OwnedLines()) != 0 {
		t.Fatalf("failed line remained owned: %v", dataPlane.OwnedLines())
	}
}

func TestLinuxDataPlaneRejectsForeignStateBeforeWrites(t *testing.T) {
	t.Parallel()
	t.Run("address", func(t *testing.T) {
		fake := newFakeNetlink("wfa0", 34)
		foreign, err := netlink.ParseAddr("192.0.2.9/24")
		if err != nil {
			t.Fatal(err)
		}
		fake.existingAddrs = map[int][]netlink.Addr{
			netlink.FAMILY_V4: {*foreign},
		}
		dataPlane := mustTestDataPlane(t, fake, &fakeDynamicAcquirer{}, time.Now)
		err = dataPlane.Configure(
			context.Background(),
			"line-foreign-address",
			staticIPv4Connection(
				"wfa0",
				"10.34.0.2",
				"10.34.0.1",
				1420,
			),
		)
		if err == nil || !strings.Contains(err.Error(), "foreign address") {
			t.Fatalf("Configure() error = %v, want foreign address conflict", err)
		}
		assertNoNetlinkWrites(t, fake)
		if !fakeHasAddress(fake, netlink.FAMILY_V4, "192.0.2.9/24") {
			t.Fatal("foreign address was removed")
		}
	})

	t.Run("route", func(t *testing.T) {
		fake := newFakeNetlink("wfr0", 35)
		dataPlane := mustTestDataPlane(t, fake, &fakeDynamicAcquirer{}, time.Now)
		table, _, err := dataPlane.routingIdentity(
			"line-foreign-route",
			"wfr0",
		)
		if err != nil {
			t.Fatal(err)
		}
		fake.existingRoutes = map[int][]netlink.Route{table: {{
			LinkIndex: 35,
			Table:     table,
			Dst:       mustIPNet(t, "198.51.100.0/24"),
		}}}
		err = dataPlane.Configure(
			context.Background(),
			"line-foreign-route",
			staticIPv4Connection(
				"wfr0",
				"10.35.0.2",
				"10.35.0.1",
				1420,
			),
		)
		if err == nil || !strings.Contains(err.Error(), "foreign route") {
			t.Fatalf("Configure() error = %v, want foreign route conflict", err)
		}
		assertNoNetlinkWrites(t, fake)
		if len(fake.existingRoutes[table]) != 1 {
			t.Fatal("foreign route was removed")
		}
	})

	t.Run("rule", func(t *testing.T) {
		fake := newFakeNetlink("wfu0", 36)
		dataPlane := mustTestDataPlane(t, fake, &fakeDynamicAcquirer{}, time.Now)
		table, _, err := dataPlane.routingIdentity(
			"line-foreign-rule",
			"wfu0",
		)
		if err != nil {
			t.Fatal(err)
		}
		rule := netlink.NewRule()
		rule.Family = unix.AF_INET
		rule.Table = table
		rule.Priority = 99
		fake.existingRules = map[int][]netlink.Rule{
			netlink.FAMILY_V4: {*rule},
		}
		err = dataPlane.Configure(
			context.Background(),
			"line-foreign-rule",
			staticIPv4Connection(
				"wfu0",
				"10.36.0.2",
				"10.36.0.1",
				1420,
			),
		)
		if err == nil || !strings.Contains(err.Error(), "foreign policy rule") {
			t.Fatalf("Configure() error = %v, want foreign rule conflict", err)
		}
		assertNoNetlinkWrites(t, fake)
		if len(fake.existingRules[netlink.FAMILY_V4]) != 1 {
			t.Fatal("foreign rule was removed")
		}
	})
}

func TestLinuxDataPlaneReleaseDeletesOnlyOwnedObjects(t *testing.T) {
	t.Parallel()
	fake := newFakeNetlink("wwan-release", 37)
	dataPlane := mustTestDataPlane(t, fake, &fakeDynamicAcquirer{}, time.Now)
	if err := dataPlane.Configure(
		context.Background(),
		"line-release",
		staticIPv4Connection("wwan-release", "10.37.0.2", "10.37.0.1", 1420),
	); err != nil {
		t.Fatalf("Configure() error = %v", err)
	}
	foreign, err := netlink.ParseAddr("192.0.2.37/24")
	if err != nil {
		t.Fatal(err)
	}
	fake.existingAddrs[netlink.FAMILY_V4] = append(
		fake.existingAddrs[netlink.FAMILY_V4],
		*foreign,
	)
	if err := dataPlane.Release(context.Background(), "line-release"); err != nil {
		t.Fatalf("Release() error = %v", err)
	}
	if !fakeHasAddress(fake, netlink.FAMILY_V4, "192.0.2.37/24") {
		t.Fatal("Release removed a foreign address")
	}
	if fakeHasAddress(fake, netlink.FAMILY_V4, "10.37.0.2/30") {
		t.Fatal("Release left its owned address behind")
	}
}

func TestLinuxDataPlaneStaticIPv6ToSLAAC(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC)
	fake := newFakeNetlink("wsm0", 38)
	fake.link.Attrs().HardwareAddr = mustHardwareAddr(t, "02:00:00:00:00:38")
	sysctl := newFakeSysctlController("wsm0")
	session := &fakeLeaseSession{
		lease: slaacLease(
			now,
			"fe80::ff:fe00:38",
			"2001:db8:38::38",
			"fe80::1",
			time.Minute,
			time.Hour,
		),
	}
	acquirer := &fakeDynamicAcquirer{ipv6: []acquireResult{
		{err: errors.New("injected SLAAC acquisition failure")},
		{session: session},
	}}
	dataPlane, err := newLinuxDataPlaneWithDependencies(
		DataPlaneOptions{StateFile: filepath.Join(t.TempDir(), "network.json")},
		fake,
		sysctl,
		acquirer,
		func() time.Time { return now },
	)
	if err != nil {
		t.Fatalf("newLinuxDataPlaneWithDependencies() error = %v", err)
	}
	if err := dataPlane.Configure(
		context.Background(),
		"line-slaac-migrate",
		staticIPv6Connection(
			"wsm0",
			"2001:db8:38:1::2",
			"2001:db8:38:1::1",
		),
	); err != nil {
		t.Fatalf("static Configure() error = %v", err)
	}
	err = dataPlane.Configure(
		context.Background(),
		"line-slaac-migrate",
		slaacConnection("wsm0", ""),
	)
	if err == nil || !strings.Contains(err.Error(), "injected SLAAC") {
		t.Fatalf("failed SLAAC Configure() error = %v", err)
	}
	if !fakeHasAddress(
		fake,
		netlink.FAMILY_V6,
		"2001:db8:38:1::2/64",
	) {
		t.Fatal("failed SLAAC acquisition did not preserve old static IPv6")
	}
	if fakeHasAddress(fake, netlink.FAMILY_V6, "fe80::ff:fe00:38/64") {
		t.Fatal("failed SLAAC acquisition left its generated link-local address")
	}
	if sysctl.value("wsm0", "accept_ra") != 1 ||
		sysctl.value("wsm0", "autoconf") != 1 {
		t.Fatal("failed SLAAC acquisition did not restore IPv6 sysctls")
	}
	if err := dataPlane.Configure(
		context.Background(),
		"line-slaac-migrate",
		slaacConnection("wsm0", ""),
	); err != nil {
		t.Fatalf("SLAAC Configure() error = %v", err)
	}
	if fakeHasAddress(
		fake,
		netlink.FAMILY_V6,
		"2001:db8:38:1::2/64",
	) {
		t.Fatal("successful SLAAC migration left old static IPv6")
	}
	if !fakeHasAddress(fake, netlink.FAMILY_V6, "2001:db8:38::38/64") {
		t.Fatal("successful SLAAC migration did not apply leased IPv6")
	}
	if sysctl.value("wsm0", "accept_ra") != 0 ||
		sysctl.value("wsm0", "autoconf") != 0 {
		t.Fatal("successful host SLAAC did not disable kernel RA/autoconf")
	}
	if err := dataPlane.Release(
		context.Background(),
		"line-slaac-migrate",
	); err != nil {
		t.Fatalf("Release() error = %v", err)
	}
}

func TestLinuxDataPlaneRetriesPendingLeaseApplication(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 7, 26, 11, 0, 0, 0, time.UTC)
	clock := &fakeClock{now: start}
	fake := newFakeNetlink("wwan-refresh", 39)
	initial := dhcp4Lease(
		start,
		"10.39.0.2",
		"10.39.0.1",
		time.Minute,
		time.Hour,
	)
	refreshed := dhcp4Lease(
		start.Add(time.Minute),
		"10.39.0.2",
		"10.39.0.1",
		30*time.Minute,
		2*time.Hour,
	)
	session := &fakeLeaseSession{
		lease: initial,
		refresh: []leaseRefreshResult{{
			lease: refreshed,
		}},
	}
	dataPlane, err := newLinuxDataPlaneWithDependencies(
		DataPlaneOptions{},
		fake,
		newFakeSysctlController("wwan-refresh"),
		&fakeDynamicAcquirer{ipv4: []acquireResult{{session: session}}},
		clock.Now,
	)
	if err != nil {
		t.Fatalf("newLinuxDataPlaneWithDependencies() error = %v", err)
	}
	timers := newManualTimerFactory()
	dataPlane.newTimer = timers.New
	dataPlane.applyRetryDelay = 5 * time.Second
	if err := dataPlane.Configure(
		context.Background(),
		"line-refresh",
		dhcp4Connection("wwan-refresh"),
	); err != nil {
		t.Fatalf("Configure() error = %v", err)
	}
	first := timers.next(t)
	if first.Duration != time.Minute {
		t.Fatalf("first runtime timer = %s, want 1m", first.Duration)
	}
	fake.routeAddErrors = []error{errors.New("injected refreshed lease apply failure")}
	first.Timer.channel <- clock.Advance(time.Minute)
	retry := timers.next(t)
	if retry.Duration != 5*time.Second {
		t.Fatalf("pending apply retry = %s, want 5s", retry.Duration)
	}
	refreshCalls, _, _ := session.counts()
	if refreshCalls != 1 {
		t.Fatalf("Refresh() calls after first apply failure = %d, want 1", refreshCalls)
	}
	dataPlane.mu.Lock()
	acquiredAt := dataPlane.byLine["line-refresh"].DynamicLeases[0].AcquiredAt
	dataPlane.mu.Unlock()
	if !acquiredAt.Equal(initial.AcquiredAt) {
		t.Fatal("failed refreshed lease application replaced persisted state")
	}
	if fake.linkDownCalls != 0 {
		t.Fatal("pending refreshed lease application brought the interface down")
	}
	retry.Timer.channel <- clock.Advance(5 * time.Second)
	_ = timers.next(t)
	refreshCalls, _, _ = session.counts()
	if refreshCalls != 1 {
		t.Fatalf(
			"pending apply retry called Refresh() again: calls=%d",
			refreshCalls,
		)
	}
	dataPlane.mu.Lock()
	acquiredAt = dataPlane.byLine["line-refresh"].DynamicLeases[0].AcquiredAt
	dataPlane.mu.Unlock()
	if !acquiredAt.Equal(refreshed.AcquiredAt) {
		t.Fatal("pending refreshed lease was not applied on retry")
	}
	if fake.linkDownCalls != 0 {
		t.Fatal("successful refreshed lease replacement brought the interface down")
	}
	if err := dataPlane.Release(context.Background(), "line-refresh"); err != nil {
		t.Fatalf("Release() error = %v", err)
	}
	if fake.linkDownCalls != 1 {
		t.Fatalf("final Release() link-down calls = %d, want 1", fake.linkDownCalls)
	}
}

func TestLinuxDataPlaneSLAACRefreshRetainsPreparation(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 7, 26, 11, 30, 0, 0, time.UTC)
	clock := &fakeClock{now: start}
	fake := newFakeNetlink("wwan-slaac", 43)
	fake.link.Attrs().HardwareAddr = mustHardwareAddr(t, "02:00:00:00:00:43")
	sysctl := newFakeSysctlController("wwan-slaac")
	initial := slaacLease(
		start,
		"fe80::ff:fe00:43",
		"2001:db8:43::43",
		"fe80::1",
		time.Minute,
		time.Hour,
	)
	refreshed := slaacLease(
		start.Add(time.Minute),
		"fe80::ff:fe00:43",
		"2001:db8:43::43",
		"fe80::1",
		30*time.Minute,
		2*time.Hour,
	)
	session := &fakeLeaseSession{
		lease: initial,
		refresh: []leaseRefreshResult{{
			lease: refreshed,
		}},
	}
	dataPlane, err := newLinuxDataPlaneWithDependencies(
		DataPlaneOptions{},
		fake,
		sysctl,
		&fakeDynamicAcquirer{ipv6: []acquireResult{{session: session}}},
		clock.Now,
	)
	if err != nil {
		t.Fatalf("newLinuxDataPlaneWithDependencies() error = %v", err)
	}
	timers := newManualTimerFactory()
	dataPlane.newTimer = timers.New
	if err := dataPlane.Configure(
		context.Background(),
		"line-slaac-refresh",
		slaacConnection("wwan-slaac", ""),
	); err != nil {
		t.Fatalf("Configure() error = %v", err)
	}
	if sysctl.writeCount() != 2 {
		t.Fatalf("initial SLAAC sysctl writes = %d, want 2", sysctl.writeCount())
	}
	first := timers.next(t)
	first.Timer.channel <- clock.Advance(time.Minute)
	_ = timers.next(t)
	if sysctl.writeCount() != 2 {
		t.Fatalf("SLAAC refresh rewrote sysctls: writes=%d", sysctl.writeCount())
	}
	if fake.linkDownCalls != 0 || len(fake.deletedAddrs) != 0 {
		t.Fatalf(
			"SLAAC refresh tore down retained preparation: down=%d deleted=%v",
			fake.linkDownCalls,
			fake.deletedAddrs,
		)
	}
	if err := dataPlane.Release(
		context.Background(),
		"line-slaac-refresh",
	); err != nil {
		t.Fatalf("Release() error = %v", err)
	}
	if sysctl.writeCount() != 4 || fake.linkDownCalls != 1 {
		t.Fatalf(
			"final SLAAC release: sysctl writes=%d link down=%d",
			sysctl.writeCount(),
			fake.linkDownCalls,
		)
	}
}

func TestLinuxDataPlaneExpiresDynamicLease(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 7, 26, 12, 0, 0, 0, time.UTC)
	clock := &fakeClock{now: start}
	fake := newFakeNetlink("wwan-expire", 40)
	lease := dhcp4Lease(
		start,
		"10.40.0.2",
		"10.40.0.1",
		2*time.Minute,
		time.Minute,
	)
	session := &fakeLeaseSession{lease: lease}
	dataPlane, err := newLinuxDataPlaneWithDependencies(
		DataPlaneOptions{StateFile: filepath.Join(t.TempDir(), "network.json")},
		fake,
		newFakeSysctlController("wwan-expire"),
		&fakeDynamicAcquirer{ipv4: []acquireResult{{session: session}}},
		clock.Now,
	)
	if err != nil {
		t.Fatalf("newLinuxDataPlaneWithDependencies() error = %v", err)
	}
	timers := newManualTimerFactory()
	dataPlane.newTimer = timers.New
	if err := dataPlane.Configure(
		context.Background(),
		"line-expire",
		dhcp4Connection("wwan-expire"),
	); err != nil {
		t.Fatalf("Configure() error = %v", err)
	}
	expiry := timers.next(t)
	if expiry.Duration != time.Minute {
		t.Fatalf("expiry timer = %s, want 1m", expiry.Duration)
	}
	expiry.Timer.channel <- clock.Advance(time.Minute)
	deadline := time.Now().Add(2 * time.Second)
	for len(dataPlane.OwnedLines()) != 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if lines := dataPlane.OwnedLines(); len(lines) != 0 {
		t.Fatalf("expired line remained owned: %v", lines)
	}
	if len(fake.existingAddrs[netlink.FAMILY_V4]) != 0 ||
		countFakeRoutes(fake) != 0 ||
		countFakeRules(fake) != 0 {
		t.Fatal("expired lease left netlink state")
	}
	_, releases, closes := session.counts()
	if releases != 0 || closes != 1 {
		t.Fatalf("expiry cleanup: release=%d close=%d, want 0/1", releases, closes)
	}
}

func TestLinuxDataPlaneRebuildsDynamicLeaseAfterRestart(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 7, 26, 13, 0, 0, 0, time.UTC)
	stateFile := filepath.Join(t.TempDir(), "network.json")
	fake := newFakeNetlink("wwan-restart", 41)
	firstSession := &fakeLeaseSession{
		lease: dhcp4Lease(
			start,
			"10.41.0.2",
			"10.41.0.1",
			30*time.Minute,
			time.Hour,
		),
	}
	first, err := newLinuxDataPlaneWithDependencies(
		DataPlaneOptions{StateFile: stateFile},
		fake,
		newFakeSysctlController("wwan-restart"),
		&fakeDynamicAcquirer{ipv4: []acquireResult{{session: firstSession}}},
		func() time.Time { return start },
	)
	if err != nil {
		t.Fatalf("first data plane: %v", err)
	}
	if err := first.Configure(
		context.Background(),
		"line-restart",
		dhcp4Connection("wwan-restart"),
	); err != nil {
		t.Fatalf("first Configure() error = %v", err)
	}
	first.mu.Lock()
	if err := first.stopRuntimeLocked(
		context.Background(),
		"line-restart",
		false,
	); err != nil {
		first.mu.Unlock()
		t.Fatalf("simulate process stop: %v", err)
	}
	first.mu.Unlock()

	secondNow := start.Add(5 * time.Minute)
	secondLease := dhcp4Lease(
		secondNow,
		"10.41.0.3",
		"10.41.0.1",
		30*time.Minute,
		time.Hour,
	)
	secondSession := &fakeLeaseSession{lease: secondLease}
	second, err := newLinuxDataPlaneWithDependencies(
		DataPlaneOptions{StateFile: stateFile},
		fake,
		newFakeSysctlController("wwan-restart"),
		&fakeDynamicAcquirer{ipv4: []acquireResult{{session: secondSession}}},
		func() time.Time { return secondNow },
	)
	if err != nil {
		t.Fatalf("restarted data plane: %v", err)
	}
	if err := second.Configure(
		context.Background(),
		"line-restart",
		dhcp4Connection("wwan-restart"),
	); err != nil {
		t.Fatalf("restarted Configure() error = %v", err)
	}
	if fakeHasAddress(fake, netlink.FAMILY_V4, "10.41.0.2/24") {
		t.Fatal("restart left the prior lease address")
	}
	if !fakeHasAddress(fake, netlink.FAMILY_V4, "10.41.0.3/24") {
		t.Fatal("restart did not apply the reacquired lease")
	}
	second.mu.Lock()
	acquiredAt := second.byLine["line-restart"].DynamicLeases[0].AcquiredAt
	second.mu.Unlock()
	if !acquiredAt.Equal(secondNow) {
		t.Fatalf("restarted lease acquired_at = %s, want %s", acquiredAt, secondNow)
	}
	if err := second.Release(context.Background(), "line-restart"); err != nil {
		t.Fatalf("Release() error = %v", err)
	}
}

func TestLinuxDataPlaneRestoresDynamicPreparationAfterHostRestart(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 7, 26, 13, 30, 0, 0, time.UTC)
	stateFile := filepath.Join(t.TempDir(), "network.json")
	fake := newFakeNetlink("wwan-reboot", 44)
	firstSession := &fakeLeaseSession{
		lease: dhcp4Lease(
			start,
			"10.44.0.2",
			"10.44.0.1",
			30*time.Minute,
			time.Hour,
		),
	}
	first, err := newLinuxDataPlaneWithDependencies(
		DataPlaneOptions{StateFile: stateFile},
		fake,
		newFakeSysctlController("wwan-reboot"),
		&fakeDynamicAcquirer{ipv4: []acquireResult{{session: firstSession}}},
		func() time.Time { return start },
	)
	if err != nil {
		t.Fatalf("first data plane: %v", err)
	}
	if err := first.Configure(
		context.Background(),
		"line-host-restart",
		dhcp4Connection("wwan-reboot"),
	); err != nil {
		t.Fatalf("first Configure() error = %v", err)
	}
	first.mu.Lock()
	if err := first.stopRuntimeLocked(
		context.Background(),
		"line-host-restart",
		false,
	); err != nil {
		first.mu.Unlock()
		t.Fatalf("simulate process stop: %v", err)
	}
	first.mu.Unlock()

	fake.link.Attrs().Flags &^= net.FlagUp
	fake.existingAddrs = make(map[int][]netlink.Addr)
	fake.existingRoutes = make(map[int][]netlink.Route)
	fake.existingRules = make(map[int][]netlink.Rule)
	secondNow := start.Add(5 * time.Minute)
	secondSession := &fakeLeaseSession{
		lease: dhcp4Lease(
			secondNow,
			"10.44.0.3",
			"10.44.0.1",
			30*time.Minute,
			time.Hour,
		),
	}
	acquirer := &fakeDynamicAcquirer{
		acquireIPv4: func(
			_ context.Context,
			_ dynamicRequest,
		) (dynamicLeaseSession, error) {
			if fake.link.Attrs().Flags&net.FlagUp == 0 {
				return nil, errors.New("DHCPv4 acquisition started before link-up")
			}
			return secondSession, nil
		},
	}
	second, err := newLinuxDataPlaneWithDependencies(
		DataPlaneOptions{StateFile: stateFile},
		fake,
		newFakeSysctlController("wwan-reboot"),
		acquirer,
		func() time.Time { return secondNow },
	)
	if err != nil {
		t.Fatalf("restarted data plane: %v", err)
	}
	if err := second.Configure(
		context.Background(),
		"line-host-restart",
		dhcp4Connection("wwan-reboot"),
	); err != nil {
		t.Fatalf("restarted Configure() error = %v", err)
	}
	if !fakeHasAddress(
		fake,
		netlink.FAMILY_V4,
		"10.44.0.3/24",
	) {
		t.Fatal("host restart did not rebuild the DHCPv4 lease")
	}
	if fake.link.Attrs().Flags&net.FlagUp == 0 {
		t.Fatal("host restart left the bearer interface down")
	}
	if err := second.Release(
		context.Background(),
		"line-host-restart",
	); err != nil {
		t.Fatalf("Release() error = %v", err)
	}
	if fake.link.Attrs().Flags&net.FlagUp != 0 {
		t.Fatal("Release did not restore the originally-down interface")
	}
}

func TestLinuxDataPlaneRestoresSLAACPreparationAfterHostRestart(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 7, 26, 13, 45, 0, 0, time.UTC)
	stateFile := filepath.Join(t.TempDir(), "network.json")
	fake := newFakeNetlink("wwan-s6", 45)
	fake.link.Attrs().HardwareAddr = mustHardwareAddr(t, "02:00:00:00:00:45")
	firstSession := &fakeLeaseSession{
		lease: slaacLease(
			start,
			"fe80::45",
			"2001:db8:45::2",
			"fe80::1",
			30*time.Minute,
			time.Hour,
		),
	}
	first, err := newLinuxDataPlaneWithDependencies(
		DataPlaneOptions{StateFile: stateFile},
		fake,
		newFakeSysctlController("wwan-s6"),
		&fakeDynamicAcquirer{ipv6: []acquireResult{{session: firstSession}}},
		func() time.Time { return start },
	)
	if err != nil {
		t.Fatalf("first data plane: %v", err)
	}
	connection := slaacConnection("wwan-s6", "fe80::45")
	if err := first.Configure(
		context.Background(),
		"line-slaac-reboot",
		connection,
	); err != nil {
		t.Fatalf("first Configure() error = %v", err)
	}
	first.mu.Lock()
	if err := first.stopRuntimeLocked(
		context.Background(),
		"line-slaac-reboot",
		false,
	); err != nil {
		first.mu.Unlock()
		t.Fatalf("simulate process stop: %v", err)
	}
	first.mu.Unlock()

	fake.link.Attrs().Flags &^= net.FlagUp
	fake.existingAddrs = make(map[int][]netlink.Addr)
	fake.existingRoutes = make(map[int][]netlink.Route)
	fake.existingRules = make(map[int][]netlink.Rule)
	secondSysctl := newFakeSysctlController("wwan-s6")
	secondNow := start.Add(5 * time.Minute)
	secondSession := &fakeLeaseSession{
		lease: slaacLease(
			secondNow,
			"fe80::45",
			"2001:db8:45::3",
			"fe80::1",
			30*time.Minute,
			time.Hour,
		),
	}
	acquirer := &fakeDynamicAcquirer{
		acquireIPv6: func(
			_ context.Context,
			request dynamicRequest,
		) (dynamicLeaseSession, error) {
			if fake.link.Attrs().Flags&net.FlagUp == 0 {
				return nil, errors.New("SLAAC started before link-up")
			}
			if request.LinkLocal != netip.MustParseAddr("fe80::45") {
				return nil, fmt.Errorf(
					"SLAAC source = %s, want ModemManager fe80::45",
					request.LinkLocal,
				)
			}
			if !fakeHasAddress(
				fake,
				netlink.FAMILY_V6,
				"fe80::45/64",
			) {
				return nil, errors.New("SLAAC started before link-local setup")
			}
			if secondSysctl.value("wwan-s6", "accept_ra") != 0 ||
				secondSysctl.value("wwan-s6", "autoconf") != 0 {
				return nil, errors.New("SLAAC started before sysctl ownership")
			}
			return secondSession, nil
		},
	}
	second, err := newLinuxDataPlaneWithDependencies(
		DataPlaneOptions{StateFile: stateFile},
		fake,
		secondSysctl,
		acquirer,
		func() time.Time { return secondNow },
	)
	if err != nil {
		t.Fatalf("restarted data plane: %v", err)
	}
	if err := second.Configure(
		context.Background(),
		"line-slaac-reboot",
		connection,
	); err != nil {
		t.Fatalf("restarted Configure() error = %v", err)
	}
	if !fakeHasAddress(
		fake,
		netlink.FAMILY_V6,
		"2001:db8:45::3/64",
	) {
		t.Fatal("host restart did not rebuild the SLAAC lease")
	}
	if err := second.Release(
		context.Background(),
		"line-slaac-reboot",
	); err != nil {
		t.Fatalf("Release() error = %v", err)
	}
	if secondSysctl.value("wwan-s6", "accept_ra") != 1 ||
		secondSysctl.value("wwan-s6", "autoconf") != 1 {
		t.Fatal("Release did not restore host SLAAC sysctls")
	}
	if fake.link.Attrs().Flags&net.FlagUp != 0 {
		t.Fatal("Release did not restore the SLAAC interface down")
	}
}

func TestLinuxDataPlaneDoesNotRestoreExternallyChangedSysctl(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 26, 14, 0, 0, 0, time.UTC)
	fake := newFakeNetlink("wwan-sysctl", 42)
	fake.link.Attrs().HardwareAddr = mustHardwareAddr(t, "02:00:00:00:00:42")
	sysctl := newFakeSysctlController("wwan-sysctl")
	session := &fakeLeaseSession{
		lease: slaacLease(
			now,
			"fe80::ff:fe00:42",
			"2001:db8:42::42",
			"fe80::1",
			time.Minute,
			time.Hour,
		),
	}
	dataPlane, err := newLinuxDataPlaneWithDependencies(
		DataPlaneOptions{},
		fake,
		sysctl,
		&fakeDynamicAcquirer{ipv6: []acquireResult{{session: session}}},
		func() time.Time { return now },
	)
	if err != nil {
		t.Fatalf("newLinuxDataPlaneWithDependencies() error = %v", err)
	}
	if err := dataPlane.Configure(
		context.Background(),
		"line-sysctl",
		slaacConnection("wwan-sysctl", ""),
	); err != nil {
		t.Fatalf("Configure() error = %v", err)
	}
	sysctl.set("wwan-sysctl", "accept_ra", 2)
	err = dataPlane.Release(context.Background(), "line-sysctl")
	if err == nil || !strings.Contains(err.Error(), "refuse to restore") {
		t.Fatalf("Release() error = %v, want external sysctl conflict", err)
	}
	if value := sysctl.value("wwan-sysctl", "accept_ra"); value != 2 {
		t.Fatalf("external accept_ra overwritten with %d", value)
	}
}

func newFakeNetlink(interfaceName string, index int) *fakeNetlinkController {
	return &fakeNetlinkController{
		link: &netlink.Dummy{LinkAttrs: netlink.LinkAttrs{
			Index: index,
			Name:  interfaceName,
			MTU:   1500,
		}},
		existingAddrs:  make(map[int][]netlink.Addr),
		existingRoutes: make(map[int][]netlink.Route),
		existingRules:  make(map[int][]netlink.Rule),
	}
}

func mustTestDataPlane(
	t *testing.T,
	controller *fakeNetlinkController,
	acquirer dynamicAcquirer,
	now func() time.Time,
) *linuxDataPlane {
	t.Helper()
	dataPlane, err := newLinuxDataPlaneWithDependencies(
		DataPlaneOptions{},
		controller,
		newFakeSysctlController(controller.link.Attrs().Name),
		acquirer,
		now,
	)
	if err != nil {
		t.Fatalf("newLinuxDataPlaneWithDependencies() error = %v", err)
	}
	return dataPlane
}

func dhcp4Connection(interfaceName string) domain.DataConnection {
	return domain.DataConnection{
		Connected: true,
		Interface: interfaceName,
		IPv4:      domain.IPConfiguration{Method: "dhcp"},
	}
}

func slaacConnection(
	interfaceName string,
	linkLocal string,
) domain.DataConnection {
	return domain.DataConnection{
		Connected: true,
		Interface: interfaceName,
		IPv6: domain.IPConfiguration{
			Method:  "dhcp",
			Address: linkLocal,
			MTU:     1420,
		},
	}
}

func staticIPv6Connection(
	interfaceName string,
	address string,
	gateway string,
) domain.DataConnection {
	return domain.DataConnection{
		Connected: true,
		Interface: interfaceName,
		IPv6: domain.IPConfiguration{
			Method:  "static",
			Address: address,
			Prefix:  64,
			Gateway: gateway,
			MTU:     1420,
		},
	}
}

func dhcp4Lease(
	now time.Time,
	address string,
	router string,
	refreshAfter time.Duration,
	expiresAfter time.Duration,
) dynamicLease {
	prefix := netip.MustParsePrefix(address + "/24")
	return dynamicLease{
		Kind:       dynamicLeaseDHCPv4,
		AcquiredAt: now,
		RefreshAt:  now.Add(refreshAfter),
		RebindAt:   now.Add(expiresAfter * 7 / 8),
		ExpiresAt:  now.Add(expiresAfter),
		Server:     router,
		Router:     router,
		DNS:        []string{"1.1.1.1"},
		MTU:        1400,
		Addresses: []leasedAddress{{
			Prefix:     prefix.String(),
			ValidUntil: now.Add(expiresAfter),
		}},
		Routes: []appliedRoute{
			{
				Family: unix.AF_INET,
				Dst:    prefix.Masked().String(),
				Source: address,
			},
			{
				Family:  unix.AF_INET,
				Gateway: router,
				Source:  address,
			},
		},
	}
}

func slaacLease(
	now time.Time,
	linkLocal string,
	address string,
	router string,
	refreshAfter time.Duration,
	expiresAfter time.Duration,
) dynamicLease {
	prefix := netip.MustParsePrefix(address + "/64")
	return dynamicLease{
		Kind:       dynamicLeaseSLAAC,
		AcquiredAt: now,
		RefreshAt:  now.Add(refreshAfter),
		ExpiresAt:  now.Add(expiresAfter),
		Router:     router,
		LinkLocal:  linkLocal,
		DNS:        []string{"2001:4860:4860::8888"},
		MTU:        1420,
		Addresses: []leasedAddress{{
			Prefix:         prefix.String(),
			PreferredUntil: now.Add(expiresAfter / 2),
			ValidUntil:     now.Add(expiresAfter),
			NoDAD:          true,
		}},
		Routes: []appliedRoute{
			{
				Family: unix.AF_INET6,
				Dst:    prefix.Masked().String(),
				Source: address,
			},
			{
				Family:  unix.AF_INET6,
				Gateway: router,
				Source:  address,
			},
		},
	}
}

func fakeHasAddress(
	fake *fakeNetlinkController,
	family int,
	expected string,
) bool {
	address, err := netlink.ParseAddr(expected)
	if err != nil {
		return false
	}
	for _, candidate := range fake.existingAddrs[family] {
		if candidate.Equal(*address) {
			return true
		}
	}
	return false
}

func mustIPNet(t *testing.T, value string) *net.IPNet {
	t.Helper()
	_, network, err := net.ParseCIDR(value)
	if err != nil {
		t.Fatal(err)
	}
	return network
}

func mustHardwareAddr(t *testing.T, value string) net.HardwareAddr {
	t.Helper()
	address, err := net.ParseMAC(value)
	if err != nil {
		t.Fatal(err)
	}
	return address
}

func assertNoNetlinkWrites(t *testing.T, fake *fakeNetlinkController) {
	t.Helper()
	if fake.linkUpCalls != 0 ||
		fake.linkDownCalls != 0 ||
		len(fake.mtuValues) != 0 ||
		len(fake.addedAddrs) != 0 ||
		len(fake.deletedAddrs) != 0 ||
		len(fake.addedRoutes) != 0 ||
		len(fake.deletedRoutes) != 0 ||
		len(fake.addedRules) != 0 ||
		len(fake.deletedRules) != 0 {
		t.Fatalf("foreign-state conflict performed a netlink write")
	}
}

func countFakeRoutes(fake *fakeNetlinkController) int {
	total := 0
	for _, routes := range fake.existingRoutes {
		total += len(routes)
	}
	return total
}

func countFakeRules(fake *fakeNetlinkController) int {
	total := 0
	for _, rules := range fake.existingRules {
		total += len(rules)
	}
	return total
}
