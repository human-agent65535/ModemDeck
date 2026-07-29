package modemmanager

import (
	"math"
	"sort"
	"strings"

	"github.com/godbus/dbus/v5"
	"github.com/human-agent65535/modemdeck/agent/internal/domain"
	"github.com/human-agent65535/modemdeck/agent/internal/operator"
)

const (
	modemInterface     = "org.freedesktop.ModemManager1.Modem"
	signalInterface    = "org.freedesktop.ModemManager1.Modem.Signal"
	simInterface       = "org.freedesktop.ModemManager1.Sim"
	voiceInterface     = "org.freedesktop.ModemManager1.Modem.Voice"
	callInterface      = "org.freedesktop.ModemManager1.Call"
	messagingInterface = "org.freedesktop.ModemManager1.Modem.Messaging"
	smsInterface       = "org.freedesktop.ModemManager1.Sms"

	accessTechnologyGSM        uint32 = 1 << 1
	accessTechnologyGSMCompact uint32 = 1 << 2
	accessTechnologyGPRS       uint32 = 1 << 3
	accessTechnologyEDGE       uint32 = 1 << 4
	accessTechnologyUMTS       uint32 = 1 << 5
	accessTechnologyHSDPA      uint32 = 1 << 6
	accessTechnologyHSUPA      uint32 = 1 << 7
	accessTechnologyHSPA       uint32 = 1 << 8
	accessTechnologyHSPAPlus   uint32 = 1 << 9
	accessTechnologyLTE        uint32 = 1 << 14
)

type Properties = map[string]dbus.Variant
type Interfaces = map[string]Properties
type ManagedObjects = map[dbus.ObjectPath]Interfaces

type ParsedObjects struct {
	Lines        []domain.Line
	Calls        []domain.Call
	Messages     []domain.Message
	LinePaths    map[string]dbus.ObjectPath
	CallPaths    map[string]dbus.ObjectPath
	CallBackends map[string]callControlBackend
	ATCallLines  map[string]string
	MessagePaths map[string]dbus.ObjectPath
	ids          *instanceIDs
}

