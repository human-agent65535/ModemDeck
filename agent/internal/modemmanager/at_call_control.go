package modemmanager

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/godbus/dbus/v5"
	"github.com/human-agent65535/modemdeck/agent/internal/domain"
)

const (
	callControlModemManager callControlBackend = "modemmanager"
	callControlQuectelAT    callControlBackend = "quectel_at"

	quectelAnswerCall        = "ATA"
	quectelHangupCall        = "AT+CHUP"
	quectelRejectWaitingCall = "AT+CHLD=0"

	atCallStartVerificationTimeout = 2 * time.Second

	atCallIdleObservationInterval   = time.Second
	atCallActiveObservationInterval = 500 * time.Millisecond
)

type callControlBackend string

type atCallRecord struct {
	index      int
	direction  string
	stateCode  int32
	number     string
	numberType int
	multiparty bool
}

type atCallLifecycle struct {
	id         string
	lineID     string
	index      int
	number     string
	direction  string
	stateCode  int32
	multiparty bool
	observedAt time.Time
}

func (p *Provider) startATCall(
	ctx context.Context,
	operation string,
	request domain.StartCallRequest,
	line domain.Line,
	path dbus.ObjectPath,
) (domain.CommandReceipt, error) {
	command, number, ok := quectelDialCommand(request.Number)
	if !ok {
		return domain.CommandReceipt{}, domain.InvalidArgument(
			operation,
			"number cannot be represented safely as a voice AT command",
		)
	}
	p.atCallStateMu.Lock()
	p.atCallSequence++
	now := p.now().UTC()
	lifecycle := fmt.Sprintf("%d:%d", now.UnixNano(), p.atCallSequence)
	callID := p.ids.atCallID(line.ID, lifecycle)
	p.atPendingCalls[line.ID] = atCallLifecycle{
		id:         callID,
		lineID:     line.ID,
		index:      -1,
		number:     number,
		direction:  "outgoing",
		stateCode:  1,
		observedAt: now,
	}
	p.atCallStateMu.Unlock()

	_, dialErr := p.commandATPath(ctx, path, operation, command)
	call, observationErr := p.waitForATCallStart(
		ctx,
		operation,
		line,
		path,
		callID,
	)
	if dialErr != nil {
		if observationErr == nil {
			slog.Warn(
				"ATD transport returned an error after the modem started the call",
				"component", "modemmanager",
				"call_id", call.ID,
				"line_id", line.ID,
				"error", dialErr,
			)
		} else {
			return domain.CommandReceipt{}, dialErr
		}
	}
	if observationErr != nil {
		rollback := ParsedObjects{
			Lines:       []domain.Line{line},
			LinePaths:   map[string]dbus.ObjectPath{line.ID: path},
			ATCallLines: map[string]string{callID: line.ID},
		}
		if rollbackErr := p.terminateCall(
			ctx,
			operation,
			domain.Call{
				ID:        callID,
				LineID:    line.ID,
				Number:    number,
				Direction: "outgoing",
				StateCode: 1,
			},
			rollback,
		); rollbackErr != nil {
			return domain.CommandReceipt{}, rollbackErr
		}
		return domain.CommandReceipt{}, observationErr
	}
	p.publishChange("at-call-command")

	return domain.CommandReceipt{
		RequestID:  request.RequestID,
		ResourceID: call.ID,
	}, nil
}

func (p *Provider) waitForATCallStart(
	ctx context.Context,
	operation string,
	line domain.Line,
	path dbus.ObjectPath,
	callID string,
) (domain.Call, error) {
	verifyContext, cancelVerify := detachedTimeoutContext(
		ctx,
		atCallStartVerificationTimeout,
	)
	defer cancelVerify()

	var lastObservationErr error
	for {
		response, err := p.commandATPath(
			verifyContext,
			path,
			operation,
			quectelCallListQuery,
		)
		if err == nil {
			records, parseErr := parseQuectelCLCC(response)
			if parseErr == nil {
				state := ParsedObjects{ATCallLines: make(map[string]string)}
				p.projectKnownATCalls(&state, &line, records, true)
				if call, found := findCall(state.Calls, callID); found &&
					call.StateCode != callStateTerminated {
					return call, nil
				}
			} else {
				lastObservationErr = parseErr
			}
		} else {
			lastObservationErr = err
		}

		timer := time.NewTimer(callTerminationObservationInterval)
		select {
		case <-verifyContext.Done():
			timer.Stop()
			p.atCallStateMu.Lock()
			delete(p.atPendingCalls, line.ID)
			p.atCallStateMu.Unlock()
			return domain.Call{}, domain.VerificationFailed(
				operation,
				"ATD was accepted but CLCC did not report the outgoing call",
				lastObservationErr,
			)
		case <-timer.C:
		}
	}
}

