package modemmanager

import (
	"context"
	"sort"
	"strings"

	"github.com/godbus/dbus/v5"
	"github.com/human-agent65535/modemdeck/agent/internal/domain"
)

func (p *Provider) SIMStatus(ctx context.Context, lineID string) (domain.SIMStatus, error) {
	const operation = "read_sim_status"
	line, _, interfaces, objects, err := p.resolveLineServices(ctx, operation, lineID)
	if err != nil {
		return domain.SIMStatus{}, err
	}
	modemProperties := interfaces[modemInterface]
	status := domain.SIMStatus{
		LineID:                 line.ID,
		Present:                line.SIMPresent,
		Identifier:             line.SIMIdentifier,
		IMSI:                   line.IMSI,
		HomeOperatorCode:       line.HomeOperatorCode,
		HomeOperatorName:       line.HomeOperatorName,
		ServingOperatorCode:    line.ServingOperatorCode,
		ServingOperatorName:    line.ServingOperatorName,
		RegistrationStateKnown: line.RegistrationStateKnown,
		RegistrationStateCode:  line.RegistrationStateCode,
		RegistrationState:      line.RegistrationState,
		Roaming:                line.Roaming,
		OperatorIdentifier:     line.OperatorIdentifier,
		OperatorName:           line.OperatorName,
		UnlockRetries:          map[string]uint32{},
		ObservedAt:             p.now().UTC(),
	}
	status.UnlockRequiredCode, _ = uint32Property(modemProperties, "UnlockRequired")
	status.UnlockRequired = modemLockName(status.UnlockRequiredCode)
	status.UnlockRetries = unlockRetriesProperty(modemProperties)
	if !line.SIMPresent {
		return status, nil
	}
	simPath := dbus.ObjectPath(line.SIMPath)
	simInterfaces := objects[simPath]
	if simProperties, found := simInterfaces[simInterface]; found {
		status.Active, _ = boolProperty(simProperties, "Active")
		status.EID, _ = stringProperty(simProperties, "Eid")
	}
	return status, nil
}

func (p *Provider) SIMCommand(
	ctx context.Context,
	request domain.SIMCommandRequest,
) (domain.CommandReceipt, error) {
	const operation = "sim_command"
	request.RequestID = strings.TrimSpace(request.RequestID)
	request.LineID = strings.TrimSpace(request.LineID)
	if err := validateRequestID(operation, request.RequestID); err != nil {
		return domain.CommandReceipt{}, err
	}
	if request.LineID == "" {
		return domain.CommandReceipt{}, domain.InvalidArgument(operation, "line id is required")
	}
	if err := validateSIMCommand(request); err != nil {
		return domain.CommandReceipt{}, err
	}

	p.configMu.Lock()
	defer p.configMu.Unlock()
	line, _, _, _, err := p.resolveLineServices(ctx, operation, request.LineID)
	if err != nil {
		return domain.CommandReceipt{}, err
	}
	if !line.SIMPresent || !dbus.ObjectPath(line.SIMPath).IsValid() {
		return domain.CommandReceipt{}, domain.FailedPrecondition(operation, "line has no active SIM", nil)
	}
	simPath := dbus.ObjectPath(line.SIMPath)
	switch request.Operation {
	case domain.SIMSendPIN:
		_, err = p.call(ctx, simPath, simInterface+".SendPin", operation, "ModemManager failed to submit the SIM PIN", request.PIN)
	case domain.SIMSendPUK:
		_, err = p.call(ctx, simPath, simInterface+".SendPuk", operation, "ModemManager failed to submit the SIM PUK", request.PUK, request.NewPIN)
	case domain.SIMEnablePIN:
		_, err = p.call(ctx, simPath, simInterface+".EnablePin", operation, "ModemManager failed to change SIM PIN protection", request.PIN, *request.Enabled)
	case domain.SIMChangePIN:
		_, err = p.call(ctx, simPath, simInterface+".ChangePin", operation, "ModemManager failed to change the SIM PIN", request.PIN, request.NewPIN)
	}
	if err != nil {
		return domain.CommandReceipt{}, err
	}
	return domain.CommandReceipt{RequestID: request.RequestID, ResourceID: line.ID}, nil
}