func ParseManagedObjects(objects ManagedObjects, ids *instanceIDs) ParsedObjects {
	parsed := ParsedObjects{
		Lines:        []domain.Line{},
		Calls:        []domain.Call{},
		Messages:     []domain.Message{},
		LinePaths:    make(map[string]dbus.ObjectPath),
		CallPaths:    make(map[string]dbus.ObjectPath),
		CallBackends: make(map[string]callControlBackend),
		ATCallLines:  make(map[string]string),
		MessagePaths: make(map[string]dbus.ObjectPath),
		ids:          ids,
	}
	seenCalls := make(map[string]struct{})
	seenMessages := make(map[string]struct{})

	for path, interfaces := range objects {
		modemProperties, ok := interfaces[modemInterface]
		if !ok {
			continue
		}

		line := domain.Line{
			State:                    "unknown",
			Drivers:                  []string{},
			OwnNumbers:               []string{},
			EmergencyNumbers:         []string{},
			CallIDs:                  []string{},
			MessageIDs:               []string{},
			SupportedMessageStorages: []uint32{},
			Capabilities: domain.LineCapabilities{
				ModemInterface: true,
			},
		}

		line.Manufacturer, _ = stringProperty(modemProperties, "Manufacturer")
		line.Model, _ = stringProperty(modemProperties, "Model")
		line.Revision, _ = stringProperty(modemProperties, "Revision")
		line.HardwareRevision, _ = stringProperty(modemProperties, "HardwareRevision")
		line.DeviceIdentifier, _ = stringProperty(modemProperties, "DeviceIdentifier")
		line.EquipmentIdentifier, _ = stringProperty(modemProperties, "EquipmentIdentifier")
		line.Device, _ = stringProperty(modemProperties, "Device")
		line.PhysicalDevice, _ = stringProperty(modemProperties, "Physdev")
		line.Drivers, _ = stringsProperty(modemProperties, "Drivers")
		line.Plugin, _ = stringProperty(modemProperties, "Plugin")
		line.PrimaryPort, _ = stringProperty(modemProperties, "PrimaryPort")
		line.Ports, _ = modemPortsProperty(modemProperties, "Ports")
		line.StateCode, _ = int32Property(modemProperties, "State")
		line.State = modemStateName(line.StateCode)
		line.PowerStateCode, _ = uint32Property(modemProperties, "PowerState")
		line.AccessTechnologies, line.AccessTechnologiesKnown =
			uint32Property(modemProperties, "AccessTechnologies")
		line.AccessTechnologiesKnown =
			line.AccessTechnologiesKnown && line.AccessTechnologies != 0
		line.SignalQuality, line.SignalQualityRecent, line.SignalQualityKnown = signalQualityProperty(modemProperties)
		if signalProperties, found := interfaces[signalInterface]; found {
			line.SignalDBM, line.SignalRSRP, line.SignalRSRQ, line.SignalSNR =
				extendedSignalProperties(
					signalProperties,
					line.AccessTechnologies,
				)
			rate, rateKnown := uint32Property(signalProperties, "Rate")
			line.SignalMetricsRecent =
				rateKnown &&
					rate > 0 &&
					line.StateCode >= modemStateEnabled &&
					signalMetricsAvailable(line)
		}
		line.OwnNumbers, _ = stringsProperty(modemProperties, "OwnNumbers")

		if simPath, present := objectPathProperty(modemProperties, "Sim"); present && simPath != "/" {
			line.SIMPresent = true
			line.SIMPath = string(simPath)
			if simProperties, found := objects[simPath][simInterface]; found {
				line.Capabilities.SIMInterface = true
				line.SIMIdentifier, _ = stringProperty(simProperties, "SimIdentifier")
				line.IMSI, _ = stringProperty(simProperties, "Imsi")
				line.HomeOperatorCode, _ = stringProperty(simProperties, "OperatorIdentifier")
				line.HomeOperatorName, _ = stringProperty(simProperties, "OperatorName")
				if details, found := operator.Lookup(line.HomeOperatorCode); found {
					line.HomeCountryISO = details.CountryISO
					if strings.TrimSpace(line.HomeOperatorName) == "" {
						line.HomeOperatorName = details.Name
					}
				}
				if strings.TrimSpace(line.HomeCountryISO) == "" {
					if details, found := operator.CountryForIMSI(line.IMSI); found {
						line.HomeCountryISO = details.CountryISO
					}
				}
				line.OperatorIdentifier = line.HomeOperatorCode
				line.OperatorName = line.HomeOperatorName
				line.EmergencyNumbers, _ = stringsProperty(simProperties, "EmergencyNumbers")
			}
		}
		if properties, found := interfaces[modem3GPPInterface]; found {
			line.ServingOperatorCode, _ = stringProperty(properties, "OperatorCode")
			line.ServingOperatorName, _ = stringProperty(properties, "OperatorName")
			if details, resolved := operator.Lookup(line.ServingOperatorCode); resolved {
				line.ServingCountryISO = details.CountryISO
				if strings.TrimSpace(line.ServingOperatorName) == "" {
					line.ServingOperatorName = details.Name
				}
			}
			line.RegistrationStateCode, line.RegistrationStateKnown =
				uint32Property(properties, "RegistrationState")
			if line.RegistrationStateKnown {
				line.RegistrationState = registrationStateName(line.RegistrationStateCode)
				line.Roaming = registrationStateIsRoaming(line.RegistrationStateCode)
			}
		}

		identity := stableLineIdentity(line)
		line.ID = identity.id
		line.IdentityPersistent = identity.persistent
		line.IdentitySource = identity.source
		line.SavedPolicySupported = identity.savedPolicySupported
		line.UnsupportedPolicyReason = identity.unsupportedPolicyReason
		routable := line.ID != ""
		if routable {
			parsed.LinePaths[line.ID] = path
		}

		if voiceProperties, found := interfaces[voiceInterface]; found {
			line.Capabilities.VoiceInterface = true
			line.Capabilities.Dial = routable
			line.Capabilities.AnswerCall = routable
			line.Capabilities.RejectCall = routable
			line.Capabilities.HangupCall = routable
			line.Capabilities.SendDTMF = routable
			line.EmergencyOnly, _ = boolProperty(voiceProperties, "EmergencyOnly")
			if routable {
				parsed.CallBackends[line.ID] = callControlModemManager
			}

			if routable {
				callPaths, _ := objectPathValuesProperty(voiceProperties, "Calls")
				for _, callPath := range callPaths {
					callProperties, found := objects[callPath][callInterface]
					if !found {
						continue
					}
					call := parseCall(callPath, line.ID, callProperties, ids)
					line.CallIDs = append(line.CallIDs, call.ID)
					if _, duplicate := seenCalls[call.ID]; !duplicate {
						seenCalls[call.ID] = struct{}{}
						parsed.CallPaths[call.ID] = callPath
						parsed.Calls = append(parsed.Calls, call)
					}
				}
			}
		}

		if messagingProperties, found := interfaces[messagingInterface]; found {
			line.Capabilities.MessagingInterface = true
			line.Capabilities.SendMessage = routable
			line.SupportedMessageStorages, _ = uint32sProperty(messagingProperties, "SupportedStorages")
			line.DefaultMessageStorage, _ = uint32Property(messagingProperties, "DefaultStorage")

			if routable {
				messagePaths, _ := objectPathValuesProperty(messagingProperties, "Messages")
				for _, messagePath := range messagePaths {
					messageProperties, found := objects[messagePath][smsInterface]
					if !found {
						continue
					}
					pduType, known := uint32Property(messageProperties, "PduType")
					if _, business := smsBusinessDirection(classifySMSPDU(pduType)); !known || !business {
						continue
					}
					message := parseMessage(messagePath, line.ID, messageProperties, ids)
					line.MessageIDs = append(line.MessageIDs, message.ID)
					if _, duplicate := seenMessages[message.ID]; !duplicate {
						seenMessages[message.ID] = struct{}{}
						parsed.MessagePaths[message.ID] = messagePath
						parsed.Messages = append(parsed.Messages, message)
					}
				}
			}
		}

		sort.Strings(line.CallIDs)
		sort.Strings(line.MessageIDs)
		parsed.Lines = append(parsed.Lines, line)
	}

	accessTechnologiesByLine := make(map[string]uint32, len(parsed.Lines))
	for _, line := range parsed.Lines {
		accessTechnologiesByLine[line.ID] = line.AccessTechnologies
	}
	for index := range parsed.Calls {
		call := &parsed.Calls[index]
		call.Bearer = cellularVoiceBearer(
			call.State,
			call.Bearer,
			accessTechnologiesByLine[call.LineID],
		)
	}

	sort.Slice(parsed.Lines, func(i, j int) bool {
		return parsed.Lines[i].ID < parsed.Lines[j].ID
	})
	sort.Slice(parsed.Calls, func(i, j int) bool {
		return parsed.Calls[i].ID < parsed.Calls[j].ID
	})
	sort.Slice(parsed.Messages, func(i, j int) bool {
		return parsed.Messages[i].ID < parsed.Messages[j].ID
	})
	return parsed
}

