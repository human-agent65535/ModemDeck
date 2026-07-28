package modemmanager

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/human-agent65535/modemdeck/agent/internal/domain"
	"github.com/human-agent65535/modemdeck/agent/internal/usbrecovery"
)

const (
	simpleInterface         = "org.freedesktop.ModemManager1.Modem.Simple"
	bearerInterface         = "org.freedesktop.ModemManager1.Bearer"
	modem3GPPInterface      = "org.freedesktop.ModemManager1.Modem.Modem3gpp"
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

	bearerTypeDefault = 1
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
		radioAlreadySet :=
			current.Radio.EnabledKnown &&
				current.Radio.Enabled == *request.RadioEnabled
		radioStateConsistent :=
			*request.RadioEnabled ||
				!current.NetworkEnabled
		if radioAlreadySet && radioStateConsistent {
			if err := p.radioStates.setEnabled(request.LineID, *request.RadioEnabled); err != nil {
				return domain.DeviceConfiguration{}, domain.Internal(
					operation,
					"radio preference could not be persisted",
					err,
				)
			}
			updated, _, _, err := p.readDeviceConfiguration(
				bounded,
				request.LineID,
				operation,
			)
			return updated, err
		}
		if !*request.RadioEnabled {
			p.callMu.Lock()
			defer p.callMu.Unlock()

			objects, err = p.managedObjects(bounded, operation)
			if err != nil {
				return domain.DeviceConfiguration{}, err
			}
			objects, err = p.hydrateCalls(bounded, operation, objects)
			if err != nil {
				return domain.DeviceConfiguration{}, err
			}
			parsed := ParseManagedObjects(objects, p.ids)
			if lineHasCall(parsed.Calls, request.LineID, "") {
				return domain.DeviceConfiguration{}, domain.Conflict(
					operation,
					"the cellular radio cannot be disabled while this line has an ongoing call",
				)
			}
			if _, err := p.deactivateOwnedBearerData(
				bounded,
				modemPath,
				request.LineID,
			); err != nil {
				return domain.DeviceConfiguration{}, err
			}
		}
		previousDesired := p.radioStates.enabled(request.LineID)
		if err := p.radioStates.setEnabled(request.LineID, *request.RadioEnabled); err != nil {
			return domain.DeviceConfiguration{}, domain.Internal(
				operation,
				"radio preference could not be persisted",
				err,
			)
		}
		if _, err := p.call(
			bounded,
			modemPath,
			modemInterface+".Enable",
			operation,
			"ModemManager failed to change the modem radio state",
			*request.RadioEnabled,
		); err != nil {
			if rollbackErr := p.radioStates.setEnabled(request.LineID, previousDesired); rollbackErr != nil {
				return domain.DeviceConfiguration{}, domain.Internal(
					operation,
					"radio state failed and its persisted preference could not be restored",
					errors.Join(err, rollbackErr),
				)
			}
			return domain.DeviceConfiguration{}, err
		}
	case domain.DeviceConfigurationConnectData:
		if !current.Capabilities.DataConnection.Writable {
			return domain.DeviceConfiguration{}, domain.NotSupported(
				operation,
				current.Capabilities.DataConnection.Reason,
			)
		}
		if !current.Radio.EnabledKnown {
			return domain.DeviceConfiguration{}, domain.FailedPrecondition(
				operation,
				"the cellular radio state is unavailable; mobile data cannot be started safely",
				nil,
			)
		}
		if current.FlightModeKnown && current.FlightMode {
			return domain.DeviceConfiguration{}, domain.FailedPrecondition(
				operation,
				"airplane mode must be turned off before starting mobile data",
				nil,
			)
		}
		if !current.Radio.Enabled {
			return domain.DeviceConfiguration{}, domain.FailedPrecondition(
				operation,
				"the cellular radio is still recovering and cannot start mobile data yet",
				nil,
			)
		}
		requestedFamily, err := bearerIPFamilyValue(request.IPFamily)
		if err != nil {
			return domain.DeviceConfiguration{}, domain.InvalidArgument(operation, err.Error())
		}
		requestedAPN := strings.TrimSpace(request.APN)
		if invalidAPN(requestedAPN) {
			return domain.DeviceConfiguration{}, domain.InvalidArgument(
				operation,
				"apn must be empty for automatic selection or contain only ASCII letters, digits, dots, and hyphens",
			)
		}
		effectiveAPN := requestedAPN
		if effectiveAPN == "" {
			effectiveAPN = strings.TrimSpace(current.AutomaticAPN)
		}
		reuseExisting := matchingConnectedData(
			current.DataConnections,
			effectiveAPN,
			requestedFamily,
		)
		if _, err := p.activateOwnedBearerData(
			bounded,
			modemPath,
			request.LineID,
			requestedAPN,
			current.AutomaticAPN,
			requestedFamily,
			reuseExisting,
		); err != nil {
			return domain.DeviceConfiguration{}, err
		}
		verified, _, _, err := p.readDeviceConfiguration(bounded, request.LineID, operation)
		if err != nil {
			if rollbackErr := p.cleanupOwnedBearerData(modemPath, request.LineID); rollbackErr != nil {
				return domain.DeviceConfiguration{}, domain.VerificationFailed(
					operation,
					"cellular data state could not be verified and bearer rollback failed",
					errors.Join(err, rollbackErr),
				)
			}
			return domain.DeviceConfiguration{}, err
		}
		if !matchingConnectedData(verified.DataConnections, effectiveAPN, requestedFamily) {
			verificationErr := errors.New(
				"ModemManager activated the modem but no matching connected Internet bearer was reported",
			)
			if rollbackErr := p.cleanupOwnedBearerData(modemPath, request.LineID); rollbackErr != nil {
				return domain.DeviceConfiguration{}, domain.VerificationFailed(
					operation,
					"cellular data verification failed and bearer rollback failed",
					errors.Join(verificationErr, rollbackErr),
				)
			}
			return domain.DeviceConfiguration{}, domain.VerificationFailed(
				operation,
				verificationErr.Error(),
				verificationErr,
			)
		}
		return verified, nil
	case domain.DeviceConfigurationDisconnectData:
		if !current.Capabilities.DataConnection.Writable {
			return domain.DeviceConfiguration{}, domain.NotSupported(
				operation,
				current.Capabilities.DataConnection.Reason,
			)
		}
		deactivated, err := p.deactivateOwnedBearerData(
			bounded,
			modemPath,
			request.LineID,
		)
		if err != nil {
			return domain.DeviceConfiguration{}, err
		}
		if !deactivated {
			if current.NetworkEnabled {
				return domain.DeviceConfiguration{}, domain.Conflict(
					operation,
					"the connected cellular bearer is not owned by ModemDeck",
				)
			}
			return current, nil
		}
	case domain.DeviceConfigurationRestartModem:
		if !current.Capabilities.Radio.Writable {
			return domain.DeviceConfiguration{}, domain.NotSupported(
				operation,
				current.Capabilities.Radio.Reason,
			)
		}
		if _, err := p.call(
			bounded,
			modemPath,
			modemInterface+".Reset",
			operation,
			"ModemManager failed to restart the modem",
		); err != nil {
			return domain.DeviceConfiguration{}, err
		}
		return current, nil
	case domain.DeviceConfigurationResetUSB:
		if !current.Capabilities.USBReset.Writable {
			return domain.DeviceConfiguration{}, domain.NotSupported(
				operation,
				current.Capabilities.USBReset.Reason,
			)
		}
		parsed := ParseManagedObjects(objects, p.ids)
		line, found := findLine(parsed.Lines, request.LineID)
		if !found {
			return domain.DeviceConfiguration{}, domain.NotFound(operation, "line was not found")
		}
		if err := p.usbRecovery.Reset(bounded, line.PhysicalDevice); err != nil {
			return domain.DeviceConfiguration{}, mapUSBRecoveryError(operation, err)
		}
		return current, nil
	case domain.DeviceConfigurationSetVoLTEPolicy:
		return domain.DeviceConfiguration{}, domain.NotSupported(
			operation,
			"this ModemManager build does not expose a standard VoLTE policy interface",
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
		if !*request.RadioEnabled && verified.NetworkEnabled {
			return domain.DeviceConfiguration{}, domain.VerificationFailed(
				operation,
				"packet data remained connected after airplane mode was enabled",
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
	if _, err := p.resolveProviderIdentity(ctx, operation); err != nil {
		return domain.DeviceConfiguration{}, nil, "", err
	}
	objects, err := p.managedObjects(ctx, operation)
	if err != nil {
		return domain.DeviceConfiguration{}, nil, "", err
	}
	parsed := ParseManagedObjects(objects, p.ids)
	p.projectVoiceCapabilities(ctx, operation, &parsed)
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
	radioDesiredEnabled := false
	radioDesiredEnabledKnown := line.SavedPolicySupported && p.radioStates != nil
	if radioDesiredEnabledKnown {
		radioDesiredEnabled = p.radioStates.enabled(line.ID)
	}
	var accessTechnologies *uint32
	if line.AccessTechnologiesKnown {
		value := line.AccessTechnologies
		accessTechnologies = &value
	}
	configuration := domain.DeviceConfiguration{
		LineID:     line.ID,
		ObservedAt: p.now().UTC(),
		Identity: domain.DeviceIdentity{
			Manufacturer:        line.Manufacturer,
			Model:               line.Model,
			Firmware:            line.Revision,
			EquipmentIdentifier: line.EquipmentIdentifier,
		},
		Details: domain.DeviceHardwareDetails{
			HardwareRevision:   line.HardwareRevision,
			PrimaryPort:        line.PrimaryPort,
			AccessTechnologies: accessTechnologies,
			SNR:                line.SignalSNR,
			Ports:              append([]domain.ModemPort(nil), line.Ports...),
		},
		Radio: domain.RadioConfiguration{
			Enabled:        radioEnabled,
			EnabledKnown:   radioKnown,
			PowerState:     modemPowerStateName(line.PowerStateCode),
			PowerStateCode: line.PowerStateCode,
		},
		FlightMode:        radioDesiredEnabledKnown && !radioDesiredEnabled,
		FlightModeKnown:   radioDesiredEnabledKnown,
		DataConnections:   []domain.DataConnection{},
		VoiceVerification: line.VoiceVerification,
	}
	configuration.Capabilities = genericConfigurationCapabilities(interfaces, line)
	configuration.Capabilities.USBReset = p.usbRecovery.Capability(line.PhysicalDevice)
	configuration.VoLTE.Provisioning = modemManagerVoLTEProvisioning(modemProperties)
	if _, found := interfaces[profileManagerInterface]; found {
		profiles, profileErr := p.connectionProfilesAtPath(ctx, modemPath, operation)
		if profileErr == nil {
			configuration.VoLTE.Provisioning.IMSProfileReported = true
			configuration.VoLTE.Provisioning.IMSProfilePresent = hasIMSProfile(profiles)
		}
	}
	configuration.AutomaticAPN = p.resolveAutomaticAPN(
		ctx,
		objects,
		interfaces,
		operation,
	)

	bearerPaths, _ := objectPathValuesProperty(modemProperties, "Bearers")
	for _, bearerPath := range bearerPaths {
		bearerProperties, found, err := p.referencedBearerProperties(
			ctx,
			objects,
			bearerPath,
			operation,
		)
		if err != nil {
			return domain.DeviceConfiguration{}, nil, "", err
		}
		if !found {
			continue
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

func modemManagerVoLTEProvisioning(properties Properties) domain.VoLTEProvisioning {
	carrierConfiguration, carrierConfigurationReported :=
		stringProperty(properties, "CarrierConfiguration")
	carrierRevision, carrierRevisionReported :=
		stringProperty(properties, "CarrierConfigurationRevision")
	return domain.VoLTEProvisioning{
		Backend:                              "modemmanager",
		CarrierConfiguration:                 strings.TrimSpace(carrierConfiguration),
		CarrierConfigurationReported:         carrierConfigurationReported,
		CarrierConfigurationRevision:         strings.TrimSpace(carrierRevision),
		CarrierConfigurationRevisionReported: carrierRevisionReported,
	}
}

func hasIMSProfile(profiles []domain.ConnectionProfile) bool {
	for _, profile := range profiles {
		if strings.EqualFold(strings.TrimSpace(profile.APN), "ims") {
			return true
		}
	}
	return false
}

func genericConfigurationCapabilities(
	interfaces Interfaces,
	line domain.Line,
) domain.DeviceConfigurationCapabilities {
	modemManager := "modemmanager"
	agent := "modemdeck_agent"
	vendor := "vendor_extension"
	_, voiceInterfacePresent := interfaces[voiceInterface]
	voiceSupported :=
		line.Capabilities.Dial ||
			line.Capabilities.AnswerCall ||
			line.Capabilities.RejectCall ||
			line.Capabilities.HangupCall
	_, dataWritable := interfaces[modemInterface]
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
		dataReason = "ModemManager does not expose bearer creation for this line"
	}
	ussdReason := ""
	if !ussdSupported {
		ussdReason = "ModemManager USSD is not currently exposed for this line"
	}
	profileReason := ""
	if !profilesSupported {
		profileReason = "ModemManager ProfileManager is not currently exposed for this line"
	}
	return domain.DeviceConfigurationCapabilities{
		Voice: domain.FeatureCapability{
			Backend:     modemManager,
			Supported:   voiceSupported,
			Implemented: true,
			Readable:    true,
			Reason: func() string {
				if voiceSupported {
					return ""
				}
				if voiceInterfacePresent {
					return "ModemManager Voice is exposed, but USB call control did not pass runtime verification"
				}
				return "ModemManager Voice is not exposed for this line"
			}(),
		},
		Radio: domain.FeatureCapability{
			Backend:     modemManager,
			Supported:   true,
			Implemented: true,
			Readable:    true,
			Writable:    true,
		},
		DataConnection: domain.FeatureCapability{
			Backend:     agent,
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
			Reason:  "no documented modem-family vendor profile was resolved",
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
			Backend:     modemManager,
			Supported:   ussdSupported,
			Implemented: ussdSupported,
			Readable:    ussdSupported,
			Writable:    ussdSupported,
			Reason:      ussdReason,
		},
		ConnectionProfile: domain.FeatureCapability{
			Backend:     modemManager,
			Supported:   profilesSupported,
			Implemented: profilesSupported,
			Readable:    profilesSupported,
			Writable:    profilesSupported,
			Reason:      profileReason,
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
	connection.BearerType, _ = uint32Property(properties, "BearerType")
	bearerProperties, found, err := nestedProperties(properties, "Properties")
	if err != nil {
		return domain.DataConnection{}, err
	}
	if found {
		connection.APN, _ = stringProperty(bearerProperties, "apn")
		connection.APNType, _ = uint32Property(bearerProperties, "apn-type")
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

func (p *Provider) referencedBearerProperties(
	ctx context.Context,
	objects ManagedObjects,
	path dbus.ObjectPath,
	operation string,
) (Properties, bool, error) {
	if interfaces, found := objects[path]; found {
		if properties, found := interfaces[bearerInterface]; found {
			return properties, true, nil
		}
	}
	body, err := p.call(
		ctx,
		path,
		propertiesInterface+".GetAll",
		operation,
		"ModemManager failed to read a referenced bearer",
		bearerInterface,
	)
	if err != nil {
		if operationError, ok := domain.AsOperationError(err); ok &&
			operationError.Code == domain.ErrorNotFound {
			return nil, false, nil
		}
		return nil, false, err
	}
	properties := Properties{}
	if err := dbus.Store(body, &properties); err != nil {
		return nil, false, domain.Internal(
			operation,
			"ModemManager bearer properties response was malformed",
			err,
		)
	}
	return properties, true, nil
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
	if len(configuration.DNS) == 0 {
		for _, key := range []string{"dns1", "dns2"} {
			server, _ := stringProperty(values, key)
			server = strings.TrimSpace(server)
			if server == "" || slicesContains(configuration.DNS, server) {
				continue
			}
			configuration.DNS = append(configuration.DNS, server)
		}
	}
	configuration.MTU, _ = uint32Property(values, "mtu")
	return configuration, nil
}

func slicesContains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
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

func initialEPSBearerAPN(objects ManagedObjects, interfaces Interfaces) string {
	properties, found := interfaces[modem3GPPInterface]
	if !found {
		return ""
	}
	settings, found, err := nestedProperties(properties, "InitialEpsBearerSettings")
	if err == nil && found {
		if apn := apnFromSettings(settings); apn != "" {
			return apn
		}
	}
	path, found := objectPathProperty(properties, "InitialEpsBearer")
	if !found || !path.IsValid() || path == "/" {
		return ""
	}
	bearerInterfaces, found := objects[path]
	if !found {
		return ""
	}
	bearerProperties, found := bearerInterfaces[bearerInterface]
	if !found {
		return ""
	}
	return bearerAPN(bearerProperties)
}

func (p *Provider) resolveAutomaticAPN(
	ctx context.Context,
	objects ManagedObjects,
	interfaces Interfaces,
	operation string,
) string {
	if apn := initialEPSBearerAPN(objects, interfaces); apn != "" {
		return apn
	}
	properties, found := interfaces[modem3GPPInterface]
	if !found {
		return ""
	}
	path, found := objectPathProperty(properties, "InitialEpsBearer")
	if !found || !path.IsValid() || path == "/" {
		return ""
	}
	bearerProperties, found, err := p.referencedBearerProperties(
		ctx,
		objects,
		path,
		operation,
	)
	if err != nil || !found {
		return ""
	}
	return bearerAPN(bearerProperties)
}

func bearerAPN(bearerProperties Properties) string {
	settings, found, err := nestedProperties(bearerProperties, "Properties")
	if err != nil || !found {
		return ""
	}
	return apnFromSettings(settings)
}

func apnFromSettings(settings Properties) string {
	apn, _ := stringProperty(settings, "apn")
	apn = strings.TrimSpace(apn)
	if invalidAPN(apn) {
		return ""
	}
	return apn
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
	case domain.DeviceConfigurationRestartModem:
		if request.RadioEnabled != nil || request.APN != "" || request.IPFamily != "" || request.VoLTEPolicy != "" {
			return domain.InvalidArgument(operation, "restart_modem does not accept operation parameters")
		}
	case domain.DeviceConfigurationResetUSB:
		if request.RadioEnabled != nil || request.APN != "" || request.IPFamily != "" || request.VoLTEPolicy != "" {
			return domain.InvalidArgument(operation, "reset_usb does not accept operation parameters")
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

func mapUSBRecoveryError(operation string, err error) error {
	var unsupported *usbrecovery.UnsupportedError
	var cooldown *usbrecovery.CooldownError
	var permission *usbrecovery.PermissionError
	switch {
	case errors.As(err, &unsupported):
		return domain.NotSupported(operation, unsupported.Reason)
	case errors.Is(err, usbrecovery.ErrBusy):
		return domain.Conflict(operation, "another USB reset is already in progress")
	case errors.As(err, &cooldown):
		return domain.FailedPrecondition(operation, cooldown.Error(), err)
	case errors.As(err, &permission):
		return domain.PermissionDenied(operation, permission.Error(), err)
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return domain.Unavailable(operation, "USB reset did not complete before the request ended", err)
	default:
		return domain.Unavailable(operation, "Linux usbfs failed to reset the modem", err)
	}
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

func matchingConnectedData(connections []domain.DataConnection, apn string, family uint32) bool {
	for _, connection := range connections {
		if !connection.Connected {
			continue
		}
		if connection.BearerType != 0 {
			if connection.BearerType != bearerTypeDefault {
				continue
			}
		} else if connection.APNType&domain.APNTypeDefault == 0 {
			continue
		}
		if apn != "" && connection.APN != apn {
			continue
		}
		if !connectedFamilyMatches(connection, family) {
			continue
		}
		return true
	}
	return false
}

func connectedFamilyMatches(connection domain.DataConnection, family uint32) bool {
	switch family {
	case 0, bearerIPFamilyAny:
		return true
	case bearerIPFamilyIPv4:
		return connection.IPFamily == "ipv4" ||
			connection.IPFamily == "ipv4v6" && ipConfigurationAvailable(connection.IPv4)
	case bearerIPFamilyIPv6:
		return connection.IPFamily == "ipv6" ||
			connection.IPFamily == "ipv4v6" && ipConfigurationAvailable(connection.IPv6)
	case bearerIPFamilyIPv4V6:
		switch connection.IPFamily {
		case "ipv4":
			return ipConfigurationAvailable(connection.IPv4)
		case "ipv6":
			return ipConfigurationAvailable(connection.IPv6)
		case "ipv4v6":
			return ipConfigurationAvailable(connection.IPv4) ||
				ipConfigurationAvailable(connection.IPv6)
		}
	}
	return false
}

func ipConfigurationAvailable(configuration domain.IPConfiguration) bool {
	return configuration.Method != "" ||
		configuration.Address != "" ||
		configuration.Gateway != "" ||
		len(configuration.DNS) > 0
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