func (p *Provider) ConnectionProfiles(
	ctx context.Context,
	lineID string,
) ([]domain.ConnectionProfile, error) {
	const operation = "list_connection_profiles"
	_, linePath, interfaces, _, err := p.resolveLineServices(ctx, operation, lineID)
	if err != nil {
		return nil, err
	}
	if _, found := interfaces[profileManagerInterface]; !found {
		return nil, domain.NotSupported(operation, "line does not expose the ModemManager ProfileManager interface")
	}
	body, err := p.call(
		ctx,
		linePath,
		profileManagerInterface+".List",
		operation,
		"ModemManager failed to list connection profiles",
	)
	if err != nil {
		return nil, err
	}
	var values []map[string]dbus.Variant
	if err := dbus.Store(body, &values); err != nil {
		return nil, domain.Internal(operation, "ModemManager connection profile response was malformed", err)
	}
	profiles := make([]domain.ConnectionProfile, 0, len(values))
	for _, value := range values {
		profiles = append(profiles, parseConnectionProfile(value))
	}
	sort.Slice(profiles, func(i, j int) bool {
		if profiles[i].ProfileID != profiles[j].ProfileID {
			return profiles[i].ProfileID < profiles[j].ProfileID
		}
		return profiles[i].ProfileName < profiles[j].ProfileName
	})
	return profiles, nil
}

func (p *Provider) SaveConnectionProfile(
	ctx context.Context,
	request domain.SaveConnectionProfileRequest,
) (domain.ConnectionProfile, error) {
	const operation = "save_connection_profile"
	request.RequestID = strings.TrimSpace(request.RequestID)
	request.LineID = strings.TrimSpace(request.LineID)
	if err := validateRequestID(operation, request.RequestID); err != nil {
		return domain.ConnectionProfile{}, err
	}
	properties, err := connectionProfileProperties(request)
	if err != nil {
		return domain.ConnectionProfile{}, err
	}

	p.configMu.Lock()
	defer p.configMu.Unlock()
	_, linePath, interfaces, _, err := p.resolveLineServices(ctx, operation, request.LineID)
	if err != nil {
		return domain.ConnectionProfile{}, err
	}
	if _, found := interfaces[profileManagerInterface]; !found {
		return domain.ConnectionProfile{}, domain.NotSupported(
			operation,
			"line does not expose the ModemManager ProfileManager interface",
		)
	}
	body, err := p.call(
		ctx,
		linePath,
		profileManagerInterface+".Set",
		operation,
		"ModemManager failed to save the connection profile",
		properties,
	)
	if err != nil {
		return domain.ConnectionProfile{}, err
	}
	var saved map[string]dbus.Variant
	if err := dbus.Store(body, &saved); err != nil {
		return domain.ConnectionProfile{}, domain.Internal(
			operation,
			"ModemManager saved profile response was malformed",
			err,
		)
	}
	return parseConnectionProfile(saved), nil
}

