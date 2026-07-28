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
	quectelPCMDisable     = "AT+QPCMV=0"
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
	voiceVerificationSupported       = "supported"
)

type voiceProbeResult struct {
	callControl      bool
	atCallControl    bool
	media            bool
	usbConfiguration string
	mediaRouting     string
	reason           string
}

type voiceMediaActivation struct {
	probeKey string
	lineID   string
	result   voiceProbeResult
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
	p.projectQuectelMediaState(ctx, operation, parsed)
}

func projectVoiceProbeResult(line *domain.Line, result voiceProbeResult) {
	line.Capabilities.Media = result.media
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
	result.media = false
	if _, err := p.commandATPath(ctx, path, operation, quectelPCMEnable); err != nil {
		result.mediaRouting = voiceVerificationRejected
		result.reason = "firmware rejected PCM voice routing"
		return result
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
		result.mediaRouting = voiceVerificationSupported
		result.media = true
		result.reason = ""
	}
	if _, err := p.commandATPath(ctx, path, operation, quectelPCMDisable); err != nil {
		slog.Warn(
			"disable PCM voice routing after capability probe",
			"component", "modemmanager",
			"error", err,
		)
	}
	return result
}

func (p *Provider) projectQuectelMediaState(
	ctx context.Context,
	operation string,
	parsed *ParsedObjects,
) {
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
	p.releaseEndedVoiceMedia(ctx, operation, parsed)
	for index := range parsed.Calls {
		call := &parsed.Calls[index]
		line := lines[call.LineID]
		if line == nil {
			continue
		}
		key := voiceProbeKey(*line)
		activation, found := p.voiceMediaActivation(call.ID, key)
		if !found {
			continue
		}
		projectQuectelUACCall(call, *line, activation.result)
	}
}

func (p *Provider) voiceMediaActivation(
	callID string,
	key string,
) (voiceMediaActivation, bool) {
	p.voiceProbeMu.Lock()
	defer p.voiceProbeMu.Unlock()
	activation, found := p.voiceMedia[callID]
	if !found || activation.probeKey != key {
		return voiceMediaActivation{}, false
	}
	return activation, true
}

func (p *Provider) activateQuectelMedia(
	ctx context.Context,
	operation string,
	path dbus.ObjectPath,
	result voiceProbeResult,
) (voiceProbeResult, error) {
	result.media = false

	if _, err := p.commandATPath(ctx, path, operation, quectelPCMEnable); err != nil {
		result.mediaRouting = voiceVerificationRejected
		result.reason = "PCM voice routing command was rejected before call setup"
		return result, err
	}
	status, err := p.commandATPath(ctx, path, operation, quectelPCMStatusQuery)
	switch {
	case err != nil:
		result.mediaRouting = voiceVerificationReadFailed
		result.reason = "PCM voice routing state could not be read before call setup"
	case !strings.EqualFold(strings.TrimSpace(status), quectelPCMReadyStatus):
		result.mediaRouting = voiceVerificationInactive
		result.reason = "PCM voice routing did not become ready before call setup"
	default:
		result.mediaRouting = voiceVerificationEnabled
		result.media = true
		result.reason = ""
	}
	return result, err
}

func (p *Provider) prepareQuectelMedia(
	ctx context.Context,
	operation string,
	line domain.Line,
	path dbus.ObjectPath,
) (voiceMediaActivation, bool) {
	if !requiresQuectelPCMProbe(line) {
		return voiceMediaActivation{}, false
	}
	key := voiceProbeKey(line)
	result, found := p.voiceProbeResult(key)
	if !found || !result.callControl {
		return voiceMediaActivation{}, false
	}
	if !result.media && result.mediaRouting != voiceVerificationProbePending {
		return voiceMediaActivation{}, false
	}
	activated, err := p.activateQuectelMedia(ctx, operation, path, result)
	if err != nil {
		slog.Warn(
			"prepare PCM voice routing before call control",
			"component", "modemmanager",
			"line_id", line.ID,
			"error", err,
		)
	}
	if result.mediaRouting == voiceVerificationProbePending {
		probed := activated
		if probed.media {
			probed.mediaRouting = voiceVerificationSupported
		}
		p.voiceProbeMu.Lock()
		p.voiceProbes[key] = probed
		p.voiceProbeMu.Unlock()
	}
	return voiceMediaActivation{
		probeKey: key,
		lineID:   line.ID,
		result:   activated,
	}, true
}

