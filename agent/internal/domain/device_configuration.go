package domain

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"
)

type DeviceConfigurationOperation string

const (
	DeviceConfigurationSetRadioEnabled DeviceConfigurationOperation = "set_radio_enabled"
	DeviceConfigurationConnectData     DeviceConfigurationOperation = "connect_data"
	DeviceConfigurationDisconnectData  DeviceConfigurationOperation = "disconnect_data"
	DeviceConfigurationSetVoLTEPolicy  DeviceConfigurationOperation = "set_volte_policy"
)

type FeatureCapability struct {
	Backend     string `json:"backend"`
	Supported   bool   `json:"supported"`
	Implemented bool   `json:"implemented"`
	Readable    bool   `json:"readable"`
	Writable    bool   `json:"writable"`
	Reason      string `json:"reason,omitempty"`
}

type DeviceIdentity struct {
	Manufacturer        string `json:"manufacturer"`
	Model               string `json:"model"`
	Firmware            string `json:"firmware"`
	EquipmentIdentifier string `json:"equipment_identifier"`
}

type RadioConfiguration struct {
	Enabled        bool   `json:"enabled"`
	EnabledKnown   bool   `json:"enabled_known"`
	PowerState     string `json:"power_state"`
	PowerStateCode uint32 `json:"power_state_code"`
}

type IPConfiguration struct {
	Method  string   `json:"method"`
	Address string   `json:"address"`
	Prefix  uint32   `json:"prefix"`
	Gateway string   `json:"gateway"`
	DNS     []string `json:"dns"`
	MTU     uint32   `json:"mtu"`
}

type DataConnection struct {
	ID        string          `json:"id"`
	Connected bool            `json:"connected"`
	APN       string          `json:"apn"`
	IPFamily  string          `json:"ip_family"`
	Interface string          `json:"interface"`
	IPv4      IPConfiguration `json:"ipv4"`
	IPv6      IPConfiguration `json:"ipv6"`
}

type VoLTEConfiguration struct {
	PolicyKnown bool   `json:"policy_known"`
	Policy      string `json:"policy,omitempty"`
	ProfileID   string `json:"profile_id,omitempty"`
}

type DeviceConfigurationCapabilities struct {
	Radio             FeatureCapability `json:"radio"`
	DataConnection    FeatureCapability `json:"data_connection"`
	FlightMode        FeatureCapability `json:"flight_mode"`
	VoWiFi            FeatureCapability `json:"vowifi"`
	VoLTE             FeatureCapability `json:"volte"`
	Alias             FeatureCapability `json:"alias"`
	ESIM              FeatureCapability `json:"esim"`
	ATTerminal        FeatureCapability `json:"at_terminal"`
	USSD              FeatureCapability `json:"ussd"`
	ConnectionProfile FeatureCapability `json:"connection_profile"`
}

type DeviceConfiguration struct {
	LineID          string                          `json:"line_id"`
	Revision        string                          `json:"revision"`
	ObservedAt      time.Time                       `json:"observed_at"`
	Identity        DeviceIdentity                  `json:"identity"`
	Radio           RadioConfiguration              `json:"radio"`
	FlightMode      bool                            `json:"flight_mode"`
	FlightModeKnown bool                            `json:"flight_mode_known"`
	NetworkEnabled  bool                            `json:"network_enabled"`
	DataConnections []DataConnection                `json:"data_connections"`
	VoLTE           VoLTEConfiguration              `json:"volte"`
	Capabilities    DeviceConfigurationCapabilities `json:"capabilities"`
}

type ApplyDeviceConfigurationRequest struct {
	RequestID        string                       `json:"request_id"`
	LineID           string                       `json:"-"`
	ExpectedRevision string                       `json:"expected_revision"`
	Operation        DeviceConfigurationOperation `json:"operation"`
	RadioEnabled     *bool                        `json:"radio_enabled,omitempty"`
	APN              string                       `json:"apn,omitempty"`
	IPFamily         string                       `json:"ip_family,omitempty"`
	VoLTEPolicy      string                       `json:"volte_policy,omitempty"`
}

type DeviceConfigurationProvider interface {
	DeviceConfiguration(context.Context, string) (DeviceConfiguration, error)
	ApplyDeviceConfiguration(context.Context, ApplyDeviceConfigurationRequest) (DeviceConfiguration, error)
}

func RevisionDeviceConfiguration(configuration DeviceConfiguration) (string, error) {
	content := struct {
		LineID          string                          `json:"line_id"`
		Identity        DeviceIdentity                  `json:"identity"`
		Radio           RadioConfiguration              `json:"radio"`
		FlightMode      bool                            `json:"flight_mode"`
		FlightModeKnown bool                            `json:"flight_mode_known"`
		NetworkEnabled  bool                            `json:"network_enabled"`
		DataConnections []DataConnection                `json:"data_connections"`
		VoLTE           VoLTEConfiguration              `json:"volte"`
		Capabilities    DeviceConfigurationCapabilities `json:"capabilities"`
	}{
		LineID:          configuration.LineID,
		Identity:        configuration.Identity,
		Radio:           configuration.Radio,
		FlightMode:      configuration.FlightMode,
		FlightModeKnown: configuration.FlightModeKnown,
		NetworkEnabled:  configuration.NetworkEnabled,
		DataConnections: configuration.DataConnections,
		VoLTE:           configuration.VoLTE,
		Capabilities:    configuration.Capabilities,
	}
	encoded, err := json.Marshal(content)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}
