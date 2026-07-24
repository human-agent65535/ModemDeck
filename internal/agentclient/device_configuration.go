package agentclient

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type DeviceConfigurationOperation string

const (
	DeviceConfigurationSetRadioEnabled DeviceConfigurationOperation = "set_radio_enabled"
	DeviceConfigurationConnectData     DeviceConfigurationOperation = "connect_data"
	DeviceConfigurationDisconnectData  DeviceConfigurationOperation = "disconnect_data"
	DeviceConfigurationSetVoLTEPolicy  DeviceConfigurationOperation = "set_volte_policy"
	DeviceConfigurationRestartModem    DeviceConfigurationOperation = "restart_modem"
)

type FeatureCapability struct {
	Backend     string `json:"backend"`
	Supported   bool   `json:"supported"`
	Implemented bool   `json:"implemented"`
	Readable    bool   `json:"readable"`
	Writable    bool   `json:"writable"`
	Reason      string `json:"reason"`
}

type DeviceIdentity struct {
	Manufacturer        string `json:"manufacturer"`
	Model               string `json:"model"`
	Firmware            string `json:"firmware"`
	EquipmentIdentifier string `json:"equipment_identifier"`
}

type ModemPort struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	TypeCode uint32 `json:"type_code"`
}

type DeviceHardwareDetails struct {
	HardwareRevision   string      `json:"hardware_revision,omitempty"`
	PrimaryPort        string      `json:"primary_port,omitempty"`
	AccessTechnologies *uint32     `json:"access_technologies,omitempty"`
	SNR                *float64    `json:"snr,omitempty"`
	Ports              []ModemPort `json:"ports,omitempty"`
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
	PolicyKnown            bool   `json:"policy_known"`
	Policy                 string `json:"policy"`
	ConfigurationMode      string `json:"configuration_mode"`
	ModemCapabilityKnown   bool   `json:"modem_capability_known"`
	ModemCapabilityEnabled bool   `json:"modem_capability_enabled"`
	RestartRequired        bool   `json:"restart_required"`
	ProfileID              string `json:"profile_id"`
}

type DeviceConfigurationCapabilities struct {
	Voice             FeatureCapability `json:"voice"`
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
	Details         DeviceHardwareDetails           `json:"details"`
	Radio           RadioConfiguration              `json:"radio"`
	FlightMode      bool                            `json:"flight_mode"`
	FlightModeKnown bool                            `json:"flight_mode_known"`
	NetworkEnabled  bool                            `json:"network_enabled"`
	AutomaticAPN    string                          `json:"automatic_apn"`
	DataConnections []DataConnection                `json:"data_connections"`
	VoLTE           VoLTEConfiguration              `json:"volte"`
	Capabilities    DeviceConfigurationCapabilities `json:"capabilities"`
}

type ApplyDeviceConfigurationRequest struct {
	RequestID        string                       `json:"request_id"`
	ExpectedRevision string                       `json:"expected_revision"`
	Operation        DeviceConfigurationOperation `json:"operation"`
	RadioEnabled     *bool                        `json:"radio_enabled,omitempty"`
	APN              string                       `json:"apn,omitempty"`
	IPFamily         string                       `json:"ip_family,omitempty"`
	VoLTEPolicy      string                       `json:"volte_policy,omitempty"`
}

func (client *Client) DeviceConfiguration(
	ctx context.Context,
	lineID string,
) (DeviceConfiguration, error) {
	lineID = strings.TrimSpace(lineID)
	if lineID == "" {
		return DeviceConfiguration{}, ErrInvalidRequest
	}
	var configuration DeviceConfiguration
	path := "/v1/lines/" + url.PathEscape(lineID) + "/configuration"
	if err := client.doJSON(
		ctx,
		http.MethodGet,
		path,
		nil,
		http.StatusOK,
		&configuration,
	); err != nil {
		return DeviceConfiguration{}, err
	}
	if configuration.DataConnections == nil {
		configuration.DataConnections = []DataConnection{}
	}
	return configuration, nil
}

func (client *Client) ApplyDeviceConfiguration(
	ctx context.Context,
	lineID string,
	request ApplyDeviceConfigurationRequest,
) (DeviceConfiguration, error) {
	lineID = strings.TrimSpace(lineID)
	if lineID == "" ||
		strings.TrimSpace(request.RequestID) == "" ||
		strings.TrimSpace(request.ExpectedRevision) == "" ||
		strings.TrimSpace(string(request.Operation)) == "" {
		return DeviceConfiguration{}, ErrInvalidRequest
	}
	var configuration DeviceConfiguration
	path := "/v1/lines/" + url.PathEscape(lineID) + "/configuration"
	if err := client.doJSON(
		ctx,
		http.MethodPatch,
		path,
		request,
		http.StatusOK,
		&configuration,
	); err != nil {
		return DeviceConfiguration{}, err
	}
	if configuration.DataConnections == nil {
		configuration.DataConnections = []DataConnection{}
	}
	return configuration, nil
}
