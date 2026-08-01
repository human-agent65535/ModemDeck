package networking

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/human-agent65535/modemdeck/agent/internal/domain"
)

const (
	defaultListenAddress  = "127.0.0.1"
	maxProxyCount         = 64
	maxProxyIDLength      = 64
	maxLineIDLength       = 128
	maxUsernameLength     = 128
	maxPasswordLength     = 512
	apnTypeDefault        = domain.APNTypeDefault
	routeReadinessTimeout = time.Second
)

var errNoConnectedDataBearer = errors.New("line has no connected data bearer")

type bearer struct {
	ConnectionID string
	Interface    string
	DNS          []string
	IPv4Expected bool
	IPv6Expected bool
}

func (b bearer) fingerprint() string {
	return b.Interface + "\x00" + strings.Join(b.DNS, "\x00")
}

type bearerReadinessChecker func(bearer) error

func normalizeProxySet(desired domain.ProxyDesiredSet) ([]domain.ProxyConfiguration, error) {
	if len(desired.Proxies) > maxProxyCount {
		return nil, domain.InvalidArgument(
			"apply_proxies",
			fmt.Sprintf("proxies must contain at most %d entries", maxProxyCount),
		)
	}

	normalized := make([]domain.ProxyConfiguration, 0, len(desired.Proxies))
	ids := make(map[string]struct{}, len(desired.Proxies))
	listeners := make(map[string]string, len(desired.Proxies))
	for index, configuration := range desired.Proxies {
		value, err := normalizeProxyConfiguration(configuration)
		if err != nil {
			return nil, domain.InvalidArgument(
				"apply_proxies",
				fmt.Sprintf("proxies[%d]: %s", index, err),
			)
		}
		if _, exists := ids[value.ID]; exists {
			return nil, domain.InvalidArgument(
				"apply_proxies",
				fmt.Sprintf("duplicate proxy id %q", value.ID),
			)
		}
		ids[value.ID] = struct{}{}

		listener := net.JoinHostPort(value.ListenAddress, fmt.Sprintf("%d", value.ListenPort))
		if otherID, exists := listeners[listener]; exists {
			return nil, domain.InvalidArgument(
				"apply_proxies",
				fmt.Sprintf(
					"proxy %q conflicts with proxy %q on listener %s",
					value.ID,
					otherID,
					listener,
				),
			)
		}
		listeners[listener] = value.ID
		normalized = append(normalized, value)
	}
	sort.Slice(normalized, func(i, j int) bool {
		return normalized[i].ID < normalized[j].ID
	})
	return normalized, nil
}

func normalizeProxyConfiguration(
	configuration domain.ProxyConfiguration,
) (domain.ProxyConfiguration, error) {
	configuration.ID = strings.TrimSpace(configuration.ID)
	if !validIdentifier(configuration.ID, maxProxyIDLength) {
		return domain.ProxyConfiguration{}, fmt.Errorf(
			"id must be 1-%d characters using letters, digits, dot, underscore, or hyphen",
			maxProxyIDLength,
		)
	}
	configuration.LineID = strings.TrimSpace(configuration.LineID)
	if configuration.LineID == "" ||
		len(configuration.LineID) > maxLineIDLength ||
		containsControl(configuration.LineID) {
		return domain.ProxyConfiguration{}, fmt.Errorf(
			"line_id must be 1-%d printable characters",
			maxLineIDLength,
		)
	}

	configuration.Mode = domain.ProxyMode(
		strings.ToLower(strings.TrimSpace(string(configuration.Mode))),
	)
	switch configuration.Mode {
	case domain.ProxyModeSOCKS5, domain.ProxyModeHTTP:
	default:
		return domain.ProxyConfiguration{}, fmt.Errorf("mode must be socks5 or http")
	}

	configuration.ListenAddress = strings.TrimSpace(configuration.ListenAddress)
	if configuration.ListenAddress == "" {
		configuration.ListenAddress = defaultListenAddress
	}
	listenIP := net.ParseIP(configuration.ListenAddress)
	if listenIP == nil {
		return domain.ProxyConfiguration{}, fmt.Errorf(
			"listen_address must be an IPv4 or IPv6 literal",
		)
	}
	configuration.ListenAddress = listenIP.String()
	if configuration.ListenPort < 1024 {
		return domain.ProxyConfiguration{}, fmt.Errorf(
			"listen_port must be between 1024 and 65535",
		)
	}

	configuration.Username = strings.TrimSpace(configuration.Username)
	if len(configuration.Username) > maxUsernameLength ||
		containsControl(configuration.Username) {
		return domain.ProxyConfiguration{}, fmt.Errorf(
			"username must be at most %d printable characters",
			maxUsernameLength,
		)
	}
	if len(configuration.Password) > maxPasswordLength ||
		containsControl(configuration.Password) {
		return domain.ProxyConfiguration{}, fmt.Errorf(
			"password must be at most %d printable characters",
			maxPasswordLength,
		)
	}
	if configuration.AuthEnabled &&
		(configuration.Username == "" || configuration.Password == "") {
		return domain.ProxyConfiguration{}, fmt.Errorf(
			"username and password are required when auth_enabled is true",
		)
	}
	if !listenIP.IsLoopback() && !configuration.AuthEnabled {
		return domain.ProxyConfiguration{}, fmt.Errorf(
			"auth_enabled must be true for a non-loopback listen_address",
		)
	}
	if !configuration.AuthEnabled {
		configuration.Username = ""
		configuration.Password = ""
	}
	if err := validateProxyProtocolConfiguration(configuration); err != nil {
		return domain.ProxyConfiguration{}, err
	}
	return configuration, nil
}