func (p *Provider) DeleteConnectionProfile(
	ctx context.Context,
	request domain.DeleteConnectionProfileRequest,
) (domain.CommandReceipt, error) {
	const operation = "delete_connection_profile"
	request.RequestID = strings.TrimSpace(request.RequestID)
	request.LineID = strings.TrimSpace(request.LineID)
	request.ProfileName = strings.TrimSpace(request.ProfileName)
	if err := validateRequestID(operation, request.RequestID); err != nil {
		return domain.CommandReceipt{}, err
	}
	if request.LineID == "" || (request.ProfileID == nil && request.ProfileName == "") {
		return domain.CommandReceipt{}, domain.InvalidArgument(
			operation,
			"line id and profile_id or profile_name are required",
		)
	}
	if request.ProfileID != nil && *request.ProfileID < 0 {
		return domain.CommandReceipt{}, domain.InvalidArgument(operation, "profile_id must not be negative")
	}
	if invalidOptionalText(request.ProfileName, 100) {
		return domain.CommandReceipt{}, domain.InvalidArgument(operation, "profile_name is invalid")
	}

	p.configMu.Lock()
	defer p.configMu.Unlock()
	_, linePath, interfaces, _, err := p.resolveLineServices(ctx, operation, request.LineID)
	if err != nil {
		return domain.CommandReceipt{}, err
	}
	if _, found := interfaces[profileManagerInterface]; !found {
		return domain.CommandReceipt{}, domain.NotSupported(
			operation,
			"line does not expose the ModemManager ProfileManager interface",
		)
	}
	properties := map[string]dbus.Variant{}
	if request.ProfileID != nil {
		properties["profile-id"] = dbus.MakeVariant(*request.ProfileID)
	}
	if request.ProfileName != "" {
		properties["profile-name"] = dbus.MakeVariant(request.ProfileName)
	}
	if _, err := p.call(
		ctx,
		linePath,
		profileManagerInterface+".Delete",
		operation,
		"ModemManager failed to delete the connection profile",
		properties,
	); err != nil {
		return domain.CommandReceipt{}, err
	}
	return domain.CommandReceipt{RequestID: request.RequestID, ResourceID: request.LineID}, nil
}

func (p *Provider) USSDStatus(ctx context.Context, lineID string) (domain.USSDStatus, error) {
	const operation = "read_ussd_status"
	line, _, interfaces, _, err := p.resolveLineServices(ctx, operation, lineID)
	if err != nil {
		return domain.USSDStatus{}, err
	}
	properties, found := interfaces[ussdInterface]
	if !found {
		return domain.USSDStatus{}, domain.NotSupported(
			operation,
			"line does not expose the ModemManager USSD interface",
		)
	}
	state, _ := uint32Property(properties, "State")
	notification, _ := stringProperty(properties, "NetworkNotification")
	networkRequest, _ := stringProperty(properties, "NetworkRequest")
	return domain.USSDStatus{
		LineID:              line.ID,
		State:               ussdStateName(state),
		StateCode:           state,
		NetworkNotification: notification,
		NetworkRequest:      networkRequest,
		ObservedAt:          p.now().UTC(),
	}, nil
}

func (p *Provider) USSDCommand(
	ctx context.Context,
	request domain.USSDRequest,
) (domain.USSDResponse, error) {
	const operation = "ussd_command"
	request.RequestID = strings.TrimSpace(request.RequestID)
	request.LineID = strings.TrimSpace(request.LineID)
	request.Command = strings.TrimSpace(request.Command)
	if err := validateRequestID(operation, request.RequestID); err != nil {
		return domain.USSDResponse{}, err
	}
	if request.LineID == "" {
		return domain.USSDResponse{}, domain.InvalidArgument(operation, "line id is required")
	}
	switch request.Action {
	case domain.USSDInitiate, domain.USSDRespond:
		if invalidText(request.Command, 182) {
			return domain.USSDResponse{}, domain.InvalidArgument(operation, "USSD command is invalid")
		}
	case domain.USSDCancel:
		if request.Command != "" {
			return domain.USSDResponse{}, domain.InvalidArgument(operation, "cancel does not accept a command")
		}
	default:
		return domain.USSDResponse{}, domain.InvalidArgument(operation, "unsupported USSD action")
	}

	p.configMu.Lock()
	defer p.configMu.Unlock()
	_, linePath, interfaces, _, err := p.resolveLineServices(ctx, operation, request.LineID)
	if err != nil {
		return domain.USSDResponse{}, err
	}
	if _, found := interfaces[ussdInterface]; !found {
		return domain.USSDResponse{}, domain.NotSupported(
			operation,
			"line does not expose the ModemManager USSD interface",
		)
	}
	method := ussdInterface + ".Cancel"
	args := []any{}
	if request.Action == domain.USSDInitiate {
		method = ussdInterface + ".Initiate"
		args = append(args, request.Command)
	} else if request.Action == domain.USSDRespond {
		method = ussdInterface + ".Respond"
		args = append(args, request.Command)
	}
	body, err := p.call(ctx, linePath, method, operation, "ModemManager USSD request failed", args...)
	if err != nil {
		return domain.USSDResponse{}, err
	}
	result := domain.USSDResponse{}
	if request.Action != domain.USSDCancel {
		if err := dbus.Store(body, &result.Response); err != nil {
			return domain.USSDResponse{}, domain.Internal(
				operation,
				"ModemManager USSD response was malformed",
				err,
			)
		}
	}
	return result, nil
}

