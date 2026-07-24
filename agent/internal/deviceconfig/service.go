package deviceconfig

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/human-agent65535/modemdeck/agent/internal/domain"
	"github.com/human-agent65535/modemdeck/agent/internal/volte"
)

type GenericProvider interface {
	ReadDeviceConfiguration(context.Context, string) (domain.DeviceConfiguration, error)
	ApplyGenericDeviceConfiguration(
		context.Context,
		domain.ApplyDeviceConfigurationRequest,
	) (domain.DeviceConfiguration, error)
}

type TransportResolver func(
	context.Context,
	string,
	volte.Identity,
) (volte.Transports, error)

type Service struct {
	generic         GenericProvider
	registry        *volte.Registry
	resolve         TransportResolver
	applyMu         sync.Mutex
	pendingMu       sync.RWMutex
	pendingRestarts map[string]bool
}

func New(
	generic GenericProvider,
	registry *volte.Registry,
	resolver TransportResolver,
) (*Service, error) {
	if generic == nil {
		return nil, errors.New("device configuration generic provider is required")
	}
	if registry == nil {
		return nil, errors.New("device configuration VoLTE registry is required")
	}
	return &Service{
		generic:         generic,
		registry:        registry,
		resolve:         resolver,
		pendingRestarts: make(map[string]bool),
	}, nil
}

func (s *Service) DeviceConfiguration(
	ctx context.Context,
	lineID string,
) (domain.DeviceConfiguration, error) {
	base, err := s.generic.ReadDeviceConfiguration(ctx, lineID)
	if err != nil {
		return domain.DeviceConfiguration{}, err
	}
	return s.enrich(ctx, base)
}

func (s *Service) ApplyDeviceConfiguration(
	ctx context.Context,
	request domain.ApplyDeviceConfigurationRequest,
) (domain.DeviceConfiguration, error) {
	const operation = "apply_device_configuration"
	if err := validateRequest(request); err != nil {
		return domain.DeviceConfiguration{}, err
	}

	s.applyMu.Lock()
	defer s.applyMu.Unlock()

	base, err := s.generic.ReadDeviceConfiguration(ctx, request.LineID)
	if err != nil {
		return domain.DeviceConfiguration{}, err
	}
	current, err := s.enrich(ctx, base)
	if err != nil {
		return domain.DeviceConfiguration{}, err
	}
	if current.Revision != strings.TrimSpace(request.ExpectedRevision) {
		return domain.DeviceConfiguration{}, domain.Conflict(
			operation,
			"device configuration changed; read the latest revision before applying",
		)
	}

	if request.Operation != domain.DeviceConfigurationSetVoLTEPolicy {
		request.ExpectedRevision = base.Revision
		updated, err := s.generic.ApplyGenericDeviceConfiguration(ctx, request)
		if err != nil {
			return domain.DeviceConfiguration{}, err
		}
		if request.Operation == domain.DeviceConfigurationRestartModem {
			s.setRestartPending(request.LineID, false)
			current.VoLTE.RestartRequired = false
			return current, nil
		}
		return s.enrich(ctx, updated)
	}

	if !current.Capabilities.VoLTE.Readable {
		return domain.DeviceConfiguration{}, domain.Unavailable(
			operation,
			firstNonEmpty(
				current.Capabilities.VoLTE.Reason,
				"VoLTE state is unavailable and cannot be verified before applying",
			),
			nil,
		)
	}
	driver, _, capability, err := s.resolveDriver(ctx, current)
	if err != nil {
		return domain.DeviceConfiguration{}, err
	}
	if !capability.Writable {
		return domain.DeviceConfiguration{}, domain.NotSupported(operation, capability.Reason)
	}
	requestedPolicy := volte.Policy(request.VoLTEPolicy)
	if current.VoLTE.PolicyKnown && current.VoLTE.Policy == string(requestedPolicy) {
		return current, nil
	}
	applied, err := driver.Apply(ctx, requestedPolicy)
	if err != nil {
		return domain.DeviceConfiguration{}, mapVoLTEError(operation, err)
	}
	if applied.RestartRequired {
		s.setRestartPending(request.LineID, true)
	}
	updated, err := s.generic.ReadDeviceConfiguration(ctx, request.LineID)
	if err != nil {
		return domain.DeviceConfiguration{}, err
	}
	updated, err = s.enrich(ctx, updated)
	if err != nil {
		return domain.DeviceConfiguration{}, err
	}
	updated.VoLTE.RestartRequired =
		updated.VoLTE.RestartRequired || applied.RestartRequired
	updated.Revision, err = domain.RevisionDeviceConfiguration(updated)
	if err != nil {
		return domain.DeviceConfiguration{}, domain.Internal(
			operation,
			"failed to revision the applied device configuration",
			err,
		)
	}
	return updated, nil
}