func cellularVoiceBearer(state, existingBearer string, accessTechnologies uint32) string {
	if existingBearer != "" {
		return existingBearer
	}
	if state != "active" && state != "held" {
		return ""
	}

	switch accessTechnologies {
	case accessTechnologyLTE:
		return "volte"
	case accessTechnologyGSM,
		accessTechnologyGSMCompact,
		accessTechnologyGPRS,
		accessTechnologyEDGE,
		accessTechnologyUMTS,
		accessTechnologyHSDPA,
		accessTechnologyHSUPA,
		accessTechnologyHSPA,
		accessTechnologyHSPAPlus:
		return "cs"
	default:
		return ""
	}
}

const callStateTerminated int32 = 7

func parseCall(path dbus.ObjectPath, lineID string, properties Properties, ids *instanceIDs) domain.Call {
	stateCode, _ := int32Property(properties, "State")
	stateReasonCode, _ := int32Property(properties, "StateReason")
	directionCode, _ := int32Property(properties, "Direction")
	number, _ := stringProperty(properties, "Number")
	multiparty, _ := boolProperty(properties, "Multiparty")
	audioPort, _ := stringProperty(properties, "AudioPort")
	audioFormat, audioFormatKnown := audioFormatProperty(properties)
	return domain.Call{
		ID:              ids.callID(path),
		LineID:          lineID,
		Number:          number,
		Direction:       callDirectionName(directionCode),
		State:           callStateName(stateCode),
		StateCode:       stateCode,
		StateReason:     callStateReasonName(stateReasonCode),
		StateReasonCode: stateReasonCode,
		Multiparty:      multiparty,
		AudioPort:       audioPort,
		AudioFormat:     audioFormat,
		MediaAvailable: audioPort != "" &&
			audioFormatKnown &&
			audioFormat.Encoding != "" &&
			audioFormat.Resolution != "" &&
			audioFormat.Rate > 0,
		// The standard Call interface has no bearer property. Active-call
		// inference is applied later from the associated line's RAT snapshot.
		Bearer: "",
	}
}

