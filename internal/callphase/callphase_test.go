package callphase

import "testing"

func TestLiveCoversTheInProgressPhasesOnly(t *testing.T) {
	for phase, want := range map[string]bool{
		Dialing: true, RingingOut: true, Ringing: true, Connecting: true, Active: true,
		" ACTIVE ": true,
		"unknown":  false, "ending": false, "ended": false, "failed": false, "": false,
	} {
		if Live(phase) != want {
			t.Fatalf("Live(%q) = %t, want %t", phase, Live(phase), want)
		}
	}
}