func (s *Service) enrich(
	ctx context.Context,
	configuration domain.DeviceConfiguration,
) (domain.DeviceConfiguration, error) {
	driver, _, capability, err := s.resolveDriver(ctx, configuration)
	if err != nil {
		return domain.DeviceConfiguration{}, err
	}
	configuration.Capabilities.VoLTE = capability
	configuration.VoLTE = domain.VoLTEConfiguration{ProfileID: driver.Capability().ProfileID}
	if capability.Readable {
		state, err := driver.Read(ctx)
		if err != nil {
			configuration.Capabilities.VoLTE.Readable = false
			configuration.Capabilities.VoLTE.Writable = false
			configuration.Capabilities.VoLTE.Reason = volteReadFailureReason(err)
		} else {
			configuration.VoLTE = domain.VoLTEConfiguration{
				PolicyKnown:            true,
				Policy:                 string(state.Policy),
				ConfigurationMode:      string(state.ConfigurationMode),
				ModemCapabilityKnown:   state.ModemCapabilityKnown,
				ModemCapabilityEnabled: state.ModemCapabilityEnabled,
				RestartRequired:        state.RestartRequired || s.restartPending(configuration.LineID),
				ProfileID:              driver.Capability().ProfileID,
			}
		}
	}
	configuration.VoLTE.RestartRequired =
		configuration.VoLTE.RestartRequired || s.restartPending(configuration.LineID)
	configuration.Revision, err = domain.RevisionDeviceConfiguration(configuration)
	if err != nil {
		return domain.DeviceConfiguration{}, domain.Internal(
			"read_device_configuration",
			"failed to revision the complete device configuration",
			err,
		)
	}
	return configuration, nil
}

func (s *Service) restartPending(lineID string) bool {
	s.pendingMu.RLock()
	defer s.pendingMu.RUnlock()
	return s.pendingRestarts[strings.TrimSpace(lineID)]
}

func (s *Service) setRestartPending(lineID string, pending bool) {
	lineID = strings.TrimSpace(lineID)
	if lineID == "" {
		return
	}
	s.pendingMu.Lock()
	defer s.pendingMu.Unlock()
	if pending {
		s.pendingRestarts[lineID] = true
		return
	}
	delete(s.pendingRestarts, lineID)
}

func (s *Service) resolveDriver(
	ctx context.Context,
	configuration domain.DeviceConfiguration,
) (volte.Driver, volte.Transports, domain.FeatureCapability, error) {
	identity := volte.Identity{
		Manufacturer: configuration.Identity.Manufacturer,
		Model:        configuration.Identity.Model,
		Firmware:     configuration.Identity.Firmware,
	}
	transports := volte.Transports{}
	candidate := s.registry.Resolve(identity, transports)
	profile := candidate.Capability()
	if !profile.Supported {
		return candidate, transports, domain.FeatureCapability{
			Backend: "vendor_extension",
			Reason:  "no exact manufacturer, model, and firmware VoLTE profile was resolved",
		}, nil
	}
	if s.resolve != nil {
		var err error
		transports, err = s.resolve(ctx, configuration.LineID, identity)
		if err != nil {
			return candidate, volte.Transports{}, domain.FeatureCapability{
				Backend:     "vendor_extension",
				Supported:   true,
				Implemented: true,
				Reason:      "exact VoLTE profile resolved, but its transport is unavailable",
			}, nil
		}
	}
	driver := s.registry.Resolve(identity, transports)
	capability := driver.Capability()
	readable := capability.Readable && protocolAvailable(capability.ReadProtocol, transports)
	writable := capability.Writable && protocolAvailable(capability.WriteProtocol, transports) && readable
	reason := ""
	if !readable {
		reason = fmt.Sprintf(
			"exact profile %q resolved, but its %s read transport is unavailable",
			capability.ProfileID,
			capability.ReadProtocol,
		)
	} else if capability.Writable && !writable {
		reason = fmt.Sprintf(
			"exact profile %q is readable, but its %s write transport is unavailable",
			capability.ProfileID,
			capability.WriteProtocol,
		)
	} else if !capability.Writable {
		reason = fmt.Sprintf("exact profile %q is read-only", capability.ProfileID)
	}
	return driver, transports, domain.FeatureCapability{
		Backend:     "vendor_extension",
		Supported:   true,
		Implemented: true,
		Readable:    readable,
		Writable:    writable,
		Reason:      reason,
	}, nil
}

