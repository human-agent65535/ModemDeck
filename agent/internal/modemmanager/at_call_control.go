package modemmanager

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
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

	quectelAnswerCall = "ATA"
	quectelHangupCall = "AT+CHUP"

	atPendingCallTTL = 10 * time.Second
)

type callControlBackend string

type atCallRecord struct {
	index      int
	direction  string
	stateCode  int32
	number     string
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
	if _, err := p.commandATPath(ctx, path, operation, command); err != nil {
		return domain.CommandReceipt{}, err
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

	return domain.CommandReceipt{
		RequestID:  request.RequestID,
		ResourceID: callID,
	}, nil
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
	ctx context.Context,
	operation string,
	parsed *ParsedObjects,
) {
	for lineIndex := range parsed.Lines {
		line := &parsed.Lines[lineIndex]
		if parsed.CallBackends[line.ID] != callControlQuectelAT {
			continue
		}
		path := parsed.LinePaths[line.ID]
		response, err := p.commandATPath(
			ctx,
			path,
			operation,
			quectelCallListQuery,
		)
		if err != nil {
			p.projectKnownATCalls(parsed, line, nil, false)
			continue
		}
		records, parseErr := parseQuectelCLCC(response)
		if parseErr != nil {
			p.projectKnownATCalls(parsed, line, nil, false)
			continue
		}
		p.projectKnownATCalls(parsed, line, records, true)
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
) {
	p.atCallStateMu.Lock()
	defer p.atCallStateMu.Unlock()

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
	}

	if pending, found := p.atPendingCalls[line.ID]; found {
		if now.Sub(pending.observedAt) < atPendingCallTTL || !authoritative {
			appendATCall(parsed, line, pending)
		} else {
			pending.stateCode = callStateTerminated
			appendATCall(parsed, line, pending)
			delete(p.atPendingCalls, line.ID)
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
	if strings.ContainsAny(normalizedLeft+normalizedRight, "*#") {
		return normalizedLeft == normalizedRight
	}
	normalizedLeft = strings.TrimPrefix(normalizedLeft, "+")
	normalizedRight = strings.TrimPrefix(normalizedRight, "+")
	normalizedLeft = strings.TrimPrefix(normalizedLeft, "00")
	normalizedRight = strings.TrimPrefix(normalizedRight, "00")
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
		callDirection := "outgoing"
		if direction == 1 {
			callDirection = "incoming"
		}
		records = append(records, atCallRecord{
			index:      index,
			direction:  callDirection,
			stateCode:  clccStateCode(status),
			number:     number,
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