func (p *Provider) disableQuectelMedia(
	ctx context.Context,
	operation string,
	path dbus.ObjectPath,
	callID string,
) {
	if _, err := p.commandATPath(ctx, path, operation, quectelPCMDisable); err != nil {
		slog.Warn(
			"disable PCM voice routing",
			"component", "modemmanager",
			"call_id", callID,
			"error", err,
		)
	}
	p.deleteVoiceMediaActivation(callID)
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
	if path, found := parsed.LinePaths[line.ID]; !found || !path.IsValid() {
		return domain.CallMediaActivation{}, domain.Unavailable(
			operation,
			"line AT path is unavailable",
			nil,
		)
	}
	key := voiceProbeKey(line)
	activation, found := p.voiceMediaActivation(call.ID, key)
	if !found {
		return domain.CallMediaActivation{}, domain.Conflict(
			operation,
			"call media was not prepared before the call control command",
		)
	}
	projectVoiceProbeResult(&line, activation.result)
	projectQuectelUACCall(&call, line, activation.result)
	return domain.CallMediaActivation{
		CallID:         call.ID,
		MediaRouting:   activation.result.mediaRouting,
		MediaAvailable: call.MediaAvailable,
		AudioPort:      call.AudioPort,
		AudioFormat:    call.AudioFormat,
		Reason:         activation.result.reason,
	}, nil
}

func (p *Provider) voiceProbeResult(key string) (voiceProbeResult, bool) {
	p.voiceProbeMu.Lock()
	defer p.voiceProbeMu.Unlock()
	result, found := p.voiceProbes[key]
	return result, found
}

func (p *Provider) storeVoiceMediaActivation(
	callID string,
	activation voiceMediaActivation,
) {
	p.voiceProbeMu.Lock()
	p.voiceMedia[callID] = activation
	p.voiceProbeMu.Unlock()
}

func (p *Provider) deleteVoiceMediaActivation(callID string) {
	p.voiceProbeMu.Lock()
	delete(p.voiceMedia, callID)
	p.voiceProbeMu.Unlock()
}

func (p *Provider) releaseEndedVoiceMedia(
	ctx context.Context,
	operation string,
	parsed *ParsedObjects,
) {
	current := make(map[string]struct{}, len(parsed.Calls))
	for _, call := range parsed.Calls {
		if call.StateCode != callStateTerminated {
			current[call.ID] = struct{}{}
		}
	}
	staleLines := make(map[string]struct{})
	p.voiceProbeMu.Lock()
	for callID, activation := range p.voiceMedia {
		if _, found := current[callID]; !found {
			delete(p.voiceMedia, callID)
			staleLines[activation.lineID] = struct{}{}
		}
	}
	p.voiceProbeMu.Unlock()
	for lineID := range staleLines {
		path := parsed.LinePaths[lineID]
		if !path.IsValid() {
			continue
		}
		p.disableQuectelMediaRoute(ctx, operation, path, lineID)
	}
}

func (p *Provider) releaseLineVoiceMedia(
	ctx context.Context,
	operation string,
	path dbus.ObjectPath,
	lineID string,
) {
	found := false
	p.voiceProbeMu.Lock()
	for callID, activation := range p.voiceMedia {
		if activation.lineID != lineID {
			continue
		}
		delete(p.voiceMedia, callID)
		found = true
	}
	p.voiceProbeMu.Unlock()
	if found && path.IsValid() {
		p.disableQuectelMediaRoute(ctx, operation, path, lineID)
	}
}

func (p *Provider) disableQuectelMediaRoute(
	ctx context.Context,
	operation string,
	path dbus.ObjectPath,
	lineID string,
) {
	if _, err := p.commandATPath(ctx, path, operation, quectelPCMDisable); err != nil {
		slog.Warn(
			"disable PCM voice routing",
			"component", "modemmanager",
			"line_id", lineID,
			"error", err,
		)
	}
}

func (p *Provider) clearVoiceMediaActivations() {
	p.voiceProbeMu.Lock()
	clear(p.voiceMedia)
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
	if strings.TrimSpace(call.AudioPort) != "" || call.AudioFormat != nil {
		return
	}
	physicalDevice := normalizePhysicalDevice(line.PhysicalDevice)
	if physicalDevice == "" {
		return
	}
	call.AudioPort = quectelUACPortPrefix + physicalDevice
	call.AudioFormat = &domain.CallAudioFormat{
		Encoding:   "pcm",
		Resolution: "s16le",
		Rate:       8000,
	}
	call.MediaAvailable = true
}

// ReprobeVoiceCapabilities rebuilds the cached voice model for one idle modem.
// The PCM route is enabled only for the probe and restored before returning.
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
	key := voiceProbeKey(line)
	p.voiceProbeMu.Lock()
	delete(p.voiceProbes, key)
	p.voiceProbeMu.Unlock()
	p.probeQuectelVoice(ctx, operation, line, path)
	return nil
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