func volteReadFailureReason(err error) string {
	typed, ok := volte.AsError(err)
	if !ok || strings.TrimSpace(typed.Message) == "" {
		return "exact VoLTE profile resolved, but current policy could not be read"
	}
	return "exact VoLTE profile resolved, but current policy could not be read: " +
		strings.TrimSpace(typed.Message)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func protocolAvailable(protocol volte.Protocol, transports volte.Transports) bool {
	switch protocol {
	case volte.ProtocolAT:
		return transports.AT != nil
	case volte.ProtocolQMI:
		return transports.QMI != nil
	default:
		return false
	}
}

func validateRequest(request domain.ApplyDeviceConfigurationRequest) error {
	const operation = "apply_device_configuration"
	if strings.TrimSpace(request.RequestID) == "" || len(strings.TrimSpace(request.RequestID)) > 128 {
		return domain.InvalidArgument(operation, "request_id is required and must be valid")
	}
	if strings.TrimSpace(request.LineID) == "" {
		return domain.InvalidArgument(operation, "line id is required")
	}
	if strings.TrimSpace(request.ExpectedRevision) == "" ||
		len(strings.TrimSpace(request.ExpectedRevision)) > 128 {
		return domain.InvalidArgument(operation, "expected_revision is required and must be valid")
	}
	switch request.Operation {
	case domain.DeviceConfigurationSetRadioEnabled,
		domain.DeviceConfigurationConnectData,
		domain.DeviceConfigurationDisconnectData,
		domain.DeviceConfigurationRestartModem:
		return nil
	case domain.DeviceConfigurationSetVoLTEPolicy:
		if request.RadioEnabled != nil || request.APN != "" || request.IPFamily != "" {
			return domain.InvalidArgument(operation, "set_volte_policy accepts only volte_policy")
		}
		if request.VoLTEPolicy != string(volte.PolicyEnabled) &&
			request.VoLTEPolicy != string(volte.PolicyDisabled) {
			return domain.InvalidArgument(operation, "volte_policy must be enabled or disabled")
		}
		return nil
	default:
		return domain.InvalidArgument(operation, "unsupported device configuration operation")
	}
}

func mapVoLTEError(operation string, err error) error {
	typed, ok := volte.AsError(err)
	if !ok {
		return domain.Internal(operation, "VoLTE extension failed", err)
	}
	switch typed.Code {
	case volte.ErrorUnsupported:
		return domain.NotSupported(operation, typed.Message)
	case volte.ErrorInvalidArgument, volte.ErrorInvalidPolicy:
		return domain.InvalidArgument(operation, typed.Message)
	case volte.ErrorUnavailable, volte.ErrorTransport, volte.ErrorTimeout, volte.ErrorCanceled:
		return domain.Unavailable(operation, typed.Message, err)
	case volte.ErrorVerification:
		return domain.VerificationFailed(operation, typed.Message, err)
	case volte.ErrorInvalidProfile, volte.ErrorDecode:
		return domain.Internal(operation, typed.Message, err)
	default:
		return domain.Internal(operation, "VoLTE extension failed", err)
	}
}

var _ domain.DeviceConfigurationProvider = (*Service)(nil)
