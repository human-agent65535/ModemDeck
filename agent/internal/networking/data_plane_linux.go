//go:build linux

package networking

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/human-agent65535/modemdeck/agent/internal/domain"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

const (
	dataPlaneStateVersion = 1
	routeTableBase        = 20000
	routeTableSlots       = 10000
	fwMarkPrefix          = uint32(0x4d000000)
	fwMarkMask            = uint32(0xff000000)
)

type netlinkController interface {
	LinkByName(string) (netlink.Link, error)
	LinkSetUp(netlink.Link) error
	LinkSetDown(netlink.Link) error
	LinkSetMTU(netlink.Link, int) error
	AddrList(netlink.Link, int) ([]netlink.Addr, error)
	AddrAdd(netlink.Link, *netlink.Addr) error
	AddrDel(netlink.Link, *netlink.Addr) error
	RouteListFiltered(int, *netlink.Route, uint64) ([]netlink.Route, error)
	RouteAdd(*netlink.Route) error
	RouteDel(*netlink.Route) error
	RuleList(int) ([]netlink.Rule, error)
	RuleAdd(*netlink.Rule) error
	RuleDel(*netlink.Rule) error
}

type systemNetlink struct{}

func (systemNetlink) LinkByName(name string) (netlink.Link, error) {
	return netlink.LinkByName(name)
}

func (systemNetlink) LinkSetUp(link netlink.Link) error {
	if err := netlink.LinkSetUp(link); err != nil {
		return err
	}
	link.Attrs().Flags |= net.FlagUp
	return nil
}

func (systemNetlink) LinkSetDown(link netlink.Link) error {
	if err := netlink.LinkSetDown(link); err != nil {
		return err
	}
	link.Attrs().Flags &^= net.FlagUp
	return nil
}

func (systemNetlink) LinkSetMTU(link netlink.Link, mtu int) error {
	if err := netlink.LinkSetMTU(link, mtu); err != nil {
		return err
	}
	link.Attrs().MTU = mtu
	return nil
}

func (systemNetlink) AddrList(link netlink.Link, family int) ([]netlink.Addr, error) {
	return netlink.AddrList(link, family)
}

func (systemNetlink) AddrAdd(link netlink.Link, address *netlink.Addr) error {
	return netlink.AddrAdd(link, address)
}

func (systemNetlink) AddrDel(link netlink.Link, address *netlink.Addr) error {
	return netlink.AddrDel(link, address)
}

func (systemNetlink) RouteListFiltered(
	family int,
	filter *netlink.Route,
	mask uint64,
) ([]netlink.Route, error) {
	return netlink.RouteListFiltered(family, filter, mask)
}

func (systemNetlink) RouteAdd(route *netlink.Route) error {
	return netlink.RouteAdd(route)
}

func (systemNetlink) RouteDel(route *netlink.Route) error {
	return netlink.RouteDel(route)
}

func (systemNetlink) RuleList(family int) ([]netlink.Rule, error) {
	return netlink.RuleList(family)
}

func (systemNetlink) RuleAdd(rule *netlink.Rule) error {
	return netlink.RuleAdd(rule)
}

func (systemNetlink) RuleDel(rule *netlink.Rule) error {
	return netlink.RuleDel(rule)
}

type appliedRoute struct {
	Family  int    `json:"family"`
	Dst     string `json:"dst,omitempty"`
	Gateway string `json:"gateway,omitempty"`
	Source  string `json:"source,omitempty"`
}

type appliedRule struct {
	Family   int    `json:"family"`
	Priority int    `json:"priority"`
	Mark     uint32 `json:"mark,omitempty"`
	Mask     uint32 `json:"mask,omitempty"`
	Source   string `json:"source,omitempty"`
	OIF      string `json:"oif,omitempty"`
}

type appliedSysctl struct {
	Name     string `json:"name"`
	Original int    `json:"original"`
	Applied  int    `json:"applied"`
}

type appliedNetwork struct {
	LineID          string          `json:"line_id"`
	Interface       string          `json:"interface"`
	LinkIndex       int             `json:"link_index"`
	Table           int             `json:"table"`
	Mark            uint32          `json:"mark"`
	OriginalMTU     int             `json:"original_mtu,omitempty"`
	AppliedMTU      int             `json:"applied_mtu,omitempty"`
	RestoreLinkDown bool            `json:"restore_link_down,omitempty"`
	Addresses       []string        `json:"addresses"`
	AuxiliaryAddrs  []string        `json:"auxiliary_addresses,omitempty"`
	BorrowedAddrs   []string        `json:"borrowed_addresses,omitempty"`
	Routes          []appliedRoute  `json:"routes"`
	Rules           []appliedRule   `json:"rules"`
	Sysctls         []appliedSysctl `json:"sysctls,omitempty"`
	DynamicLeases   []dynamicLease  `json:"dynamic_leases,omitempty"`
}

type pendingNetwork struct {
	LineID          string          `json:"line_id"`
	Interface       string          `json:"interface"`
	LinkIndex       int             `json:"link_index"`
	OriginalMTU     int             `json:"original_mtu,omitempty"`
	RestoreLinkDown bool            `json:"restore_link_down,omitempty"`
	AuxiliaryAddrs  []string        `json:"auxiliary_addresses,omitempty"`
	BorrowedAddrs   []string        `json:"borrowed_addresses,omitempty"`
	Sysctls         []appliedSysctl `json:"sysctls,omitempty"`
}

type dataPlaneStateDocument struct {
	Version  int              `json:"version"`
	Networks []appliedNetwork `json:"networks"`
	Pending  []pendingNetwork `json:"pending,omitempty"`
}

type linuxDataPlane struct {
	mu              sync.Mutex
	stateFile       string
	netlink         netlinkController
	sysctl          sysctlController
	dynamic         dynamicAcquirer
	now             func() time.Time
	newTimer        func(time.Duration) dynamicRuntimeTimer
	applyRetryDelay time.Duration
	byLine          map[string]appliedNetwork
	pending         map[string]pendingNetwork
	runtimes        map[string]*lineDynamicRuntime
}

func NewDataPlane(options DataPlaneOptions) (DataPlane, error) {
	return newLinuxDataPlane(options, systemNetlink{})
}

func newLinuxDataPlane(
	options DataPlaneOptions,
	controller netlinkController,
) (*linuxDataPlane, error) {
	return newLinuxDataPlaneWithDependencies(
		options,
		controller,
		newProcSysctl(),
		newProductionDynamicAcquirer(),
		time.Now,
	)
}

func newLinuxDataPlaneWithDependencies(
	options DataPlaneOptions,
	controller netlinkController,
	sysctl sysctlController,
	dynamic dynamicAcquirer,
	now func() time.Time,
) (*linuxDataPlane, error) {
	if controller == nil {
		return nil, errors.New("netlink controller is required")
	}
	if sysctl == nil {
		return nil, errors.New("sysctl controller is required")
	}
	if dynamic == nil {
		return nil, errors.New("dynamic address acquirer is required")
	}
	if now == nil {
		return nil, errors.New("clock is required")
	}
	dataPlane := &linuxDataPlane{
		stateFile: strings.TrimSpace(options.StateFile),
		netlink:   controller,
		sysctl:    sysctl,
		dynamic:   dynamic,
		now:       now,
		newTimer: func(duration time.Duration) dynamicRuntimeTimer {
			return systemDynamicRuntimeTimer{timer: time.NewTimer(duration)}
		},
		applyRetryDelay: 5 * time.Second,
		byLine:          make(map[string]appliedNetwork),
		pending:         make(map[string]pendingNetwork),
		runtimes:        make(map[string]*lineDynamicRuntime),
	}
	if err := dataPlane.load(); err != nil {
		return nil, err
	}
	return dataPlane, nil
}