func parseMessage(path dbus.ObjectPath, lineID string, properties Properties, ids *instanceIDs) domain.Message {
	stateCode, _ := uint32Property(properties, "State")
	pduType, _ := uint32Property(properties, "PduType")
	number, _ := stringProperty(properties, "Number")
	text, _ := stringProperty(properties, "Text")
	timestamp, _ := stringProperty(properties, "Timestamp")
	messageID := ids.messageID(path)
	if stableID, ok := stableIncomingMessageID(lineID, properties); ok {
		messageID = stableID
	}
	return domain.Message{
		ID:        messageID,
		LineID:    lineID,
		Number:    number,
		Text:      text,
		Direction: messageDirectionName(pduType),
		State:     messageStateName(stateCode),
		StateCode: stateCode,
		Timestamp: timestamp,
	}
}

func modemStateName(code int32) string {
	switch code {
	case -1:
		return "failed"
	case 0:
		return "unknown"
	case 1:
		return "initializing"
	case 2:
		return "locked"
	case 3:
		return "disabled"
	case 4:
		return "disabling"
	case 5:
		return "enabling"
	case 6:
		return "enabled"
	case 7:
		return "searching"
	case 8:
		return "registered"
	case 9:
		return "disconnecting"
	case 10:
		return "connecting"
	case 11:
		return "connected"
	default:
		return "unknown"
	}
}

func registrationStateName(code uint32) string {
	switch code {
	case 0:
		return "idle"
	case 1:
		return "home"
	case 2:
		return "searching"
	case 3:
		return "denied"
	case 4:
		return "unknown"
	case 5:
		return "roaming"
	case 6:
		return "home-sms-only"
	case 7:
		return "roaming-sms-only"
	case 8:
		return "emergency-only"
	case 9:
		return "home-csfb-not-preferred"
	case 10:
		return "roaming-csfb-not-preferred"
	case 11:
		return "attached-rlos"
	default:
		return "unknown"
	}
}

func registrationStateIsRoaming(code uint32) bool {
	switch code {
	case 5, 7, 10:
		return true
	default:
		return false
	}
}

func callStateName(code int32) string {
	switch code {
	case 1:
		return "dialing"
	case 2:
		return "ringing-out"
	case 3:
		return "ringing-in"
	case 4:
		return "active"
	case 5:
		return "held"
	case 6:
		return "waiting"
	case callStateTerminated:
		return "terminated"
	default:
		return "unknown"
	}
}

