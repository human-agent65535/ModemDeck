package store

import "strings"

// MergeLineMetadata completes live lines with what the store knows about them.
// The modem is the authority for everything it reports. The store supplies
// what only it holds — the user's label and colour — and fills fields the
// modem left empty: phone number, operator, home operator, IMEI and module
// name. A module name belongs to the modem, so it is also found through the
// IMEI when the line itself has no stored record.
//
// Every surface that shows lines uses this one rule, so a line reads the same
// in the Web client and in Telegram.
func MergeLineMetadata(liveLines, persistedLines []LineSummary) []LineSummary {
	byLineID := make(map[string]LineSummary, len(persistedLines))
	deviceNameByIMEI := make(map[string]string, len(persistedLines))
	for _, line := range persistedLines {
		if lineID := strings.TrimSpace(line.ID); lineID != "" {
			byLineID[lineID] = line
		}
		imei := strings.TrimSpace(line.DeviceIMEI)
		if name := strings.TrimSpace(line.DeviceName); imei != "" && name != "" {
			deviceNameByIMEI[imei] = name
		}
	}

	merged := make([]LineSummary, len(liveLines))
	for index, line := range liveLines {
		if persisted, found := byLineID[strings.TrimSpace(line.ID)]; found {
			line.LineLabel = persisted.LineLabel
			line.LineColor = persisted.LineColor
			if line.PhoneNumber == "" {
				line.PhoneNumber = persisted.PhoneNumber
			}
			if line.Operator == "" {
				line.Operator = persisted.Operator
			}
			if line.HomeOperatorCode == "" {
				line.HomeOperatorCode = persisted.HomeOperatorCode
			}
			if line.HomeOperatorName == "" {
				line.HomeOperatorName = persisted.HomeOperatorName
			}
			if line.HomeOperatorName == "" {
				line.HomeOperatorName = line.Operator
			}
			if line.DeviceIMEI == "" {
				line.DeviceIMEI = persisted.DeviceIMEI
			}
			if line.DeviceName == "" {
				line.DeviceName = persisted.DeviceName
			}
		}
		if line.DeviceName == "" {
			line.DeviceName = deviceNameByIMEI[strings.TrimSpace(line.DeviceIMEI)]
		}
		merged[index] = line
	}
	return merged
}