func quectelDialCommand(value string) (command string, number string, ok bool) {
	var normalized strings.Builder
	hasDigit := false
	for _, char := range strings.TrimSpace(value) {
		switch {
		case char >= '0' && char <= '9':
			normalized.WriteRune(char)
			hasDigit = true
		case char == '+' && normalized.Len() == 0:
			normalized.WriteRune(char)
		case char == '*' || char == '#':
			normalized.WriteRune(char)
		case unicode.IsSpace(char) || strings.ContainsRune("()-.", char):
			continue
		default:
			return "", "", false
		}
	}
	number = normalized.String()
	if !hasDigit || len(number) > 64 {
		return "", "", false
	}
	return "ATD" + number + ";", number, true
}

func (p *Provider) projectATCalls(
	_ context.Context,
	_ string,
	parsed *ParsedObjects,
) {
	for lineIndex := range parsed.Lines {
		line := &parsed.Lines[lineIndex]
		if parsed.CallBackends[line.ID] != callControlQuectelAT {
			continue
		}
		p.projectKnownATCalls(parsed, line, nil, false)
	}
	sort.Slice(parsed.Calls, func(i, j int) bool {
		return parsed.Calls[i].ID < parsed.Calls[j].ID
	})
}

func (p *Provider) projectKnownATCalls(
	parsed *ParsedObjects,
	line *domain.Line,
	records []atCallRecord,
	authoritative bool,
) bool {
	p.atCallStateMu.Lock()
	defer p.atCallStateMu.Unlock()

	before := p.atLineStateFingerprintLocked(line.ID)
	now := p.now().UTC()
	lineCalls := p.atCalls[line.ID]
	if lineCalls == nil {
		lineCalls = make(map[int]atCallLifecycle)
		p.atCalls[line.ID] = lineCalls
	}

	seen := make(map[int]struct{}, len(records))
	for _, record := range records {
		seen[record.index] = struct{}{}
		lifecycle, found := lineCalls[record.index]
		if !found || atCallRecordStartsNewLifecycle(lifecycle, record) {
			lifecycle = p.newATCallLifecycle(line.ID, record, now)
			if pending, pendingFound := p.atPendingCalls[line.ID]; pendingFound && record.direction == "outgoing" &&
				(record.number == "" || atNumbersEqual(pending.number, record.number)) {
				lifecycle.id = pending.id
				delete(p.atPendingCalls, line.ID)
			}
		}
		lifecycle.index = record.index
		lifecycle.number = record.number
		lifecycle.direction = record.direction
		lifecycle.stateCode = record.stateCode
		lifecycle.multiparty = record.multiparty
		lifecycle.observedAt = now
		lineCalls[record.index] = lifecycle
		appendATCall(parsed, line, lifecycle)
	}

	if authoritative {
		for index, lifecycle := range lineCalls {
			if _, found := seen[index]; found {
				continue
			}
			lifecycle.stateCode = callStateTerminated
			lifecycle.observedAt = now
			appendATCall(parsed, line, lifecycle)
			delete(lineCalls, index)
		}
	} else {
		indexes := make([]int, 0, len(lineCalls))
		for index := range lineCalls {
			if _, alreadyProjected := seen[index]; !alreadyProjected {
				indexes = append(indexes, index)
			}
		}
		sort.Ints(indexes)
		for _, index := range indexes {
			appendATCall(parsed, line, lineCalls[index])
		}
	}

	return before != p.atLineStateFingerprintLocked(line.ID)
}

func (p *Provider) atLineStateFingerprintLocked(lineID string) string {
	var fingerprint strings.Builder
	indexes := make([]int, 0, len(p.atCalls[lineID]))
	for index := range p.atCalls[lineID] {
		indexes = append(indexes, index)
	}
	sort.Ints(indexes)
	for _, index := range indexes {
		call := p.atCalls[lineID][index]
		fmt.Fprintf(
			&fingerprint,
			"%d:%s:%s:%d:%t;",
			index,
			call.direction,
			call.number,
			call.stateCode,
			call.multiparty,
		)
	}
	if pending, found := p.atPendingCalls[lineID]; found {
		fmt.Fprintf(
			&fingerprint,
			"pending:%s:%s:%d;",
			pending.direction,
			pending.number,
			pending.stateCode,
		)
	}
	return fingerprint.String()
}

