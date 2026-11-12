package communication

import (
	"fmt"

	"github.com/human-agent65535/modemdeck/internal/agentclient"
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