func (dataPlane *linuxDataPlane) Configure(
	ctx context.Context,
	lineID string,
	connection domain.DataConnection,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	lineID = strings.TrimSpace(lineID)
	if lineID == "" {
		return errors.New("line id is required")
	}
	if err := validateInterfaceName(connection.Interface); err != nil {
		return err
	}

	dataPlane.mu.Lock()
	defer dataPlane.mu.Unlock()

	if err := dataPlane.recoverPendingLocked(lineID); err != nil {
		return err
	}
	if runtime := dataPlane.runtimes[lineID]; runtime != nil &&
		dynamicConnectionsEqual(runtime.connection, connection) {
		return nil
	}
	if err := dataPlane.stopRuntimeLocked(ctx, lineID, true); err != nil {
		return fmt.Errorf("stop previous dynamic lease: %w", err)
	}
	if err := validateDynamicMethods(connection); err != nil {
		return err
	}
	link, err := dataPlane.netlink.LinkByName(connection.Interface)
	if err != nil {
		return fmt.Errorf("find bearer interface %q: %w", connection.Interface, err)
	}
	previous, hadPrevious := dataPlane.byLine[lineID]
	preparation, preparationOwned, err := dataPlane.prepareDynamicLocked(
		lineID,
		link,
		connection,
		previous,
		hadPrevious,
	)
	if err != nil {
		return err
	}
	sessions, leases, err := dataPlane.acquireDynamicLocked(
		ctx,
		link,
		connection,
		preparation,
	)
	if err != nil {
		rollbackErr := dataPlane.rollbackPreparationLocked(
			lineID,
			preparation,
			preparationOwned,
		)
		return errors.Join(err, rollbackErr)
	}
	releaseSessions := func() error {
		return releaseDynamicSessions(ctx, sessions)
	}
	desired, err := dataPlane.desiredNetwork(lineID, link, connection, leases)
	if err != nil {
		return errors.Join(
			err,
			releaseSessions(),
			dataPlane.rollbackPreparationLocked(
				lineID,
				preparation,
				preparationOwned,
			),
		)
	}
	desired.AuxiliaryAddrs = slices.Clone(preparation.AuxiliaryAddrs)
	desired.BorrowedAddrs = slices.Clone(preparation.BorrowedAddrs)
	desired.Sysctls = slices.Clone(preparation.Sysctls)
	desired.RestoreLinkDown = preparation.RestoreLinkDown
	if hadPrevious && previous.Interface == desired.Interface {
		desired.OriginalMTU = previous.OriginalMTU
	}
	sameNetwork := hadPrevious && appliedNetworksEqual(previous, desired)
	retained := appliedNetwork{}
	if hadPrevious && !sameNetwork {
		removal, kept := splitReplacementState(previous, desired)
		retained = kept
		if err := dataPlane.remove(
			linkForApplied(previous, dataPlane.netlink),
			removal,
		); err != nil {
			return errors.Join(
				fmt.Errorf("replace previous line network state: %w", err),
				releaseSessions(),
				dataPlane.rollbackPreparationLocked(
					lineID,
					preparation,
					preparationOwned,
				),
			)
		}
	}
	var existing *appliedNetwork
	switch {
	case sameNetwork:
		existing = &previous
	default:
		value := retained
		if preparationOwned {
			value = mergeAppliedOwnership(
				value,
				preparation.asAppliedNetwork(),
			)
		}
		if hasAppliedOwnership(value) {
			existing = &value
		}
	}
	if err := dataPlane.apply(link, desired, existing); err != nil {
		var rollbackErr error
		if hadPrevious {
			rollbackErr = dataPlane.apply(
				linkForApplied(previous, dataPlane.netlink),
				previous,
				&previous,
			)
		}
		rollbackErr = errors.Join(rollbackErr, releaseSessions())
		rollbackErr = errors.Join(
			rollbackErr,
			dataPlane.rollbackPreparationLocked(
				lineID,
				preparation,
				preparationOwned,
			),
		)
		return errors.Join(err, rollbackErr)
	}
	if sameNetwork {
		dataPlane.startRuntimeLocked(lineID, connection, sessions)
		return nil
	}
	next := cloneAppliedNetworks(dataPlane.byLine)
	next[lineID] = desired
	nextPending := clonePendingNetworks(dataPlane.pending)
	delete(nextPending, lineID)
	if err := dataPlane.persist(next, nextPending); err != nil {
		rollbackErr := dataPlane.remove(link, desired)
		if hadPrevious {
			rollbackErr = errors.Join(
				rollbackErr,
				dataPlane.apply(
					linkForApplied(previous, dataPlane.netlink),
					previous,
					&previous,
				),
			)
		}
		rollbackErr = errors.Join(rollbackErr, releaseSessions())
		rollbackErr = errors.Join(
			rollbackErr,
			dataPlane.rollbackPreparationLocked(
				lineID,
				preparation,
				preparationOwned,
			),
		)
		return errors.Join(
			fmt.Errorf("persist applied network state: %w", err),
			rollbackErr,
		)
	}
	dataPlane.byLine = next
	dataPlane.pending = nextPending
	dataPlane.startRuntimeLocked(lineID, connection, sessions)
	return nil
}

func (dataPlane *linuxDataPlane) Release(ctx context.Context, lineID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	lineID = strings.TrimSpace(lineID)
	if lineID == "" {
		return errors.New("line id is required")
	}

	dataPlane.mu.Lock()
	defer dataPlane.mu.Unlock()
	runtimeErr := dataPlane.stopRuntimeLocked(ctx, lineID, true)
	pendingErr := dataPlane.recoverPendingLocked(lineID)
	applied, found := dataPlane.byLine[lineID]
	if !found {
		return errors.Join(runtimeErr, pendingErr)
	}
	link, err := dataPlane.netlink.LinkByName(applied.Interface)
	if err != nil && !isMissingNetlinkObject(err) {
		return fmt.Errorf("find bearer interface %q: %w", applied.Interface, err)
	}
	if err != nil {
		link = linkForApplied(applied, dataPlane.netlink)
	}
	if err := dataPlane.remove(link, applied); err != nil {
		return errors.Join(runtimeErr, pendingErr, err)
	}
	next := cloneAppliedNetworks(dataPlane.byLine)
	delete(next, lineID)
	if err := dataPlane.persist(next, dataPlane.pending); err != nil {
		return errors.Join(
			runtimeErr,
			pendingErr,
			fmt.Errorf("persist released network state: %w", err),
		)
	}
	dataPlane.byLine = next
	return errors.Join(runtimeErr, pendingErr)
}

func (dataPlane *linuxDataPlane) OwnedLines() []string {
	dataPlane.mu.Lock()
	defer dataPlane.mu.Unlock()
	lines := make([]string, 0, len(dataPlane.byLine))
	for lineID := range dataPlane.byLine {
		lines = append(lines, lineID)
	}
	for lineID := range dataPlane.pending {
		if _, found := dataPlane.byLine[lineID]; !found {
			lines = append(lines, lineID)
		}
	}
	sort.Strings(lines)
	return lines
}

func validateDynamicMethods(connection domain.DataConnection) error {
	for family, configuration := range map[string]domain.IPConfiguration{
		"IPv4": connection.IPv4,
		"IPv6": connection.IPv6,
	} {
		method := strings.ToLower(strings.TrimSpace(configuration.Method))
		switch method {
		case "", "unknown", "static", "dhcp":
		case "ppp":
			return fmt.Errorf(
				"%w: %s PPP requires a serial TTY plus LCP/IPCP lifecycle; "+
					"it cannot share the netlink bearer ownership model",
				errDynamicNotSupported,
				family,
			)
		default:
			return dynamicMethodError(family, method)
		}
	}
	return nil
}

func dynamicConnectionsEqual(
	left domain.DataConnection,
	right domain.DataConnection,
) bool {
	return left.Interface == right.Interface &&
		ipConfigurationsEqual(left.IPv4, right.IPv4) &&
		ipConfigurationsEqual(left.IPv6, right.IPv6)
}

func ipConfigurationsEqual(
	left domain.IPConfiguration,
	right domain.IPConfiguration,
) bool {
	return strings.EqualFold(strings.TrimSpace(left.Method), strings.TrimSpace(right.Method)) &&
		strings.TrimSpace(left.Address) == strings.TrimSpace(right.Address) &&
		left.Prefix == right.Prefix &&
		strings.TrimSpace(left.Gateway) == strings.TrimSpace(right.Gateway) &&
		left.MTU == right.MTU
}

func (pending pendingNetwork) asAppliedNetwork() appliedNetwork {
	return appliedNetwork{
		LineID:          pending.LineID,
		Interface:       pending.Interface,
		LinkIndex:       pending.LinkIndex,
		OriginalMTU:     pending.OriginalMTU,
		RestoreLinkDown: pending.RestoreLinkDown,
		AuxiliaryAddrs:  slices.Clone(pending.AuxiliaryAddrs),
		BorrowedAddrs:   slices.Clone(pending.BorrowedAddrs),
		Sysctls:         slices.Clone(pending.Sysctls),
	}
}

func (dataPlane *linuxDataPlane) recoverPendingLocked(lineID string) error {
	pending, found := dataPlane.pending[lineID]
	if !found {
		return nil
	}
	applied := pending.asAppliedNetwork()
	if err := dataPlane.remove(
		linkForApplied(applied, dataPlane.netlink),
		applied,
	); err != nil {
		return fmt.Errorf("recover pending dynamic network setup: %w", err)
	}
	nextPending := clonePendingNetworks(dataPlane.pending)
	delete(nextPending, lineID)
	if err := dataPlane.persist(dataPlane.byLine, nextPending); err != nil {
		return fmt.Errorf("persist recovered dynamic network setup: %w", err)
	}
	dataPlane.pending = nextPending
	return nil
}

func (dataPlane *linuxDataPlane) prepareDynamicLocked(
	lineID string,
	link netlink.Link,
	connection domain.DataConnection,
	previous appliedNetwork,
	hadPrevious bool,
) (pendingNetwork, bool, error) {
	ipv4Dynamic := strings.EqualFold(
		strings.TrimSpace(connection.IPv4.Method),
		"dhcp",
	)
	ipv6Dynamic := strings.EqualFold(
		strings.TrimSpace(connection.IPv6.Method),
		"dhcp",
	)
	if !ipv4Dynamic && !ipv6Dynamic {
		return pendingNetwork{}, false, nil
	}
	if hadPrevious &&
		previous.Interface == connection.Interface &&
		(!ipv4Dynamic ||
			hasDynamicLease(previous.DynamicLeases, dynamicLeaseDHCPv4)) &&
		(!ipv6Dynamic ||
			(hasDynamicLease(previous.DynamicLeases, dynamicLeaseSLAAC) &&
				(len(previous.AuxiliaryAddrs) > 0 ||
					len(previous.BorrowedAddrs) > 0))) {
		preparation := pendingNetwork{
			LineID:          lineID,
			Interface:       previous.Interface,
			LinkIndex:       previous.LinkIndex,
			OriginalMTU:     previous.OriginalMTU,
			RestoreLinkDown: previous.RestoreLinkDown,
			AuxiliaryAddrs:  slices.Clone(previous.AuxiliaryAddrs),
			BorrowedAddrs:   slices.Clone(previous.BorrowedAddrs),
			Sysctls:         slices.Clone(previous.Sysctls),
		}
		if borrowedAddressesPresent(
			preparation.BorrowedAddrs,
			link,
			dataPlane.netlink,
		) {
			if err := dataPlane.apply(
				link,
				preparation.asAppliedNetwork(),
				&previous,
			); err != nil {
				return pendingNetwork{}, false, fmt.Errorf(
					"restore dynamic network preparation: %w",
					err,
				)
			}
			return preparation, false, nil
		}
	}
	preparation, err := dataPlane.buildDynamicPreparation(
		lineID,
		link,
		connection,
	)
	if err != nil {
		return pendingNetwork{}, false, err
	}
	if !dynamicPreparationChangesState(preparation) {
		return preparation, false, nil
	}
	nextPending := clonePendingNetworks(dataPlane.pending)
	nextPending[lineID] = preparation
	if err := dataPlane.persist(dataPlane.byLine, nextPending); err != nil {
		return pendingNetwork{}, false, fmt.Errorf(
			"journal host SLAAC preparation: %w",
			err,
		)
	}
	dataPlane.pending = nextPending
	applied := preparation.asAppliedNetwork()
	var owned *appliedNetwork
	if hadPrevious && previous.Interface == preparation.Interface {
		owned = &previous
	}
	if err := dataPlane.apply(link, applied, owned); err != nil {
		rollbackErr := dataPlane.rollbackPreparationLocked(
			lineID,
			preparation,
			true,
		)
		return pendingNetwork{}, false, errors.Join(err, rollbackErr)
	}
	return preparation, true, nil
}

