package modemmanager

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/godbus/dbus/v5"
	"github.com/human-agent65535/modemdeck/agent/internal/domain"
)

const (
	quectelUSBVoiceQuery  = `AT+QCFG="USBCFG"`
	quectelCallListQuery  = "AT+CLCC"
	quectelPCMEnable      = "AT+QPCMV=1,2"
	quectelPCMStatusQuery = "AT+QPCMV?"
	quectelPCMReadyStatus = "+QPCMV: 1,2"
	quectelUACPortPrefix  = "quectel-uac:"

	voiceVerificationEnabled         = "enabled"
	voiceVerificationDisabled        = "disabled"
	voiceVerificationReadFailed      = "read_failed"
	voiceVerificationInvalidResponse = "invalid_response"
	voiceVerificationRejected        = "rejected"
	voiceVerificationInactive        = "inactive"
	voiceVerificationProbePending    = "probe_pending"
)

type voiceProbeResult struct {
	callControl      bool
	atCallControl    bool
	media            bool
	usbConfiguration string
	mediaRouting     string
	reason           string
}

func (p *Provider) initializeVoiceModel(
	ctx context.Context,
	operation string,
	parsed *ParsedObjects,
) {
	if parsed == nil {
		return
	}
	for index := range parsed.Lines {
		line := &parsed.Lines[index]
		if !requiresQuectelPCMProbe(*line) {
			continue
		}
		path, found := parsed.LinePaths[line.ID]
		if !found || !path.IsValid() {
			continue
		}
		p.probeQuectelVoice(ctx, operation, parsed.ids, *line, path)
	}
	p.projectVoiceCapabilities(ctx, operation, parsed)
}

