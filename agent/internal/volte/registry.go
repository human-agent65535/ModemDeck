package volte

import (
	"context"
	"errors"
	"sync"
)

type Registry struct {
	mu       sync.RWMutex
	profiles map[Identity]Profile
}

func NewRegistry(profiles ...Profile) (*Registry, error) {
	registry := &Registry{profiles: make(map[Identity]Profile, len(profiles))}
	for _, profile := range profiles {
		if err := registry.Register(profile); err != nil {
			return nil, err
		}
	}
	return registry, nil
}

func (r *Registry) Register(profile Profile) error {
	const operation = "register"
	if err := profile.validate(); err != nil {
		return newError(
			ErrorInvalidProfile,
			operation,
			profile.ID,
			"",
			"VoLTE profile is invalid",
			err,
		)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.profiles == nil {
		r.profiles = make(map[Identity]Profile)
	}
	if existing, found := r.profiles[profile.Identity]; found {
		return newError(
			ErrorInvalidProfile,
			operation,
			profile.ID,
			"",
			"an exact profile is already registered for this modem identity",
			errors.New(existing.ID),
		)
	}
	r.profiles[profile.Identity] = profile
	return nil
}

// Resolve uses direct map equality for all three identity fields. It performs
// no normalization, wildcard matching, model aliasing, or firmware fallback.
func (r *Registry) Resolve(identity Identity, transports Transports) Driver {
	if r != nil {
		r.mu.RLock()
		profile, found := r.profiles[identity]
		r.mu.RUnlock()
		if found {
			return &profileDriver{
				profile:    profile,
				transports: transports,
			}
		}
	}
	return unsupportedDriver{identity: identity}
}

type unsupportedDriver struct {
	identity Identity
}

func (d unsupportedDriver) Capability() Capability {
	return Capability{Identity: d.identity}
}

func (d unsupportedDriver) Read(context.Context) (State, error) {
	return State{}, newError(
		ErrorUnsupported,
		"read",
		"",
		"",
		"no exact VoLTE vendor profile is registered for this modem identity",
		nil,
	)
}

func (d unsupportedDriver) Apply(context.Context, Policy) (State, error) {
	return State{}, newError(
		ErrorUnsupported,
		"apply",
		"",
		"",
		"no exact VoLTE vendor profile is registered for this modem identity",
		nil,
	)
}

type profileDriver struct {
	profile    Profile
	transports Transports
}

func (d *profileDriver) Capability() Capability {
	return d.profile.capability()
}

func (d *profileDriver) Read(ctx context.Context) (State, error) {
	const operation = "read"
	if d.profile.Read == nil {
		return State{}, newError(
			ErrorUnsupported,
			operation,
			d.profile.ID,
			"",
			"profile does not declare a read method",
			nil,
		)
	}
	if ctx == nil {
		return State{}, newError(
			ErrorInvalidArgument,
			operation,
			d.profile.ID,
			d.profile.Read.Protocol(),
			"context is required",
			nil,
		)
	}

	bounded, cancel := context.WithTimeout(ctx, d.profile.OperationTimeout)
	defer cancel()
	state, err := d.profile.Read.read(bounded, execution{
		profileID:  d.profile.ID,
		transports: d.transports,
	})
	if err != nil {
		return State{}, err
	}
	if !state.Policy.valid() {
		return State{}, newError(
			ErrorDecode,
			operation,
			d.profile.ID,
			d.profile.Read.Protocol(),
			"profile returned an invalid policy",
			nil,
		)
	}
	return state, nil
}

func (d *profileDriver) Apply(ctx context.Context, policy Policy) (State, error) {
	const operation = "apply"
	if !policy.valid() {
		return State{}, newError(
			ErrorInvalidPolicy,
			operation,
			d.profile.ID,
			"",
			"policy must be enabled or disabled",
			nil,
		)
	}
	if d.profile.Write == nil {
		return State{}, newError(
			ErrorUnsupported,
			operation,
			d.profile.ID,
			"",
			"profile does not declare a write method",
			nil,
		)
	}
	if ctx == nil {
		return State{}, newError(
			ErrorInvalidArgument,
			operation,
			d.profile.ID,
			d.profile.Write.Protocol(),
			"context is required",
			nil,
		)
	}

	bounded, cancel := context.WithTimeout(ctx, d.profile.OperationTimeout)
	defer cancel()
	run := execution{profileID: d.profile.ID, transports: d.transports}
	if err := d.profile.Write.write(bounded, run, policy); err != nil {
		return State{}, err
	}
	state, err := d.profile.Read.read(bounded, run)
	if err != nil {
		return State{}, newError(
			ErrorVerification,
			operation,
			d.profile.ID,
			d.profile.Read.Protocol(),
			"write completed but read-back failed; resulting policy is unknown",
			err,
		)
	}
	if !state.Policy.valid() {
		return State{}, newError(
			ErrorDecode,
			operation,
			d.profile.ID,
			d.profile.Read.Protocol(),
			"read-back returned an invalid policy",
			nil,
		)
	}
	if state.Policy != policy {
		return State{}, newError(
			ErrorVerification,
			operation,
			d.profile.ID,
			d.profile.Read.Protocol(),
			"read-back did not match the requested policy",
			nil,
		)
	}
	state.RestartRequired = d.profile.ApplyRequiresRestart
	return state, nil
}

func executionError(
	operation string,
	profileID string,
	protocol Protocol,
	message string,
	err error,
) error {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return newError(ErrorTimeout, operation, profileID, protocol, message, err)
	case errors.Is(err, context.Canceled):
		return newError(ErrorCanceled, operation, profileID, protocol, message, err)
	default:
		return newError(ErrorTransport, operation, profileID, protocol, message, err)
	}
}

var _ Driver = (*profileDriver)(nil)
var _ Driver = unsupportedDriver{}
