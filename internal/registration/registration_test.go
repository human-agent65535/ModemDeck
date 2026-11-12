package registration

import "testing"

func TestParseCoversEveryStateTheAgentReports(t *testing.T) {
	// The names produced by registrationStateName in the Agent.
	for name, want := range map[string]State{
		"idle":                       Idle,
		"home":                       Home,
		"searching":                  Searching,
		"denied":                     Denied,
		"unknown":                    Unknown,
		"roaming":                    Roaming,
		"home-sms-only":              Home,
		"roaming-sms-only":           Roaming,
		"emergency-only":             Restricted,
		"home-csfb-not-preferred":    Home,
		"roaming-csfb-not-preferred": Roaming,
		"attached-rlos":              Restricted,
		" HOME ":                     Home,
		"":                           Unknown,
	} {
		if got := Parse(name); got != want {
			t.Fatalf("Parse(%q) = %d, want %d", name, got, want)
		}
	}
	for state, want := range map[State]bool{
		Home: true, Roaming: true,
		Unknown: false, Idle: false, Searching: false, Denied: false, Restricted: false,
	} {
		if state.Registered() != want {
			t.Fatalf("State(%d).Registered() = %t, want %t", state, state.Registered(), want)
		}
	}
}
