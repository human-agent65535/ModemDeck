//go:build linux

package networking

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"net"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"

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
	LinkSetMTU(netlink.Link, int) error
	AddrAdd(netlink.Link, *netlink.Addr) error
	AddrDel(netlink.Link, *netlink.Addr) error
	RouteAdd(*netlink.Route) error
	RouteDel(*netlink.Route) error
	RuleAdd(*netlink.Rule) error
	RuleDel(*netlink.Rule) error
}

type systemNetlink struct{}

func (systemNetlink) LinkByName(name string) (netlink.Link, error) {
	return netlink.LinkByName(name)
}

func (systemNetlink) LinkSetUp(link netlink.Link) error {
	return netlink.LinkSetUp(link)
}

func (systemNetlink) LinkSetMTU(link netlink.Link, mtu int) error {
	return netlink.LinkSetMTU(link, mtu)
}

func (systemNetlink) AddrAdd(link netlink.Link, address *netlink.Addr) error {
	return netlink.AddrAdd(link, address)
}

func (systemNetlink) AddrDel(link netlink.Link, address *netlink.Addr) error {
	return netlink.AddrDel(link, address)
}

func (systemNetlink) RouteAdd(route *netlink.Route) error {
	return netlink.RouteAdd(route)
}

func (systemNetlink) RouteDel(route *netlink.Route) error {
	return netlink.RouteDel(route)
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

type appliedNetwork struct {
	LineID      string         `json:"line_id"`
	Interface   string         `json:"interface"`
	LinkIndex   int            `json:"link_index"`
	Table       int            `json:"table"`
	Mark        uint32         `json:"mark"`
	OriginalMTU int            `json:"original_mtu,omitempty"`
	AppliedMTU  int            `json:"applied_mtu,omitempty"`
	Addresses   []string       `json:"addresses"`
	Routes      []appliedRoute `json:"routes"`
	Rules       []appliedRule  `json:"rules"`
}

type dataPlaneStateDocument struct {
	Version  int              `json:"version"`
	Networks []appliedNetwork `json:"networks"`
}

type linuxDataPlane struct {
	mu        sync.Mutex
	stateFile string
	netlink   netlinkController
	byLine    map[string]appliedNetwork
}

func NewDataPlane(options DataPlaneOptions) (DataPlane, error) {
	return newLinuxDataPlane(options, systemNetlink{})
}

func newLinuxDataPlane(
	options DataPlaneOptions,
	controller netlinkController,
) (*linuxDataPlane, error) {
	if controller == nil {
		return nil, errors.New("netlink controller is required")
	}
	dataPlane := &linuxDataPlane{
		stateFile: strings.TrimSpace(options.StateFile),
		netlink:   controller,
		byLine:    make(map[string]appliedNetwork),
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

	link, err := dataPlane.netlink.LinkByName(connection.Interface)
	if err != nil {
		return fmt.Errorf("find bearer interface %q: %w", connection.Interface, err)
	}
	desired, err := dataPlane.desiredNetwork(lineID, link, connection)
	if err != nil {
		return err
	}
	previous, hadPrevious := dataPlane.byLine[lineID]
	if hadPrevious && previous.Interface == desired.Interface {
		desired.OriginalMTU = previous.OriginalMTU
	}
	sameNetwork := hadPrevious && appliedNetworksEqual(previous, desired)
	if hadPrevious && !sameNetwork {
		if err := dataPlane.remove(linkForApplied(previous, dataPlane.netlink), previous); err != nil {
			return fmt.Errorf("replace previous line network state: %w", err)
		}
	}
	allowExisting := sameNetwork
	if err := dataPlane.apply(link, desired, allowExisting); err != nil {
		var rollbackErr error
		if hadPrevious {
			rollbackErr = dataPlane.apply(
				linkForApplied(previous, dataPlane.netlink),
				previous,
				true,
			)
		}
		return errors.Join(err, rollbackErr)
	}
	if sameNetwork {
		return nil
	}
	next := cloneAppliedNetworks(dataPlane.byLine)
	next[lineID] = desired
	if err := dataPlane.persist(next); err != nil {
		rollbackErr := dataPlane.remove(link, desired)
		if hadPrevious {
			rollbackErr = errors.Join(
				rollbackErr,
				dataPlane.apply(
					linkForApplied(previous, dataPlane.netlink),
					previous,
					true,
				),
			)
		}
		return errors.Join(
			fmt.Errorf("persist applied network state: %w", err),
			rollbackErr,
		)
	}
	dataPlane.byLine = next
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
	applied, found := dataPlane.byLine[lineID]
	if !found {
		return nil
	}
	link, err := dataPlane.netlink.LinkByName(applied.Interface)
	if err != nil && !isMissingNetlinkObject(err) {
		return fmt.Errorf("find bearer interface %q: %w", applied.Interface, err)
	}
	if err != nil {
		link = linkForApplied(applied, dataPlane.netlink)
	}
	if err := dataPlane.remove(link, applied); err != nil {
		return err
	}
	next := cloneAppliedNetworks(dataPlane.byLine)
	delete(next, lineID)
	if err := dataPlane.persist(next); err != nil {
		return fmt.Errorf("persist released network state: %w", err)
	}
	dataPlane.byLine = next
	return nil
}

func (dataPlane *linuxDataPlane) OwnedLines() []string {
	dataPlane.mu.Lock()
	defer dataPlane.mu.Unlock()
	lines := make([]string, 0, len(dataPlane.byLine))
	for lineID := range dataPlane.byLine {
		lines = append(lines, lineID)
	}
	sort.Strings(lines)
	return lines
}

func (dataPlane *linuxDataPlane) desiredNetwork(
	lineID string,
	link netlink.Link,
	connection domain.DataConnection,
) (appliedNetwork, error) {
	table, mark, err := dataPlane.routingIdentity(lineID, connection.Interface)
	if err != nil {
		return appliedNetwork{}, err
	}
	network := appliedNetwork{
		LineID:      lineID,
		Interface:   connection.Interface,
		LinkIndex:   link.Attrs().Index,
		Table:       table,
		Mark:        mark,
		OriginalMTU: link.Attrs().MTU,
		Addresses:   []string{},
		Routes:      []appliedRoute{},
		Rules:       []appliedRule{},
	}
	for _, candidate := range []domain.IPConfiguration{connection.IPv4, connection.IPv6} {
		if err := appendIPConfiguration(&network, candidate); err != nil {
			return appliedNetwork{}, err
		}
		if candidate.MTU > 0 &&
			(network.AppliedMTU == 0 || int(candidate.MTU) < network.AppliedMTU) {
			network.AppliedMTU = int(candidate.MTU)
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

	families := map[int]struct{}{}
	for _, route := range network.Routes {
		families[route.Family] = struct{}{}
	}
	slot := table - routeTableBase
	for family := range families {
		network.Rules = append(network.Rules,
			appliedRule{
				Family:   family,
				Priority: 10000 + slot,
				Mark:     mark,
				Mask:     ^uint32(0),
			},
			appliedRule{
				Family:   family,
				Priority: 30000 + slot,
				OIF:      connection.Interface,
			},
		)
	}
	for _, address := range network.Addresses {
		ip, _, _ := net.ParseCIDR(address)
		family := unix.AF_INET6
		if ip.To4() != nil {
			family = unix.AF_INET
		}
		hostBits := 128
		if ip.To4() != nil {
			hostBits = 32
		}
		network.Rules = append(network.Rules, appliedRule{
			Family:   family,
			Priority: 20000 + slot,
			Source:   (&net.IPNet{IP: ip, Mask: net.CIDRMask(hostBits, hostBits)}).String(),
		})
	}
	sort.Slice(network.Rules, func(i, j int) bool {
		if network.Rules[i].Priority != network.Rules[j].Priority {
			return network.Rules[i].Priority < network.Rules[j].Priority
		}
		return network.Rules[i].Family < network.Rules[j].Family
	})
	return network, nil
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
		case "dhcp":
			return errors.New("DHCP bearer configuration requires a lease client")
		case "ppp":
			return errors.New("PPP bearer configuration requires a PPP session")
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
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(lineID))
	value := hash.Sum32()
	table := routeTableBase + int(value%routeTableSlots)
	mark := fwMarkPrefix | value&^fwMarkMask
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

func (dataPlane *linuxDataPlane) apply(
	link netlink.Link,
	network appliedNetwork,
	allowExisting bool,
) (result error) {
	applied := network
	applied.Addresses = nil
	applied.Routes = nil
	applied.Rules = nil
	applied.AppliedMTU = 0
	defer func() {
		if result != nil {
			result = errors.Join(result, dataPlane.remove(link, applied))
		}
	}()

	if err := dataPlane.netlink.LinkSetUp(link); err != nil {
		return fmt.Errorf("set bearer interface %q up: %w", network.Interface, err)
	}
	if network.AppliedMTU > 0 && network.AppliedMTU != link.Attrs().MTU {
		if err := dataPlane.netlink.LinkSetMTU(link, network.AppliedMTU); err != nil {
			return fmt.Errorf("set bearer interface %q MTU: %w", network.Interface, err)
		}
		applied.AppliedMTU = network.AppliedMTU
	}
	for _, value := range network.Addresses {
		address, err := netlink.ParseAddr(value)
		if err != nil {
			return err
		}
		if err := dataPlane.netlink.AddrAdd(link, address); err != nil {
			if errors.Is(err, unix.EEXIST) && allowExisting {
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
			if errors.Is(err, unix.EEXIST) && allowExisting {
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
			if errors.Is(err, unix.EEXIST) && allowExisting {
				continue
			}
			return fmt.Errorf("configure bearer routing rule %d: %w", value.Priority, err)
		}
		applied.Rules = append(applied.Rules, value)
	}
	return nil
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
	if network.AppliedMTU > 0 &&
		network.OriginalMTU > 0 &&
		link.Attrs().MTU == network.AppliedMTU &&
		network.OriginalMTU != network.AppliedMTU {
		if err := dataPlane.netlink.LinkSetMTU(link, network.OriginalMTU); err != nil &&
			!isMissingNetlinkObject(err) {
			result = errors.Join(result, err)
		}
	}
	return result
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

func appliedNetworksEqual(left appliedNetwork, right appliedNetwork) bool {
	return left.LineID == right.LineID &&
		left.Interface == right.Interface &&
		left.LinkIndex == right.LinkIndex &&
		left.Table == right.Table &&
		left.Mark == right.Mark &&
		left.OriginalMTU == right.OriginalMTU &&
		left.AppliedMTU == right.AppliedMTU &&
		slices.Equal(left.Addresses, right.Addresses) &&
		slices.Equal(left.Routes, right.Routes) &&
		slices.Equal(left.Rules, right.Rules)
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
		if strings.TrimSpace(network.LineID) == "" ||
			validateInterfaceName(network.Interface) != nil ||
			network.LinkIndex <= 0 ||
			network.Table < routeTableBase ||
			network.Table >= routeTableBase+routeTableSlots ||
			network.Mark&fwMarkMask != fwMarkPrefix {
			return errors.New("network state contains an invalid entry")
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
	return nil
}

func (dataPlane *linuxDataPlane) persist(networks map[string]appliedNetwork) error {
	if dataPlane.stateFile == "" {
		return nil
	}
	document := dataPlaneStateDocument{
		Version:  dataPlaneStateVersion,
		Networks: make([]appliedNetwork, 0, len(networks)),
	}
	for _, network := range networks {
		document.Networks = append(document.Networks, network)
	}
	sort.Slice(document.Networks, func(i, j int) bool {
		return document.Networks[i].LineID < document.Networks[j].LineID
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
