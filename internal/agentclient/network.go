package agentclient

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"
	"unicode"
)

const (
	maxProxyConfigurations = 64
	maxProxyIDBytes        = 64
	maxProxyLineIDBytes    = 128
	maxProxyUsernameBytes  = 128
	maxProxyPasswordBytes  = 512
)

type ProxyMode string

const (
	ProxyModeSOCKS5 ProxyMode = "socks5"
	ProxyModeHTTP   ProxyMode = "http"
)

type ProxyRuntimeState string

const (
	ProxyStateDisabled         ProxyRuntimeState = "disabled"
	ProxyStateWaitingForBearer ProxyRuntimeState = "waiting_for_bearer"
	ProxyStateRunning          ProxyRuntimeState = "running"
	ProxyStateError            ProxyRuntimeState = "error"
)

type ProxyConfiguration struct {
	ID            string    `json:"id"`
	LineID        string    `json:"line_id"`
	Enabled       bool      `json:"enabled"`
	Mode          ProxyMode `json:"mode"`
	ListenAddress string    `json:"listen_address"`
	ListenPort    uint16    `json:"listen_port"`
	AuthEnabled   bool      `json:"auth_enabled"`
	Username      string    `json:"username,omitempty"`
	Password      string    `json:"password,omitempty"`
}

type NetworkLine struct {
	LineID    string   `json:"line_id"`
	Connected bool     `json:"connected"`
	Interface string   `json:"interface"`
	Addresses []string `json:"addresses"`
	DNS       []string `json:"dns"`
	RXBytes   uint64   `json:"rx_bytes"`
	TXBytes   uint64   `json:"tx_bytes"`
	Error     string   `json:"error"`
}

type NetworkProxy struct {
	ID                string            `json:"id"`
	LineID            string            `json:"line_id"`
	State             ProxyRuntimeState `json:"state"`
	Running           bool              `json:"running"`
	Mode              ProxyMode         `json:"mode"`
	ListenAddress     string            `json:"listen_address"`
	ListenPort        uint16            `json:"listen_port"`
	Interface         string            `json:"interface"`
	RuntimeEpoch      string            `json:"runtime_epoch"`
	StartedAt         *time.Time        `json:"started_at"`
	BytesUp           uint64            `json:"bytes_up"`
	BytesDown         uint64            `json:"bytes_down"`
	Connections       uint64            `json:"connections"`
	ActiveConnections uint64            `json:"active_connections"`
	LastError         string            `json:"last_error"`
}

type NetworkSnapshot struct {
	BootEpoch  string         `json:"boot_epoch"`
	ObservedAt time.Time      `json:"observed_at"`
	Lines      []NetworkLine  `json:"lines"`
	Proxies    []NetworkProxy `json:"proxies"`
}

type networkSnapshotWire struct {
	BootEpoch  *string         `json:"boot_epoch"`
	ObservedAt *time.Time      `json:"observed_at"`
	Lines      *[]NetworkLine  `json:"lines"`
	Proxies    *[]NetworkProxy `json:"proxies"`
}

func (client *Client) Network(ctx context.Context) (NetworkSnapshot, error) {
	var wire networkSnapshotWire
	if err := client.doJSON(
		ctx,
		http.MethodGet,
		"/v1/network",
		nil,
		http.StatusOK,
		&wire,
	); err != nil {
		return NetworkSnapshot{}, err
	}
	return validatedNetworkSnapshot(wire)
}

func (client *Client) PutProxies(
	ctx context.Context,
	proxies []ProxyConfiguration,
) (NetworkSnapshot, error) {
	if proxies == nil {
		proxies = []ProxyConfiguration{}
	}
	if err := validateProxyConfigurations(proxies); err != nil {
		return NetworkSnapshot{}, err
	}
	var wire networkSnapshotWire
	if err := client.doJSON(
		ctx,
		http.MethodPut,
		"/v1/proxies",
		struct {
			Proxies []ProxyConfiguration `json:"proxies"`
		}{Proxies: proxies},
		http.StatusOK,
		&wire,
	); err != nil {
		return NetworkSnapshot{}, err
	}
	return validatedNetworkSnapshot(wire)
}

