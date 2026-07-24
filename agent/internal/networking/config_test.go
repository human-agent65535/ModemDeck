package networking

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"

	"github.com/human-agent65535/modemdeck/agent/internal/domain"
)

func TestNormalizeProxySetValidatesSecurityAndConflicts(t *testing.T) {
	valid := validProxyConfiguration()
	normalized, err := normalizeProxySet(domain.ProxyDesiredSet{
		Proxies: []domain.ProxyConfiguration{valid},
	})
	if err != nil {
		t.Fatalf("normalize valid proxy set: %v", err)
	}
	if len(normalized) != 1 ||
		normalized[0].ListenAddress != "127.0.0.1" ||
		normalized[0].Mode != domain.ProxyModeSOCKS5 {
		t.Fatalf("unexpected normalized proxy: %+v", normalized)
	}

	tests := []struct {
		name    string
		mutate  func(*domain.ProxyConfiguration)
		message string
	}{
		{
			name: "privileged port",
			mutate: func(configuration *domain.ProxyConfiguration) {
				configuration.ListenPort = 80
			},
			message: "1024",
		},
		{
			name: "non-loopback without auth",
			mutate: func(configuration *domain.ProxyConfiguration) {
				configuration.ListenAddress = "0.0.0.0"
			},
			message: "auth_enabled",
		},
		{
			name: "missing auth secret",
			mutate: func(configuration *domain.ProxyConfiguration) {
				configuration.AuthEnabled = true
				configuration.Username = "user"
			},
			message: "username and password",
		},
		{
			name: "invalid mode",
			mutate: func(configuration *domain.ProxyConfiguration) {
				configuration.Mode = "transparent"
			},
			message: "socks5 or http",
		},
		{
			name: "invalid id",
			mutate: func(configuration *domain.ProxyConfiguration) {
				configuration.ID = "../proxy"
			},
			message: "id must",
		},
		{
			name: "SOCKS5 password exceeds protocol field",
			mutate: func(configuration *domain.ProxyConfiguration) {
				configuration.AuthEnabled = true
				configuration.Username = "user"
				configuration.Password = strings.Repeat("p", 256)
			},
			message: "255 bytes",
		},
		{
			name: "HTTP Basic username contains separator",
			mutate: func(configuration *domain.ProxyConfiguration) {
				configuration.Mode = domain.ProxyModeHTTP
				configuration.AuthEnabled = true
				configuration.Username = "invalid:user"
				configuration.Password = "password"
			},
			message: "must not contain a colon",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			configuration := valid
			test.mutate(&configuration)
			_, err := normalizeProxySet(domain.ProxyDesiredSet{
				Proxies: []domain.ProxyConfiguration{configuration},
			})
			assertInvalidArgument(t, err, test.message)
		})
	}

	duplicateID := valid
	duplicateID.ListenPort++
	_, err = normalizeProxySet(domain.ProxyDesiredSet{
		Proxies: []domain.ProxyConfiguration{valid, duplicateID},
	})
	assertInvalidArgument(t, err, "duplicate proxy id")

	listenerConflict := valid
	listenerConflict.ID = "second"
	_, err = normalizeProxySet(domain.ProxyDesiredSet{
		Proxies: []domain.ProxyConfiguration{valid, listenerConflict},
	})
	assertInvalidArgument(t, err, "conflicts")
}

