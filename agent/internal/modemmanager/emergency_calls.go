package modemmanager

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/human-agent65535/modemdeck/agent/internal/domain"
)

const emergencyCallCleanupTimeout = 2 * time.Second

// EmergencyHangupAll is the watchdog's narrow fail-safe. It bypasses the
// application control lease, ends every observable call, and resets only a
// modem whose AT-only call state cannot be terminated or verified.
func (p *Provider) EmergencyHangupAll(ctx context.Context, reason string) error {
	const operation = "emergency_hangup_all"
	ctx = normalizeEmergencyContext(ctx)
	p.callMu.Lock()
	defer p.callMu.Unlock()

	parsed, err := p.snapshotContent(ctx, operation)
	if err != nil {
		return err
	}

	var result error
	for _, call := range activeCallsForEmergency(parsed.Calls) {
		if _, isATCall := parsed.ATCallLines[call.ID]; isATCall {
			continue
		}
		if err := p.terminateCall(ctx, operation, call, parsed); err != nil {
			if errors.Is(err, domain.ErrForcedCallTermination) {
				continue
			}
			result = errors.Join(
				result,
				fmt.Errorf("end ModemManager call %s: %w", call.ID, err),
			)
		}
	}

	lineIndexes := make([]int, 0, len(parsed.Lines))
	for index := range parsed.Lines {
		line := parsed.Lines[index]
		if line.Capabilities.VoiceInterface || !requiresQuectelPCMProbe(line) {
			continue
		}
		lineIndexes = append(lineIndexes, index)
	}
	sort.Slice(lineIndexes, func(i, j int) bool {
		return parsed.Lines[lineIndexes[i]].ID < parsed.Lines[lineIndexes[j]].ID
	})
	for _, index := range lineIndexes {
		line := parsed.Lines[index]
		path := parsed.LinePaths[line.ID]
		if !path.IsValid() {
			continue
		}
		if err := p.emergencyHangupATLine(ctx, line, path, reason); err != nil {
			result = errors.Join(result, err)
		}
	}
	return result
}

func (p *Provider) emergencyHangupATLine(
	ctx context.Context,
	line domain.Line,
	path dbus.ObjectPath,
	reason string,
) error {
	response, queryErr := p.commandATPath(
		ctx,
		path,
		"emergency_hangup_all",
		quectelCallListQuery,
	)
	if queryErr == nil {
		records, err := parseQuectelCLCC(response)
		if err != nil {
			queryErr = err
		} else if len(records) == 0 {
			return nil
		}
	}

	_, hangupErr := p.commandATPath(
		ctx,
		path,
		"emergency_hangup_all",
		quectelHangupCall,
	)
	if hangupErr == nil {
		hangupErr = p.waitForATCallsToEnd(ctx, path)
	}
	if hangupErr == nil {
		slog.Warn(
			"AT calls ended by hardware watchdog",
			"component", "call_watchdog",
			"line_id", line.ID,
			"reason", reason,
		)
		return nil
	}

	cause := errors.Join(queryErr, hangupErr)
	slog.Error(
		"AT call cleanup failed; resetting modem",
		"component", "call_watchdog",
		"line_id", line.ID,
		"reason", reason,
		"error", cause,
	)
	_, resetErr := p.call(
		ctx,
		path,
		modemInterface+".Reset",
		"emergency_hangup_all",
		"ModemManager failed to reset a modem after emergency call cleanup",
	)
	if resetErr != nil {
		return fmt.Errorf(
			"end AT calls on line %s and reset modem: %w",
			line.ID,
			errors.Join(cause, resetErr),
		)
	}
	return nil
}

func (p *Provider) waitForATCallsToEnd(
	ctx context.Context,
	path dbus.ObjectPath,
) error {
	verifyContext, cancel := context.WithTimeout(
		context.WithoutCancel(normalizeEmergencyContext(ctx)),
		emergencyCallCleanupTimeout,
	)
	defer cancel()

	for {
		response, err := p.commandATPath(
			verifyContext,
			path,
			"emergency_hangup_all",
			quectelCallListQuery,
		)
		if err == nil {
			records, decodeErr := parseQuectelCLCC(response)
			if decodeErr == nil && len(records) == 0 {
				return nil
			}
			if decodeErr != nil {
				err = decodeErr
			}
		}
		timer := time.NewTimer(callTerminationObservationInterval)
		select {
		case <-verifyContext.Done():
			timer.Stop()
			if err != nil {
				return err
			}
			return errors.New("AT calls remained active after emergency hangup")
		case <-timer.C:
		}
	}
}

func activeCallsForEmergency(calls []domain.Call) []domain.Call {
	active := make([]domain.Call, 0, len(calls))
	for _, call := range calls {
		if call.ID == "" || call.StateCode == callStateTerminated {
			continue
		}
		active = append(active, call)
	}
	sort.Slice(active, func(i, j int) bool {
		return active[i].ID < active[j].ID
	})
	return active
}

func normalizeEmergencyContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}