func (p *Provider) resolveLineServices(
	ctx context.Context,
	operation string,
	lineID string,
) (domain.Line, dbus.ObjectPath, Interfaces, ManagedObjects, error) {
	lineID = strings.TrimSpace(lineID)
	if lineID == "" {
		return domain.Line{}, "", nil, nil, domain.InvalidArgument(operation, "line id is required")
	}
	identity, err := p.resolveProviderIdentity(ctx, operation)
	if err != nil {
		return domain.Line{}, "", nil, nil, err
	}
	objects, err := p.managedObjects(ctx, operation)
	if err != nil {
		return domain.Line{}, "", nil, nil, err
	}
	objects, err = p.hydrateReferencedSIMs(ctx, operation, objects)
	if err != nil {
		return domain.Line{}, "", nil, nil, err
	}
	parsed := ParseManagedObjects(objects, identity)
	line, found := findLine(parsed.Lines, lineID)
	if !found {
		return domain.Line{}, "", nil, nil, domain.NotFound(operation, "line was not found")
	}
	linePath := parsed.LinePaths[line.ID]
	interfaces, found := objects[linePath]
	if !found {
		return domain.Line{}, "", nil, nil, domain.Internal(
			operation,
			"ModemManager line path was missing from the object snapshot",
			nil,
		)
	}
	return line, linePath, interfaces, objects, nil
}

func validateSIMCommand(request domain.SIMCommandRequest) error {
	const operation = "sim_command"
	switch request.Operation {
	case domain.SIMSendPIN:
		if !validPIN(request.PIN) || request.PUK != "" || request.NewPIN != "" || request.Enabled != nil {
			return domain.InvalidArgument(operation, "send_pin accepts one valid PIN")
		}
	case domain.SIMSendPUK:
		if !validPUK(request.PUK) || !validPIN(request.NewPIN) || request.PIN != "" || request.Enabled != nil {
			return domain.InvalidArgument(operation, "send_puk requires a valid PUK and new PIN")
		}
	case domain.SIMEnablePIN:
		if !validPIN(request.PIN) || request.Enabled == nil || request.PUK != "" || request.NewPIN != "" {
			return domain.InvalidArgument(operation, "enable_pin requires a valid PIN and enabled value")
		}
	case domain.SIMChangePIN:
		if !validPIN(request.PIN) || !validPIN(request.NewPIN) || request.PUK != "" || request.Enabled != nil {
			return domain.InvalidArgument(operation, "change_pin requires the current and new PIN")
		}
	default:
		return domain.InvalidArgument(operation, "unsupported SIM operation")
	}
	return nil
}

func validPIN(value string) bool {
	return digitsOnly(value, 4, 8)
}

func validPUK(value string) bool {
	return digitsOnly(value, 8, 8)
}

