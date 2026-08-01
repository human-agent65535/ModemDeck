package communication

import (
	"log/slog"
	"strings"

	"github.com/human-agent65535/modemdeck/internal/agentclient"
	"github.com/human-agent65535/modemdeck/internal/store"
)

func (s *Service) loggerForDiagnostics() *slog.Logger {
	s.loggerMu.RLock()
	logger := s.logger
	s.loggerMu.RUnlock()
	if logger == nil {
		return slog.Default()
	}
	return logger
}

func (s *Service) logSnapshotTransitions(
	previousStatus Status,
	previousSnapshot agentclient.Snapshot,
	status Status,
	snapshot agentclient.Snapshot,
	incomingMessages []store.Message,
	deliveryReportIDs []string,
) {
	logger := s.loggerForDiagnostics()
	switch {
	case strings.TrimSpace(previousStatus.BootEpoch) == "":
		logger.Info(
			"hardware snapshot synchronized",
			"provider", status.ProviderName,
			"agent_version", status.AgentVersion,
			"line_count", len(status.Lines),
			"call_count", len(snapshot.Calls),
			"revision", status.Revision,
		)
	case !previousStatus.Connected:
		logger.Info(
			"hardware connection recovered",
			"provider", status.ProviderName,
			"line_count", len(status.Lines),
			"call_count", len(snapshot.Calls),
		)
	case previousStatus.BootEpoch != status.BootEpoch:
		logger.Warn(
			"hardware agent epoch changed",
			"previous_boot_epoch", previousStatus.BootEpoch,
			"boot_epoch", status.BootEpoch,
		)
	}
	if strings.TrimSpace(previousStatus.BootEpoch) != "" {
		logLinePresenceTransitions(logger, previousStatus.Lines, status.Lines)
	}
	logCallTransitions(logger, previousStatus.Lines, previousSnapshot.Calls, status.Lines, snapshot.Calls)
	for _, message := range incomingMessages {
		logger.Info(
			"SMS received",
			"line_id", message.LineID,
			"message_id", message.ID,
		)
	}
	if deliveryReportCount := s.newDeliveryReportCount(status.BootEpoch, deliveryReportIDs); deliveryReportCount > 0 {
		logger.Info(
			"SMS delivery reports processed",
			"report_count", deliveryReportCount,
		)
	}
}

func (s *Service) newDeliveryReportCount(bootEpoch string, reportIDs []string) int {
	s.diagnosticLogMu.Lock()
	defer s.diagnosticLogMu.Unlock()
	if s.loggedDeliveryReportIDs == nil {
		s.loggedDeliveryReportIDs = make(map[string]struct{})
	}
	if len(s.loggedDeliveryReportIDs) >= 2048 {
		clear(s.loggedDeliveryReportIDs)
	}
	count := 0
	for _, reportID := range reportIDs {
		reportID = strings.TrimSpace(reportID)
		if reportID == "" {
			continue
		}
		key := strings.TrimSpace(bootEpoch) + "\x00" + reportID
		if _, found := s.loggedDeliveryReportIDs[key]; found {
			continue
		}
		s.loggedDeliveryReportIDs[key] = struct{}{}
		count++
	}
	return count
}

func logLinePresenceTransitions(logger *slog.Logger, previous, current []store.LineSummary) {
	previousByID := make(map[string]store.LineSummary, len(previous))
	for _, line := range previous {
		if line.ID != "" {
			previousByID[line.ID] = line
		}
	}
	currentIDs := make(map[string]struct{}, len(current))
	for _, line := range current {
		if line.ID == "" {
			continue
		}
		currentIDs[line.ID] = struct{}{}
		_, found := previousByID[line.ID]
		if !found {
			logger.Info("cellular line attached", "line_id", line.ID)
		}
	}
	for _, line := range previous {
		if line.ID == "" {
			continue
		}
		if _, found := currentIDs[line.ID]; !found {
			logger.Warn("cellular line detached", "line_id", line.ID)
		}
	}
}

func logCallTransitions(
	logger *slog.Logger,
	previousLines []store.LineSummary,
	previous []agentclient.Call,
	currentLines []store.LineSummary,
	current []agentclient.Call,
) {
	previousByID := make(map[string]agentclient.Call, len(previous))
	for _, call := range previous {
		if call.ID != "" {
			previousByID[call.ID] = call
		}
	}
	currentIDs := make(map[string]struct{}, len(current))
	for _, call := range current {
		if call.ID == "" {
			continue
		}
		currentIDs[call.ID] = struct{}{}
		before, found := previousByID[call.ID]
		lineID := stableLineID(currentLines, call.LineID)
		if !found {
			logger.Info(
				"call observed",
				"call_id", call.ID,
				"line_id", lineID,
				"direction", call.Direction,
				"state", call.State,
				"bearer", call.Bearer,
				"media_available", call.MediaAvailable,
			)
			continue
		}
		if before.State == call.State && before.Bearer == call.Bearer &&
			before.MediaAvailable == call.MediaAvailable && before.MediaActive == call.MediaActive {
			continue
		}
		logger.Info(
			"call state changed",
			"call_id", call.ID,
			"line_id", lineID,
			"previous_state", before.State,
			"state", call.State,
			"previous_bearer", before.Bearer,
			"bearer", call.Bearer,
			"media_available", call.MediaAvailable,
			"media_active", call.MediaActive,
		)
	}
	for _, call := range previous {
		if call.ID == "" {
			continue
		}
		if _, found := currentIDs[call.ID]; !found {
			logger.Info(
				"call no longer present in hardware snapshot",
				"call_id", call.ID,
				"line_id", stableLineID(previousLines, call.LineID),
				"previous_state", call.State,
			)
		}
	}
}

func stableLineID(lines []store.LineSummary, endpointID string) string {
	for _, line := range lines {
		if line.EndpointID == endpointID {
			return line.ID
		}
	}
	return endpointID
}

func cloneAgentSnapshot(snapshot agentclient.Snapshot) agentclient.Snapshot {
	snapshot.Lines = append([]agentclient.Line(nil), snapshot.Lines...)
	snapshot.Calls = append([]agentclient.Call(nil), snapshot.Calls...)
	snapshot.Messages = append([]agentclient.Message(nil), snapshot.Messages...)
	snapshot.DeliveryReports = append(
		[]agentclient.MessageDeliveryReport(nil),
		snapshot.DeliveryReports...,
	)
	return snapshot
}
