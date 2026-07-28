package modemmanager

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/human-agent65535/modemdeck/agent/internal/domain"
)

const (
	callTerminationObservationInterval = 200 * time.Millisecond
	callTerminationVerificationTimeout = 2 * time.Second
	callTerminationCommandTimeout      = 5 * time.Second
	forcedModemResetTimeout            = 10 * time.Second
)

func detachedTimeoutContext(
	ctx context.Context,
	timeout time.Duration,
) (context.Context, context.CancelFunc) {
	parent := context.Background()
	if ctx != nil {
		parent = context.WithoutCancel(ctx)
	}
	return context.WithTimeout(parent, timeout)
}

func (p *Provider) verifyCallTerminated(
	ctx context.Context,
	operation string,
	call domain.Call,
	parsed ParsedObjects,
) error {
	verifyContext, cancelVerify := detachedTimeoutContext(
		ctx,
		callTerminationVerificationTimeout,
	)
	defer cancelVerify()

	var lastObservationErr error
	for {
		terminated, err := p.observeCallTermination(
			verifyContext,
			operation,
			call,
			parsed,
		)
		if terminated {
			return nil
		}
		if err != nil {
			lastObservationErr = err
		}

		timer := time.NewTimer(callTerminationObservationInterval)
		select {
		case <-verifyContext.Done():
			timer.Stop()
			if lastObservationErr != nil {
				return lastObservationErr
			}
			return errors.New("the call remained active after the termination command")
		case <-timer.C:
		}
	}
}

func (p *Provider) observeCallTermination(
	ctx context.Context,
	operation string,
	call domain.Call,
	parsed ParsedObjects,
) (bool, error) {
	if lineID, isATCall := parsed.ATCallLines[call.ID]; isATCall {
		line, found := findLine(parsed.Lines, lineID)
		path := parsed.LinePaths[lineID]
		if !found || !path.IsValid() {
			return false, errors.New("the AT call line is unavailable")
		}
		response, err := p.commandATPath(
			ctx,
			path,
			operation,
			quectelCallListQuery,
		)
		if err != nil {
			return false, fmt.Errorf("verify AT call termination: %w", err)
		}
		records, err := parseQuectelCLCC(response)
		if err != nil {
			return false, fmt.Errorf("verify AT call termination state: %w", err)
		}
		state := ParsedObjects{ATCallLines: make(map[string]string)}
		p.projectKnownATCalls(&state, &line, records, true)
		current, found := findCall(state.Calls, call.ID)
		return !found || current.StateCode == callStateTerminated, nil
	}

	current, err := p.snapshotContent(ctx, operation)
	if err != nil {
		return false, fmt.Errorf("verify ModemManager call termination: %w", err)
	}
	if currentCall, found := findCall(current.Calls, call.ID); found &&
		currentCall.StateCode != callStateTerminated {
		return false, nil
	}
	return true, nil
}

func (p *Provider) terminateCall(
	ctx context.Context,
	operation string,
	call domain.Call,
	parsed ParsedObjects,
) error {
	lineID := call.LineID
	if atLineID, isATCall := parsed.ATCallLines[call.ID]; isATCall {
		lineID = atLineID
		command := quectelHangupCall
		if call.StateCode == 6 {
			command = quectelRejectWaitingCall
		}
		if _, err := p.commandATPath(
			ctx,
			parsed.LinePaths[lineID],
			operation,
			command,
		); err != nil {
			return p.forceTerminateCall(ctx, operation, call, parsed, err)
		}
	} else if _, err := p.call(
		ctx,
		parsed.CallPaths[call.ID],
		callInterface+".Hangup",
		operation,
		"ModemManager failed to terminate the call",
	); err != nil {
		return p.forceTerminateCall(ctx, operation, call, parsed, err)
	}

	if err := p.verifyCallTerminated(ctx, operation, call, parsed); err != nil {
		return p.forceTerminateCall(ctx, operation, call, parsed, err)
	}

	line, found := findLine(parsed.Lines, lineID)
	if found && requiresQuectelPCMProbe(line) {
		cleanupContext, cancelCleanup := detachedTimeoutContext(
			ctx,
			callTerminationVerificationTimeout,
		)
		p.disableQuectelMedia(
			cleanupContext,
			operation,
			parsed.LinePaths[lineID],
			call.ID,
		)
		cancelCleanup()
	}
	if _, isATCall := parsed.ATCallLines[call.ID]; isATCall {
		p.publishChange("at-call-command")
	} else {
		p.publishChange("call-command")
	}
	return nil
}

func (p *Provider) forceTerminateCall(
	ctx context.Context,
	operation string,
	call domain.Call,
	parsed ParsedObjects,
	hangupErr error,
) error {
	modemPath := parsed.LinePaths[call.LineID]
	if !modemPath.IsValid() {
		pathErr := errors.New("the call modem path is unavailable")
		slog.Error(
			"call hangup failed and forced modem reset was unavailable",
			"component", "call_safety",
			"call_id", call.ID,
			"line_id", call.LineID,
			"hangup_error", hangupErr,
			"reset_error", pathErr,
		)
		return domain.Internal(
			operation,
			"call hangup failed and the modem could not be reset",
			errors.Join(hangupErr, pathErr),
		)
	}

	slog.Error(
		"call hangup failed; forcing modem reset",
		"component", "call_safety",
		"call_id", call.ID,
		"line_id", call.LineID,
		"hangup_error", hangupErr,
	)
	resetContext, cancelReset := detachedTimeoutContext(
		ctx,
		forcedModemResetTimeout,
	)
	defer cancelReset()
	_, resetErr := p.call(
		resetContext,
		modemPath,
		modemInterface+".Reset",
		operation,
		"ModemManager failed to force-reset the modem after a call hangup failure",
	)
	if resetErr != nil {
		slog.Error(
			"forced modem reset after call hangup failure also failed",
			"component", "call_safety",
			"call_id", call.ID,
			"line_id", call.LineID,
			"hangup_error", hangupErr,
			"reset_error", resetErr,
		)
		return domain.Internal(
			operation,
			"call hangup and forced modem reset both failed",
			errors.Join(hangupErr, resetErr),
		)
	}

	p.clearLineCallRuntime(call.LineID)
	p.publishChange("call-forced-modem-reset")
	slog.Error(
		"modem reset forced after call hangup failure",
		"component", "call_safety",
		"call_id", call.ID,
		"line_id", call.LineID,
	)
	return domain.VerificationFailed(
		operation,
		"call hangup failed; the modem was reset to force termination",
		errors.Join(hangupErr, domain.ErrForcedCallTermination),
	)
}

func (p *Provider) clearLineCallRuntime(lineID string) {
	p.atCallStateMu.Lock()
	delete(p.atCalls, lineID)
	delete(p.atPendingCalls, lineID)
	p.atCallStateMu.Unlock()

	p.voiceProbeMu.Lock()
	for callID, activation := range p.voiceMedia {
		if activation.lineID == lineID {
			delete(p.voiceMedia, callID)
		}
	}
	p.voiceProbeMu.Unlock()
}