func callDirectionName(code int32) string {
	switch code {
	case 1:
		return "incoming"
	case 2:
		return "outgoing"
	default:
		return "unknown"
	}
}

func callStateReasonName(code int32) string {
	switch code {
	case 1:
		return "outgoing_started"
	case 2:
		return "incoming_new"
	case 3:
		return "accepted"
	case 4:
		return "terminated"
	case 5:
		return "refused_or_busy"
	case 6:
		return "error"
	case 7:
		return "audio_setup_failed"
	case 8:
		return "transferred"
	case 9:
		return "deflected"
	default:
		return "unknown"
	}
}

func messageStateName(code uint32) string {
	switch code {
	case 1:
		return "stored"
	case 2:
		return "receiving"
	case 3:
		return "received"
	case 4:
		return "sending"
	case 5:
		return "sent"
	default:
		return "unknown"
	}
}

func messageDirectionName(pduType uint32) string {
	direction, _ := smsBusinessDirection(classifySMSPDU(pduType))
	return direction
}

func stringProperty(properties Properties, name string) (string, bool) {
	value, ok := propertyValue(properties, name)
	if !ok {
		return "", false
	}
	result, ok := value.(string)
	return result, ok
}

func stringsProperty(properties Properties, name string) ([]string, bool) {
	value, ok := propertyValue(properties, name)
	if !ok {
		return []string{}, false
	}
	result, ok := value.([]string)
	if !ok {
		return []string{}, false
	}
	return append([]string(nil), result...), true
}

func int32Property(properties Properties, name string) (int32, bool) {
	value, ok := propertyValue(properties, name)
	if !ok {
		return 0, false
	}
	result, ok := value.(int32)
	return result, ok
}

func uint32Property(properties Properties, name string) (uint32, bool) {
	value, ok := propertyValue(properties, name)
	if !ok {
		return 0, false
	}
	result, ok := value.(uint32)
	return result, ok
}

func uint32sProperty(properties Properties, name string) ([]uint32, bool) {
	value, ok := propertyValue(properties, name)
	if !ok {
		return []uint32{}, false
	}
	result, ok := value.([]uint32)
	if !ok {
		return []uint32{}, false
	}
	return append([]uint32(nil), result...), true
}

func boolProperty(properties Properties, name string) (bool, bool) {
	value, ok := propertyValue(properties, name)
	if !ok {
		return false, false
	}
	result, ok := value.(bool)
	return result, ok
}

func audioFormatProperty(properties Properties) (*domain.CallAudioFormat, bool) {
	value, ok := propertyValue(properties, "AudioFormat")
	if !ok {
		return nil, false
	}
	formatProperties, ok := value.(map[string]dbus.Variant)
	if !ok {
		return nil, false
	}
	format := &domain.CallAudioFormat{}
	format.Encoding, _ = stringProperty(formatProperties, "encoding")
	format.Resolution, _ = stringProperty(formatProperties, "resolution")
	format.Rate, _ = uint32Property(formatProperties, "rate")
	return format, true
}

func objectPathProperty(properties Properties, name string) (dbus.ObjectPath, bool) {
	value, ok := propertyValue(properties, name)
	if !ok {
		return "", false
	}
	result, ok := value.(dbus.ObjectPath)
	return result, ok
}

func objectPathValuesProperty(properties Properties, name string) ([]dbus.ObjectPath, bool) {
	value, ok := propertyValue(properties, name)
	if !ok {
		return []dbus.ObjectPath{}, false
	}
	paths, ok := value.([]dbus.ObjectPath)
	if !ok {
		return []dbus.ObjectPath{}, false
	}
	return append([]dbus.ObjectPath(nil), paths...), true
}

