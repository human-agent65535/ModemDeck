package modemmanager

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/human-agent65535/modemdeck/agent/internal/domain"
)

const (
	voiceProbeReadyTTL    = 10 * time.Minute
	voiceProbeNotReadyTTL = 15 * time.Second
	quectelUSBVoiceQuery  = `AT+QCFG="USBCFG"`
	quectelCallListQuery  = "AT+CLCC"
	quectelPCMEnable      = "AT+QPCMV=1,2"
	quectelPCMStatusQuery = "AT+QPCMV?"
	quectelPCMReadyStatus = "+QPCMV: 1,2"

	voiceVerificationEnabled         = "enabled"
	voiceVerificationDisabled        = "disabled"
	voiceVerificationReadFailed      = "read_failed"
	voiceVerificationInvalidResponse = "invalid_response"
	voiceVerificationRejected        = "rejected"
	voiceVerificationInactive        = "inactive"
)

type voiceProbeResult struct {
	callControl      bool
	atCallControl    bool
	media            bool
	usbConfiguration string
	mediaRouting     string
	reason           string
	retrySoon        bool
	expiresAt        time.Time
}

func (p *Provider) projectVoiceCapabilities(
	ctx context.Context,
	operation string,
	parsed *ParsedObjects,
) {
	if parsed == nil {
		return
	}
	blockedLines := make(map[string]struct{})
	for index := range parsed.Lines {
		line := &parsed.Lines[index]
		if !requiresQuectelPCMProbe(*line) {
			continue
		}
		path, found := parsed.LinePaths[line.ID]
		if !found || !path.IsValid() {
			disableLineCallControl(line)
			blockedLines[line.ID] = struct{}{}
			continue
		}
		result := p.probeQuectelVoice(ctx, operation, *line, path)
		line.Capabilities.Media = result.media
		line.VoiceVerification = &domain.VoiceRuntimeVerification{
			USBConfiguration: result.usbConfiguration,
			MediaRouting:     result.mediaRouting,
		}
		if result.callControl {
			if line.Capabilities.VoiceInterface {
				parsed.CallBackends[line.ID] = callControlModemManager
			} else if result.atCallControl {
				enableLineCallControl(line)
				parsed.CallBackends[line.ID] = callControlQuectelAT
			} else {
				disableLineCallControl(line)
				delete(parsed.CallBackends, line.ID)
				blockedLines[line.ID] = struct{}{}
				continue
			}
			if !result.media {
				slog.Debug(
					"line media capability withheld",
					"component", "modemmanager",
					"line_id", line.ID,
					"firmware", line.Revision,
					"reason", result.reason,
				)
			}
			continue
		}
		disableLineCallControl(line)
		delete(parsed.CallBackends, line.ID)
		blockedLines[line.ID] = struct{}{}
		slog.Debug(
			"line call control withheld",
			"component", "modemmanager",
			"line_id", line.ID,
			"firmware", line.Revision,
			"reason", result.reason,
		)
	}

	if len(blockedLines) > 0 {
		calls := parsed.Calls[:0]
		for _, call := range parsed.Calls {
			if _, blocked := blockedLines[call.LineID]; blocked {
				delete(parsed.CallPaths, call.ID)
				continue
			}
			calls = append(calls, call)
		}
		parsed.Calls = calls
	}
	p.projectATCalls(ctx, operation, parsed)
}

func enableLineCallControl(line *domain.Line) {
	line.Capabilities.Dial = true
	line.Capabilities.AnswerCall = true
	line.Capabilities.RejectCall = true
	line.Capabilities.HangupCall = true
	line.Capabilities.SendDTMF = true
}

func disableLineCallControl(line *domain.Line) {
	line.Capabilities.Dial = false
	line.Capabilities.AnswerCall = false
	line.Capabilities.RejectCall = false
	line.Capabilities.HangupCall = false
	line.Capabilities.SendDTMF = false
	line.CallIDs = []string{}
}

func requiresQuectelPCMProbe(line domain.Line) bool {
	revision := strings.ToUpper(strings.TrimSpace(line.Revision))
	for _, model := range []string{
		"EC20",
		"EC21",
		"EC25",
		"EG21",
		"EG25",
		"QDC507",
	} {
		if strings.HasPrefix(revision, model) {
			return true
		}
	}
	return false
}