func borrowedAddressesPresent(
	values []string,
	link netlink.Link,
	controller netlinkController,
) bool {
	if len(values) == 0 {
		return true
	}
	addresses, err := controller.AddrList(link, netlink.FAMILY_V6)
	if err != nil {
		return false
	}
	for _, value := range values {
		prefix, err := netip.ParsePrefix(value)
		if err != nil || !containsNetlinkAddress(addresses, prefix.Addr()) {
			return false
		}
	}
	return true
}

func hasDynamicLease(leases []dynamicLease, kind string) bool {
	for _, lease := range leases {
		if lease.Kind == kind {
			return true
		}
	}
	return false
}

func (dataPlane *linuxDataPlane) buildDynamicPreparation(
	lineID string,
	link netlink.Link,
	connection domain.DataConnection,
) (pendingNetwork, error) {
	preparation := pendingNetwork{
		LineID:          lineID,
		Interface:       link.Attrs().Name,
		LinkIndex:       link.Attrs().Index,
		OriginalMTU:     link.Attrs().MTU,
		RestoreLinkDown: link.Attrs().Flags&net.FlagUp == 0,
	}
	if !strings.EqualFold(
		strings.TrimSpace(connection.IPv6.Method),
		"dhcp",
	) {
		return preparation, nil
	}
	addresses, err := dataPlane.netlink.AddrList(link, netlink.FAMILY_V6)
	if err != nil {
		return pendingNetwork{}, fmt.Errorf("list IPv6 interface addresses: %w", err)
	}
	linkLocal, borrowed, err := selectSLAACLinkLocal(
		connection.IPv6,
		link,
		addresses,
	)
	if err != nil {
		return pendingNetwork{}, err
	}
	linkLocalPrefix := netip.PrefixFrom(linkLocal, 64).String()
	if borrowed {
		preparation.BorrowedAddrs = []string{linkLocalPrefix}
	} else {
		preparation.AuxiliaryAddrs = []string{linkLocalPrefix}
	}
	for _, name := range managedIPv6Sysctls {
		original, err := dataPlane.sysctl.Read(link.Attrs().Name, name)
		if err != nil {
			return pendingNetwork{}, err
		}
		preparation.Sysctls = append(preparation.Sysctls, appliedSysctl{
			Name:     name,
			Original: original,
			Applied:  0,
		})
	}
	return preparation, nil
}

func dynamicPreparationChangesState(preparation pendingNetwork) bool {
	if preparation.RestoreLinkDown || len(preparation.AuxiliaryAddrs) > 0 {
		return true
	}
	for _, setting := range preparation.Sysctls {
		if setting.Original != setting.Applied {
			return true
		}
	}
	return false
}

func selectSLAACLinkLocal(
	configuration domain.IPConfiguration,
	link netlink.Link,
	current []netlink.Addr,
) (netip.Addr, bool, error) {
	requested := strings.TrimSpace(configuration.Address)
	if requested != "" {
		address, err := netip.ParseAddr(requested)
		if err != nil || !address.Is6() || !address.IsLinkLocalUnicast() {
			return netip.Addr{}, false, fmt.Errorf(
				"ModemManager host-SLAAC address %q is not link-local IPv6",
				requested,
			)
		}
		return address, containsNetlinkAddress(current, address), nil
	}
	linkLocals := make([]netip.Addr, 0, len(current))
	for _, candidate := range current {
		address, ok := netip.AddrFromSlice(candidate.IP)
		if ok && address.Is6() && address.IsLinkLocalUnicast() {
			linkLocals = append(linkLocals, address.Unmap())
		}
	}
	switch len(linkLocals) {
	case 1:
		return linkLocals[0], true, nil
	case 0:
		address, err := eui64LinkLocal(link.Attrs().HardwareAddr)
		if err != nil {
			return netip.Addr{}, false, err
		}
		return address, false, nil
	default:
		return netip.Addr{}, false, errors.New(
			"host SLAAC found multiple link-local addresses and ModemManager did not select one",
		)
	}
}

func containsNetlinkAddress(addresses []netlink.Addr, expected netip.Addr) bool {
	for _, candidate := range addresses {
		address, ok := netip.AddrFromSlice(candidate.IP)
		if ok && address.Unmap() == expected.Unmap() {
			return true
		}
	}
	return false
}

func eui64LinkLocal(hardware net.HardwareAddr) (netip.Addr, error) {
	if len(hardware) != 6 {
		return netip.Addr{}, errors.New(
			"host SLAAC has no ModemManager link-local address and interface has no Ethernet MAC",
		)
	}
	var value [16]byte
	value[0], value[1] = 0xfe, 0x80
	value[8] = hardware[0] ^ 0x02
	value[9] = hardware[1]
	value[10] = hardware[2]
	value[11], value[12] = 0xff, 0xfe
	value[13] = hardware[3]
	value[14] = hardware[4]
	value[15] = hardware[5]
	return netip.AddrFrom16(value), nil
}

func (dataPlane *linuxDataPlane) acquireDynamicLocked(
	ctx context.Context,
	link netlink.Link,
	connection domain.DataConnection,
	preparation pendingNetwork,
) ([]dynamicLeaseSession, []dynamicLease, error) {
	sessions := []dynamicLeaseSession{}
	fail := func(err error) ([]dynamicLeaseSession, []dynamicLease, error) {
		return nil, nil, errors.Join(err, releaseDynamicSessions(ctx, sessions))
	}
	baseRequest := dynamicRequest{
		Interface: connection.Interface,
		LinkIndex: link.Attrs().Index,
		Hardware:  append(net.HardwareAddr(nil), link.Attrs().HardwareAddr...),
	}
	if strings.EqualFold(strings.TrimSpace(connection.IPv4.Method), "dhcp") {
		session, err := dataPlane.dynamic.AcquireIPv4(ctx, baseRequest)
		if err != nil {
			return fail(err)
		}
		sessions = append(sessions, session)
	}
	if strings.EqualFold(strings.TrimSpace(connection.IPv6.Method), "dhcp") {
		linkLocal, err := preparationLinkLocal(preparation)
		if err != nil {
			return fail(err)
		}
		request := baseRequest
		request.LinkLocal = linkLocal
		request.RequestedMTU = connection.IPv6.MTU
		session, err := dataPlane.dynamic.AcquireIPv6(ctx, request)
		if err != nil {
			return fail(err)
		}
		sessions = append(sessions, session)
	}
	leases := make([]dynamicLease, 0, len(sessions))
	for _, session := range sessions {
		leases = append(leases, session.Lease())
	}
	return sessions, leases, nil
}

func preparationLinkLocal(preparation pendingNetwork) (netip.Addr, error) {
	values := append(
		slices.Clone(preparation.AuxiliaryAddrs),
		preparation.BorrowedAddrs...,
	)
	if len(values) != 1 {
		return netip.Addr{}, errors.New("host SLAAC preparation has no unique link-local address")
	}
	prefix, err := netip.ParsePrefix(values[0])
	if err != nil || !prefix.Addr().IsLinkLocalUnicast() {
		return netip.Addr{}, errors.New("host SLAAC preparation has an invalid link-local address")
	}
	return prefix.Addr(), nil
}

func releaseDynamicSessions(
	ctx context.Context,
	sessions []dynamicLeaseSession,
) error {
	var result error
	for _, session := range sessions {
		if session == nil {
			continue
		}
		result = errors.Join(result, session.Release(ctx), session.Close())
	}
	return result
}

func (dataPlane *linuxDataPlane) rollbackPreparationLocked(
	lineID string,
	preparation pendingNetwork,
	owned bool,
) error {
	if !owned {
		return nil
	}
	applied := preparation.asAppliedNetwork()
	removeErr := dataPlane.remove(
		linkForApplied(applied, dataPlane.netlink),
		applied,
	)
	nextPending := clonePendingNetworks(dataPlane.pending)
	delete(nextPending, lineID)
	persistErr := dataPlane.persist(dataPlane.byLine, nextPending)
	if persistErr == nil {
		dataPlane.pending = nextPending
	}
	return errors.Join(removeErr, persistErr)
}

func (dataPlane *linuxDataPlane) startRuntimeLocked(
	lineID string,
	connection domain.DataConnection,
	sessions []dynamicLeaseSession,
) {
	if len(sessions) == 0 {
		return
	}
	runtimeContext, cancel := context.WithCancel(context.Background())
	runtime := &lineDynamicRuntime{
		connection: cloneDataConnection(connection),
		sessions:   sessions,
		cancel:     cancel,
	}
	dataPlane.runtimes[lineID] = runtime
	go dataPlane.runDynamicRuntime(runtimeContext, lineID, runtime)
}

func cloneDataConnection(connection domain.DataConnection) domain.DataConnection {
	connection.IPv4.DNS = slices.Clone(connection.IPv4.DNS)
	connection.IPv6.DNS = slices.Clone(connection.IPv6.DNS)
	return connection
}

func (dataPlane *linuxDataPlane) stopRuntimeLocked(
	ctx context.Context,
	lineID string,
	release bool,
) error {
	runtime := dataPlane.runtimes[lineID]
	if runtime == nil {
		return nil
	}
	delete(dataPlane.runtimes, lineID)
	runtime.cancel()
	if release {
		return releaseDynamicSessions(ctx, runtime.sessions)
	}
	var result error
	for _, session := range runtime.sessions {
		result = errors.Join(result, session.Close())
	}
	return result
}

