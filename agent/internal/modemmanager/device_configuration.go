package modemmanager

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/human-agent65535/modemdeck/agent/internal/domain"
)

const (
	simpleInterface         = "org.freedesktop.ModemManager1.Modem.Simple"
	bearerInterface         = "org.freedesktop.ModemManager1.Bearer"
	ussdInterface           = "org.freedesktop.ModemManager1.Modem.Modem3gpp.Ussd"
	profileManagerInterface = "org.freedesktop.ModemManager1.Modem.Modem3gpp.ProfileManager"

	deviceConfigurationReadTimeout  = 5 * time.Second
	deviceConfigurationWriteTimeout = 45 * time.Second

	modemStateDisabled = 3
	modemStateEnabled  = 6

	modemPowerStateUnknown = 0
	modemPowerStateOff     = 1
	modemPowerStateLow     = 2
	modemPowerStateOn      = 3

	bearerIPFamilyIPv4   = 1
	bearerIPFamilyIPv6   = 2
	bearerIPFamilyIPv4V6 = 4
	bearerIPFamilyAny    = 8
)

type genericConfigurationProvider interface {
	ReadDeviceConfiguration(context.Context, string) (domain.DeviceConfiguration, error)
	ApplyGenericDeviceConfiguration(
		context.Context,
		domain.ApplyDeviceConfigurationRequest,
	) (domain.DeviceConfiguration, error)
}

func (p *Provider) ReadDeviceConfiguration(
	ctx context.Context,
	lineID string,
) (domain.DeviceConfiguration, error) {
	const operation = "read_device_configuration"
	bounded, cancel, err := configurationContext(ctx, deviceConfigurationReadTimeout, operation)
	if err != nil {
		return domain.DeviceConfiguration{}, err
	}
	defer cancel()
	configuration, _, _, err := p.readDeviceConfiguration(bounded, lineID, operation)
	return configuration, err
}