func validateProxyConfigurations(proxies []ProxyConfiguration) error {
	if len(proxies) > maxProxyConfigurations {
		return ErrInvalidRequest
	}
	seen := make(map[string]struct{}, len(proxies))
	listeners := make(map[string]struct{}, len(proxies))
	for _, proxy := range proxies {
		id := strings.TrimSpace(proxy.ID)
		if id == "" ||
			len(id) > maxProxyIDBytes ||
			!validProxyIdentifier(id) ||
			strings.TrimSpace(proxy.LineID) == "" ||
			len(strings.TrimSpace(proxy.LineID)) > maxProxyLineIDBytes ||
			containsControl(strings.TrimSpace(proxy.LineID)) ||
			strings.TrimSpace(proxy.ListenAddress) == "" ||
			proxy.ListenPort < 1024 {
			return ErrInvalidRequest
		}
		if _, exists := seen[id]; exists {
			return ErrInvalidRequest
		}
		seen[id] = struct{}{}
		listenIP := net.ParseIP(proxy.ListenAddress)
		if !validProxyMode(proxy.Mode) || listenIP == nil {
			return ErrInvalidRequest
		}
		listener := net.JoinHostPort(
			listenIP.String(),
			fmt.Sprintf("%d", proxy.ListenPort),
		)
		if _, exists := listeners[listener]; exists {
			return ErrInvalidRequest
		}
		listeners[listener] = struct{}{}
		if proxy.AuthEnabled {
			username := strings.TrimSpace(proxy.Username)
			if username == "" ||
				len(username) > maxProxyUsernameBytes ||
				containsControl(username) ||
				proxy.Password == "" ||
				len(proxy.Password) > maxProxyPasswordBytes ||
				containsControl(proxy.Password) {
				return ErrInvalidRequest
			}
		} else if proxy.Username != "" || proxy.Password != "" || !listenIP.IsLoopback() {
			return ErrInvalidRequest
		}
	}
	return nil
}