func (dataPlane *linuxDataPlane) runDynamicRuntime(
	ctx context.Context,
	lineID string,
	runtime *lineDynamicRuntime,
) {
	appliedLeases := runtimeLeases(runtime)
	applyPending := false
	applyRetryAt := time.Time{}
	refreshRetryAt := make(map[int]time.Time)
	for {
		leases := runtimeLeases(runtime)
		now := dataPlane.now()
		next, expired := nextDynamicRuntimeEvent(
			now,
			leases,
			appliedLeases,
			applyPending,
			applyRetryAt,
			refreshRetryAt,
		)
		if expired {
			dataPlane.expireDynamicRuntime(lineID, runtime)
			return
		}
		wait := next.Sub(now)
		if wait < 0 {
			wait = 0
		}
		timer := dataPlane.newTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C():
		}
		refreshed := false
		now = dataPlane.now()
		for index, session := range runtime.sessions {
			lease := session.Lease()
			refreshAt := lease.RefreshAt
			if retryAt, retrying := refreshRetryAt[index]; retrying {
				refreshAt = retryAt
			}
			if refreshAt.IsZero() || now.Before(refreshAt) {
				continue
			}
			refreshContext, cancel := context.WithTimeout(ctx, 15*time.Second)
			_, err := session.Refresh(refreshContext)
			cancel()
			if err != nil {
				delay := dynamicRetryDelay(dataPlane.now(), lease.ExpiresAt)
				if delay > 0 {
					refreshRetryAt[index] = dataPlane.now().Add(delay)
				}
				continue
			}
			delete(refreshRetryAt, index)
			refreshed = true
		}
		if refreshed {
			applyPending = true
			applyRetryAt = dataPlane.now()
		}
		if applyPending && !dataPlane.now().Before(applyRetryAt) {
			if err := dataPlane.replaceRuntimeLeases(lineID, runtime); err != nil {
				delay := dataPlane.applyRetryDelay
				if delay <= 0 {
					delay = 5 * time.Second
				}
				applyRetryAt = dataPlane.now().Add(delay)
				continue
			}
			applyPending = false
			applyRetryAt = time.Time{}
			appliedLeases = runtimeLeases(runtime)
		}
	}
}

func nextDynamicRuntimeEvent(
	now time.Time,
	leases []dynamicLease,
	appliedLeases []dynamicLease,
	applyPending bool,
	applyRetryAt time.Time,
	refreshRetryAt map[int]time.Time,
) (time.Time, bool) {
	next := time.Time{}
	for index, lease := range leases {
		if !lease.ExpiresAt.After(now) {
			return now, true
		}
		next = earlierTime(next, lease.ExpiresAt)
		refreshAt := lease.RefreshAt
		if retryAt, retrying := refreshRetryAt[index]; retrying {
			refreshAt = retryAt
		}
		if !refreshAt.IsZero() {
			next = earlierTime(next, refreshAt)
		}
	}
	if applyPending {
		for _, lease := range appliedLeases {
			if !lease.ExpiresAt.After(now) {
				return now, true
			}
			next = earlierTime(next, lease.ExpiresAt)
		}
		if applyRetryAt.IsZero() {
			applyRetryAt = now
		}
		next = earlierTime(next, applyRetryAt)
	}
	if next.IsZero() {
		return now.Add(time.Hour), false
	}
	return next, false
}

func earlierTime(current time.Time, candidate time.Time) time.Time {
	if candidate.IsZero() {
		return current
	}
	if current.IsZero() || candidate.Before(current) {
		return candidate
	}
	return current
}

func runtimeLeases(runtime *lineDynamicRuntime) []dynamicLease {
	leases := make([]dynamicLease, 0, len(runtime.sessions))
	for _, session := range runtime.sessions {
		leases = append(leases, session.Lease())
	}
	return leases
}

func nextDynamicEvent(
	now time.Time,
	leases []dynamicLease,
) (time.Time, bool) {
	next := time.Time{}
	for _, lease := range leases {
		if !lease.ExpiresAt.After(now) {
			return now, true
		}
		candidate := lease.RefreshAt
		if candidate.IsZero() || candidate.Before(now) {
			candidate = now
		}
		if next.IsZero() || candidate.Before(next) {
			next = candidate
		}
	}
	if next.IsZero() {
		return now.Add(time.Hour), false
	}
	return next, false
}

func dynamicRetryDelay(now time.Time, expires time.Time) time.Duration {
	remaining := expires.Sub(now)
	if remaining <= 0 {
		return 0
	}
	delay := remaining / 4
	if delay > 30*time.Second {
		delay = 30 * time.Second
	}
	if delay < time.Second {
		delay = time.Second
	}
	return delay
}

func (dataPlane *linuxDataPlane) replaceRuntimeLeases(
	lineID string,
	runtime *lineDynamicRuntime,
) error {
	dataPlane.mu.Lock()
	defer dataPlane.mu.Unlock()
	if dataPlane.runtimes[lineID] != runtime {
		return context.Canceled
	}
	previous, found := dataPlane.byLine[lineID]
	if !found {
		return errors.New("dynamic line no longer owns network state")
	}
	link, err := dataPlane.netlink.LinkByName(previous.Interface)
	if err != nil {
		return err
	}
	desired, err := dataPlane.desiredNetwork(
		lineID,
		link,
		runtime.connection,
		runtimeLeases(runtime),
	)
	if err != nil {
		return err
	}
	desired.OriginalMTU = previous.OriginalMTU
	desired.AuxiliaryAddrs = slices.Clone(previous.AuxiliaryAddrs)
	desired.BorrowedAddrs = slices.Clone(previous.BorrowedAddrs)
	desired.Sysctls = slices.Clone(previous.Sysctls)
	desired.RestoreLinkDown = previous.RestoreLinkDown
	if appliedNetworksEqual(previous, desired) {
		return nil
	}
	removal, retained := splitReplacementState(previous, desired)
	if err := dataPlane.remove(link, removal); err != nil {
		return err
	}
	if err := dataPlane.apply(link, desired, &retained); err != nil {
		return errors.Join(err, dataPlane.apply(link, previous, &previous))
	}
	next := cloneAppliedNetworks(dataPlane.byLine)
	next[lineID] = desired
	if err := dataPlane.persist(next, dataPlane.pending); err != nil {
		return errors.Join(
			err,
			dataPlane.remove(link, desired),
			dataPlane.apply(link, previous, &previous),
		)
	}
	dataPlane.byLine = next
	return nil
}

func (dataPlane *linuxDataPlane) expireDynamicRuntime(
	lineID string,
	runtime *lineDynamicRuntime,
) {
	dataPlane.mu.Lock()
	defer dataPlane.mu.Unlock()
	if dataPlane.runtimes[lineID] != runtime {
		return
	}
	applied, found := dataPlane.byLine[lineID]
	if !found {
		delete(dataPlane.runtimes, lineID)
		return
	}
	if err := dataPlane.remove(
		linkForApplied(applied, dataPlane.netlink),
		applied,
	); err != nil {
		return
	}
	next := cloneAppliedNetworks(dataPlane.byLine)
	delete(next, lineID)
	if err := dataPlane.persist(next, dataPlane.pending); err != nil {
		_ = dataPlane.apply(
			linkForApplied(applied, dataPlane.netlink),
			applied,
			&applied,
		)
		return
	}
	delete(dataPlane.runtimes, lineID)
	dataPlane.byLine = next
	runtime.cancel()
	for _, session := range runtime.sessions {
		_ = session.Close()
	}
}

func (dataPlane *linuxDataPlane) desiredNetwork(
	lineID string,
	link netlink.Link,
	connection domain.DataConnection,
	leases []dynamicLease,
) (appliedNetwork, error) {
	table, mark, err := dataPlane.routingIdentity(lineID, connection.Interface)
	if err != nil {
		return appliedNetwork{}, err
	}
	network := appliedNetwork{
		LineID:        lineID,
		Interface:     connection.Interface,
		LinkIndex:     link.Attrs().Index,
		Table:         table,
		Mark:          mark,
		OriginalMTU:   link.Attrs().MTU,
		Addresses:     []string{},
		Routes:        []appliedRoute{},
		Rules:         []appliedRule{},
		DynamicLeases: slices.Clone(leases),
	}
	for _, candidate := range []domain.IPConfiguration{connection.IPv4, connection.IPv6} {
		method := strings.ToLower(strings.TrimSpace(candidate.Method))
		if method == "dhcp" {
			continue
		}
		if err := appendIPConfiguration(&network, candidate); err != nil {
			return appliedNetwork{}, err
		}
		if candidate.MTU > 0 &&
			(network.AppliedMTU == 0 || int(candidate.MTU) < network.AppliedMTU) {
			network.AppliedMTU = int(candidate.MTU)
		}
	}
	for _, lease := range leases {
		if err := appendDynamicLease(&network, lease, dataPlane.now()); err != nil {
			return appliedNetwork{}, err
		}
	}
	if len(network.Addresses) == 0 {
		return appliedNetwork{}, unsupportedDynamicConfiguration(connection)
	}
	sort.Strings(network.Addresses)
	sort.Slice(network.Routes, func(i, j int) bool {
		left := network.Routes[i]
		right := network.Routes[j]
		return fmt.Sprintf("%d/%s/%s", left.Family, left.Dst, left.Gateway) <
			fmt.Sprintf("%d/%s/%s", right.Family, right.Dst, right.Gateway)
	})

	rules, err := appliedRulesForNetwork(network)
	if err != nil {
		return appliedNetwork{}, err
	}
	network.Rules = rules
	return network, nil
}