func (p *Provider) ApplyGenericDeviceConfiguration(
	ctx context.Context,
	request domain.ApplyDeviceConfigurationRequest,
) (domain.DeviceConfiguration, error) {
	const operation = "apply_device_configuration"
	if err := validateConfigurationRequest(request); err != nil {
		return domain.DeviceConfiguration{}, err
	}
	bounded, cancel, err := configurationContext(ctx, deviceConfigurationWriteTimeout, operation)
	if err != nil {
		return domain.DeviceConfiguration{}, err
	}
	defer cancel()

	p.configMu.Lock()
	defer p.configMu.Unlock()

	current, objects, modemPath, err := p.readDeviceConfiguration(bounded, request.LineID, operation)
	if err != nil {
		return domain.DeviceConfiguration{}, err
	}
	if current.Revision != request.ExpectedRevision {
		return domain.DeviceConfiguration{}, domain.Conflict(
			operation,
			"device configuration changed; read the latest revision before applying",
		)
	}

	switch request.Operation {
	case domain.DeviceConfigurationSetRadioEnabled:
		if !current.Capabilities.Radio.Writable {
			return domain.DeviceConfiguration{}, domain.NotSupported(operation, current.Capabilities.Radio.Reason)
		}
		if request.RadioEnabled == nil {
			return domain.DeviceConfiguration{}, domain.InvalidArgument(operation, "radio_enabled is required")
		}
		if current.Radio.EnabledKnown && current.Radio.Enabled == *request.RadioEnabled {
			return current, nil
		}
		if !*request.RadioEnabled {
			parsed := ParseManagedObjects(objects, p.ids)
			if lineHasCall(parsed.Calls, request.LineID, "") {
				return domain.DeviceConfiguration{}, domain.Conflict(
					operation,
					"the cellular radio cannot be disabled while this line has an ongoing call",
				)
			}
		}
		if _, err := p.call(
			bounded,
			modemPath,
			modemInterface+".Enable",
			operation,
			"ModemManager failed to change the modem radio state",
			*request.RadioEnabled,
		); err != nil {
			return domain.DeviceConfiguration{}, err
		}
	case domain.DeviceConfigurationConnectData:
		if !current.Capabilities.DataConnection.Writable {
			return domain.DeviceConfiguration{}, domain.NotSupported(
				operation,
				current.Capabilities.DataConnection.Reason,
			)
		}
		requestedFamily, err := bearerIPFamilyValue(request.IPFamily)
		if err != nil {
			return domain.DeviceConfiguration{}, domain.InvalidArgument(operation, err.Error())
		}
		apn := strings.TrimSpace(request.APN)
		if invalidAPN(apn) {
			return domain.DeviceConfiguration{}, domain.InvalidArgument(
				operation,
				"apn must be empty for automatic selection or contain only ASCII letters, digits, dots, and hyphens",
			)
		}
		if matchingConnectedData(current.DataConnections, apn, requestedFamily) {
			return current, nil
		}
		properties := map[string]dbus.Variant{}
		if apn != "" {
			properties["apn"] = dbus.MakeVariant(apn)
		}
		if requestedFamily != 0 {
			properties["ip-type"] = dbus.MakeVariant(requestedFamily)
		}
		body, err := p.call(
			bounded,
			modemPath,
			simpleInterface+".Connect",
			operation,
			"ModemManager failed to connect the packet data bearer",
			properties,
		)
		if err != nil {
			return domain.DeviceConfiguration{}, err
		}
		bearerPath, err := objectPathResult(
			operation,
			"ModemManager returned an invalid packet data bearer path",
			body,
		)
		if err != nil {
			return domain.DeviceConfiguration{}, err
		}
		verified, _, _, err := p.readDeviceConfiguration(bounded, request.LineID, operation)
		if err != nil {
			return domain.DeviceConfiguration{}, err
		}
		if err := verifyConnectedBearer(p.ids.bearerID(bearerPath), apn, requestedFamily, verified); err != nil {
			return domain.DeviceConfiguration{}, domain.VerificationFailed(operation, err.Error(), err)
		}
		return verified, nil
	case domain.DeviceConfigurationDisconnectData:
		if !current.Capabilities.DataConnection.Writable {
			return domain.DeviceConfiguration{}, domain.NotSupported(
				operation,
				current.Capabilities.DataConnection.Reason,
			)
		}
		if !current.NetworkEnabled {
			return current, nil
		}
		if _, err := p.call(
			bounded,
			modemPath,
			simpleInterface+".Disconnect",
			operation,
			"ModemManager failed to disconnect packet data bearers",
			dbus.ObjectPath("/"),
		); err != nil {
			return domain.DeviceConfiguration{}, err
		}
	case domain.DeviceConfigurationSetVoLTEPolicy:
		return domain.DeviceConfiguration{}, domain.NotSupported(
			operation,
			"VoLTE is owned by the exact vendor-profile extension",
		)
	default:
		return domain.DeviceConfiguration{}, domain.InvalidArgument(operation, "unsupported device configuration operation")
	}

	verified, _, _, err := p.readDeviceConfiguration(bounded, request.LineID, operation)
	if err != nil {
		return domain.DeviceConfiguration{}, err
	}
	switch request.Operation {
	case domain.DeviceConfigurationSetRadioEnabled:
		if !verified.Radio.EnabledKnown || verified.Radio.Enabled != *request.RadioEnabled {
			return domain.DeviceConfiguration{}, domain.VerificationFailed(
				operation,
				"radio state read-back did not match the requested value",
				nil,
			)
		}
	case domain.DeviceConfigurationDisconnectData:
		if verified.NetworkEnabled {
			return domain.DeviceConfiguration{}, domain.VerificationFailed(
				operation,
				"packet data remained connected after disconnect",
				nil,
			)
		}
	}
	return verified, nil
}

