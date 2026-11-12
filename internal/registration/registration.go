// Package registration interprets the network registration state the Host
// Agent reports for a line. The names are ModemManager's 3GPP registration
// states; this is the one place the application decides what each one means.
package registration

import "strings"

// State is what a reported registration state means for the line.
type State int

const (
	// Unknown: not reported, or a name this application does not know.
	Unknown State = iota
	// Idle: not registered and not searching.
	Idle
	// Searching for a network.
	Searching
	// Denied by the network.
	Denied
	// Home: registered on the home network, including the SMS-only and
	// CSFB-not-preferred forms.
	Home
	// Roaming: registered on a visited network, including the SMS-only and
	// CSFB-not-preferred forms.
	Roaming
	// Restricted: attached for emergency or restricted local operator services
	// only. The SIM cannot be used for ordinary calls or messages.
	Restricted
)

// Parse interprets a registration state name reported by the Host Agent.
func Parse(name string) State {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "idle":
		return Idle
	case "searching":
		return Searching
	case "denied":
		return Denied
	case "home", "home-sms-only", "home-csfb-not-preferred":
		return Home
	case "roaming", "roaming-sms-only", "roaming-csfb-not-preferred":
		return Roaming
	case "emergency-only", "attached-rlos":
		return Restricted
	default:
		return Unknown
	}
}

// Registered reports whether the line is registered for ordinary service.
func (s State) Registered() bool {
	return s == Home || s == Roaming
}
