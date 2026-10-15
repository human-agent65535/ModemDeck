package communication

import "github.com/human-agent65535/modemdeck/internal/agentclient"

// Correlate the durable API projection with the source event sequence, without
// recording phone numbers or media payloads. Unchanged snapshots stay quiet.
func (s *Service) logCallObservations(previous, current agentclient.Snapshot) {
	before := make(map[string]agentclient.Call, len(previous.Calls))
	for _, call := range previous.Calls {
		before[call.ID] = call
	}
	for _, call := range current.Calls {
		old, found := before[call.ID]
		if found && old.State == call.State &&
			old.StateRevision == call.StateRevision && old.MediaAvailable == call.MediaAvailable &&
			old.StateReason == call.StateReason {
			continue
		}
		s.loggerForDiagnostics().Info("call connection state committed",
			"component", "call_connection", "stage", "hardware_projection",
			"call_id", stableInstanceID("call", call.LineID, call.ID),
			"endpoint_call_id", call.ID, "direction", call.Direction,
			"state", call.State, "reported_state", call.ReportedState,
			"source", call.StateSource, "state_revision", call.StateRevision,
			"state_reason_code", call.StateReasonCode, "media_available", call.MediaAvailable,
			"observed_at", current.ObservedAt)
	}
}