func (p *Provider) probeQuectelVoice(
	ctx context.Context,
	operation string,
	line domain.Line,
	path dbus.ObjectPath,
) voiceProbeResult {
	key := voiceProbeKey(line)
	now := p.now()

	p.voiceProbeMu.Lock()
	defer p.voiceProbeMu.Unlock()
	if cached, found := p.voiceProbes[key]; found && now.Before(cached.expiresAt) {
		return cached
	}

	// The supported Quectel family is verified from AT state. A firmware that
	// rejects the USBCFG read is inconclusive, not disabled; CLCC provides a
	// safe secondary proof without mutating the modem or an active call.
	result := voiceProbeResult{
		callControl: line.Capabilities.VoiceInterface,
	}
	usbVoiceResponse, err := p.commandATPath(ctx, path, operation, quectelUSBVoiceQuery)
	if err != nil {
		result.usbConfiguration = voiceVerificationReadFailed
		result.reason = "USB configuration could not be read"
		result.retrySoon = true
	} else {
		callControlEnabled, decodeErr := parseQuectelUSBCallControl(usbVoiceResponse)
		switch {
		case decodeErr != nil:
			result.usbConfiguration = voiceVerificationInvalidResponse
			result.reason = "USB configuration response was invalid"
			result.retrySoon = true
		case !callControlEnabled:
			result.usbConfiguration = voiceVerificationDisabled
			result.callControl = false
			result.reason = "USB call control is disabled"
		default:
			result.usbConfiguration = voiceVerificationEnabled
			result.callControl = true
			result.atCallControl = true
		}
	}
	if result.usbConfiguration != voiceVerificationDisabled &&
		!result.atCallControl {
		if _, listErr := p.commandATPath(
			ctx,
			path,
			operation,
			quectelCallListQuery,
		); listErr == nil {
			result.callControl = true
			result.atCallControl = true
			result.retrySoon = false
		} else {
			result.reason = "AT call control could not be verified"
			result.retrySoon = true
		}
	}
	if result.callControl {
		if _, err = p.commandATPath(ctx, path, operation, quectelPCMEnable); err != nil {
			result.mediaRouting = voiceVerificationRejected
			result.reason = "PCM voice routing command was rejected"
		} else {
			status, statusErr := p.commandATPath(
				ctx,
				path,
				operation,
				quectelPCMStatusQuery,
			)
			if statusErr != nil {
				result.mediaRouting = voiceVerificationReadFailed
				result.reason = "PCM voice routing state could not be read"
				result.retrySoon = true
			} else if !strings.EqualFold(strings.TrimSpace(status), quectelPCMReadyStatus) {
				result.mediaRouting = voiceVerificationInactive
				result.reason = "PCM voice routing did not become active"
				result.retrySoon = true
			} else {
				result.mediaRouting = voiceVerificationEnabled
				result.media = true
			}
		}
	} else {
		result.mediaRouting = voiceVerificationDisabled
	}
	if result.callControl && !result.retrySoon {
		result.expiresAt = now.Add(voiceProbeReadyTTL)
	} else {
		result.expiresAt = now.Add(voiceProbeNotReadyTTL)
	}
	p.voiceProbes[key] = result
	return result
}

func voiceProbeKey(line domain.Line) string {
	deviceIdentity := strings.TrimSpace(line.EquipmentIdentifier)
	if deviceIdentity == "" {
		deviceIdentity = strings.TrimSpace(line.DeviceIdentifier)
	}
	if deviceIdentity == "" {
		deviceIdentity = strings.TrimSpace(line.PhysicalDevice)
	}
	return strings.Join(
		[]string{
			deviceIdentity,
			strings.ToUpper(strings.TrimSpace(line.Revision)),
		},
		"\x00",
	)
}

func parseQuectelUSBCallControl(response string) (bool, error) {
	response = strings.TrimSpace(response)
	const prefix = "+QCFG:"
	if !strings.HasPrefix(strings.ToUpper(response), prefix) {
		return false, fmt.Errorf("unexpected USB configuration response")
	}
	reader := csv.NewReader(strings.NewReader(strings.TrimSpace(response[len(prefix):])))
	reader.TrimLeadingSpace = true
	reader.FieldsPerRecord = -1
	fields, err := reader.Read()
	if err != nil {
		return false, fmt.Errorf("decode USB configuration: %w", err)
	}
	if _, err := reader.Read(); err != io.EOF {
		return false, fmt.Errorf("USB configuration returned multiple records")
	}
	if len(fields) < 2 || !strings.EqualFold(strings.TrimSpace(fields[0]), "usbcfg") {
		return false, fmt.Errorf("USB configuration response is incomplete")
	}
	switch strings.TrimSpace(fields[len(fields)-1]) {
	case "0":
		return false, nil
	case "1":
		return true, nil
	default:
		return false, fmt.Errorf("USB call-control flag is invalid")
	}
}
