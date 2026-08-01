package domain

import (
	"context"
	"time"
)

type ProxyMode string

const (
	ProxyModeSOCKS5 ProxyMode = "socks5"
	ProxyModeHTTP   ProxyMode = "http"
)

type ProxyState string

const (
	ProxyStateDisabled         ProxyState = "disabled"
	ProxyStateWaitingForBearer ProxyState = "waiting_for_bearer"
	ProxyStateRunning          ProxyState = "running"
	ProxyStateError            ProxyState = "error"
)

type ProxyConfiguration struct {
	ID            string    `json:"id"`
	LineID        string    `json:"line_id"`
	Enabled       bool      `json:"enabled"`
	Mode          ProxyMode `json:"mode"`
	ListenAddress string    `json:"listen_address"`
	ListenPort    uint16    `json:"listen_port"`
	AuthEnabled   bool      `json:"auth_enabled"`
	Username      string    `json:"username"`
	Password      string    `json:"password"`
}

type ProxyDesiredSet struct {
	Proxies []ProxyConfiguration `json:"proxies"`
}

type LineNetworkStatus struct {
	LineID    string   `json:"line_id"`
	Connected bool     `json:"connected"`
	Interface string   `json:"interface"`
	Addresses []string `json:"addresses"`
	DNS       []string `json:"dns"`
	RXBytes   uint64   `json:"rx_bytes"`
	TXBytes   uint64   `json:"tx_bytes"`
	Error     string   `json:"error"`
}

// LineNetworkConfiguration is the authoritative packet-data configuration
// needed by the networking runtime. Device settings and provisioning details
// deliberately do not cross this boundary.
type LineNetworkConfiguration struct {
	LineID          string
	DataConnections []DataConnection
}

type ProxyNetworkStatus struct {
	ID                string     `json:"id"`
	LineID            string     `json:"line_id"`
	State             ProxyState `json:"state"`
	Running           bool       `json:"running"`
	Mode              ProxyMode  `json:"mode"`
	ListenAddress     string     `json:"listen_address"`
	ListenPort        uint16     `json:"listen_port"`
	Interface         string     `json:"interface"`
	RuntimeEpoch      string     `json:"runtime_epoch"`
	StartedAt         *time.Time `json:"started_at"`
	BytesUp           uint64     `json:"bytes_up"`
	BytesDown         uint64     `json:"bytes_down"`
	Connections       uint64     `json:"connections"`
	ActiveConnections uint64     `json:"active_connections"`
	LastError         string     `json:"last_error"`
}

type NetworkSnapshot struct {
	BootEpoch  string               `json:"boot_epoch"`
	ObservedAt time.Time            `json:"observed_at"`
	Lines      []LineNetworkStatus  `json:"lines"`
	Proxies    []ProxyNetworkStatus `json:"proxies"`
}

type NetworkProvider interface {
	ApplyProxySet(context.Context, ProxyDesiredSet) (NetworkSnapshot, error)
	NetworkSnapshot(context.Context) (NetworkSnapshot, error)
}
