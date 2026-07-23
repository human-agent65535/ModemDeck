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
}

func (provider *fakeGenericProvider) ReadDeviceConfiguration(
	context.Context,
	string,
) (domain.DeviceConfiguration, error) {
	return provider.configuration, nil
}

func (provider *fakeGenericProvider) ApplyGenericDeviceConfiguration(
	_ context.Context,
	_ domain.ApplyDeviceConfigurationRequest,
) (domain.DeviceConfiguration, error) {
	provider.applyCalls++
	return provider.configuration, nil
}

type fakeATTransport struct {
	policy   volte.Policy
	commands []string
}

func (transport *fakeATTransport) Command(_ context.Context, command string) (string, error) {
	transport.commands = append(transport.commands, command)
	switch command {
	case "AT+TESTVOLTE?":
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

func TestUnknownQDC507AndEG25IdentitiesRemainUnsupported(t *testing.T) {
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

func TestExactVoLTEProfileReadsAppliesAndVerifies(t *testing.T) {
	t.Parallel()
	identity := domain.DeviceIdentity{
		Manufacturer: "Fixture Vendor",
		Model:        "Fixture Model",
		Firmware:     "fixture-fw-1",
	}
	profile := volte.Profile{
		ID: "fixture-volte-v1",
		Identity: volte.Identity{
			Manufacturer: identity.Manufacturer,
			Model:        identity.Model,
			Firmware:     identity.Firmware,
		},
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
	if updated.VoLTE.Policy != "enabled" || generic.applyCalls != 0 {
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

func TestNearMatchDoesNotUseRegisteredVoLTEProfile(t *testing.T) {
	t.Parallel()
	profile := volte.Profile{
		ID: "exact-only",
		Identity: volte.Identity{
			Manufacturer: "Vendor",
			Model:        "Model",
			Firmware:     "fw-1",
		},
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