func TestResolveBearerRequiresConnectedInterfaceAndDNS(t *testing.T) {
	configuration := domain.DeviceConfiguration{
		DataConnections: []domain.DataConnection{
			{
				ID:        "z",
				Connected: true,
				APNType:   apnTypeDefault,
				Interface: "wwan1",
				IPv4: domain.IPConfiguration{
					DNS: []string{"1.1.1.1", "invalid", "1.1.1.1"},
				},
				IPv6: domain.IPConfiguration{
					DNS: []string{"2606:4700:4700::1111"},
				},
			},
			{
				ID:        "a",
				Connected: true,
				APNType:   1 << 2,
				Interface: "wwan0",
				IPv4: domain.IPConfiguration{
					DNS: []string{"8.8.8.8"},
				},
			},
		},
	}
	selected, err := resolveBearer(configuration)
	if err != nil {
		t.Fatalf("resolve bearer: %v", err)
	}
	if selected.ConnectionID != "z" ||
		selected.Interface != "wwan1" ||
		len(selected.DNS) != 2 ||
		selected.DNS[0] != "1.1.1.1" ||
		selected.DNS[1] != "2606:4700:4700::1111" {
		t.Fatalf("unexpected bearer: %+v", selected)
	}

	configuration.DataConnections[1].APNType = apnTypeDefault
	if _, err := resolveBearer(configuration); err == nil ||
		!strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("ambiguous default bearer error = %v", err)
	}

	configuration.DataConnections[0].Connected = false
	configuration.DataConnections[1].Connected = false
	if _, err := resolveBearer(configuration); err == nil ||
		!strings.Contains(err.Error(), "no connected") {
		t.Fatalf("disconnected bearer error = %v", err)
	}

	configuration.DataConnections = []domain.DataConnection{{
		ID:        "broken",
		Connected: true,
		APNType:   apnTypeDefault,
		Interface: "../../eth0",
		IPv4:      domain.IPConfiguration{DNS: []string{"8.8.8.8"}},
	}}
	if _, err := resolveBearer(configuration); err == nil ||
		!strings.Contains(err.Error(), "unsupported characters") {
		t.Fatalf("invalid interface error = %v", err)
	}

	configuration.DataConnections[0].Interface = "wwan0"
	configuration.DataConnections[0].IPv4.DNS = nil
	if _, err := resolveBearer(configuration); err == nil ||
		!strings.Contains(err.Error(), "no valid DNS") {
		t.Fatalf("missing DNS error = %v", err)
	}

	configuration.DataConnections[0].APNType = 1 << 2
	if _, err := resolveBearer(configuration); err == nil ||
		!strings.Contains(err.Error(), "no default Internet bearer") {
		t.Fatalf("non-Internet bearer error = %v", err)
	}
}

func TestSystemBearerReadinessRejectsMissingInterface(t *testing.T) {
	err := systemBearerReadiness(bearer{
		Interface:    "modemdeck-missing",
		IPv4Expected: true,
	})
	if err == nil || !strings.Contains(err.Error(), "inspect bearer interface") {
		t.Fatalf("missing interface readiness error = %v", err)
	}
}

func TestVerifyBoundRouteRequiresDNSAndGeneralEgress(t *testing.T) {
	selected := bearer{
		Interface: "wwan0",
		DNS:       []string{"8.8.8.8"},
	}
	var attempts []string
	var peers []net.Conn
	dial := func(
		_ context.Context,
		network string,
		address string,
	) (net.Conn, error) {
		attempts = append(attempts, network+" "+address)
		if address != "8.8.8.8:53" {
			return nil, errors.New("network is unreachable")
		}
		connection, peer := net.Pipe()
		peers = append(peers, peer)
		return connection, nil
	}
	t.Cleanup(func() {
		for _, peer := range peers {
			_ = peer.Close()
		}
	})

	err := verifyBoundRouteWithDial(selected, true, false, dial)
	if err == nil || !strings.Contains(err.Error(), "no general outbound route") {
		t.Fatalf("route readiness error = %v", err)
	}
	if len(attempts) != 2 ||
		attempts[0] != "udp4 8.8.8.8:53" ||
		attempts[1] != "udp4 192.0.2.1:9" {
		t.Fatalf("route attempts = %v", attempts)
	}
}

func TestVerifyBoundRouteAllowsUsableFamilyFallback(t *testing.T) {
	selected := bearer{
		Interface: "wwan0",
		DNS:       []string{"2001:4860:4860::8888"},
	}
	var peers []net.Conn
	dial := func(
		_ context.Context,
		network string,
		address string,
	) (net.Conn, error) {
		if network == "udp4" {
			return nil, errors.New("IPv4 route unavailable")
		}
		connection, peer := net.Pipe()
		peers = append(peers, peer)
		return connection, nil
	}
	t.Cleanup(func() {
		for _, peer := range peers {
			_ = peer.Close()
		}
	})

	if err := verifyBoundRouteWithDial(selected, true, true, dial); err != nil {
		t.Fatalf("dual-stack route fallback: %v", err)
	}
}

func validProxyConfiguration() domain.ProxyConfiguration {
	return domain.ProxyConfiguration{
		ID:            "main-proxy",
		LineID:        "line-main",
		Enabled:       true,
		Mode:          domain.ProxyModeSOCKS5,
		ListenAddress: "",
		ListenPort:    1080,
	}
}

func assertInvalidArgument(t *testing.T, err error, message string) {
	t.Helper()
	operationError, ok := domain.AsOperationError(err)
	if !ok || operationError.Code != domain.ErrorInvalidArgument {
		t.Fatalf("error = %v, want invalid_argument", err)
	}
	if !strings.Contains(operationError.Message, message) {
		t.Fatalf("error message = %q, want %q", operationError.Message, message)
	}
}
