package volte

import (
	"context"
	"encoding/csv"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const QuectelLTEStandardQCFGIMSProfileID = "quectel-lte-standard-qcfg-ims"

// QuectelLTEStandardQCFGIMSProfile covers the EC2x, EG2x, EG9x, and EM05
// families named by Quectel's QCFG command manual.
func QuectelLTEStandardQCFGIMSProfile() Profile {
	return Profile{
		ID:                   QuectelLTEStandardQCFGIMSProfileID,
		Matches:              matchesQuectelLTEStandardQCFG,
		OperationTimeout:     5 * time.Second,
		Read:                 quectelQCFGReadMethod{},
		Write:                quectelQCFGWriteMethod{},
		ApplyRequiresRestart: true,
	}
}

func matchesQuectelLTEStandardQCFG(identity Identity) bool {
	documentedModels := []string{
		"EC20",
		"EC21",
		"EC25",
		"EG21",
		"EG25",
		"EG91",
		"EG95",
		"EM05",
	}
	for _, value := range []string{identity.Manufacturer, identity.Model} {
		normalized := strings.ToUpper(strings.TrimSpace(value))
		if normalized == "" {
			continue
		}
		for _, model := range documentedModels {
			if hasCompleteToken(normalized, model) {
				return true
			}
		}
	}
	firmware := strings.ToUpper(strings.TrimSpace(identity.Firmware))
	for _, model := range documentedModels {
		if hasDocumentedFirmwarePrefix(firmware, model) {
			return true
		}
	}
	return false
}

func hasDocumentedFirmwarePrefix(value string, model string) bool {
	if !strings.HasPrefix(value, model) {
		return false
	}
	if len(value) == len(model) {
		return true
	}
	next := value[len(model)]
	return next < '0' || next > '9'
}

func hasCompleteToken(value string, token string) bool {
	for offset := 0; offset+len(token) <= len(value); offset++ {
		if offset > 0 && isASCIIAlphaNumeric(value[offset-1]) {
			continue
		}
		if !strings.HasPrefix(value[offset:], token) {
			continue
		}
		end := offset + len(token)
		if end == len(value) || !isASCIIAlphaNumeric(value[end]) {
			return true
		}
	}
	return false
}

func isASCIIAlphaNumeric(value byte) bool {
	return value >= 'A' && value <= 'Z' || value >= '0' && value <= '9'
}

type quectelQCFGReadMethod struct{}

func (quectelQCFGReadMethod) Protocol() Protocol {
	return ProtocolAT
}

func (quectelQCFGReadMethod) validate() error {
	return nil
}

func (quectelQCFGReadMethod) read(ctx context.Context, run execution) (State, error) {
	const operation = "read"
	if run.transports.AT == nil {
		return State{}, newError(
			ErrorUnavailable,
			operation,
			run.profileID,
			ProtocolAT,
			"AT transport is unavailable",
			nil,
		)
	}
	imsResponse, err := run.transports.AT.Command(ctx, `AT+QCFG="ims"`)
	if err != nil {
		return State{}, executionError(
			operation,
			run.profileID,
			ProtocolAT,
			`AT+QCFG="ims" read failed`,
			err,
		)
	}
	state, err := decodeQuectelIMS(imsResponse)
	if err != nil {
		return State{}, newError(
			ErrorDecode,
			operation,
			run.profileID,
			ProtocolAT,
			`AT+QCFG="ims" response was invalid`,
			err,
		)
	}
	disableResponse, err := run.transports.AT.Command(ctx, `AT+QCFG="volte_disable"`)
	if err != nil {
		return State{}, executionError(
			operation,
			run.profileID,
			ProtocolAT,
			`AT+QCFG="volte_disable" read failed`,
			err,
		)
	}
	disabled, err := decodeQuectelVoLTEDisable(disableResponse)
	if err != nil {
		return State{}, newError(
			ErrorDecode,
			operation,
			run.profileID,
			ProtocolAT,
			`AT+QCFG="volte_disable" response was invalid`,
			err,
		)
	}
	if disabled || state.ConfigurationMode == ConfigurationModeForcedDisabled {
		state.Policy = PolicyDisabled
	} else {
		state.Policy = PolicyEnabled
	}
	return state, nil
}

type quectelQCFGWriteMethod struct{}

func (quectelQCFGWriteMethod) Protocol() Protocol {
	return ProtocolAT
}

func (quectelQCFGWriteMethod) validate() error {
	return nil
}

func (quectelQCFGWriteMethod) write(
	ctx context.Context,
	run execution,
	policy Policy,
) error {
	const operation = "apply"
	if run.transports.AT == nil {
		return newError(
			ErrorUnavailable,
			operation,
			run.profileID,
			ProtocolAT,
			"AT transport is unavailable",
			nil,
		)
	}
	commands := []string{`AT+QCFG="volte_disable",1`}
	if policy == PolicyEnabled {
		commands = []string{
			`AT+QCFG="volte_disable",0`,
			`AT+QCFG="ims",0`,
		}
	} else if policy != PolicyDisabled {
		return newError(
			ErrorInvalidPolicy,
			operation,
			run.profileID,
			ProtocolAT,
			"policy must be enabled or disabled",
			nil,
		)
	}
	for _, command := range commands {
		response, err := run.transports.AT.Command(ctx, command)
		if err != nil {
			return executionError(
				operation,
				run.profileID,
				ProtocolAT,
				"AT write command failed",
				err,
			)
		}
		if err := validateATWriteResponse(response); err != nil {
			return newError(
				ErrorVerification,
				operation,
				run.profileID,
				ProtocolAT,
				"AT command was not acknowledged",
				err,
			)
		}
	}
	return nil
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
			Policy:                 PolicyEnabled,
			ModemCapabilityKnown:   true,
			ModemCapabilityEnabled: capability == 1,
		}
		switch mode {
		case 0:
			state.ConfigurationMode = ConfigurationModeAutomatic
		case 1:
			state.ConfigurationMode = ConfigurationModeForcedEnabled
		case 2:
			state.ConfigurationMode = ConfigurationModeForcedDisabled
			state.Policy = PolicyDisabled
		}
		return state, nil
	}
	return State{}, fmt.Errorf(`AT+QCFG="ims" response is invalid`)
}

func decodeQuectelVoLTEDisable(response string) (bool, error) {
	for _, line := range atResponseLines(response) {
		if !strings.HasPrefix(line, "+QCFG:") {
			continue
		}
		record, err := csv.NewReader(
			strings.NewReader(strings.TrimSpace(strings.TrimPrefix(line, "+QCFG:"))),
		).Read()
		if err != nil || len(record) != 2 {
			continue
		}
		key := strings.TrimSpace(record[0])
		if key != "volte_disable" && key != "volte/disable" {
			continue
		}
		value, err := strconv.Atoi(strings.TrimSpace(record[1]))
		if err != nil || value < 0 || value > 1 {
			continue
		}
		return value == 1, nil
	}
	return false, fmt.Errorf(`AT+QCFG="volte_disable" response is invalid`)
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
