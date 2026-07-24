package volte

import "context"

// Identity is the complete modem identity used to select a vendor profile.
// Every field participates in an exact, case-sensitive match.
type Identity struct {
	Manufacturer string
	Model        string
	Firmware     string
}

type Protocol string

const (
	ProtocolAT  Protocol = "at"
	ProtocolQMI Protocol = "qmi"
)

type Policy string

const (
	PolicyDisabled Policy = "disabled"
	PolicyEnabled  Policy = "enabled"
)

func (p Policy) valid() bool {
	return p == PolicyDisabled || p == PolicyEnabled
}

type ConfigurationMode string

const (
	ConfigurationModeAutomatic      ConfigurationMode = "automatic"
	ConfigurationModeForcedEnabled  ConfigurationMode = "forced_enabled"
	ConfigurationModeForcedDisabled ConfigurationMode = "forced_disabled"
)

// State is the vendor setting returned by the registered profile. It is not
// evidence of IMS registration or of the bearer used by an active call.
type State struct {
	Policy                 Policy
	ConfigurationMode      ConfigurationMode
	ModemCapabilityKnown   bool
	ModemCapabilityEnabled bool
	RestartRequired        bool
}

type Capability struct {
	Supported            bool
	ProfileID            string
	Identity             Identity
	Readable             bool
	Writable             bool
	ReadProtocol         Protocol
	WriteProtocol        Protocol
	ApplyRequiresRestart bool
}

// Driver exposes a single resolved vendor profile. Apply always verifies the
// write through the profile's declared read method before it returns success.
type Driver interface {
	Capability() Capability
	Read(context.Context) (State, error)
	Apply(context.Context, Policy) (State, error)
}

type ATTransport interface {
	Command(context.Context, string) (string, error)
}

type QMIRequest struct {
	Service string
	Method  string
	Payload []byte
}

type QMITransport interface {
	Request(context.Context, QMIRequest) ([]byte, error)
}

type Transports struct {
	AT  ATTransport
	QMI QMITransport
}