func (p *Provider) refreshATLine(
	ctx context.Context,
	operation string,
	line *domain.Line,
	path dbus.ObjectPath,
) bool {
	if line == nil || !path.IsValid() {
		return false
	}
	response, err := p.commandATPath(ctx, path, operation, quectelCallListQuery)
	if err != nil {
		slog.Debug(
			"observe AT calls",
			"component", "modemmanager",
			"line_id", line.ID,
			"error", err,
		)
		return false
	}
	records, err := parseQuectelCLCC(response)
	if err != nil {
		slog.Warn(
			"decode AT call state",
			"component", "modemmanager",
			"line_id", line.ID,
			"error", err,
		)
		return false
	}
	parsed := ParsedObjects{
		ATCallLines: make(map[string]string),
	}
	changed := p.projectKnownATCalls(&parsed, line, records, true)
	return changed
}

func (p *Provider) observeATCalls(ctx context.Context) {
	const operation = "observe_at_calls"
	p.callMu.Lock()
	defer p.callMu.Unlock()

	parsed, err := p.snapshotContent(ctx, operation)
	if err != nil {
		slog.Debug(
			"resolve AT call lines",
			"component", "modemmanager",
			"error", err,
		)
		return
	}
	p.initializeVoiceModel(ctx, operation, &parsed)
	changed := false
	for index := range parsed.Lines {
		line := &parsed.Lines[index]
		if parsed.CallBackends[line.ID] != callControlQuectelAT {
			continue
		}
		if p.refreshATLine(ctx, operation, line, parsed.LinePaths[line.ID]) {
			changed = true
		}
	}
	if changed {
		p.publishChange("at-call-state")
	}
}

func (p *Provider) hasATCallActivity() bool {
	p.atCallStateMu.Lock()
	defer p.atCallStateMu.Unlock()
	if len(p.atPendingCalls) > 0 {
		return true
	}
	for _, calls := range p.atCalls {
		if len(calls) > 0 {
			return true
		}
	}
	return false
}

func (p *Provider) hasATLineActivity(lineID string) bool {
	p.atCallStateMu.Lock()
	defer p.atCallStateMu.Unlock()
	if _, found := p.atPendingCalls[lineID]; found {
		return true
	}
	return len(p.atCalls[lineID]) > 0
}

func (p *Provider) RunATCallObserver(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}
	for {
		p.observeATCalls(ctx)
		interval := atCallIdleObservationInterval
		if p.hasATCallActivity() {
			interval = atCallActiveObservationInterval
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return
		case <-timer.C:
		}
	}
}

func (p *Provider) newATCallLifecycle(
	lineID string,
	record atCallRecord,
	now time.Time,
) atCallLifecycle {
	p.atCallSequence++
	lifecycle := fmt.Sprintf("%d:%d", now.UnixNano(), p.atCallSequence)
	return atCallLifecycle{
		id:         p.ids.atCallID(lineID, lifecycle),
		lineID:     lineID,
		index:      record.index,
		number:     record.number,
		direction:  record.direction,
		stateCode:  record.stateCode,
		multiparty: record.multiparty,
		observedAt: now,
	}
}

func appendATCall(
	parsed *ParsedObjects,
	line *domain.Line,
	lifecycle atCallLifecycle,
) {
	call := domain.Call{
		ID:              lifecycle.id,
		LineID:          lifecycle.lineID,
		Number:          lifecycle.number,
		Direction:       lifecycle.direction,
		State:           callStateName(lifecycle.stateCode),
		StateCode:       lifecycle.stateCode,
		StateReason:     atCallStateReason(lifecycle),
		StateReasonCode: atCallStateReasonCode(lifecycle),
		Multiparty:      lifecycle.multiparty,
		Bearer: cellularVoiceBearer(
			callStateName(lifecycle.stateCode),
			"",
			line.AccessTechnologies,
		),
	}
	parsed.Calls = append(parsed.Calls, call)
	parsed.ATCallLines[call.ID] = line.ID
	if !containsString(line.CallIDs, call.ID) {
		line.CallIDs = append(line.CallIDs, call.ID)
		sort.Strings(line.CallIDs)
	}
}

func atCallRecordStartsNewLifecycle(
	current atCallLifecycle,
	record atCallRecord,
) bool {
	if current.direction != record.direction {
		return true
	}
	if current.number != "" && record.number != "" &&
		!atNumbersEqual(current.number, record.number) {
		return true
	}
	return current.stateCode == 4 && (record.stateCode == 1 || record.stateCode == 2)
}

