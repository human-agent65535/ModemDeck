package deviceconfig

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/human-agent65535/modemdeck/agent/internal/domain"
	"github.com/human-agent65535/modemdeck/agent/internal/volte"
)

type fakeGenericProvider struct {
	configuration domain.DeviceConfiguration
	applyCalls    int
	applyRequests []domain.ApplyDeviceConfigurationRequest
}

func (provider *fakeGenericProvider) ReadDeviceConfiguration(
	context.Context,
	string,
) (domain.DeviceConfiguration, error) {
	return provider.configuration, nil
}

func (provider *fakeGenericProvider) ApplyGenericDeviceConfiguration(
	_ context.Context,
	request domain.ApplyDeviceConfigurationRequest,
) (domain.DeviceConfiguration, error) {
	provider.applyCalls++
	provider.applyRequests = append(provider.applyRequests, request)
	if request.Operation == domain.DeviceConfigurationSetVoLTEPolicy &&
		provider.configuration.Capabilities.VoLTE.Backend == "modemmanager" {
		provider.configuration.VoLTE.PolicyKnown = true
		provider.configuration.VoLTE.Policy = request.VoLTEPolicy
	}
	return provider.configuration, nil
}

type fakeATTransport struct {
	policy   volte.Policy
	commands []string
	readErr  error
}

func (transport *fakeATTransport) Command(_ context.Context, command string) (string, error) {
	transport.commands = append(transport.commands, command)
	switch command {
	case `AT+QCFG="ims"`:
		return `+QCFG: "ims",0,1`, nil
	case `AT+QCFG="ims",1`,
		`AT+QCFG="ims",2`:
		return "", nil
	case "AT+TESTVOLTE?":
		if transport.readErr != nil {
			return "", transport.readErr
		}
		return string(transport.policy), nil
	case "AT+TESTVOLTE=0":
		transport.policy = volte.PolicyDisabled
		return "OK", nil
	case "AT+TESTVOLTE=1":
		transport.policy = volte.PolicyEnabled
		return "OK", nil
	default:
		return "", fmt.Errorf("unexpected command %q", command)
	}
}