func (p *Provider) readDeviceConfiguration(
	ctx context.Context,
	lineID string,
	operation string,
) (domain.DeviceConfiguration, ManagedObjects, dbus.ObjectPath, error) {
	lineID = strings.TrimSpace(lineID)
	if lineID == "" {
		return domain.DeviceConfiguration{}, nil, "", domain.InvalidArgument(operation, "line id is required")
	}
	objects, err := p.managedObjects(ctx, operation)
	if err != nil {
		return domain.DeviceConfiguration{}, nil, "", err
	}
	parsed := ParseManagedObjects(objects, p.ids)
	line, found := findLine(parsed.Lines, lineID)
	if !found {
		return domain.DeviceConfiguration{}, nil, "", domain.NotFound(operation, "line was not found")
	}
	modemPath := parsed.LinePaths[line.ID]
	interfaces, found := objects[modemPath]
	if !found {
		return domain.DeviceConfiguration{}, nil, "", domain.Internal(
			operation,
			"ModemManager line path was missing from the object snapshot",
			nil,
		)
	}
	modemProperties, found := interfaces[modemInterface]
	if !found {
		return domain.DeviceConfiguration{}, nil, "", domain.Internal(
			operation,
			"ModemManager line did not expose the modem interface",
			nil,
		)
	}

	radioEnabled, radioKnown := modemEnabled(line.StateCode)
	configuration := domain.DeviceConfiguration{
		LineID:     line.ID,
		ObservedAt: p.now().UTC(),
		Identity: domain.DeviceIdentity{
			Manufacturer:        line.Manufacturer,
			Model:               line.Model,
			Firmware:            line.Revision,
			EquipmentIdentifier: line.EquipmentIdentifier,
		},
		Radio: domain.RadioConfiguration{
			Enabled:        radioEnabled,
			EnabledKnown:   radioKnown,
			PowerState:     modemPowerStateName(line.PowerStateCode),
			PowerStateCode: line.PowerStateCode,
		},
		FlightMode:      !radioEnabled,
		FlightModeKnown: radioKnown,
		DataConnections: []domain.DataConnection{},
	}
	configuration.Capabilities = genericConfigurationCapabilities(interfaces)

	bearerPaths, _ := objectPathValuesProperty(modemProperties, "Bearers")
	for _, bearerPath := range bearerPaths {
		bearerInterfaces, found := objects[bearerPath]
		if !found {
			return domain.DeviceConfiguration{}, nil, "", domain.Internal(
				operation,
				"ModemManager bearer path was missing from the object snapshot",
				nil,
			)
		}
		bearerProperties, found := bearerInterfaces[bearerInterface]
		if !found {
			return domain.DeviceConfiguration{}, nil, "", domain.Internal(
				operation,
				"ModemManager bearer did not expose the bearer interface",
				nil,
			)
		}
		connection, err := parseDataConnection(p.ids.bearerID(bearerPath), bearerProperties)
		if err != nil {
			return domain.DeviceConfiguration{}, nil, "", domain.Internal(
				operation,
				"ModemManager bearer properties were malformed",
				err,
			)
		}
		configuration.DataConnections = append(configuration.DataConnections, connection)
		configuration.NetworkEnabled = configuration.NetworkEnabled || connection.Connected
	}
	sort.Slice(configuration.DataConnections, func(i, j int) bool {
		return configuration.DataConnections[i].ID < configuration.DataConnections[j].ID
	})
	configuration.Revision, err = domain.RevisionDeviceConfiguration(configuration)
	if err != nil {
		return domain.DeviceConfiguration{}, nil, "", domain.Internal(
			operation,
			"failed to revision the device configuration",
			err,
		)
	}
	return configuration, objects, modemPath, nil
}

func genericConfigurationCapabilities(interfaces Interfaces) domain.DeviceConfigurationCapabilities {
	modemManager := "modemmanager"
	vendor := "vendor_extension"
	application := "application"
	dataWritable := false
	if _, found := interfaces[simpleInterface]; found {
		dataWritable = true
	}
	ussdSupported := false
	if _, found := interfaces[ussdInterface]; found {
		ussdSupported = true
	}
	profilesSupported := false
	if _, found := interfaces[profileManagerInterface]; found {
		profilesSupported = true
	}
	dataReason := ""
	if !dataWritable {
		dataReason = "active bearer APN/IP is readable; ModemManager Simple is not exposed for connect and disconnect"
	}
	ussdReason := "USSD is an operational session API and is not exposed by this configuration endpoint"
	if !ussdSupported {
		ussdReason = "ModemManager USSD is not currently exposed for this line"
	}
	profileReason := "Connection profile mutation is deferred; active bearer APN/IP is implemented"
	if !profilesSupported {
		profileReason = "ModemManager ProfileManager is not currently exposed for this line"
	}
	return domain.DeviceConfigurationCapabilities{
		Radio: domain.FeatureCapability{
			Backend:     modemManager,
			Supported:   true,
			Implemented: true,
			Readable:    true,
			Writable:    true,
		},
		DataConnection: domain.FeatureCapability{
			Backend:     modemManager,
			Supported:   true,
			Implemented: true,
			Readable:    true,
			Writable:    dataWritable,
			Reason:      dataReason,
		},
		FlightMode: domain.FeatureCapability{
			Backend:     modemManager,
			Supported:   true,
			Implemented: true,
			Readable:    true,
			Writable:    true,
			Reason:      "represented by radio.enabled; disabling the modem enters low-power state",
		},
		VoWiFi: domain.FeatureCapability{
			Backend: vendor,
			Reason:  "ModemManager has no generic VoWiFi policy interface",
		},
		VoLTE: domain.FeatureCapability{
			Backend: vendor,
			Reason:  "no exact manufacturer, model, and firmware profile was resolved",
		},
		Alias: domain.FeatureCapability{
			Backend: application,
			Reason:  "display aliases are owned by the ModemDeck application database",
		},
		ESIM: domain.FeatureCapability{
			Backend: vendor,
			Reason:  "ModemManager SIM slots and connection profiles do not provide eUICC profile provisioning",
		},
		ATTerminal: domain.FeatureCapability{
			Backend: vendor,
			Reason:  "arbitrary AT access is intentionally not exposed by the configuration API",
		},
		USSD: domain.FeatureCapability{
			Backend:   modemManager,
			Supported: ussdSupported,
			Reason:    ussdReason,
		},
		ConnectionProfile: domain.FeatureCapability{
			Backend:   modemManager,
			Supported: profilesSupported,
			Reason:    profileReason,
		},
	}
}