func (p *Provider) projectVoiceCapabilities(
	ctx context.Context,
	operation string,
	parsed *ParsedObjects,
) {
	if parsed == nil {
		return
	}
	for index := range parsed.Lines {
		line := &parsed.Lines[index]
		if !requiresQuectelPCMProbe(*line) {
			continue
		}
		path, found := parsed.LinePaths[line.ID]
		if !found || !path.IsValid() {
			disableLineCallControl(line)
			delete(parsed.CallBackends, line.ID)
			continue
		}
		result, modeled := p.voiceProbeResult(voiceProbeKey(parsed.ids, *line, path))
		if !modeled {
			disableLineCallControl(line)
			delete(parsed.CallBackends, line.ID)
			continue
		}
		projectVoiceProbeResult(line, result)
		if result.callControl {
			if line.Capabilities.VoiceInterface {
				parsed.CallBackends[line.ID] = callControlModemManager
			} else if result.atCallControl {
				enableLineCallControl(line)
				parsed.CallBackends[line.ID] = callControlQuectelAT
			} else {
				disableLineCallControl(line)
				delete(parsed.CallBackends, line.ID)
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
		slog.Debug(
			"line call control withheld",
			"component", "modemmanager",
			"line_id", line.ID,
			"firmware", line.Revision,
			"reason", result.reason,
		)
	}

	p.projectATCalls(ctx, operation, parsed)
	p.projectQuectelMediaState(parsed)
}

func projectVoiceProbeResult(line *domain.Line, result voiceProbeResult) {
	line.Capabilities.Media = result.media
	line.AudioPort = ""
	if result.media {
		line.AudioPort = quectelUACAudioPort(*line)
		line.Capabilities.Media = line.AudioPort != ""
	}
	line.VoiceVerification = &domain.VoiceRuntimeVerification{
		USBConfiguration: result.usbConfiguration,
		MediaRouting:     result.mediaRouting,
	}
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
	ids *instanceIDs,
	line domain.Line,
	path dbus.ObjectPath,
) voiceProbeResult {
	key := voiceProbeKey(ids, line, path)

	p.voiceProbeMu.Lock()
	defer p.voiceProbeMu.Unlock()
	if cached, found := p.voiceProbes[key]; found {
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
	} else {
		callControlEnabled, decodeErr := parseQuectelUSBCallControl(usbVoiceResponse)
		switch {
		case decodeErr != nil:
			result.usbConfiguration = voiceVerificationInvalidResponse
			result.reason = "USB configuration response was invalid"
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
	if result.usbConfiguration == voiceVerificationDisabled {
		result.callControl = false
		result.mediaRouting = voiceVerificationDisabled
		p.voiceProbes[key] = result
		return result
	}
	callList, callListErr := p.commandATPath(
		ctx,
		path,
		operation,
		quectelCallListQuery,
	)
	callRecords, callListParseErr := parseQuectelCLCC(callList)
	if callListErr == nil && callListParseErr == nil {
		result.callControl = true
		result.atCallControl = true
		// The capability probe is also the first authoritative CLCC read after
		// Agent startup. Preserve those calls in the AT lifecycle cache so the
		// same snapshot can project and safely recover them.
		state := ParsedObjects{ATCallLines: make(map[string]string)}
		p.projectKnownATCalls(&state, &line, callRecords, true)
	} else if !result.callControl {
		result.reason = "AT call control could not be verified"
	}
	if !result.callControl {
		result.mediaRouting = voiceVerificationDisabled
		p.voiceProbes[key] = result
		return result
	}
	if callListErr != nil || callListParseErr != nil {
		result.mediaRouting = voiceVerificationReadFailed
		result.reason = "call state could not be read before the PCM capability probe"
		p.voiceProbes[key] = result
		return result
	}
	if len(callRecords) > 0 {
		result.mediaRouting = voiceVerificationProbePending
		result.reason = "PCM voice routing was not probed because the line was busy"
		p.voiceProbes[key] = result
		return result
	}

	result = p.probeQuectelMediaCapability(ctx, operation, path, result)
	p.voiceProbes[key] = result
	return result
}

func (p *Provider) probeQuectelMediaCapability(
	ctx context.Context,
	operation string,
	path dbus.ObjectPath,
	result voiceProbeResult,
) voiceProbeResult {
	result, _ = p.applyQuectelMediaRouting(ctx, operation, path, result)
	return result
}

func (p *Provider) applyQuectelMediaRouting(
	ctx context.Context,
	operation string,
	path dbus.ObjectPath,
	result voiceProbeResult,
) (voiceProbeResult, error) {
	result.media = false
	if _, err := p.commandATPath(ctx, path, operation, quectelPCMEnable); err != nil {
		result.mediaRouting = voiceVerificationRejected
		result.reason = "firmware rejected PCM voice routing"
		return result, err
	}
	status, err := p.commandATPath(ctx, path, operation, quectelPCMStatusQuery)
	switch {
	case err != nil:
		result.mediaRouting = voiceVerificationReadFailed
		result.reason = "PCM voice routing state could not be read"
	case !strings.EqualFold(strings.TrimSpace(status), quectelPCMReadyStatus):
		result.mediaRouting = voiceVerificationInactive
		result.reason = "PCM voice routing did not become ready"
	default:
		result.mediaRouting = voiceVerificationEnabled
		result.media = true
		result.reason = ""
	}
	return result, err
}

func (p *Provider) ensureQuectelMediaRouting(
	ctx context.Context,
	operation string,
	ids *instanceIDs,
	line domain.Line,
	path dbus.ObjectPath,
) (voiceProbeResult, bool) {
	if !requiresQuectelPCMProbe(line) || !path.IsValid() {
		return voiceProbeResult{}, false
	}
	key := voiceProbeKey(ids, line, path)
	result, found := p.voiceProbeResult(key)
	if !found || !result.callControl {
		return result, found
	}

	status, statusErr := p.commandATPath(ctx, path, operation, quectelPCMStatusQuery)
	if statusErr == nil &&
		strings.EqualFold(strings.TrimSpace(status), quectelPCMReadyStatus) {
		result.media = true
		result.mediaRouting = voiceVerificationEnabled
		result.reason = ""
		p.storeVoiceProbe(key, result)
		return result, true
	}

	refreshed := result
	refreshed.media = false
	_, err := p.commandATPath(ctx, path, operation, quectelPCMEnable)
	if err == nil {
		refreshed.media = true
		refreshed.mediaRouting = voiceVerificationEnabled
		refreshed.reason = ""
	} else {
		refreshed.mediaRouting = voiceVerificationRejected
		refreshed.reason = "firmware rejected PCM voice routing"
	}
	p.storeVoiceProbe(key, refreshed)
	if !refreshed.media {
		slog.Warn(
			"Quectel call media route is unavailable",
			"component", "modemmanager",
			"operation", operation,
			"line_id", line.ID,
			"media_routing", refreshed.mediaRouting,
			"reason", refreshed.reason,
			"status_error", statusErr,
			"error", err,
		)
	}
	return refreshed, true
}

func (p *Provider) projectQuectelMediaState(parsed *ParsedObjects) {
	if parsed == nil {
		return
	}
	lines := make(map[string]*domain.Line, len(parsed.Lines))
	for index := range parsed.Lines {
		line := &parsed.Lines[index]
		if requiresQuectelPCMProbe(*line) {
			lines[line.ID] = line
		}
	}
	for index := range parsed.Calls {
		call := &parsed.Calls[index]
		line := lines[call.LineID]
		if line == nil {
			continue
		}
		path := parsed.LinePaths[line.ID]
		if !path.IsValid() {
			continue
		}
		result, found := p.voiceProbeResult(voiceProbeKey(parsed.ids, *line, path))
		if !found {
			continue
		}
		projectQuectelUACCall(call, *line, result)
	}
}

func (p *Provider) ActivateCallMedia(
	ctx context.Context,
	request domain.CallCommandRequest,
) (domain.CallMediaActivation, error) {
	const operation = "activate_call_media"
	request.RequestID = strings.TrimSpace(request.RequestID)
	request.CallID = strings.TrimSpace(request.CallID)
	if err := validateRequestID(operation, request.RequestID); err != nil {
		return domain.CallMediaActivation{}, err
	}
	if request.CallID == "" {
		return domain.CallMediaActivation{}, domain.InvalidArgument(operation, "call id is required")
	}

	p.callMu.Lock()
	defer p.callMu.Unlock()

	parsed, err := p.snapshotContent(ctx, operation)
	if err != nil {
		return domain.CallMediaActivation{}, err
	}
	p.projectVoiceCapabilities(ctx, operation, &parsed)
	call, found := findCall(parsed.Calls, request.CallID)
	if !found {
		return domain.CallMediaActivation{}, domain.NotFound(operation, "call was not found")
	}
	line, found := findLine(parsed.Lines, call.LineID)
	if !found {
		return domain.CallMediaActivation{}, domain.NotFound(operation, "call line was not found")
	}
	if !requiresQuectelPCMProbe(line) {
		return domain.CallMediaActivation{}, domain.NotSupported(
			operation,
			"line does not use the supported Quectel call-media route",
		)
	}
	path, found := parsed.LinePaths[line.ID]
	if !found || !path.IsValid() {
		return domain.CallMediaActivation{}, domain.Unavailable(
			operation,
			"line AT path is unavailable",
			nil,
		)
	}
	result, found := p.voiceProbeResult(voiceProbeKey(parsed.ids, line, path))
	if !found || !result.media {
		return domain.CallMediaActivation{}, domain.Conflict(
			operation,
			"line media route is not initialized",
		)
	}
	projectVoiceProbeResult(&line, result)
	projectQuectelUACCall(&call, line, result)
	return domain.CallMediaActivation{
		CallID:         call.ID,
		MediaRouting:   result.mediaRouting,
		MediaAvailable: call.MediaAvailable,
		AudioPort:      call.AudioPort,
		AudioFormat:    call.AudioFormat,
		Reason:         result.reason,
	}, nil
}

func (p *Provider) voiceProbeResult(key string) (voiceProbeResult, bool) {
	p.voiceProbeMu.Lock()
	defer p.voiceProbeMu.Unlock()
	result, found := p.voiceProbes[key]
	return result, found
}

func (p *Provider) clearVoiceProbes() {
	p.voiceProbeMu.Lock()
	clear(p.voiceProbes)
	p.voiceProbeMu.Unlock()
}

func (p *Provider) deleteVoiceProbe(key string) {
	p.voiceProbeMu.Lock()
	delete(p.voiceProbes, key)
	p.voiceProbeMu.Unlock()
}

func (p *Provider) storeVoiceProbe(key string, result voiceProbeResult) {
	p.voiceProbeMu.Lock()
	p.voiceProbes[key] = result
	p.voiceProbeMu.Unlock()
}

func projectQuectelUACCall(
	call *domain.Call,
	line domain.Line,
	result voiceProbeResult,
) {
	if call == nil || call.StateCode != 4 || !result.media {
		return
	}
	if call.MediaAvailable {
		return
	}
	audioPort := quectelUACAudioPort(line)
	if audioPort == "" {
		return
	}
	call.AudioPort = audioPort
	call.AudioFormat = &domain.CallAudioFormat{
		Encoding:   "pcm",
		Resolution: "s16le",
		Rate:       8000,
	}
	call.MediaAvailable = true
}

func quectelUACAudioPort(line domain.Line) string {
	physicalDevice := normalizePhysicalDevice(line.PhysicalDevice)
	if physicalDevice == "" {
		return ""
	}
	return quectelUACPortPrefix + physicalDevice
}

// ReprobeVoiceCapabilities rebuilds and initializes the cached voice model for
// one idle modem.
func (p *Provider) ReprobeVoiceCapabilities(ctx context.Context, lineID string) error {
	const operation = "reprobe_voice_capabilities"
	lineID = strings.TrimSpace(lineID)
	if lineID == "" {
		return domain.InvalidArgument(operation, "line id is required")
	}
	p.callMu.Lock()
	defer p.callMu.Unlock()

	parsed, err := p.snapshotContent(ctx, operation)
	if err != nil {
		return err
	}
	line, found := findLine(parsed.Lines, lineID)
	if !found {
		return domain.NotFound(operation, "line was not found")
	}
	if !requiresQuectelPCMProbe(line) {
		return domain.NotSupported(operation, "line is not in the supported voice-probe family")
	}
	path, found := parsed.LinePaths[line.ID]
	if !found || !path.IsValid() {
		return domain.Unavailable(operation, "line AT path is unavailable", nil)
	}
	p.refreshATLine(ctx, operation, &line, path)
	p.projectATCalls(ctx, operation, &parsed)
	if lineHasCall(parsed.Calls, line.ID, "") {
		return domain.Conflict(operation, "voice capabilities can only be reprobed while the line is idle")
	}
	key := voiceProbeKey(parsed.ids, line, path)
	p.deleteVoiceProbe(key)
	p.probeQuectelVoice(ctx, operation, parsed.ids, line, path)
	return nil
}

func voiceProbeKey(ids *instanceIDs, line domain.Line, path dbus.ObjectPath) string {
	deviceIdentity := strings.TrimSpace(line.EquipmentIdentifier)
	if deviceIdentity == "" {
		deviceIdentity = strings.TrimSpace(line.DeviceIdentifier)
	}
	if deviceIdentity == "" {
		deviceIdentity = strings.TrimSpace(line.PhysicalDevice)
	}
	providerEpoch := ""
	if ids != nil {
		providerEpoch = ids.providerEpoch()
	}
	return strings.Join(
		[]string{
			providerEpoch,
			string(path),
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