func validatedNetworkSnapshot(wire networkSnapshotWire) (NetworkSnapshot, error) {
	if wire.BootEpoch == nil ||
		strings.TrimSpace(*wire.BootEpoch) == "" ||
		wire.ObservedAt == nil ||
		wire.ObservedAt.IsZero() ||
		wire.Lines == nil ||
		wire.Proxies == nil {
		return NetworkSnapshot{}, fmt.Errorf("%w: network snapshot is incomplete", ErrProtocol)
	}
	snapshot := NetworkSnapshot{
		BootEpoch:  strings.TrimSpace(*wire.BootEpoch),
		ObservedAt: wire.ObservedAt.UTC(),
		Lines:      append([]NetworkLine(nil), (*wire.Lines)...),
		Proxies:    append([]NetworkProxy(nil), (*wire.Proxies)...),
	}
	if snapshot.Lines == nil {
		snapshot.Lines = []NetworkLine{}
	}
	if snapshot.Proxies == nil {
		snapshot.Proxies = []NetworkProxy{}
	}

	lineIDs := make(map[string]struct{}, len(snapshot.Lines))
	for index := range snapshot.Lines {
		line := &snapshot.Lines[index]
		line.LineID = strings.TrimSpace(line.LineID)
		line.Interface = strings.TrimSpace(line.Interface)
		line.Error = strings.TrimSpace(line.Error)
		if line.LineID == "" {
			return NetworkSnapshot{}, fmt.Errorf("%w: network line ID is empty", ErrProtocol)
		}
		if _, exists := lineIDs[line.LineID]; exists {
			return NetworkSnapshot{}, fmt.Errorf("%w: duplicate network line ID", ErrProtocol)
		}
		lineIDs[line.LineID] = struct{}{}
		if line.Connected && line.Interface == "" && line.Error == "" {
			return NetworkSnapshot{}, fmt.Errorf(
				"%w: connected network line has neither interface nor error",
				ErrProtocol,
			)
		}
		if line.Addresses == nil {
			line.Addresses = []string{}
		}
		for addressIndex := range line.Addresses {
			line.Addresses[addressIndex] = strings.TrimSpace(line.Addresses[addressIndex])
			if net.ParseIP(line.Addresses[addressIndex]) == nil {
				return NetworkSnapshot{}, fmt.Errorf(
					"%w: network line contains an invalid IP address",
					ErrProtocol,
				)
			}
		}
		if line.DNS == nil {
			line.DNS = []string{}
		}
		for dnsIndex := range line.DNS {
			line.DNS[dnsIndex] = strings.TrimSpace(line.DNS[dnsIndex])
			if net.ParseIP(line.DNS[dnsIndex]) == nil {
				return NetworkSnapshot{}, fmt.Errorf(
					"%w: network line contains an invalid DNS address",
					ErrProtocol,
				)
			}
		}
	}

	proxyIDs := make(map[string]struct{}, len(snapshot.Proxies))
	for index := range snapshot.Proxies {
		proxy := &snapshot.Proxies[index]
		proxy.ID = strings.TrimSpace(proxy.ID)
		proxy.LineID = strings.TrimSpace(proxy.LineID)
		proxy.ListenAddress = strings.TrimSpace(proxy.ListenAddress)
		proxy.Interface = strings.TrimSpace(proxy.Interface)
		proxy.RuntimeEpoch = strings.TrimSpace(proxy.RuntimeEpoch)
		proxy.LastError = strings.TrimSpace(proxy.LastError)
		if proxy.ID == "" ||
			proxy.LineID == "" ||
			!validProxyMode(proxy.Mode) ||
			net.ParseIP(proxy.ListenAddress) == nil ||
			proxy.ListenPort == 0 ||
			!validProxyState(proxy.State) {
			return NetworkSnapshot{}, fmt.Errorf("%w: proxy runtime is invalid", ErrProtocol)
		}
		if _, exists := proxyIDs[proxy.ID]; exists {
			return NetworkSnapshot{}, fmt.Errorf("%w: duplicate proxy runtime ID", ErrProtocol)
		}
		proxyIDs[proxy.ID] = struct{}{}
		if proxy.Running != (proxy.State == ProxyStateRunning) {
			return NetworkSnapshot{}, fmt.Errorf(
				"%w: proxy running flag contradicts state",
				ErrProtocol,
			)
		}
		if proxy.Running &&
			(proxy.Interface == "" ||
				proxy.RuntimeEpoch == "" ||
				proxy.StartedAt == nil ||
				proxy.StartedAt.IsZero()) {
			return NetworkSnapshot{}, fmt.Errorf(
				"%w: running proxy runtime is incomplete",
				ErrProtocol,
			)
		}
		if proxy.State == ProxyStateError && proxy.LastError == "" {
			return NetworkSnapshot{}, fmt.Errorf(
				"%w: failed proxy runtime has no error",
				ErrProtocol,
			)
		}
		if proxy.ActiveConnections > proxy.Connections {
			return NetworkSnapshot{}, fmt.Errorf(
				"%w: proxy active connection count is invalid",
				ErrProtocol,
			)
		}
		if proxy.StartedAt != nil {
			startedAt := proxy.StartedAt.UTC()
			proxy.StartedAt = &startedAt
		}
	}
	return snapshot, nil
}

func validProxyMode(mode ProxyMode) bool {
	return mode == ProxyModeSOCKS5 || mode == ProxyModeHTTP
}

func validProxyState(state ProxyRuntimeState) bool {
	switch state {
	case ProxyStateDisabled, ProxyStateWaitingForBearer, ProxyStateRunning, ProxyStateError:
		return true
	default:
		return false
	}
}

func validProxyIdentifier(value string) bool {
	for _, character := range value {
		if (character >= 'a' && character <= 'z') ||
			(character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') ||
			character == '.' ||
			character == '_' ||
			character == '-' {
			continue
		}
		return false
	}
	return true
}

func containsControl(value string) bool {
	for _, character := range value {
		if unicode.IsControl(character) {
			return true
		}
	}
	return false
}