func parseDataConnection(id string, properties Properties) (domain.DataConnection, error) {
	connection := domain.DataConnection{
		ID:   id,
		IPv4: domain.IPConfiguration{DNS: []string{}},
		IPv6: domain.IPConfiguration{DNS: []string{}},
	}
	connection.Connected, _ = boolProperty(properties, "Connected")
	connection.Interface, _ = stringProperty(properties, "Interface")
	bearerProperties, found, err := nestedProperties(properties, "Properties")
	if err != nil {
		return domain.DataConnection{}, err
	}
	if found {
		connection.APN, _ = stringProperty(bearerProperties, "apn")
		family, _ := uint32Property(bearerProperties, "ip-type")
		connection.IPFamily = bearerIPFamilyName(family)
	}
	connection.IPv4, err = parseIPConfiguration(properties, "Ip4Config")
	if err != nil {
		return domain.DataConnection{}, err
	}
	connection.IPv6, err = parseIPConfiguration(properties, "Ip6Config")
	if err != nil {
		return domain.DataConnection{}, err
	}
	return connection, nil
}

func parseIPConfiguration(properties Properties, name string) (domain.IPConfiguration, error) {
	configuration := domain.IPConfiguration{DNS: []string{}}
	values, found, err := nestedProperties(properties, name)
	if err != nil || !found {
		return configuration, err
	}
	method, _ := uint32Property(values, "method")
	configuration.Method = ipMethodName(method)
	configuration.Address, _ = stringProperty(values, "address")
	configuration.Prefix, _ = uint32Property(values, "prefix")
	configuration.Gateway, _ = stringProperty(values, "gateway")
	configuration.DNS, _ = stringsProperty(values, "dns")
	configuration.MTU, _ = uint32Property(values, "mtu")
	return configuration, nil
}

func nestedProperties(properties Properties, name string) (Properties, bool, error) {
	value, found := propertyValue(properties, name)
	if !found {
		return nil, false, nil
	}
	values, ok := value.(map[string]dbus.Variant)
	if !ok {
		return nil, false, fmt.Errorf("%s is %T, want map[string]dbus.Variant", name, value)
	}
	return values, true, nil
}

