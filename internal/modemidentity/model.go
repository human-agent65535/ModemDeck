package modemidentity

import (
	"regexp"
	"strings"
)

var quectelModelPrefix = regexp.MustCompile(
	`^(?:QUECTEL[\s_-]*)?(QDC507|EC20|EC21|EC25|EG21|EG25|EG91|EG95|EM05)(?:[\s_-]*([A-Z]))?`,
)

// DisplayModel replaces generic ModemManager model strings with a concrete
// model derived from the modem firmware or hardware revision.
func DisplayModel(model, firmware, hardwareRevision string) string {
	model = strings.TrimSpace(model)
	if !isGenericModel(model) {
		return model
	}
	for _, identity := range []string{hardwareRevision, firmware} {
		if specific := quectelModel(identity); specific != "" {
			return specific
		}
	}
	return model
}

func isGenericModel(model string) bool {
	normalized := strings.ToUpper(strings.TrimSpace(model))
	return normalized == "" ||
		normalized == "MODEM" ||
		strings.Contains(normalized, "MOBILE BROADBAND MODULE")
}

func quectelModel(identity string) string {
	matches := quectelModelPrefix.FindStringSubmatch(
		strings.ToUpper(strings.TrimSpace(identity)),
	)
	if len(matches) == 0 {
		return ""
	}
	family := matches[1]
	if family == "QDC507" || len(matches) < 3 || matches[2] == "" {
		return family
	}
	return family + "-" + matches[2]
}