func TestMatchedVoLTEReadFailureKeepsGenericConfiguration(t *testing.T) {
	t.Parallel()
	identity := domain.DeviceIdentity{
		Manufacturer: "Fixture Vendor",
		Model:        "Fixture Model",
		Firmware:     "fixture-fw-1",
	}
	profile := volte.Profile{
		ID:               "fixture-volte-v1",
		Matches:          matchDeviceIdentity(identity),
		OperationTimeout: time.Second,
		Read: volte.ATRead("AT+TESTVOLTE?", func(response string) (volte.State, error) {
			return volte.State{Policy: volte.Policy(response)}, nil
		}),
	}
	registry, err := volte.NewRegistry(profile)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	generic := &fakeGenericProvider{configuration: baseConfiguration(t, identity)}
	at := &fakeATTransport{readErr: fmt.Errorf("ModemManager command disabled")}
	service, err := New(
		generic,
		registry,
		func(context.Context, string, volte.Identity) (volte.Transports, error) {
			return volte.Transports{AT: at}, nil
		},
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	configuration, err := service.DeviceConfiguration(context.Background(), "line-1")
	if err != nil {
		t.Fatalf("DeviceConfiguration() error = %v", err)
	}
	if !configuration.Capabilities.VoLTE.Supported ||
		!configuration.Capabilities.VoLTE.Implemented ||
		configuration.Capabilities.VoLTE.Readable ||
		configuration.Capabilities.VoLTE.Writable ||
		configuration.VoLTE.PolicyKnown ||
		configuration.VoLTE.ProfileID != profile.ID ||
		configuration.Capabilities.VoLTE.Reason == "" {
		t.Fatalf("configuration = %+v", configuration)
	}
	_, err = service.ApplyDeviceConfiguration(
		context.Background(),
		domain.ApplyDeviceConfigurationRequest{
			RequestID:        "volte-request-read-failure",
			LineID:           "line-1",
			ExpectedRevision: configuration.Revision,
			Operation:        domain.DeviceConfigurationSetVoLTEPolicy,
			VoLTEPolicy:      "enabled",
		},
	)
	operationError, ok := domain.AsOperationError(err)
	if !ok || operationError.Code != domain.ErrorUnavailable {
		t.Fatalf("ApplyDeviceConfiguration() error = %v", err)
	}
}

func TestModemManagerVoLTEReportTakesPrecedenceOverVendorProfile(t *testing.T) {
	t.Parallel()
	identity := domain.DeviceIdentity{
		Manufacturer: "Fixture Vendor",
		Model:        "Fixture Model",
		Firmware:     "fixture-fw-mm-volte",
	}
	profile := volte.Profile{
		ID:               "vendor-profile-must-not-run",
		Matches:          matchDeviceIdentity(identity),
		OperationTimeout: time.Second,
		Read: volte.ATRead("AT+VENDORVOLTE?", func(string) (volte.State, error) {
			return volte.State{Policy: volte.PolicyEnabled}, nil
		}),
	}
	registry, err := volte.NewRegistry(profile)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	configuration := baseConfiguration(t, identity)
	configuration.Capabilities.VoLTE = domain.FeatureCapability{
		Backend:     "modemmanager",
		Supported:   true,
		Implemented: true,
		Readable:    true,
		Writable:    true,
	}
	configuration.VoLTE = domain.VoLTEConfiguration{
		PolicyKnown: true,
		Policy:      string(volte.PolicyEnabled),
	}
	generic := &fakeGenericProvider{configuration: configuration}
	resolverCalls := 0
	service, err := New(
		generic,
		registry,
		func(context.Context, string, volte.Identity) (volte.Transports, error) {
			resolverCalls++
			return volte.Transports{AT: &fakeATTransport{}}, nil
		},
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	current, err := service.DeviceConfiguration(context.Background(), "line-1")
	if err != nil {
		t.Fatalf("DeviceConfiguration() error = %v", err)
	}
	if resolverCalls != 0 ||
		current.Capabilities.VoLTE.Backend != "modemmanager" ||
		current.VoLTE.Policy != string(volte.PolicyEnabled) {
		t.Fatalf("ModemManager VoLTE report was not authoritative: %+v", current)
	}

	updated, err := service.ApplyDeviceConfiguration(
		context.Background(),
		domain.ApplyDeviceConfigurationRequest{
			RequestID:        "mm-volte-policy",
			LineID:           "line-1",
			ExpectedRevision: current.Revision,
			Operation:        domain.DeviceConfigurationSetVoLTEPolicy,
			VoLTEPolicy:      string(volte.PolicyDisabled),
		},
	)
	if err != nil {
		t.Fatalf("ApplyDeviceConfiguration() error = %v", err)
	}
	if resolverCalls != 0 ||
		generic.applyCalls != 1 ||
		updated.VoLTE.Policy != string(volte.PolicyDisabled) {
		t.Fatalf(
			"ModemManager VoLTE apply was not authoritative: resolver=%d applies=%d state=%+v",
			resolverCalls,
			generic.applyCalls,
			updated,
		)
	}
}

func TestEmptyRegistryLeavesQDC507AndEG25Unsupported(t *testing.T) {
	t.Parallel()
	registry, err := volte.NewRegistry()
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	for _, identity := range []domain.DeviceIdentity{
		{Manufacturer: "Baiwang", Model: "QDC507", Firmware: "QDC507GLEFM21"},
		{Manufacturer: "Quectel", Model: "EG25-G", Firmware: "EG25GGBR07A08M2G"},
	} {
		provider := &fakeGenericProvider{
			configuration: baseConfiguration(t, identity),
		}
		service, err := New(provider, registry, nil)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		configuration, err := service.DeviceConfiguration(context.Background(), "line-1")
		if err != nil {
			t.Fatalf("DeviceConfiguration() error = %v", err)
		}
		if configuration.Capabilities.VoLTE.Supported ||
			configuration.Capabilities.VoLTE.Readable ||
			configuration.Capabilities.VoLTE.Writable ||
			configuration.VoLTE.PolicyKnown {
			t.Fatalf("identity %+v gained an unregistered VoLTE profile: %+v", identity, configuration)
		}
	}
}

func TestMatchedVoLTEProfileReadsAppliesAndVerifies(t *testing.T) {
	t.Parallel()
	identity := domain.DeviceIdentity{
		Manufacturer: "Fixture Vendor",
		Model:        "Fixture Model",
		Firmware:     "fixture-fw-1",
	}
	profile := volte.Profile{
		ID:               "fixture-volte-v1",
		Matches:          matchDeviceIdentity(identity),
		OperationTimeout: time.Second,
		Read: volte.ATRead("AT+TESTVOLTE?", func(response string) (volte.State, error) {
			return volte.State{Policy: volte.Policy(response)}, nil
		}),
		Write: volte.ATWrite(func(policy volte.Policy) (string, error) {
			if policy == volte.PolicyEnabled {
				return "AT+TESTVOLTE=1", nil
			}
			return "AT+TESTVOLTE=0", nil
		}, func(response string) error {
			if response != "OK" {
				return fmt.Errorf("response is not OK")
			}
			return nil
		}),
		ApplyRequiresRestart: true,
	}
	registry, err := volte.NewRegistry(profile)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	generic := &fakeGenericProvider{configuration: baseConfiguration(t, identity)}
	at := &fakeATTransport{policy: volte.PolicyDisabled}
	service, err := New(
		generic,
		registry,
		func(context.Context, string, volte.Identity) (volte.Transports, error) {
			return volte.Transports{AT: at}, nil
		},
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	current, err := service.DeviceConfiguration(context.Background(), "line-1")
	if err != nil {
		t.Fatalf("DeviceConfiguration() error = %v", err)
	}
	if !current.Capabilities.VoLTE.Supported ||
		!current.Capabilities.VoLTE.Readable ||
		!current.Capabilities.VoLTE.Writable ||
		current.VoLTE.Policy != "disabled" ||
		current.VoLTE.ProfileID != profile.ID {
		t.Fatalf("unexpected exact-profile configuration: %+v", current)
	}
	updated, err := service.ApplyDeviceConfiguration(
		context.Background(),
		domain.ApplyDeviceConfigurationRequest{
			RequestID:        "volte-request-1",
			LineID:           "line-1",
			ExpectedRevision: current.Revision,
			Operation:        domain.DeviceConfigurationSetVoLTEPolicy,
			VoLTEPolicy:      "enabled",
		},
	)
	if err != nil {
		t.Fatalf("ApplyDeviceConfiguration() error = %v", err)
	}
	if updated.VoLTE.Policy != "enabled" ||
		!updated.VoLTE.RestartRequired ||
		generic.applyCalls != 0 {
		t.Fatalf("updated configuration = %+v, generic apply calls = %d", updated, generic.applyCalls)
	}
	wantCommands := []string{
		"AT+TESTVOLTE?",
		"AT+TESTVOLTE?",
		"AT+TESTVOLTE=1",
		"AT+TESTVOLTE?",
		"AT+TESTVOLTE?",
	}
	if fmt.Sprint(at.commands) != fmt.Sprint(wantCommands) {
		t.Fatalf("AT commands = %v, want %v", at.commands, wantCommands)
	}
}

func TestVoLTERestartRequirementPersistsUntilUserRestartsModem(t *testing.T) {
	t.Parallel()
	service, generic, _ := newRestartingVoLTEService(t, volte.PolicyDisabled)

	current, err := service.DeviceConfiguration(context.Background(), "line-1")
	if err != nil {
		t.Fatalf("DeviceConfiguration() error = %v", err)
	}
	updated, err := service.ApplyDeviceConfiguration(
		context.Background(),
		domain.ApplyDeviceConfigurationRequest{
			RequestID:        "enable-volte-before-restart",
			LineID:           "line-1",
			ExpectedRevision: current.Revision,
			Operation:        domain.DeviceConfigurationSetVoLTEPolicy,
			VoLTEPolicy:      string(volte.PolicyEnabled),
		},
	)
	if err != nil {
		t.Fatalf("ApplyDeviceConfiguration(enable VoLTE) error = %v", err)
	}
	if !updated.VoLTE.RestartRequired {
		t.Fatalf("updated VoLTE = %+v, want restart required", updated.VoLTE)
	}

	pending, err := service.DeviceConfiguration(context.Background(), "line-1")
	if err != nil {
		t.Fatalf("DeviceConfiguration(after VoLTE write) error = %v", err)
	}
	if !pending.VoLTE.RestartRequired {
		t.Fatalf("VoLTE after GET = %+v, want restart required", pending.VoLTE)
	}

	restarted, err := service.ApplyDeviceConfiguration(
		context.Background(),
		domain.ApplyDeviceConfigurationRequest{
			RequestID:        "restart-modem-after-volte",
			LineID:           "line-1",
			ExpectedRevision: pending.Revision,
			Operation:        domain.DeviceConfigurationRestartModem,
		},
	)
	if err != nil {
		t.Fatalf("ApplyDeviceConfiguration(restart modem) error = %v", err)
	}
	if restarted.VoLTE.RestartRequired {
		t.Fatalf("restarted VoLTE = %+v, want pending restart cleared", restarted.VoLTE)
	}
	if generic.applyCalls != 1 ||
		len(generic.applyRequests) != 1 ||
		generic.applyRequests[0].Operation != domain.DeviceConfigurationRestartModem {
		t.Fatalf(
			"generic apply calls = %d, requests = %+v",
			generic.applyCalls,
			generic.applyRequests,
		)
	}

	afterRestart, err := service.DeviceConfiguration(context.Background(), "line-1")
	if err != nil {
		t.Fatalf("DeviceConfiguration(after restart) error = %v", err)
	}
	if afterRestart.VoLTE.RestartRequired {
		t.Fatalf("VoLTE after restart GET = %+v, want pending restart cleared", afterRestart.VoLTE)
	}
}

func TestUSBResetClearsPendingRestartThroughGenericProvider(t *testing.T) {
	t.Parallel()
	service, generic, _ := newRestartingVoLTEService(t, volte.PolicyDisabled)

	current, err := service.DeviceConfiguration(context.Background(), "line-1")
	if err != nil {
		t.Fatalf("DeviceConfiguration() error = %v", err)
	}
	pending, err := service.ApplyDeviceConfiguration(
		context.Background(),
		domain.ApplyDeviceConfigurationRequest{
			RequestID:        "enable-volte-before-usb-reset",
			LineID:           "line-1",
			ExpectedRevision: current.Revision,
			Operation:        domain.DeviceConfigurationSetVoLTEPolicy,
			VoLTEPolicy:      string(volte.PolicyEnabled),
		},
	)
	if err != nil {
		t.Fatalf("ApplyDeviceConfiguration(enable VoLTE) error = %v", err)
	}
	if !pending.VoLTE.RestartRequired {
		t.Fatalf("VoLTE = %+v, want restart required", pending.VoLTE)
	}

	reset, err := service.ApplyDeviceConfiguration(
		context.Background(),
		domain.ApplyDeviceConfigurationRequest{
			RequestID:        "usb-reset-after-volte",
			LineID:           "line-1",
			ExpectedRevision: pending.Revision,
			Operation:        domain.DeviceConfigurationResetUSB,
		},
	)
	if err != nil {
		t.Fatalf("ApplyDeviceConfiguration(reset USB) error = %v", err)
	}
	if reset.VoLTE.RestartRequired {
		t.Fatalf("VoLTE after reset = %+v, want pending restart cleared", reset.VoLTE)
	}
	if generic.applyCalls != 1 ||
		len(generic.applyRequests) != 1 ||
		generic.applyRequests[0].Operation != domain.DeviceConfigurationResetUSB {
		t.Fatalf(
			"generic apply calls = %d, requests = %+v",
			generic.applyCalls,
			generic.applyRequests,
		)
	}
}

func TestApplyingCurrentVoLTEPolicyDoesNotWriteOrCreatePendingRestart(t *testing.T) {
	t.Parallel()
	service, generic, at := newRestartingVoLTEService(t, volte.PolicyEnabled)

	current, err := service.DeviceConfiguration(context.Background(), "line-1")
	if err != nil {
		t.Fatalf("DeviceConfiguration() error = %v", err)
	}
	at.commands = nil

	unchanged, err := service.ApplyDeviceConfiguration(
		context.Background(),
		domain.ApplyDeviceConfigurationRequest{
			RequestID:        "keep-volte-enabled",
			LineID:           "line-1",
			ExpectedRevision: current.Revision,
			Operation:        domain.DeviceConfigurationSetVoLTEPolicy,
			VoLTEPolicy:      string(volte.PolicyEnabled),
		},
	)
	if err != nil {
		t.Fatalf("ApplyDeviceConfiguration(unchanged VoLTE) error = %v", err)
	}
	if unchanged.VoLTE.RestartRequired {
		t.Fatalf("unchanged VoLTE = %+v, want no pending restart", unchanged.VoLTE)
	}
	if generic.applyCalls != 0 {
		t.Fatalf("generic apply calls = %d, want 0", generic.applyCalls)
	}
	for _, command := range at.commands {
		if command == "AT+TESTVOLTE=0" || command == "AT+TESTVOLTE=1" {
			t.Fatalf("AT commands = %v, unchanged policy must not be written", at.commands)
		}
	}

	afterApply, err := service.DeviceConfiguration(context.Background(), "line-1")
	if err != nil {
		t.Fatalf("DeviceConfiguration(after unchanged policy) error = %v", err)
	}
	if afterApply.VoLTE.RestartRequired {
		t.Fatalf("VoLTE after unchanged policy GET = %+v, want no pending restart", afterApply.VoLTE)
	}
}

func TestMatchedVoLTEProfilePreservesVendorCapabilityState(t *testing.T) {
	t.Parallel()
	identity := domain.DeviceIdentity{
		Manufacturer: "Fixture Vendor",
		Model:        "Fixture Model",
		Firmware:     "fixture-fw-capability",
	}
	profile := volte.Profile{
		ID:               "fixture-volte-capability",
		Matches:          matchDeviceIdentity(identity),
		OperationTimeout: time.Second,
		Read: volte.ATRead("AT+TESTVOLTE?", func(string) (volte.State, error) {
			return volte.State{
				Policy:                 volte.PolicyEnabled,
				ConfigurationMode:      volte.ConfigurationModeForcedEnabled,
				ModemCapabilityKnown:   true,
				ModemCapabilityEnabled: false,
			}, nil
		}),
	}
	registry, err := volte.NewRegistry(profile)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	service, err := New(
		&fakeGenericProvider{configuration: baseConfiguration(t, identity)},
		registry,
		func(context.Context, string, volte.Identity) (volte.Transports, error) {
			return volte.Transports{AT: &fakeATTransport{}}, nil
		},
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	configuration, err := service.DeviceConfiguration(context.Background(), "line-1")
	if err != nil {
		t.Fatalf("DeviceConfiguration() error = %v", err)
	}
	if configuration.VoLTE.Policy != "enabled" ||
		configuration.VoLTE.ConfigurationMode != "forced_enabled" ||
		!configuration.VoLTE.ModemCapabilityKnown ||
		configuration.VoLTE.ModemCapabilityEnabled {
		t.Fatalf("VoLTE configuration = %+v", configuration.VoLTE)
	}
}

func TestNearMatchDoesNotUseRegisteredVoLTEProfile(t *testing.T) {
	t.Parallel()
	profile := volte.Profile{
		ID: "exact-only",
		Matches: matchDeviceIdentity(domain.DeviceIdentity{
			Manufacturer: "Vendor",
			Model:        "Model",
			Firmware:     "fw-1",
		}),
		OperationTimeout: time.Second,
		Read: volte.ATRead("AT+TESTVOLTE?", func(string) (volte.State, error) {
			return volte.State{Policy: volte.PolicyEnabled}, nil
		}),
	}
	registry, err := volte.NewRegistry(profile)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	resolverCalls := 0
	service, err := New(
		&fakeGenericProvider{configuration: baseConfiguration(t, domain.DeviceIdentity{
			Manufacturer: "Vendor",
			Model:        "Model",
			Firmware:     "fw-2",
		})},
		registry,
		func(context.Context, string, volte.Identity) (volte.Transports, error) {
			resolverCalls++
			return volte.Transports{}, nil
		},
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	configuration, err := service.DeviceConfiguration(context.Background(), "line-1")
	if err != nil {
		t.Fatalf("DeviceConfiguration() error = %v", err)
	}
	if configuration.Capabilities.VoLTE.Supported || resolverCalls != 0 {
		t.Fatalf("near-match configuration = %+v, resolver calls = %d", configuration, resolverCalls)
	}
}

func newRestartingVoLTEService(
	t *testing.T,
	initialPolicy volte.Policy,
) (*Service, *fakeGenericProvider, *fakeATTransport) {
	t.Helper()
	identity := domain.DeviceIdentity{
		Manufacturer: "Restart Fixture Vendor",
		Model:        "Restart Fixture Model",
		Firmware:     "restart-fixture-fw-1",
	}
	profile := volte.Profile{
		ID:               "restart-fixture-volte",
		Matches:          matchDeviceIdentity(identity),
		OperationTimeout: time.Second,
		Read: volte.ATRead("AT+TESTVOLTE?", func(response string) (volte.State, error) {
			return volte.State{Policy: volte.Policy(response)}, nil
		}),
		Write: volte.ATWrite(func(policy volte.Policy) (string, error) {
			if policy == volte.PolicyEnabled {
				return "AT+TESTVOLTE=1", nil
			}
			return "AT+TESTVOLTE=0", nil
		}, func(response string) error {
			if response != "OK" {
				return fmt.Errorf("response is not OK")
			}
			return nil
		}),
		ApplyRequiresRestart: true,
	}
	registry, err := volte.NewRegistry(profile)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	generic := &fakeGenericProvider{configuration: baseConfiguration(t, identity)}
	at := &fakeATTransport{policy: initialPolicy}
	service, err := New(
		generic,
		registry,
		func(context.Context, string, volte.Identity) (volte.Transports, error) {
			return volte.Transports{AT: at}, nil
		},
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return service, generic, at
}

func matchDeviceIdentity(identity domain.DeviceIdentity) volte.IdentityMatcher {
	expected := volte.Identity{
		Manufacturer: identity.Manufacturer,
		Model:        identity.Model,
		Firmware:     identity.Firmware,
	}
	return func(candidate volte.Identity) bool {
		return candidate == expected
	}
}

func baseConfiguration(
	t *testing.T,
	identity domain.DeviceIdentity,
) domain.DeviceConfiguration {
	t.Helper()
	configuration := domain.DeviceConfiguration{
		LineID:          "line-1",
		ObservedAt:      time.Date(2026, time.July, 23, 0, 0, 0, 0, time.UTC),
		Identity:        identity,
		Radio:           domain.RadioConfiguration{Enabled: true, EnabledKnown: true},
		FlightModeKnown: true,
		DataConnections: []domain.DataConnection{},
		Capabilities: domain.DeviceConfigurationCapabilities{
			VoLTE: domain.FeatureCapability{Backend: "vendor_extension"},
		},
	}
	revision, err := domain.RevisionDeviceConfiguration(configuration)
	if err != nil {
		t.Fatalf("RevisionDeviceConfiguration() error = %v", err)
	}
	configuration.Revision = revision
	return configuration
}