func appliedRulesForNetwork(network appliedNetwork) ([]appliedRule, error) {
	families := map[int]struct{}{}
	for _, route := range network.Routes {
		families[route.Family] = struct{}{}
	}
	slot := network.Table - routeTableBase
	rules := make([]appliedRule, 0, len(families)*2+len(network.Addresses))
	for family := range families {
		rules = append(rules,
			appliedRule{
				Family:   family,
				Priority: 10000 + slot,
				Mark:     network.Mark,
				Mask:     ^uint32(0),
			},
			appliedRule{
				Family:   family,
				Priority: 30000 + slot,
				OIF:      network.Interface,
			},
		)
	}
	for _, address := range network.Addresses {
		prefix, err := netip.ParsePrefix(address)
		if err != nil {
			return nil, fmt.Errorf("invalid owned address %q: %w", address, err)
		}
		family := unix.AF_INET6
		hostBits := 128
		if prefix.Addr().Is4() {
			family = unix.AF_INET
			hostBits = 32
		}
		rules = append(rules, appliedRule{
			Family:   family,
			Priority: 20000 + slot,
			Source: netip.PrefixFrom(
				prefix.Addr(),
				hostBits,
			).String(),
		})
	}
	sort.Slice(rules, func(i, j int) bool {
		if rules[i].Priority != rules[j].Priority {
			return rules[i].Priority < rules[j].Priority
		}
		if rules[i].Family != rules[j].Family {
			return rules[i].Family < rules[j].Family
		}
		left := fmt.Sprintf(
			"%08x/%08x/%s/%s",
			rules[i].Mark,
			rules[i].Mask,
			rules[i].Source,
			rules[i].OIF,
		)
		right := fmt.Sprintf(
			"%08x/%08x/%s/%s",
			rules[j].Mark,
			rules[j].Mask,
			rules[j].Source,
			rules[j].OIF,
		)
		return left < right
	})
	return rules, nil
}

func appendDynamicLease(
	network *appliedNetwork,
	lease dynamicLease,
	now time.Time,
) error {
	if lease.ExpiresAt.IsZero() || !lease.ExpiresAt.After(now) {
		return fmt.Errorf("%s lease is already expired", lease.Kind)
	}
	for _, address := range lease.Addresses {
		prefix, err := netip.ParsePrefix(address.Prefix)
		if err != nil {
			return fmt.Errorf("invalid %s lease address %q: %w", lease.Kind, address.Prefix, err)
		}
		if !address.ValidUntil.IsZero() && !address.ValidUntil.After(now) {
			return fmt.Errorf("%s lease address %s is expired", lease.Kind, address.Prefix)
		}
		network.Addresses = append(network.Addresses, prefix.String())
	}
	network.Routes = append(network.Routes, lease.Routes...)
	if lease.MTU > 0 &&
		(network.AppliedMTU == 0 || lease.MTU < network.AppliedMTU) {
		network.AppliedMTU = lease.MTU
	}
	return nil
}

func appendIPConfiguration(
	network *appliedNetwork,
	configuration domain.IPConfiguration,
) error {
	method := strings.ToLower(strings.TrimSpace(configuration.Method))
	address := strings.TrimSpace(configuration.Address)
	if address == "" {
		switch method {
		case "", "unknown":
			return nil
		case "ppp":
			return dynamicMethodError("IP", method)
		default:
			return fmt.Errorf("bearer %s configuration has no address", method)
		}
	}
	ip := net.ParseIP(address)
	if ip == nil {
		return fmt.Errorf("invalid bearer address %q", address)
	}
	bits := 128
	family := unix.AF_INET6
	if ip.To4() != nil {
		bits = 32
		family = unix.AF_INET
		ip = ip.To4()
	}
	prefix := int(configuration.Prefix)
	if prefix < 0 || prefix > bits {
		return fmt.Errorf("invalid prefix %d for bearer address %q", prefix, address)
	}
	if prefix == 0 {
		if bits == 32 {
			prefix = 32
		} else {
			prefix = 128
		}
	}
	cidr := fmt.Sprintf("%s/%d", ip.String(), prefix)
	network.Addresses = append(network.Addresses, cidr)

	_, subnet, _ := net.ParseCIDR(cidr)
	network.Routes = append(network.Routes, appliedRoute{
		Family: family,
		Dst:    subnet.String(),
		Source: ip.String(),
	})
	gateway := strings.TrimSpace(configuration.Gateway)
	if gateway != "" {
		gatewayIP := net.ParseIP(gateway)
		if gatewayIP == nil || (gatewayIP.To4() != nil) != (bits == 32) {
			return fmt.Errorf("invalid bearer gateway %q for address %q", gateway, address)
		}
		gateway = gatewayIP.String()
	}
	network.Routes = append(network.Routes, appliedRoute{
		Family:  family,
		Gateway: gateway,
		Source:  ip.String(),
	})
	return nil
}

func unsupportedDynamicConfiguration(connection domain.DataConnection) error {
	methods := make([]string, 0, 2)
	for _, configuration := range []domain.IPConfiguration{connection.IPv4, connection.IPv6} {
		method := strings.TrimSpace(configuration.Method)
		if method != "" && method != "unknown" {
			methods = append(methods, method)
		}
	}
	if len(methods) == 0 {
		return errors.New("connected bearer did not provide usable IP configuration")
	}
	return fmt.Errorf(
		"connected bearer requires unsupported dynamic configuration: %s",
		strings.Join(methods, ", "),
	)
}

func (dataPlane *linuxDataPlane) routingIdentity(
	lineID string,
	interfaceName string,
) (int, uint32, error) {
	table, mark := routingIdentityForLine(lineID)
	for existingLine, network := range dataPlane.byLine {
		if existingLine == lineID {
			continue
		}
		if network.Table == table || network.Mark == mark {
			return 0, 0, fmt.Errorf(
				"line routing identity collides with %q; choose a different stable line identity",
				existingLine,
			)
		}
		if network.Interface == interfaceName {
			return 0, 0, fmt.Errorf(
				"bearer interface is already owned by line %q",
				existingLine,
			)
		}
	}
	return table, mark, nil
}

func routingIdentityForLine(lineID string) (int, uint32) {
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(lineID))
	value := hash.Sum32()
	table := routeTableBase + int(value%routeTableSlots)
	mark := fwMarkPrefix | value&^fwMarkMask
	return table, mark
}

func (dataPlane *linuxDataPlane) apply(
	link netlink.Link,
	network appliedNetwork,
	owned *appliedNetwork,
) (result error) {
	applied := network
	applied.Addresses = nil
	applied.AuxiliaryAddrs = nil
	applied.BorrowedAddrs = nil
	applied.Routes = nil
	applied.Rules = nil
	applied.Sysctls = nil
	applied.DynamicLeases = nil
	applied.AppliedMTU = 0
	applied.RestoreLinkDown = false
	defer func() {
		if result != nil {
			result = errors.Join(result, dataPlane.remove(link, applied))
		}
	}()

	if err := dataPlane.verifyNoForeignNetworkState(link, network, owned); err != nil {
		return err
	}
	for _, setting := range network.Sysctls {
		current, err := dataPlane.sysctl.Read(network.Interface, setting.Name)
		if err != nil {
			return err
		}
		switch {
		case current == setting.Applied && setting.Original == setting.Applied:
			continue
		case current == setting.Applied && ownsSysctl(owned, setting):
			continue
		case current != setting.Original:
			return fmt.Errorf(
				"IPv6 sysctl %s.%s changed outside this line: current=%d expected=%d",
				network.Interface,
				setting.Name,
				current,
				setting.Original,
			)
		}
		if err := dataPlane.sysctl.Write(
			network.Interface,
			setting.Name,
			setting.Applied,
		); err != nil {
			return err
		}
		applied.Sysctls = append(applied.Sysctls, setting)
	}
	linkWasUp := link.Attrs().Flags&net.FlagUp != 0
	if err := dataPlane.netlink.LinkSetUp(link); err != nil {
		return fmt.Errorf("set bearer interface %q up: %w", network.Interface, err)
	}
	if network.RestoreLinkDown && !linkWasUp {
		applied.RestoreLinkDown = true
	}
	if network.AppliedMTU > 0 && network.AppliedMTU != link.Attrs().MTU {
		if err := dataPlane.netlink.LinkSetMTU(link, network.AppliedMTU); err != nil {
			return fmt.Errorf("set bearer interface %q MTU: %w", network.Interface, err)
		}
		applied.AppliedMTU = network.AppliedMTU
	}
	for _, value := range network.AuxiliaryAddrs {
		if err := dataPlane.addAddress(link, network, value, true); err != nil {
			if errors.Is(err, unix.EEXIST) && ownsAddress(owned, value) {
				continue
			}
			return fmt.Errorf("configure bearer auxiliary address %s: %w", value, err)
		}
		applied.AuxiliaryAddrs = append(applied.AuxiliaryAddrs, value)
	}
	for _, value := range network.Addresses {
		if err := dataPlane.addAddress(link, network, value, false); err != nil {
			if errors.Is(err, unix.EEXIST) && ownsAddress(owned, value) {
				continue
			}
			return fmt.Errorf("configure bearer address %s: %w", value, err)
		}
		applied.Addresses = append(applied.Addresses, value)
	}
	for _, value := range network.Routes {
		route, err := netlinkRoute(link, network.Table, value)
		if err != nil {
			return err
		}
		if err := dataPlane.netlink.RouteAdd(route); err != nil {
			if errors.Is(err, unix.EEXIST) && ownsRoute(owned, value) {
				continue
			}
			return fmt.Errorf("configure bearer route %s: %w", value.Dst, err)
		}
		applied.Routes = append(applied.Routes, value)
	}
	for _, value := range network.Rules {
		rule, err := netlinkRule(network.Table, value)
		if err != nil {
			return err
		}
		if err := dataPlane.netlink.RuleAdd(rule); err != nil {
			if errors.Is(err, unix.EEXIST) && ownsRule(owned, value) {
				continue
			}
			return fmt.Errorf("configure bearer routing rule %d: %w", value.Priority, err)
		}
		applied.Rules = append(applied.Rules, value)
	}
	return nil
}

func (dataPlane *linuxDataPlane) addAddress(
	link netlink.Link,
	network appliedNetwork,
	value string,
	auxiliary bool,
) error {
	address, err := netlink.ParseAddr(value)
	if err != nil {
		return err
	}
	if auxiliary {
		address.Flags |= unix.IFA_F_NODAD
	}
	for _, lease := range network.DynamicLeases {
		for _, candidate := range lease.Addresses {
			if candidate.Prefix != value {
				continue
			}
			now := dataPlane.now()
			address.ValidLft = lifetimeSeconds(now, candidate.ValidUntil)
			address.PreferedLft = lifetimeSeconds(now, candidate.PreferredUntil)
			if candidate.PreferredUntil.IsZero() {
				address.PreferedLft = address.ValidLft
			}
			if candidate.NoDAD {
				address.Flags |= unix.IFA_F_NODAD
			}
		}
	}
	return dataPlane.netlink.AddrAdd(link, address)
}