func validateConfigurationRequest(request domain.ApplyDeviceConfigurationRequest) error {
	const operation = "apply_device_configuration"
	request.LineID = strings.TrimSpace(request.LineID)
	request.ExpectedRevision = strings.TrimSpace(request.ExpectedRevision)
	if err := validateRequestID(operation, strings.TrimSpace(request.RequestID)); err != nil {
		return err
	}
	if request.LineID == "" {
		return domain.InvalidArgument(operation, "line id is required")
	}
	if request.ExpectedRevision == "" || len(request.ExpectedRevision) > 128 {
		return domain.InvalidArgument(operation, "expected_revision is required and must be valid")
	}
	switch request.Operation {
	case domain.DeviceConfigurationSetRadioEnabled:
		if request.RadioEnabled == nil || request.APN != "" || request.IPFamily != "" || request.VoLTEPolicy != "" {
			return domain.InvalidArgument(operation, "set_radio_enabled accepts only radio_enabled")
		}
	case domain.DeviceConfigurationConnectData:
		if request.RadioEnabled != nil || request.VoLTEPolicy != "" {
			return domain.InvalidArgument(operation, "connect_data accepts only apn and ip_family")
		}
	case domain.DeviceConfigurationDisconnectData:
		if request.RadioEnabled != nil || request.APN != "" || request.IPFamily != "" || request.VoLTEPolicy != "" {
			return domain.InvalidArgument(operation, "disconnect_data does not accept operation parameters")
		}
	case domain.DeviceConfigurationSetVoLTEPolicy:
		if request.RadioEnabled != nil || request.APN != "" || request.IPFamily != "" {
			return domain.InvalidArgument(operation, "set_volte_policy accepts only volte_policy")
		}
		if request.VoLTEPolicy != "enabled" && request.VoLTEPolicy != "disabled" {
			return domain.InvalidArgument(operation, "volte_policy must be enabled or disabled")
		}
	default:
		return domain.InvalidArgument(operation, "unsupported device configuration operation")
	}
	return nil
}

func configurationContext(
	ctx context.Context,
	timeout time.Duration,
	operation string,
) (context.Context, context.CancelFunc, error) {
	if ctx == nil {
		return nil, nil, domain.InvalidArgument(operation, "request context is required")
	}
	bounded, cancel := context.WithTimeout(ctx, timeout)
	return bounded, cancel, nil
}

func verifyConnectedBearer(
	bearerID string,
	apn string,
	requestedFamily uint32,
	configuration domain.DeviceConfiguration,
) error {
	for _, connection := range configuration.DataConnections {
		if connection.ID != bearerID {
			continue
		}
		if !connection.Connected {
			return fmt.Errorf("returned bearer is not connected")
		}
		if apn != "" && connection.APN != apn {
			return fmt.Errorf("bearer APN read-back did not match the request")
		}
		if requestedFamily != 0 && connection.IPFamily != bearerIPFamilyName(requestedFamily) {
			return fmt.Errorf("bearer IP family read-back did not match the request")
		}
		return nil
	}
	return fmt.Errorf("returned bearer was absent from the authoritative modem snapshot")
}

func matchingConnectedData(connections []domain.DataConnection, apn string, family uint32) bool {
	for _, connection := range connections {
		if !connection.Connected {
			continue
		}
		if apn != "" && connection.APN != apn {
			continue
		}
		if family != 0 && connection.IPFamily != bearerIPFamilyName(family) {
			continue
		}
		return true
	}
	return false
}

func invalidAPN(value string) bool {
	if len(value) > 100 {
		return true
	}
	for _, character := range value {
		if character >= 'a' && character <= 'z' ||
			character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' ||
			character == '.' || character == '-' {
			continue
		}
		return true
	}
	return false
}

func bearerIPFamilyValue(value string) (uint32, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "auto":
		return 0, nil
	case "ipv4":
		return bearerIPFamilyIPv4, nil
	case "ipv6":
		return bearerIPFamilyIPv6, nil
	case "ipv4v6":
		return bearerIPFamilyIPv4V6, nil
	default:
		return 0, fmt.Errorf("ip_family must be auto, ipv4, ipv6, or ipv4v6")
	}
}

func bearerIPFamilyName(value uint32) string {
	switch value {
	case bearerIPFamilyIPv4:
		return "ipv4"
	case bearerIPFamilyIPv6:
		return "ipv6"
	case bearerIPFamilyIPv4V6:
		return "ipv4v6"
	case bearerIPFamilyAny:
		return "any"
	default:
		return "unknown"
	}
}

func modemEnabled(state int32) (bool, bool) {
	switch {
	case state == modemStateDisabled:
		return false, true
	case state >= modemStateEnabled:
		return true, true
	default:
		return false, false
	}
}

func modemPowerStateName(value uint32) string {
	switch value {
	case modemPowerStateOff:
		return "off"
	case modemPowerStateLow:
		return "low"
	case modemPowerStateOn:
		return "on"
	case modemPowerStateUnknown:
		fallthrough
	default:
		return "unknown"
	}
}

func ipMethodName(value uint32) string {
	switch value {
	case 1:
		return "ppp"
	case 2:
		return "static"
	case 3:
		return "dhcp"
	default:
		return "unknown"
	}
}

var _ genericConfigurationProvider = (*Provider)(nil)