func resolveBearer(configuration domain.LineNetworkConfiguration) (bearer, error) {
	connection, err := selectDefaultInternetConnection(configuration)
	if err != nil {
		return bearer{}, err
	}

	selected, err := bearerFromConnection(connection)
	if err != nil {
		return bearer{}, err
	}
	if len(selected.DNS) == 0 {
		return bearer{}, fmt.Errorf(
			"connected default Internet bearer %q has no valid DNS servers",
			connection.ID,
		)
	}
	return selected, nil
}

func selectDefaultInternetConnection(
	configuration domain.LineNetworkConfiguration,
) (domain.DataConnection, error) {
	connected := make([]domain.DataConnection, 0, len(configuration.DataConnections))
	defaults := make([]domain.DataConnection, 0, len(configuration.DataConnections))
	for _, connection := range configuration.DataConnections {
		if !connection.Connected {
			continue
		}
		connected = append(connected, connection)
		if connection.APNType&apnTypeDefault != 0 {
			defaults = append(defaults, connection)
		}
	}
	if len(connected) == 0 {
		return domain.DataConnection{}, errNoConnectedDataBearer
	}
	if len(defaults) == 0 {
		return domain.DataConnection{}, fmt.Errorf(
			"line has connected data bearers but no default Internet bearer",
		)
	}
	if len(defaults) > 1 {
		ids := make([]string, 0, len(defaults))
		for _, connection := range defaults {
			id := strings.TrimSpace(connection.ID)
			if id == "" {
				id = "<unknown>"
			}
			ids = append(ids, id)
		}
		sort.Strings(ids)
		return domain.DataConnection{}, fmt.Errorf(
			"line has ambiguous connected default Internet bearers: %s",
			strings.Join(ids, ", "),
		)
	}
	return defaults[0], nil
}

func bearerFromConnection(connection domain.DataConnection) (bearer, error) {
	if err := validateInterfaceName(connection.Interface); err != nil {
		return bearer{}, err
	}
	ipv4Expected, ipv6Expected := expectedAddressFamilies(connection)
	return bearer{
		ConnectionID: connection.ID,
		Interface:    connection.Interface,
		DNS:          validDNSServers(connection),
		IPv4Expected: ipv4Expected,
		IPv6Expected: ipv6Expected,
	}, nil
}

func expectedAddressFamilies(connection domain.DataConnection) (bool, bool) {
	ipv4Expected := connection.IPFamily == "ipv4" ||
		connection.IPFamily == "ipv4v6" ||
		ipConfigurationPresent(connection.IPv4)
	ipv6Expected := connection.IPFamily == "ipv6" ||
		connection.IPFamily == "ipv4v6" ||
		ipConfigurationPresent(connection.IPv6)
	return ipv4Expected, ipv6Expected
}

func ipConfigurationPresent(configuration domain.IPConfiguration) bool {
	return configuration.Method != "" ||
		configuration.Address != "" ||
		configuration.Gateway != "" ||
		len(configuration.DNS) > 0
}

func systemBearerReadiness(selected bearer) error {
	networkInterface, err := net.InterfaceByName(selected.Interface)
	if err != nil {
		return fmt.Errorf(
			"inspect bearer interface %q: %w",
			selected.Interface,
			err,
		)
	}
	if networkInterface.Flags&net.FlagUp == 0 {
		return fmt.Errorf("bearer interface %q is down", selected.Interface)
	}
	if networkInterface.Flags&net.FlagLoopback != 0 {
		return fmt.Errorf("bearer interface %q is loopback", selected.Interface)
	}

	addresses, err := networkInterface.Addrs()
	if err != nil {
		return fmt.Errorf(
			"read bearer interface %q addresses: %w",
			selected.Interface,
			err,
		)
	}
	hasIPv4 := false
	hasIPv6 := false
	for _, address := range addresses {
		ip, _, parseErr := net.ParseCIDR(address.String())
		if parseErr != nil {
			continue
		}
		if ip.IsUnspecified() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
			continue
		}
		if ip.To4() != nil {
			hasIPv4 = true
		} else {
			hasIPv6 = true
		}
	}
	if !hasIPv4 && !hasIPv6 {
		return fmt.Errorf(
			"bearer interface %q has no usable address",
			selected.Interface,
		)
	}
	if (selected.IPv4Expected || selected.IPv6Expected) &&
		!((selected.IPv4Expected && hasIPv4) || (selected.IPv6Expected && hasIPv6)) {
		return fmt.Errorf(
			"bearer interface %q has no usable address for the bearer IP family",
			selected.Interface,
		)
	}
	if err := verifyBoundRoute(selected, hasIPv4, hasIPv6); err != nil {
		return err
	}
	return nil
}