func lifetimeSeconds(now time.Time, until time.Time) int {
	if until.IsZero() {
		return 0
	}
	remaining := until.Sub(now)
	if remaining <= 0 {
		return 0
	}
	seconds := int(remaining / time.Second)
	if seconds == 0 {
		return 1
	}
	return seconds
}

func (dataPlane *linuxDataPlane) remove(
	link netlink.Link,
	network appliedNetwork,
) error {
	var result error
	for index := len(network.Rules) - 1; index >= 0; index-- {
		rule, err := netlinkRule(network.Table, network.Rules[index])
		if err == nil {
			err = dataPlane.netlink.RuleDel(rule)
		}
		if err != nil && !isMissingNetlinkObject(err) {
			result = errors.Join(result, err)
		}
	}
	for index := len(network.Routes) - 1; index >= 0; index-- {
		route, err := netlinkRoute(link, network.Table, network.Routes[index])
		if err == nil {
			err = dataPlane.netlink.RouteDel(route)
		}
		if err != nil && !isMissingNetlinkObject(err) {
			result = errors.Join(result, err)
		}
	}
	for _, value := range network.Addresses {
		address, err := netlink.ParseAddr(value)
		if err == nil {
			err = dataPlane.netlink.AddrDel(link, address)
		}
		if err != nil && !isMissingNetlinkObject(err) {
			result = errors.Join(result, err)
		}
	}
	for _, value := range network.AuxiliaryAddrs {
		address, err := netlink.ParseAddr(value)
		if err == nil {
			err = dataPlane.netlink.AddrDel(link, address)
		}
		if err != nil && !isMissingNetlinkObject(err) {
			result = errors.Join(result, err)
		}
	}
	if network.AppliedMTU > 0 &&
		network.OriginalMTU > 0 &&
		link.Attrs().MTU == network.AppliedMTU &&
		network.OriginalMTU != network.AppliedMTU {
		if err := dataPlane.netlink.LinkSetMTU(link, network.OriginalMTU); err != nil &&
			!isMissingNetlinkObject(err) {
			result = errors.Join(result, err)
		}
	}
	for index := len(network.Sysctls) - 1; index >= 0; index-- {
		setting := network.Sysctls[index]
		current, err := dataPlane.sysctl.Read(network.Interface, setting.Name)
		if err != nil {
			result = errors.Join(result, err)
			continue
		}
		switch {
		case current == setting.Original:
			continue
		case current != setting.Applied:
			result = errors.Join(result, fmt.Errorf(
				"refuse to restore IPv6 sysctl %s.%s: current=%d owned=%d",
				network.Interface,
				setting.Name,
				current,
				setting.Applied,
			))
		default:
			result = errors.Join(result, dataPlane.sysctl.Write(
				network.Interface,
				setting.Name,
				setting.Original,
			))
		}
	}
	if network.RestoreLinkDown {
		current, err := dataPlane.netlink.LinkByName(network.Interface)
		if err != nil {
			if !isMissingNetlinkObject(err) {
				result = errors.Join(result, err)
			}
		} else if current.Attrs().Flags&net.FlagUp != 0 {
			if err := dataPlane.netlink.LinkSetDown(current); err != nil &&
				!isMissingNetlinkObject(err) {
				result = errors.Join(result, err)
			}
		}
	}
	return result
}

func (dataPlane *linuxDataPlane) verifyNoForeignNetworkState(
	link netlink.Link,
	network appliedNetwork,
	owned *appliedNetwork,
) error {
	families := map[int]struct{}{}
	for _, address := range append(
		slices.Clone(network.Addresses),
		network.AuxiliaryAddrs...,
	) {
		prefix, err := netip.ParsePrefix(address)
		if err != nil {
			return err
		}
		family := netlink.FAMILY_V6
		if prefix.Addr().Is4() {
			family = netlink.FAMILY_V4
		}
		families[family] = struct{}{}
	}
	for _, route := range network.Routes {
		families[route.Family] = struct{}{}
	}
	for _, rule := range network.Rules {
		families[rule.Family] = struct{}{}
	}
	for family := range families {
		addresses, err := dataPlane.netlink.AddrList(link, family)
		if err != nil {
			return fmt.Errorf("inspect existing bearer addresses: %w", err)
		}
		for _, address := range addresses {
			value, ok := netip.AddrFromSlice(address.IP)
			if !ok {
				continue
			}
			if family == netlink.FAMILY_V6 && value.IsLinkLocalUnicast() {
				continue
			}
			if ownsNetlinkAddress(owned, address) {
				continue
			}
			return fmt.Errorf(
				"bearer interface %q has foreign address %s",
				network.Interface,
				address.String(),
			)
		}
		if network.Table == 0 {
			continue
		}
		routes, err := dataPlane.netlink.RouteListFiltered(
			family,
			&netlink.Route{Table: network.Table},
			netlink.RT_FILTER_TABLE,
		)
		if err != nil {
			return fmt.Errorf("inspect owned routing table %d: %w", network.Table, err)
		}
		for _, route := range routes {
			if ownsNetlinkRoute(owned, route) {
				continue
			}
			return fmt.Errorf(
				"routing table %d contains a foreign route",
				network.Table,
			)
		}
		rules, err := dataPlane.netlink.RuleList(family)
		if err != nil {
			return fmt.Errorf("inspect policy rules: %w", err)
		}
		for _, rule := range rules {
			if !ruleConflictsWithNetwork(rule, network) {
				continue
			}
			if ownsNetlinkRule(owned, rule) {
				continue
			}
			return fmt.Errorf(
				"line routing identity conflicts with foreign policy rule priority %d",
				rule.Priority,
			)
		}
	}
	return nil
}

func ownsAddress(network *appliedNetwork, value string) bool {
	if network == nil {
		return false
	}
	return slices.Contains(network.Addresses, value) ||
		slices.Contains(network.AuxiliaryAddrs, value)
}

func ownsRoute(network *appliedNetwork, value appliedRoute) bool {
	return network != nil && slices.Contains(network.Routes, value)
}

func ownsRule(network *appliedNetwork, value appliedRule) bool {
	return network != nil && slices.Contains(network.Rules, value)
}

func ownsSysctl(network *appliedNetwork, value appliedSysctl) bool {
	return network != nil && slices.Contains(network.Sysctls, value)
}

func ownsNetlinkAddress(network *appliedNetwork, address netlink.Addr) bool {
	if network == nil {
		return false
	}
	for _, value := range append(
		slices.Clone(network.Addresses),
		network.AuxiliaryAddrs...,
	) {
		expected, err := netlink.ParseAddr(value)
		if err == nil && expected.Equal(address) {
			return true
		}
	}
	return false
}

func ownsNetlinkRoute(network *appliedNetwork, route netlink.Route) bool {
	if network == nil {
		return false
	}
	link := &netlink.Dummy{LinkAttrs: netlink.LinkAttrs{
		Index: network.LinkIndex,
		Name:  network.Interface,
		MTU:   network.AppliedMTU,
	}}
	for _, value := range network.Routes {
		expected, err := netlinkRoute(link, network.Table, value)
		if err == nil && netlinkRoutesEqual(*expected, route) {
			return true
		}
	}
	return false
}

func netlinkRoutesEqual(left netlink.Route, right netlink.Route) bool {
	return left.LinkIndex == right.LinkIndex &&
		left.Table == right.Table &&
		left.Scope == right.Scope &&
		left.Protocol == right.Protocol &&
		ipNetsEqual(left.Dst, right.Dst) &&
		left.Gw.Equal(right.Gw) &&
		left.Src.Equal(right.Src)
}

func ownsNetlinkRule(network *appliedNetwork, rule netlink.Rule) bool {
	if network == nil {
		return false
	}
	for _, value := range network.Rules {
		expected, err := netlinkRule(network.Table, value)
		if err == nil && netlinkRulesEqual(*expected, rule) {
			return true
		}
	}
	return false
}

func netlinkRulesEqual(left netlink.Rule, right netlink.Rule) bool {
	return left.Table == right.Table &&
		left.Family == right.Family &&
		left.Priority == right.Priority &&
		left.Mark == right.Mark &&
		ruleMasksEqual(left.Mask, right.Mask) &&
		left.OifName == right.OifName &&
		ipNetsEqual(left.Src, right.Src)
}

func ruleMasksEqual(left *uint32, right *uint32) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func ipNetsEqual(left *net.IPNet, right *net.IPNet) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.String() == right.String()
}

func ruleConflictsWithNetwork(rule netlink.Rule, network appliedNetwork) bool {
	if rule.Table == network.Table || rule.Mark == network.Mark {
		return true
	}
	for _, desired := range network.Rules {
		if rule.Priority == desired.Priority {
			return true
		}
	}
	return false
}

func netlinkRoute(
	link netlink.Link,
	table int,
	applied appliedRoute,
) (*netlink.Route, error) {
	route := &netlink.Route{
		LinkIndex: link.Attrs().Index,
		Table:     table,
		Protocol:  unix.RTPROT_STATIC,
	}
	if applied.Dst != "" {
		_, destination, err := net.ParseCIDR(applied.Dst)
		if err != nil {
			return nil, err
		}
		route.Dst = destination
		route.Scope = netlink.SCOPE_LINK
	}
	if applied.Gateway != "" {
		route.Gw = net.ParseIP(applied.Gateway)
		if route.Gw == nil {
			return nil, fmt.Errorf("invalid applied gateway %q", applied.Gateway)
		}
	}
	if applied.Dst == "" && applied.Gateway == "" {
		route.Scope = netlink.SCOPE_LINK
	}
	if applied.Source != "" {
		route.Src = net.ParseIP(applied.Source)
		if route.Src == nil {
			return nil, fmt.Errorf("invalid applied source %q", applied.Source)
		}
	}
	return route, nil
}

