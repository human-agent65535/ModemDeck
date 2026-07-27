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
	quectelPCMEnable      = "AT+QPCMV=1,2"
	quectelPCMStatusQuery = "AT+QPCMV?"
	quectelPCMReadyStatus = "+QPCMV: 1,2"
)

type voiceProbeResult struct {
	callControl bool
	media       bool
	reason      string
	expiresAt   time.Time
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
		if !line.Capabilities.VoiceInterface || !requiresQuectelPCMProbe(*line) {
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
		if result.callControl {
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
		blockedLines[line.ID] = struct{}{}
		slog.Debug(
			"line call control withheld",
			"component", "modemmanager",
			"line_id", line.ID,
			"firmware", line.Revision,
			"reason", result.reason,
		)
	}
	if len(blockedLines) == 0 {
		return
	}

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

func disableLineCallControl(line *domain.Line) {
	line.Capabilities.Dial = false
	line.Capabilities.AnswerCall = false
	line.Capabilities.RejectCall = false
	line.Capabilities.HangupCall = false
	line.Capabilities.SendDTMF = false
	line.CallIDs = []string{}
}

func requiresQuectelPCMProbe(line domain.Line) bool {
	manufacturer := strings.ToUpper(strings.TrimSpace(line.Manufacturer))
	model := strings.ToUpper(strings.TrimSpace(line.Model))
	revision := strings.ToUpper(strings.TrimSpace(line.Revision))
	if !strings.Contains(manufacturer+" "+model, "QUECTEL") {
		return false
	}
	return strings.HasPrefix(revision, "EC2") ||
		strings.HasPrefix(revision, "EG2") ||
		strings.HasPrefix(revision, "QDC507")
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

	result := voiceProbeResult{}
	usbVoiceResponse, err := p.commandATPath(ctx, path, operation, quectelUSBVoiceQuery)
	if err != nil {
		result.reason = "USB voice configuration could not be read"
	} else {
		callControlEnabled, decodeErr := parseQuectelUSBCallControl(usbVoiceResponse)
		switch {
		case decodeErr != nil:
			result.reason = decodeErr.Error()
		case !callControlEnabled:
			result.reason = "USB call control is disabled"
		default:
			result.callControl = true
			if _, err = p.commandATPath(ctx, path, operation, quectelPCMEnable); err != nil {
				result.reason = "PCM voice routing command was rejected"
			} else {
				status, statusErr := p.commandATPath(
					ctx,
					path,
					operation,
					quectelPCMStatusQuery,
				)
				if statusErr != nil {
					result.reason = "PCM voice routing state could not be read"
				} else if !strings.EqualFold(strings.TrimSpace(status), quectelPCMReadyStatus) {
					result.reason = "PCM voice routing did not become active"
				} else {
					result.media = true
				}
			}
		}
	}
	if result.callControl {
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
