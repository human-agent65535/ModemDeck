package modemmanager

import (
	"sort"

	"github.com/godbus/dbus/v5"
	"github.com/human-agent65535/modemdeck/agent/internal/domain"
)

const (
	modemInterface     = "org.freedesktop.ModemManager1.Modem"
	simInterface       = "org.freedesktop.ModemManager1.Sim"
	voiceInterface     = "org.freedesktop.ModemManager1.Modem.Voice"
	callInterface      = "org.freedesktop.ModemManager1.Call"
	messagingInterface = "org.freedesktop.ModemManager1.Modem.Messaging"
	smsInterface       = "org.freedesktop.ModemManager1.Sms"
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
	MessagePaths map[string]dbus.ObjectPath
}

func ParseManagedObjects(objects ManagedObjects, ids *instanceIDs) ParsedObjects {
	parsed := ParsedObjects{
		Lines:        []domain.Line{},
		Calls:        []domain.Call{},
		Messages:     []domain.Message{},
		LinePaths:    make(map[string]dbus.ObjectPath),
		CallPaths:    make(map[string]dbus.ObjectPath),
		MessagePaths: make(map[string]dbus.ObjectPath),
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
		line.DeviceIdentifier, _ = stringProperty(modemProperties, "DeviceIdentifier")
		line.EquipmentIdentifier, _ = stringProperty(modemProperties, "EquipmentIdentifier")
		line.Device, _ = stringProperty(modemProperties, "Device")
		line.PhysicalDevice, _ = stringProperty(modemProperties, "Physdev")
		line.Drivers, _ = stringsProperty(modemProperties, "Drivers")
		line.Plugin, _ = stringProperty(modemProperties, "Plugin")
		line.PrimaryPort, _ = stringProperty(modemProperties, "PrimaryPort")
		line.StateCode, _ = int32Property(modemProperties, "State")
		line.State = modemStateName(line.StateCode)
		line.PowerStateCode, _ = uint32Property(modemProperties, "PowerState")
		line.AccessTechnologies, _ = uint32Property(modemProperties, "AccessTechnologies")
		line.SignalQuality, line.SignalQualityRecent, line.SignalQualityKnown = signalQualityProperty(modemProperties)
		line.OwnNumbers, _ = stringsProperty(modemProperties, "OwnNumbers")

		if simPath, present := objectPathProperty(modemProperties, "Sim"); present && simPath != "/" {
			line.SIMPresent = true
			line.SIMPath = string(simPath)
			if simProperties, found := objects[simPath][simInterface]; found {
				line.Capabilities.SIMInterface = true
				line.SIMIdentifier, _ = stringProperty(simProperties, "SimIdentifier")
				line.IMSI, _ = stringProperty(simProperties, "Imsi")
				line.OperatorIdentifier, _ = stringProperty(simProperties, "OperatorIdentifier")
				line.OperatorName, _ = stringProperty(simProperties, "OperatorName")
				line.EmergencyNumbers, _ = stringsProperty(simProperties, "EmergencyNumbers")
			}
		}

		line.ID = ids.lineID(path, line)
		parsed.LinePaths[line.ID] = path

		if voiceProperties, found := interfaces[voiceInterface]; found {
			line.Capabilities.VoiceInterface = true
			line.Capabilities.Dial = true
			line.Capabilities.AnswerCall = true
			line.Capabilities.RejectCall = true
			line.Capabilities.HangupCall = true
			line.Capabilities.SendDTMF = true
			line.EmergencyOnly, _ = boolProperty(voiceProperties, "EmergencyOnly")

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

		if messagingProperties, found := interfaces[messagingInterface]; found {
			line.Capabilities.MessagingInterface = true
			line.Capabilities.SendMessage = true
			line.SupportedMessageStorages, _ = uint32sProperty(messagingProperties, "SupportedStorages")
			line.DefaultMessageStorage, _ = uint32Property(messagingProperties, "DefaultStorage")

			messagePaths, _ := objectPathValuesProperty(messagingProperties, "Messages")
			for _, messagePath := range messagePaths {
				messageProperties, found := objects[messagePath][smsInterface]
				if !found {
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

		sort.Strings(line.CallIDs)
		sort.Strings(line.MessageIDs)
		parsed.Lines = append(parsed.Lines, line)
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
		// The standard ModemManager Call interface does not expose the
		// cellular bearer. Never infer VoLTE or VoWiFi from unrelated fields.
		Bearer: "",
	}
}

func parseMessage(path dbus.ObjectPath, lineID string, properties Properties, ids *instanceIDs) domain.Message {
	stateCode, _ := uint32Property(properties, "State")
	pduType, _ := uint32Property(properties, "PduType")
	number, _ := stringProperty(properties, "Number")
	text, _ := stringProperty(properties, "Text")
	timestamp, _ := stringProperty(properties, "Timestamp")
	return domain.Message{
		ID:        ids.messageID(path),
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

func callStateName(code int32) string {
	switch code {
	case 1:
		return "dialing"
	case 2:
		return "ringing_out"
	case 3:
		return "ringing_in"
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
	switch pduType {
	case 1, 3, 32:
		return "incoming"
	case 2, 33:
		return "outgoing"
	default:
		return "unknown"
	}
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

func propertyValue(properties Properties, name string) (any, bool) {
	variant, ok := properties[name]
	if !ok {
		return nil, false
	}
	return variant.Value(), true
}