func netlinkRule(table int, applied appliedRule) (*netlink.Rule, error) {
	rule := netlink.NewRule()
	rule.Table = table
	rule.Family = applied.Family
	rule.Priority = applied.Priority
	rule.Mark = applied.Mark
	if applied.Mask != 0 {
		mask := applied.Mask
		rule.Mask = &mask
	}
	rule.OifName = applied.OIF
	if applied.Source != "" {
		_, source, err := net.ParseCIDR(applied.Source)
		if err != nil {
			return nil, err
		}
		rule.Src = source
	}
	return rule, nil
}

func linkForApplied(
	applied appliedNetwork,
	controller netlinkController,
) netlink.Link {
	link, err := controller.LinkByName(applied.Interface)
	if err == nil {
		return link
	}
	return &netlink.Dummy{LinkAttrs: netlink.LinkAttrs{
		Index: applied.LinkIndex,
		Name:  applied.Interface,
		MTU:   applied.AppliedMTU,
	}}
}

func isMissingNetlinkObject(err error) bool {
	return errors.Is(err, unix.ENOENT) ||
		errors.Is(err, unix.ENODEV) ||
		errors.Is(err, unix.ESRCH)
}

func cloneAppliedNetworks(source map[string]appliedNetwork) map[string]appliedNetwork {
	cloned := make(map[string]appliedNetwork, len(source))
	for lineID, network := range source {
		cloned[lineID] = network
	}
	return cloned
}

func clonePendingNetworks(source map[string]pendingNetwork) map[string]pendingNetwork {
	cloned := make(map[string]pendingNetwork, len(source))
	for lineID, network := range source {
		cloned[lineID] = network
	}
	return cloned
}

func splitReplacementState(
	previous appliedNetwork,
	desired appliedNetwork,
) (appliedNetwork, appliedNetwork) {
	removed := previous
	retained := appliedNetwork{
		LineID:      previous.LineID,
		Interface:   previous.Interface,
		LinkIndex:   previous.LinkIndex,
		Table:       previous.Table,
		Mark:        previous.Mark,
		OriginalMTU: previous.OriginalMTU,
	}
	removed.Addresses, retained.Addresses = splitOwnedValues(
		previous.Addresses,
		desired.Addresses,
	)
	removed.AuxiliaryAddrs, retained.AuxiliaryAddrs = splitOwnedValues(
		previous.AuxiliaryAddrs,
		desired.AuxiliaryAddrs,
	)
	removed.BorrowedAddrs, retained.BorrowedAddrs = splitOwnedValues(
		previous.BorrowedAddrs,
		desired.BorrowedAddrs,
	)
	removed.Routes, retained.Routes = splitOwnedValues(
		previous.Routes,
		desired.Routes,
	)
	removed.Rules, retained.Rules = splitOwnedValues(
		previous.Rules,
		desired.Rules,
	)
	removed.Sysctls, retained.Sysctls = splitOwnedValues(
		previous.Sysctls,
		desired.Sysctls,
	)
	removed.DynamicLeases = nil
	retained.DynamicLeases = nil
	if previous.AppliedMTU == desired.AppliedMTU &&
		previous.OriginalMTU == desired.OriginalMTU {
		removed.AppliedMTU = 0
		retained.AppliedMTU = previous.AppliedMTU
	}
	if previous.RestoreLinkDown && desired.RestoreLinkDown {
		removed.RestoreLinkDown = false
		retained.RestoreLinkDown = true
	}
	return removed, retained
}

func splitOwnedValues[T comparable](
	previous []T,
	desired []T,
) ([]T, []T) {
	removed := make([]T, 0, len(previous))
	retained := make([]T, 0, len(previous))
	for _, value := range previous {
		if slices.Contains(desired, value) {
			retained = append(retained, value)
		} else {
			removed = append(removed, value)
		}
	}
	return removed, retained
}

func mergeAppliedOwnership(
	base appliedNetwork,
	addition appliedNetwork,
) appliedNetwork {
	if base.Interface == "" {
		base.LineID = addition.LineID
		base.Interface = addition.Interface
		base.LinkIndex = addition.LinkIndex
		base.Table = addition.Table
		base.Mark = addition.Mark
		base.OriginalMTU = addition.OriginalMTU
	}
	base.Addresses = appendUnique(base.Addresses, addition.Addresses)
	base.AuxiliaryAddrs = appendUnique(
		base.AuxiliaryAddrs,
		addition.AuxiliaryAddrs,
	)
	base.BorrowedAddrs = appendUnique(
		base.BorrowedAddrs,
		addition.BorrowedAddrs,
	)
	base.Routes = appendUnique(base.Routes, addition.Routes)
	base.Rules = appendUnique(base.Rules, addition.Rules)
	base.Sysctls = appendUnique(base.Sysctls, addition.Sysctls)
	return base
}

func appendUnique[T comparable](destination []T, values []T) []T {
	for _, value := range values {
		if !slices.Contains(destination, value) {
			destination = append(destination, value)
		}
	}
	return destination
}

func hasAppliedOwnership(network appliedNetwork) bool {
	return len(network.Addresses) > 0 ||
		len(network.AuxiliaryAddrs) > 0 ||
		len(network.Routes) > 0 ||
		len(network.Rules) > 0 ||
		len(network.Sysctls) > 0
}

func appliedNetworksEqual(left appliedNetwork, right appliedNetwork) bool {
	return left.LineID == right.LineID &&
		left.Interface == right.Interface &&
		left.LinkIndex == right.LinkIndex &&
		left.Table == right.Table &&
		left.Mark == right.Mark &&
		left.OriginalMTU == right.OriginalMTU &&
		left.AppliedMTU == right.AppliedMTU &&
		left.RestoreLinkDown == right.RestoreLinkDown &&
		slices.Equal(left.Addresses, right.Addresses) &&
		slices.Equal(left.AuxiliaryAddrs, right.AuxiliaryAddrs) &&
		slices.Equal(left.BorrowedAddrs, right.BorrowedAddrs) &&
		slices.Equal(left.Routes, right.Routes) &&
		slices.Equal(left.Rules, right.Rules) &&
		slices.Equal(left.Sysctls, right.Sysctls) &&
		dynamicLeasesEqual(left.DynamicLeases, right.DynamicLeases)
}

func (dataPlane *linuxDataPlane) load() error {
	if dataPlane.stateFile == "" {
		return nil
	}
	content, err := os.ReadFile(dataPlane.stateFile)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	document := dataPlaneStateDocument{}
	if err := json.Unmarshal(content, &document); err != nil {
		return fmt.Errorf("decode network state: %w", err)
	}
	if document.Version != dataPlaneStateVersion {
		return fmt.Errorf("unsupported network state version %d", document.Version)
	}
	tables := make(map[int]string, len(document.Networks))
	marks := make(map[uint32]string, len(document.Networks))
	interfaces := make(map[string]string, len(document.Networks))
	for _, network := range document.Networks {
		if err := validateAppliedNetworkState(network); err != nil {
			return err
		}
		if _, duplicate := dataPlane.byLine[network.LineID]; duplicate {
			return fmt.Errorf("network state contains duplicate line %q", network.LineID)
		}
		if owner, duplicate := tables[network.Table]; duplicate {
			return fmt.Errorf(
				"network state table %d is shared by %q and %q",
				network.Table,
				owner,
				network.LineID,
			)
		}
		if owner, duplicate := marks[network.Mark]; duplicate {
			return fmt.Errorf(
				"network state mark %d is shared by %q and %q",
				network.Mark,
				owner,
				network.LineID,
			)
		}
		if owner, duplicate := interfaces[network.Interface]; duplicate {
			return fmt.Errorf(
				"network state interface %q is shared by %q and %q",
				network.Interface,
				owner,
				network.LineID,
			)
		}
		dataPlane.byLine[network.LineID] = network
		tables[network.Table] = network.LineID
		marks[network.Mark] = network.LineID
		interfaces[network.Interface] = network.LineID
	}
	for _, pending := range document.Pending {
		if err := validatePendingNetwork(pending); err != nil {
			return err
		}
		if _, duplicate := dataPlane.pending[pending.LineID]; duplicate {
			return fmt.Errorf(
				"network state contains duplicate pending line %q",
				pending.LineID,
			)
		}
		if network, found := dataPlane.byLine[pending.LineID]; found &&
			network.Interface != pending.Interface {
			return fmt.Errorf(
				"pending line %q changed interface",
				pending.LineID,
			)
		}
		dataPlane.pending[pending.LineID] = pending
	}
	return nil
}

func validateAppliedNetworkState(network appliedNetwork) error {
	if strings.TrimSpace(network.LineID) == "" ||
		validateInterfaceName(network.Interface) != nil ||
		network.LinkIndex <= 0 {
		return errors.New("network state contains an invalid entry")
	}
	expectedTable, expectedMark := routingIdentityForLine(network.LineID)
	if network.Table != expectedTable || network.Mark != expectedMark {
		return fmt.Errorf(
			"network state for line %q has an invalid routing identity",
			network.LineID,
		)
	}
	if !validPersistedMTU(network.OriginalMTU, false) ||
		!validPersistedMTU(network.AppliedMTU, true) {
		return fmt.Errorf(
			"network state for line %q has an invalid MTU",
			network.LineID,
		)
	}
	seenAddresses := make(map[string]struct{})
	for _, address := range network.Addresses {
		if err := validatePersistedAddress(
			address,
			false,
			seenAddresses,
		); err != nil {
			return fmt.Errorf(
				"network state for line %q: %w",
				network.LineID,
				err,
			)
		}
	}
	for _, address := range network.AuxiliaryAddrs {
		if err := validatePersistedAddress(
			address,
			true,
			seenAddresses,
		); err != nil {
			return fmt.Errorf(
				"network state for line %q: %w",
				network.LineID,
				err,
			)
		}
	}
	for _, address := range network.BorrowedAddrs {
		if err := validatePersistedAddress(
			address,
			true,
			seenAddresses,
		); err != nil {
			return fmt.Errorf(
				"network state for line %q: %w",
				network.LineID,
				err,
			)
		}
	}
	seenRoutes := make(map[appliedRoute]struct{})
	for _, route := range network.Routes {
		if _, duplicate := seenRoutes[route]; duplicate {
			return fmt.Errorf(
				"network state for line %q contains a duplicate route",
				network.LineID,
			)
		}
		seenRoutes[route] = struct{}{}
		if err := validatePersistedRoute(route); err != nil {
			return fmt.Errorf(
				"network state for line %q: %w",
				network.LineID,
				err,
			)
		}
	}
	expectedRules, err := appliedRulesForNetwork(network)
	if err != nil {
		return fmt.Errorf(
			"network state for line %q has invalid rules: %w",
			network.LineID,
			err,
		)
	}
	if !slices.Equal(network.Rules, expectedRules) {
		return fmt.Errorf(
			"network state for line %q contains rules outside its routing identity",
			network.LineID,
		)
	}
	if err := validatePersistedSysctls(network.Sysctls); err != nil {
		return fmt.Errorf(
			"network state for line %q: %w",
			network.LineID,
			err,
		)
	}
	if err := validatePersistedDynamicLeases(network); err != nil {
		return fmt.Errorf(
			"network state for line %q: %w",
			network.LineID,
			err,
		)
	}
	return nil
}