func atNumbersEqual(left, right string) bool {
	_, normalizedLeft, leftOK := quectelDialCommand(left)
	_, normalizedRight, rightOK := quectelDialCommand(right)
	if !leftOK || !rightOK {
		return false
	}
	return normalizedLeft == normalizedRight
}

func atCallStateReasonCode(call atCallLifecycle) int32 {
	switch call.stateCode {
	case 1, 2:
		return 1
	case 3, 6:
		return 2
	case 4, 5:
		return 3
	case callStateTerminated:
		return 4
	default:
		return 0
	}
}

func atCallStateReason(call atCallLifecycle) string {
	return callStateReasonName(atCallStateReasonCode(call))
}

func parseQuectelCLCC(response string) ([]atCallRecord, error) {
	response = strings.TrimSpace(response)
	if response == "" {
		return []atCallRecord{}, nil
	}

	records := make([]atCallRecord, 0)
	seen := make(map[int]struct{})
	for _, line := range strings.Split(strings.ReplaceAll(response, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.EqualFold(line, "OK") {
			continue
		}
		const prefix = "+CLCC:"
		if !strings.HasPrefix(strings.ToUpper(line), prefix) {
			return nil, fmt.Errorf("unexpected CLCC response line")
		}
		reader := csv.NewReader(strings.NewReader(strings.TrimSpace(line[len(prefix):])))
		reader.TrimLeadingSpace = true
		reader.FieldsPerRecord = -1
		fields, err := reader.Read()
		if err != nil {
			return nil, fmt.Errorf("decode CLCC response: %w", err)
		}
		if _, err := reader.Read(); err != io.EOF {
			return nil, fmt.Errorf("CLCC response returned multiple records")
		}
		if len(fields) < 5 {
			return nil, fmt.Errorf("CLCC response is incomplete")
		}
		index, indexErr := strconv.Atoi(strings.TrimSpace(fields[0]))
		direction, directionErr := strconv.Atoi(strings.TrimSpace(fields[1]))
		status, statusErr := strconv.Atoi(strings.TrimSpace(fields[2]))
		mode, modeErr := strconv.Atoi(strings.TrimSpace(fields[3]))
		multiparty, multipartyErr := strconv.Atoi(strings.TrimSpace(fields[4]))
		if indexErr != nil || index <= 0 ||
			directionErr != nil || (direction != 0 && direction != 1) ||
			statusErr != nil || status < 0 || status > 5 ||
			modeErr != nil ||
			multipartyErr != nil || (multiparty != 0 && multiparty != 1) {
			return nil, fmt.Errorf("CLCC response contains invalid fields")
		}
		if mode != 0 {
			continue
		}
		if _, duplicate := seen[index]; duplicate {
			return nil, fmt.Errorf("CLCC response repeated a call index")
		}
		seen[index] = struct{}{}

		number := ""
		if len(fields) >= 6 {
			number = strings.TrimSpace(fields[5])
		}
		numberType := 0
		if len(fields) >= 7 {
			numberType, err = strconv.Atoi(strings.TrimSpace(fields[6]))
			if err != nil || numberType < 0 || numberType > 255 {
				return nil, fmt.Errorf("CLCC response contains an invalid number type")
			}
			if numberType == 145 && number != "" && !strings.HasPrefix(number, "+") {
				number = "+" + number
			}
		}
		callDirection := "outgoing"
		if direction == 1 {
			callDirection = "incoming"
		}
		records = append(records, atCallRecord{
			index:      index,
			direction:  callDirection,
			stateCode:  clccStateCode(status),
			number:     number,
			numberType: numberType,
			multiparty: multiparty == 1,
		})
	}
	sort.Slice(records, func(i, j int) bool {
		return records[i].index < records[j].index
	})
	return records, nil
}

func clccStateCode(status int) int32 {
	switch status {
	case 0:
		return 4
	case 1:
		return 5
	case 2:
		return 1
	case 3:
		return 2
	case 4:
		return 3
	case 5:
		return 6
	default:
		return 0
	}
}

func (p *Provider) clearATCallState() {
	p.atCallStateMu.Lock()
	p.atCalls = make(map[string]map[int]atCallLifecycle)
	p.atPendingCalls = make(map[string]atCallLifecycle)
	p.atCallSequence = 0
	p.atCallStateMu.Unlock()
}
