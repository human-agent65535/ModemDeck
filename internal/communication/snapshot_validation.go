package communication

import (
	"fmt"
	"slices"
	"strings"

	"github.com/human-agent65535/modemdeck/internal/agentclient"
	"github.com/human-agent65535/modemdeck/internal/timeutil"
)

// validateSnapshotCallLines rejects a snapshot in which a call names a line the
// snapshot does not contain.
//
// Such a call cannot be committed: it would be stored without a phone number,
// home country or endpoint line, and line-scoped authorization would evaluate
// an empty line. It cannot be left out either, because the snapshot is the
// authority for live calls and a call missing from it is recorded as ended
// while the modem still carries it. The whole snapshot is therefore refused as
// a protocol error and the previous committed state stays in place.
func validateSnapshotCallLines(snapshot agentclient.Snapshot) error {
	lineIDs := make(map[string]struct{}, len(snapshot.Lines))
	for _, line := range snapshot.Lines {
		lineIDs[line.ID] = struct{}{}
	}
	for _, call := range snapshot.Calls {
		if _, found := lineIDs[call.LineID]; !found {
			return fmt.Errorf(
				"%w: call %q is on line %q, which the snapshot does not contain",
				agentclient.ErrProtocol,
				call.ID,
				call.LineID,
			)
		}
	}
	return nil
}

// maxReportedTimestampIDs bounds the set of objects already reported by
// logUnreadableModemTimestamps.
const maxReportedTimestampIDs = 1024

// logUnreadableModemTimestamps reports messages and delivery reports whose
// modem timestamp is present but cannot be read. The projection stores the
// snapshot's observation time for them, which silently changes when the user
// sees the message as sent or received. An object is reported once; a missing
// timestamp is normal and is not reported.
func (s *Service) logUnreadableModemTimestamps(snapshot agentclient.Snapshot) {
	report := func(kind, id, lineID, value string) {
		if strings.TrimSpace(value) == "" {
			return
		}
		if _, ok := timeutil.ParseModemTimestamp(value); ok {
			return
		}
		key := kind + "\x00" + lineID + "\x00" + id
		s.timestampReportMu.Lock()
		if _, reported := s.reportedTimestamps[key]; reported {
			s.timestampReportMu.Unlock()
			return
		}
		if s.reportedTimestamps == nil || len(s.reportedTimestamps) >= maxReportedTimestampIDs {
			s.reportedTimestamps = make(map[string]struct{})
		}
		s.reportedTimestamps[key] = struct{}{}
		s.timestampReportMu.Unlock()
		s.loggerForDiagnostics().Warn(
			"modem timestamp could not be read; the observation time is stored instead",
			"component", "communication",
			"kind", kind,
			"endpoint_id", id,
			"endpoint_line_id", lineID,
			"value", value,
		)
	}
	for _, message := range snapshot.Messages {
		report("message", message.ID, message.LineID, message.Timestamp)
	}
	for _, deliveryReport := range snapshot.DeliveryReports {
		report("delivery report", deliveryReport.ID, deliveryReport.LineID, deliveryReport.Timestamp)
	}
}

// reportDuplicateSubscriptionQuarantine logs when the set of modem lines hidden
// because they carry the same SIM changes. Their calls and messages are
// filtered out of the snapshot, so the operator must be able to see that it
// happened and which lines lost.
func (s *Service) reportDuplicateSubscriptionQuarantine(lineIDs []string) {
	s.quarantineReportMu.Lock()
	changed := !slices.Equal(s.reportedQuarantine, lineIDs)
	s.reportedQuarantine = slices.Clone(lineIDs)
	s.quarantineReportMu.Unlock()
	if !changed {
		return
	}
	if len(lineIDs) == 0 {
		s.loggerForDiagnostics().Info(
			"no modem line is hidden for a duplicate SIM any more",
			"component", "communication",
		)
		return
	}
	s.loggerForDiagnostics().Warn(
		"modem lines with a SIM that another line also reports are hidden; their calls and messages are not shown",
		"component", "communication",
		"endpoint_line_ids", strings.Join(lineIDs, ","),
	)
}