func validPersistedMTU(value int, optional bool) bool {
	if optional && value == 0 {
		return true
	}
	return value >= 68 && value <= 65535
}

func validatePersistedAddress(
	value string,
	linkLocal bool,
	seen map[string]struct{},
) error {
	prefix, err := netip.ParsePrefix(value)
	if err != nil || prefix.String() != value {
		return fmt.Errorf("network state contains invalid address %q", value)
	}
	address := prefix.Addr()
	if !address.IsValid() || address.IsUnspecified() || address.IsMulticast() {
		return fmt.Errorf("network state contains invalid address %q", value)
	}
	if linkLocal {
		if !address.Is6() ||
			!address.IsLinkLocalUnicast() ||
			prefix.Bits() != 64 {
			return fmt.Errorf(
				"network state contains invalid SLAAC link-local address %q",
				value,
			)
		}
	} else if address.IsLinkLocalUnicast() {
		return fmt.Errorf(
			"network state contains unexpected owned link-local address %q",
			value,
		)
	}
	if _, duplicate := seen[value]; duplicate {
		return fmt.Errorf("network state contains duplicate address %q", value)
	}
	seen[value] = struct{}{}
	return nil
}

func validatePersistedRoute(route appliedRoute) error {
	if route.Family != unix.AF_INET && route.Family != unix.AF_INET6 {
		return fmt.Errorf("network state contains invalid route family %d", route.Family)
	}
	ipv4 := route.Family == unix.AF_INET
	if route.Dst != "" {
		destination, err := netip.ParsePrefix(route.Dst)
		if err != nil ||
			destination.Masked().String() != route.Dst ||
			destination.Addr().Is4() != ipv4 {
			return fmt.Errorf(
				"network state contains invalid route destination %q",
				route.Dst,
			)
		}
	}
	for label, value := range map[string]string{
		"gateway": route.Gateway,
		"source":  route.Source,
	} {
		if value == "" {
			continue
		}
		address, err := netip.ParseAddr(value)
		if err != nil ||
			address.Is4() != ipv4 ||
			address.IsUnspecified() ||
			address.IsMulticast() {
			return fmt.Errorf(
				"network state contains invalid route %s %q",
				label,
				value,
			)
		}
	}
	if route.Source == "" {
		return errors.New("network state contains a route without an owned source")
	}
	return nil
}

func validatePersistedSysctls(settings []appliedSysctl) error {
	seen := make(map[string]struct{}, len(settings))
	for _, setting := range settings {
		if !isManagedIPv6Sysctl(setting.Name) {
			return fmt.Errorf(
				"network state contains unmanaged sysctl %q",
				setting.Name,
			)
		}
		if _, duplicate := seen[setting.Name]; duplicate {
			return fmt.Errorf(
				"network state contains duplicate sysctl %q",
				setting.Name,
			)
		}
		seen[setting.Name] = struct{}{}
		if setting.Applied != 0 ||
			(setting.Name == "accept_ra" &&
				(setting.Original < 0 || setting.Original > 2)) ||
			(setting.Name == "autoconf" &&
				(setting.Original < 0 || setting.Original > 1)) {
			return fmt.Errorf(
				"network state contains invalid sysctl values for %q",
				setting.Name,
			)
		}
	}
	return nil
}

func validatePersistedDynamicLeases(network appliedNetwork) error {
	kinds := make(map[string]struct{}, len(network.DynamicLeases))
	hasSLAAC := false
	for _, lease := range network.DynamicLeases {
		if _, duplicate := kinds[lease.Kind]; duplicate {
			return fmt.Errorf(
				"network state contains duplicate dynamic lease %q",
				lease.Kind,
			)
		}
		kinds[lease.Kind] = struct{}{}
		family := unix.AF_INET
		switch lease.Kind {
		case dynamicLeaseDHCPv4:
		case dynamicLeaseSLAAC:
			family = unix.AF_INET6
			hasSLAAC = true
		default:
			return fmt.Errorf(
				"network state contains unknown dynamic lease %q",
				lease.Kind,
			)
		}
		if lease.AcquiredAt.IsZero() ||
			!lease.ExpiresAt.After(lease.AcquiredAt) ||
			(!lease.RefreshAt.IsZero() &&
				!lease.RefreshAt.After(lease.AcquiredAt)) ||
			(!lease.RebindAt.IsZero() &&
				!lease.RebindAt.After(lease.AcquiredAt)) {
			return fmt.Errorf(
				"network state contains invalid %s lease lifetime",
				lease.Kind,
			)
		}
		if !validPersistedMTU(lease.MTU, true) {
			return fmt.Errorf(
				"network state contains invalid %s lease MTU",
				lease.Kind,
			)
		}
		if len(lease.Addresses) == 0 {
			return fmt.Errorf(
				"network state contains %s lease without addresses",
				lease.Kind,
			)
		}
		for _, address := range lease.Addresses {
			prefix, err := netip.ParsePrefix(address.Prefix)
			if err != nil ||
				prefix.String() != address.Prefix ||
				(prefix.Addr().Is4() != (family == unix.AF_INET)) ||
				address.ValidUntil.IsZero() ||
				!address.ValidUntil.After(lease.AcquiredAt) ||
				(!address.PreferredUntil.IsZero() &&
					address.PreferredUntil.After(address.ValidUntil)) ||
				(address.NoDAD && family != unix.AF_INET6) ||
				!slices.Contains(network.Addresses, address.Prefix) {
				return fmt.Errorf(
					"network state contains invalid %s leased address %q",
					lease.Kind,
					address.Prefix,
				)
			}
		}
		for _, route := range lease.Routes {
			if route.Family != family ||
				!slices.Contains(network.Routes, route) {
				return fmt.Errorf(
					"network state contains invalid %s leased route",
					lease.Kind,
				)
			}
		}
	}
	linkLocals := append(
		slices.Clone(network.AuxiliaryAddrs),
		network.BorrowedAddrs...,
	)
	if hasSLAAC {
		if len(linkLocals) != 1 ||
			len(network.Sysctls) != len(managedIPv6Sysctls) {
			return errors.New(
				"network state contains incomplete host SLAAC ownership",
			)
		}
		slaacLinkLocal := ""
		for _, lease := range network.DynamicLeases {
			if lease.Kind == dynamicLeaseSLAAC {
				slaacLinkLocal = lease.LinkLocal
				break
			}
		}
		prefix, _ := netip.ParsePrefix(linkLocals[0])
		linkLocal, err := netip.ParseAddr(slaacLinkLocal)
		if err != nil || prefix.Addr() != linkLocal {
			return errors.New(
				"network state host SLAAC link-local does not match its lease",
			)
		}
	} else if len(linkLocals) != 0 || len(network.Sysctls) != 0 {
		return errors.New(
			"network state contains host SLAAC preparation without a lease",
		)
	}
	return nil
}

func validatePendingNetwork(pending pendingNetwork) error {
	if strings.TrimSpace(pending.LineID) == "" ||
		validateInterfaceName(pending.Interface) != nil ||
		pending.LinkIndex <= 0 ||
		!validPersistedMTU(pending.OriginalMTU, false) {
		return errors.New("network state contains an invalid pending entry")
	}
	if err := validatePersistedSysctls(pending.Sysctls); err != nil {
		return err
	}
	seenAddresses := make(map[string]struct{})
	for _, address := range append(
		slices.Clone(pending.AuxiliaryAddrs),
		pending.BorrowedAddrs...,
	) {
		if err := validatePersistedAddress(
			address,
			true,
			seenAddresses,
		); err != nil {
			return err
		}
	}
	if len(pending.Sysctls) > 0 &&
		(len(seenAddresses) != 1 ||
			len(pending.Sysctls) != len(managedIPv6Sysctls)) {
		return errors.New(
			"network state contains incomplete pending host SLAAC ownership",
		)
	}
	return nil
}

func (dataPlane *linuxDataPlane) persist(
	networks map[string]appliedNetwork,
	pending map[string]pendingNetwork,
) error {
	if dataPlane.stateFile == "" {
		return nil
	}
	document := dataPlaneStateDocument{
		Version:  dataPlaneStateVersion,
		Networks: make([]appliedNetwork, 0, len(networks)),
		Pending:  make([]pendingNetwork, 0, len(pending)),
	}
	for _, network := range networks {
		document.Networks = append(document.Networks, network)
	}
	sort.Slice(document.Networks, func(i, j int) bool {
		return document.Networks[i].LineID < document.Networks[j].LineID
	})
	for _, network := range pending {
		document.Pending = append(document.Pending, network)
	}
	sort.Slice(document.Pending, func(i, j int) bool {
		return document.Pending[i].LineID < document.Pending[j].LineID
	})
	content, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return err
	}
	content = append(content, '\n')
	directory := filepath.Dir(dataPlane.stateFile)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(directory, ".network-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(content); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, dataPlane.stateFile); err != nil {
		return err
	}
	directoryHandle, err := os.Open(directory)
	if err != nil {
		return err
	}
	defer directoryHandle.Close()
	return directoryHandle.Sync()
}

var _ DataPlane = (*linuxDataPlane)(nil)