func digitsOnly(value string, minimum, maximum int) bool {
	if len(value) < minimum || len(value) > maximum {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

func invalidOptionalText(value string, maximum int) bool {
	return value != "" && invalidText(value, maximum)
}

func connectionProfileProperties(
	request domain.SaveConnectionProfileRequest,
) (map[string]dbus.Variant, error) {
	const operation = "save_connection_profile"
	if request.LineID == "" {
		return nil, domain.InvalidArgument(operation, "line id is required")
	}
	request.ProfileName = strings.TrimSpace(request.ProfileName)
	request.APN = strings.TrimSpace(request.APN)
	request.IPFamily = strings.TrimSpace(request.IPFamily)
	request.User = strings.TrimSpace(request.User)
	if request.ProfileID != nil && *request.ProfileID < 0 {
		return nil, domain.InvalidArgument(operation, "profile_id must not be negative")
	}
	if invalidOptionalText(request.ProfileName, 100) ||
		invalidOptionalText(request.User, 100) ||
		invalidOptionalText(request.Password, 100) ||
		invalidAPN(request.APN) {
		return nil, domain.InvalidArgument(operation, "connection profile contains an invalid value")
	}
	ipType, err := bearerIPFamilyValue(request.IPFamily)
	if err != nil {
		return nil, domain.InvalidArgument(operation, err.Error())
	}
	properties := map[string]dbus.Variant{}
	if request.ProfileID != nil {
		properties["profile-id"] = dbus.MakeVariant(*request.ProfileID)
	}
	if request.ProfileName != "" {
		properties["profile-name"] = dbus.MakeVariant(request.ProfileName)
	}
	if request.APN != "" {
		properties["apn"] = dbus.MakeVariant(request.APN)
	}
	if request.IPFamily != "" {
		properties["ip-type"] = dbus.MakeVariant(ipType)
	}
	if request.APNType != 0 {
		properties["apn-type"] = dbus.MakeVariant(request.APNType)
	}
	if request.AllowedAuth != 0 {
		properties["allowed-auth"] = dbus.MakeVariant(request.AllowedAuth)
	}
	if request.User != "" {
		properties["user"] = dbus.MakeVariant(request.User)
	}
	if request.Password != "" {
		properties["password"] = dbus.MakeVariant(request.Password)
	}
	if request.AccessTypePreference != 0 {
		properties["access-type-preference"] = dbus.MakeVariant(request.AccessTypePreference)
	}
	if request.RoamingAllowance != 0 {
		properties["roaming-allowance"] = dbus.MakeVariant(request.RoamingAllowance)
	}
	if len(properties) == 0 {
		return nil, domain.InvalidArgument(operation, "connection profile is empty")
	}
	return properties, nil
}

func parseConnectionProfile(properties Properties) domain.ConnectionProfile {
	profile := domain.ConnectionProfile{}
	profile.ProfileID, _ = int32Property(properties, "profile-id")
	profile.ProfileName, _ = stringProperty(properties, "profile-name")
	profile.APN, _ = stringProperty(properties, "apn")
	profile.IPType, _ = uint32Property(properties, "ip-type")
	profile.IPFamily = bearerIPFamilyName(profile.IPType)
	profile.APNType, _ = uint32Property(properties, "apn-type")
	profile.AllowedAuth, _ = uint32Property(properties, "allowed-auth")
	profile.User, _ = stringProperty(properties, "user")
	profile.AccessTypePreference, _ = uint32Property(properties, "access-type-preference")
	profile.RoamingAllowance, _ = uint32Property(properties, "roaming-allowance")
	profile.ProfileSource, _ = uint32Property(properties, "profile-source")
	return profile
}

func unlockRetriesProperty(properties Properties) map[string]uint32 {
	result := map[string]uint32{}
	value, found := propertyValue(properties, "UnlockRetries")
	if !found {
		return result
	}
	values, ok := value.(map[uint32]uint32)
	if !ok {
		return result
	}
	for lock, retries := range values {
		result[modemLockName(lock)] = retries
	}
	return result
}

func modemLockName(value uint32) string {
	switch value {
	case 1:
		return "none"
	case 2:
		return "sim-pin"
	case 3:
		return "sim-pin2"
	case 4:
		return "sim-puk"
	case 5:
		return "sim-puk2"
	case 6:
		return "service-provider-pin"
	case 7:
		return "service-provider-puk"
	case 8:
		return "network-pin"
	case 9:
		return "network-puk"
	case 10:
		return "sim-pin"
	case 11:
		return "corporate-pin"
	case 12:
		return "corporate-puk"
	case 13:
		return "fixed-sim-pin"
	case 14:
		return "fixed-sim-puk"
	case 15:
		return "network-subset-pin"
	case 16:
		return "network-subset-puk"
	default:
		return "unknown"
	}
}

func ussdStateName(value uint32) string {
	switch value {
	case 1:
		return "idle"
	case 2:
		return "active"
	case 3:
		return "user-response"
	default:
		return "unknown"
	}
}

var _ domain.LineServiceProvider = (*Provider)(nil)