func verifyBoundRoute(selected bearer, hasIPv4 bool, hasIPv6 bool) error {
	route, err := newBoundRoute(selected)
	if err != nil {
		return fmt.Errorf("build bound route for interface %q: %w", selected.Interface, err)
	}
	return verifyBoundRouteWithDial(selected, hasIPv4, hasIPv6, route.Dial)
}

func verifyBoundRouteWithDial(
	selected bearer,
	hasIPv4 bool,
	hasIPv6 bool,
	dial DialContextFunc,
) error {
	if err := verifyRouteTargets(
		dial,
		dnsRouteTargets(selected.DNS, hasIPv4, hasIPv6),
	); err != nil {
		return fmt.Errorf(
			"bearer interface %q has no bound route to its DNS servers: %w",
			selected.Interface,
			err,
		)
	}
	if err := verifyRouteTargets(
		dial,
		egressRouteTargets(hasIPv4, hasIPv6),
	); err != nil {
		return fmt.Errorf(
			"bearer interface %q has no general outbound route: %w",
			selected.Interface,
			err,
		)
	}
	return nil
}

type routeTarget struct {
	network string
	address string
}

func dnsRouteTargets(
	servers []string,
	hasIPv4 bool,
	hasIPv6 bool,
) []routeTarget {
	targets := make([]routeTarget, 0, len(servers))
	for _, server := range servers {
		ip := net.ParseIP(server)
		if ip == nil {
			continue
		}
		network := "udp6"
		if ip.To4() != nil {
			if !hasIPv4 {
				continue
			}
			network = "udp4"
		} else if !hasIPv6 {
			continue
		}
		targets = append(targets, routeTarget{
			network: network,
			address: net.JoinHostPort(ip.String(), strconv.Itoa(53)),
		})
	}
	return targets
}

func egressRouteTargets(hasIPv4 bool, hasIPv6 bool) []routeTarget {
	targets := make([]routeTarget, 0, 2)
	// UDP connect performs a kernel route lookup without transmitting a packet.
	if hasIPv4 {
		targets = append(targets, routeTarget{
			network: "udp4",
			address: "192.0.2.1:9",
		})
	}
	if hasIPv6 {
		targets = append(targets, routeTarget{
			network: "udp6",
			address: "[2001:db8::1]:9",
		})
	}
	return targets
}

func verifyRouteTargets(
	dial DialContextFunc,
	targets []routeTarget,
) error {
	var routeErrors []string
	for _, target := range targets {
		ctx, cancel := context.WithTimeout(context.Background(), routeReadinessTimeout)
		connection, err := dial(
			ctx,
			target.network,
			target.address,
		)
		cancel()
		if err != nil {
			routeErrors = append(
				routeErrors,
				fmt.Sprintf("%s %s: %v", target.network, target.address, err),
			)
			continue
		}
		if connection == nil {
			routeErrors = append(
				routeErrors,
				fmt.Sprintf("%s %s: dial returned no connection", target.network, target.address),
			)
			continue
		}
		_ = connection.Close()
		return nil
	}
	if len(routeErrors) == 0 {
		return fmt.Errorf("no usable route target")
	}
	return fmt.Errorf("%s", strings.Join(routeErrors, "; "))
}

func validDNSServers(connection domain.DataConnection) []string {
	candidates := append([]string(nil), connection.IPv4.DNS...)
	candidates = append(candidates, connection.IPv6.DNS...)
	seen := make(map[string]struct{}, len(candidates))
	result := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		ip := net.ParseIP(strings.TrimSpace(candidate))
		if ip == nil {
			continue
		}
		canonical := ip.String()
		if _, exists := seen[canonical]; exists {
			continue
		}
		seen[canonical] = struct{}{}
		result = append(result, canonical)
	}
	sort.Strings(result)
	return result
}

func validIdentifier(value string, maxLength int) bool {
	if value == "" || len(value) > maxLength {
		return false
	}
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) ||
			r == '.' || r == '_' || r == '-' {
			continue
		}
		return false
	}
	return true
}

func containsControl(value string) bool {
	for _, r := range value {
		if unicode.IsControl(r) {
			return true
		}
	}
	return false
}
