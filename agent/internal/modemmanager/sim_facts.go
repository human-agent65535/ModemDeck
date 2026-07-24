package modemmanager

import (
	"strings"

	"github.com/godbus/dbus/v5"
	"github.com/human-agent65535/modemdeck/agent/internal/domain"
)

const (
	modemManagerSIMTypeUnknown  uint32 = 0
	modemManagerSIMTypePhysical uint32 = 1
	modemManagerSIMTypeESIM     uint32 = 2

	modemManagerESIMStatusUnknown      uint32 = 0
	modemManagerESIMStatusNoProfiles   uint32 = 1
	modemManagerESIMStatusWithProfiles uint32 = 2

	eidLength              = 32
	eidVisibleSuffixLength = 4

	simProfileManagementNotSupportedReason = "ModemManager exposes eSIM identity and slot metadata but not eUICC profile inventory or management"
)

type standardSIMFacts struct {
	SIMType    domain.SIMType
	ESIMStatus domain.ESIMStatus
	EIDMasked  string
}

type standardSIMSlotFacts struct {
	Slots            []domain.SIMSlot
	SlotsKnown       bool
	PrimarySlot      uint32
	PrimarySlotKnown bool
	CurrentSlot      uint32
	CurrentSlotKnown bool
}

func readStandardSIMFacts(properties Properties) standardSIMFacts {
	facts := standardSIMFacts{
		SIMType:    domain.SIMTypeUnknown,
		ESIMStatus: domain.ESIMStatusUnknown,
	}
	if value, known := uint32Property(properties, "SimType"); known {
		facts.SIMType = standardSIMType(value)
	}
	if value, known := uint32Property(properties, "EsimStatus"); known {
		facts.ESIMStatus = standardESIMStatus(value)
	}
	if eid, known := stringProperty(properties, "Eid"); known {
		facts.EIDMasked = maskEID(eid)
	}
	return facts
}

func readStandardSIMSlotFacts(
	modemProperties Properties,
	objects ManagedObjects,
	currentSIMPath dbus.ObjectPath,
) standardSIMSlotFacts {
	paths, known := objectPathValuesProperty(modemProperties, "SimSlots")
	result := standardSIMSlotFacts{
		Slots:      []domain.SIMSlot{},
		SlotsKnown: known,
	}
	if known {
		result.Slots = make([]domain.SIMSlot, 0, len(paths))
		currentMatches := make([]int, 0, 1)
		for index, path := range paths {
			slot := domain.SIMSlot{
				Index:      uint32(index + 1),
				SIMType:    domain.SIMTypeUnknown,
				ESIMStatus: domain.ESIMStatusUnknown,
			}
			if validSIMObjectPath(path) {
				slot.Present = true
				if path == currentSIMPath {
					currentMatches = append(currentMatches, index)
				}
				if properties, found := objects[path][simInterface]; found {
					facts := readStandardSIMFacts(properties)
					slot.SIMType = facts.SIMType
					slot.ESIMStatus = facts.ESIMStatus
					slot.EIDMasked = facts.EIDMasked
				}
			}
			result.Slots = append(result.Slots, slot)
		}
		if len(currentMatches) == 1 {
			index := currentMatches[0]
			result.Slots[index].Current = true
			result.CurrentSlot = uint32(index + 1)
			result.CurrentSlotKnown = true
		}
	}

	if primary, primaryKnown := uint32Property(modemProperties, "PrimarySimSlot"); primaryKnown &&
		primary > 0 &&
		(!known || primary <= uint32(len(paths))) {
		result.PrimarySlot = primary
		result.PrimarySlotKnown = true
	}
	return result
}

func standardSIMType(value uint32) domain.SIMType {
	switch value {
	case modemManagerSIMTypePhysical:
		return domain.SIMTypePhysical
	case modemManagerSIMTypeESIM:
		return domain.SIMTypeESIM
	case modemManagerSIMTypeUnknown:
		fallthrough
	default:
		return domain.SIMTypeUnknown
	}
}

func standardESIMStatus(value uint32) domain.ESIMStatus {
	switch value {
	case modemManagerESIMStatusNoProfiles:
		return domain.ESIMStatusNoProfiles
	case modemManagerESIMStatusWithProfiles:
		return domain.ESIMStatusWithProfiles
	case modemManagerESIMStatusUnknown:
		fallthrough
	default:
		return domain.ESIMStatusUnknown
	}
}

func maskEID(value string) string {
	value = strings.TrimSpace(value)
	if len(value) != eidLength {
		return ""
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return ""
		}
	}
	return "****" + value[len(value)-eidVisibleSuffixLength:]
}

func validSIMObjectPath(path dbus.ObjectPath) bool {
	return path != "/" && path.IsValid()
}
