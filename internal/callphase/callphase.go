// Package callphase names the phases of a call projected from hardware state
// and the one definition of which of them are live.
package callphase

import "strings"

const (
	Dialing    = "dialing"
	RingingOut = "ringing-out"
	Ringing    = "ringing"
	Connecting = "connecting"
	Active     = "active"
)

// Live reports whether a call in this phase is still in progress: it can be
// owned, controlled and prepared for recording. Every other phase is terminal
// or not yet known.
func Live(phase string) bool {
	switch strings.ToLower(strings.TrimSpace(phase)) {
	case Dialing, RingingOut, Ringing, Connecting, Active:
		return true
	default:
		return false
	}
}
