package volte

import (
	"encoding/csv"
	"fmt"
	"strconv"
	"strings"
	"time"
)

var QDC507GLEFM21Identity = Identity{
	Manufacturer: "QUALCOMM INCORPORATED",
	Model:        "QUECTEL Mobile Broadband Module",
	Firmware:     "QDC507GLEFM21",
}

const QDC507GLEFM21ProfileID = "qdc507glefm21-qcfg-ims"

func QDC507GLEFM21Profile() Profile {
	return Profile{
		ID:                   QDC507GLEFM21ProfileID,
		Identity:             QDC507GLEFM21Identity,
		OperationTimeout:     5 * time.Second,
		Read:                 ATRead(`AT+QCFG="ims"`, decodeQuectelIMS),
		Write:                ATWrite(encodeQuectelIMS, validateATWriteResponse),
		ApplyRequiresRestart: true,
	}
}

func decodeQuectelIMS(response string) (State, error) {
	for _, line := range atResponseLines(response) {
		if !strings.HasPrefix(line, "+QCFG:") {
			continue
		}
		record, err := csv.NewReader(
			strings.NewReader(strings.TrimSpace(strings.TrimPrefix(line, "+QCFG:"))),
		).Read()
		if err != nil || len(record) != 3 || strings.TrimSpace(record[0]) != "ims" {
			continue
		}
		mode, modeErr := strconv.Atoi(strings.TrimSpace(record[1]))
		capability, capabilityErr := strconv.Atoi(strings.TrimSpace(record[2]))
		if modeErr != nil ||
			capabilityErr != nil ||
			mode < 0 ||
			mode > 2 ||
			(capability != 0 && capability != 1) {
			continue
		}

		state := State{
			Policy:                 PolicyDisabled,
			ModemCapabilityKnown:   true,
			ModemCapabilityEnabled: capability == 1,
		}
		switch mode {
		case 0:
			state.ConfigurationMode = ConfigurationModeAutomatic
			if capability == 1 {
				state.Policy = PolicyEnabled
			}
		case 1:
			state.ConfigurationMode = ConfigurationModeForcedEnabled
			state.Policy = PolicyEnabled
		case 2:
			state.ConfigurationMode = ConfigurationModeForcedDisabled
			state.Policy = PolicyDisabled
		}
		return state, nil
	}
	return State{}, fmt.Errorf(`AT+QCFG="ims" response is invalid`)
}

func encodeQuectelIMS(policy Policy) (string, error) {
	switch policy {
	case PolicyEnabled:
		return `AT+QCFG="ims",1`, nil
	case PolicyDisabled:
		return `AT+QCFG="ims",2`, nil
	default:
		return "", fmt.Errorf("unsupported policy %q", policy)
	}
}

func validateATWriteResponse(response string) error {
	if strings.TrimSpace(response) == "" {
		return nil
	}
	for _, line := range atResponseLines(response) {
		if line == "OK" {
			return nil
		}
	}
	return fmt.Errorf("AT command was not acknowledged")
}

func atResponseLines(response string) []string {
	normalized := strings.ReplaceAll(response, "\r\n", "\n")
	lines := strings.Split(normalized, "\n")
	for index := range lines {
		lines[index] = strings.TrimSpace(lines[index])
	}
	return lines
}