func signalQualityProperty(properties Properties) (uint32, bool, bool) {
	value, ok := propertyValue(properties, "SignalQuality")
	if !ok {
		return 0, false, false
	}
	parts, ok := value.([]any)
	if !ok || len(parts) != 2 {
		return 0, false, false
	}
	quality, qualityOK := parts[0].(uint32)
	recent, recentOK := parts[1].(bool)
	if !qualityOK || !recentOK {
		return 0, false, false
	}
	return quality, recent, true
}

func extendedSignalProperties(
	properties Properties,
	accessTechnologies uint32,
) (*float64, *float64, *float64, *float64) {
	technologyOrder := []string{"Lte", "Nr5g", "Umts", "Gsm", "Cdma", "Evdo"}
	if accessTechnologies&accessTechnologyLTE == 0 {
		technologyOrder = []string{"Nr5g", "Umts", "Gsm", "Lte", "Cdma", "Evdo"}
	}
	for _, technology := range technologyOrder {
		values, ok := signalValuesProperty(properties, technology)
		if !ok || len(values) == 0 {
			continue
		}
		rssi := finiteSignalValue(values, "rssi")
		rsrp := finiteSignalValue(values, "rsrp")
		rsrq := finiteSignalValue(values, "rsrq")
		snr := finiteSignalValue(values, "snr")
		return rssi, rsrp, rsrq, snr
	}
	return nil, nil, nil, nil
}

func signalMetricsAvailable(line domain.Line) bool {
	return line.SignalDBM != nil ||
		line.SignalRSRP != nil ||
		line.SignalRSRQ != nil ||
		line.SignalSNR != nil
}

func modemPortsProperty(properties Properties, name string) ([]domain.ModemPort, bool) {
	value, ok := propertyValue(properties, name)
	if !ok {
		return nil, false
	}
	var tuples [][]any
	switch typed := value.(type) {
	case [][]any:
		tuples = typed
	case []any:
		tuples = make([][]any, 0, len(typed))
		for _, item := range typed {
			tuple, tupleOK := item.([]any)
			if !tupleOK {
				return nil, false
			}
			tuples = append(tuples, tuple)
		}
	default:
		return nil, false
	}
	ports := make([]domain.ModemPort, 0, len(tuples))
	for _, tuple := range tuples {
		if len(tuple) != 2 {
			return nil, false
		}
		portName, nameOK := tuple[0].(string)
		portType, typeOK := tuple[1].(uint32)
		portName = strings.TrimSpace(portName)
		if !nameOK || !typeOK || portName == "" {
			return nil, false
		}
		ports = append(ports, domain.ModemPort{
			Name:     portName,
			Type:     modemPortTypeName(portType),
			TypeCode: portType,
		})
	}
	sort.Slice(ports, func(i, j int) bool {
		if ports[i].Name != ports[j].Name {
			return ports[i].Name < ports[j].Name
		}
		return ports[i].TypeCode < ports[j].TypeCode
	})
	return ports, true
}

func modemPortTypeName(portType uint32) string {
	switch portType {
	case 2:
		return "net"
	case 3:
		return "at"
	case 4:
		return "qcdm"
	case 5:
		return "gps"
	case 6:
		return "qmi"
	case 7:
		return "mbim"
	case 8:
		return "audio"
	case 9:
		return "ignored"
	default:
		return "unknown"
	}
}

func signalValuesProperty(properties Properties, name string) (Properties, bool) {
	value, ok := propertyValue(properties, name)
	if !ok {
		return nil, false
	}
	values, ok := value.(map[string]dbus.Variant)
	return values, ok
}

func finiteSignalValue(properties Properties, name string) *float64 {
	value, ok := propertyValue(properties, name)
	if !ok {
		return nil
	}
	number, ok := value.(float64)
	if !ok || math.IsNaN(number) || math.IsInf(number, 0) {
		return nil
	}
	return &number
}

func propertyValue(properties Properties, name string) (any, bool) {
	variant, ok := properties[name]
	if !ok {
		return nil, false
	}
	return variant.Value(), true
}
