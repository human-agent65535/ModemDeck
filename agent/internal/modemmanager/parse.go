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
	messagingInterface = "org.freedesktop.ModemManager1.Modem.Messaging"
)

type Properties map[string]dbus.Variant
type Interfaces map[string]Properties
type ManagedObjects map[dbus.ObjectPath]Interfaces

func ParseManagedObjects(objects ManagedObjects) []domain.Line {
	lines := make([]domain.Line, 0)
	for path, interfaces := range objects {
		modemProperties, ok := interfaces[modemInterface]
		if !ok {
			continue
		}

		line := domain.Line{
			ID:                       string(path),
			State:                    "unknown",
			Drivers:                  []string{},
			OwnNumbers:               []string{},
			EmergencyNumbers:         []string{},
			CallIDs:                  []string{},
			MessageIDs:               []string{},
			SupportedMessageStorages: []uint32{},
			Capabilities: domain.LineCapabilities{
				ModemInterface: true,
				Dial:           false,
				AnswerCall:     false,
				HangupCall:     false,
				SendMessage:    false,
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

		if voiceProperties, found := interfaces[voiceInterface]; found {
			line.Capabilities.VoiceInterface = true
			line.EmergencyOnly, _ = boolProperty(voiceProperties, "EmergencyOnly")
			line.CallIDs, _ = objectPathsProperty(voiceProperties, "Calls")
		}

		if messagingProperties, found := interfaces[messagingInterface]; found {
			line.Capabilities.MessagingInterface = true
			line.MessageIDs, _ = objectPathsProperty(messagingProperties, "Messages")
			line.SupportedMessageStorages, _ = uint32sProperty(messagingProperties, "SupportedStorages")
			line.DefaultMessageStorage, _ = uint32Property(messagingProperties, "DefaultStorage")
		}

		lines = append(lines, line)
	}

	sort.Slice(lines, func(i, j int) bool {
		return lines[i].ID < lines[j].ID
	})
	return lines
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

func objectPathProperty(properties Properties, name string) (dbus.ObjectPath, bool) {
	value, ok := propertyValue(properties, name)
	if !ok {
		return "", false
	}
	result, ok := value.(dbus.ObjectPath)
	return result, ok
}

func objectPathsProperty(properties Properties, name string) ([]string, bool) {
	value, ok := propertyValue(properties, name)
	if !ok {
		return []string{}, false
	}
	paths, ok := value.([]dbus.ObjectPath)
	if !ok {
		return []string{}, false
	}
	result := make([]string, 0, len(paths))
	for _, path := range paths {
		result = append(result, string(path))
	}
	return result, true
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
