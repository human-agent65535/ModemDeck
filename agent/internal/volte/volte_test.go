package volte

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"
)

var testIdentity = Identity{
	Manufacturer: "Test Vendor",
	Model:        "Test Modem",
	Firmware:     "TEST_01.001",
}

func TestUnknownQDC507AndEG25ProfilesAreUnsupported(t *testing.T) {
	registry, err := NewRegistry()
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	identities := []Identity{
		{Manufacturer: "Baiwang", Model: "QDC507", Firmware: "QDC507GLEFM21"},
		{Manufacturer: "Quectel", Model: "EG25-G", Firmware: "EG25GGBR07A08M2G"},
	}
	for _, identity := range identities {
		t.Run(identity.Model, func(t *testing.T) {
			driver := registry.Resolve(identity, Transports{})
			if capability := driver.Capability(); capability.Supported {
				t.Fatalf("unknown identity unexpectedly supported: %+v", capability)
			}
			_, err := driver.Read(context.Background())
			assertErrorCode(t, err, ErrorUnsupported)
		})
	}
}

func TestQDC507GLEFM21ProfileUsesVerifiedQCFGCommands(t *testing.T) {
	t.Parallel()
	policy := PolicyDisabled
	at := &fakeAT{
		command: func(_ context.Context, command string) (string, error) {
			switch command {
			case `AT+QCFG="ims"`:
				if policy == PolicyEnabled {
					return "+QCFG: \"ims\",1,0\r\nOK\r\n", nil
				}
				return "+QCFG: \"ims\",2,1\r\nOK\r\n", nil
			case `AT+QCFG="ims",1`:
				policy = PolicyEnabled
				// ModemManager removes the final OK and returns an empty
				// payload for commands without response data.
				return "", nil
			default:
				return "", fmt.Errorf("unexpected command %q", command)
			}
		},
	}
	registry := mustRegistry(t, QDC507GLEFM21Profile())
	driver := registry.Resolve(QDC507GLEFM21Identity, Transports{AT: at})

	state, err := driver.Apply(context.Background(), PolicyEnabled)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if state.Policy != PolicyEnabled {
		t.Fatalf("state = %+v", state)
	}
	if state.ConfigurationMode != ConfigurationModeForcedEnabled ||
		state.ModemCapabilityEnabled ||
		!state.RestartRequired {
		t.Fatalf("state did not preserve QCFG mode and capability: %+v", state)
	}
	want := []string{`AT+QCFG="ims",1`, `AT+QCFG="ims"`}
	if !slices.Equal(at.commands, want) {
		t.Fatalf("commands = %#v, want %#v", at.commands, want)
	}
}

func TestDecodeQuectelIMSPreservesConfigurationAndCapability(t *testing.T) {
	t.Parallel()
	tests := []struct {
		response       string
		wantPolicy     Policy
		wantMode       ConfigurationMode
		wantCapability bool
	}{
		{
			response:       "+QCFG: \"ims\",0,1\r\nOK\r\n",
			wantPolicy:     PolicyEnabled,
			wantMode:       ConfigurationModeAutomatic,
			wantCapability: true,
		},
		{
			response:       "+QCFG: \"ims\",0,0\r\nOK\r\n",
			wantPolicy:     PolicyDisabled,
			wantMode:       ConfigurationModeAutomatic,
			wantCapability: false,
		},
		{
			response:       "+QCFG: \"ims\",1,0\r\nOK\r\n",
			wantPolicy:     PolicyEnabled,
			wantMode:       ConfigurationModeForcedEnabled,
			wantCapability: false,
		},
		{
			response:       "+QCFG: \"ims\",2,1\r\nOK\r\n",
			wantPolicy:     PolicyDisabled,
			wantMode:       ConfigurationModeForcedDisabled,
			wantCapability: true,
		},
	}
	for _, test := range tests {
		state, err := decodeQuectelIMS(test.response)
		if err != nil {
			t.Fatalf("decodeQuectelIMS(%q) error = %v", test.response, err)
		}
		if state.Policy != test.wantPolicy ||
			state.ConfigurationMode != test.wantMode ||
			!state.ModemCapabilityKnown ||
			state.ModemCapabilityEnabled != test.wantCapability ||
			state.RestartRequired {
			t.Fatalf("decodeQuectelIMS(%q) = %+v", test.response, state)
		}
	}
}

func TestProfileIdentityMatchIsExact(t *testing.T) {
	registry := mustRegistry(t, readOnlyProfile(testIdentity))
	cases := []Identity{
		{Manufacturer: "test vendor", Model: testIdentity.Model, Firmware: testIdentity.Firmware},
		{Manufacturer: testIdentity.Manufacturer, Model: "Test Modem ", Firmware: testIdentity.Firmware},
		{Manufacturer: testIdentity.Manufacturer, Model: testIdentity.Model, Firmware: "TEST_01.002"},
	}
	for _, identity := range cases {
		if capability := registry.Resolve(identity, Transports{}).Capability(); capability.Supported {
			t.Fatalf("inexact identity unexpectedly matched: %+v", identity)
		}
	}
}

func TestReadUsesOnlyDeclaredQMIRequest(t *testing.T) {
	profile := Profile{
		ID:               "test-qmi-read",
		Identity:         testIdentity,
		OperationTimeout: time.Second,
		Read: QMIRead("dms", "get-volte-policy", []byte{0x01}, func(response []byte) (State, error) {
			if !slices.Equal(response, []byte{0x01}) {
				return State{}, fmt.Errorf("unexpected response %x", response)
			}
			return State{Policy: PolicyEnabled}, nil
		}),
	}
	qmi := &fakeQMI{
		request: func(_ context.Context, request QMIRequest) ([]byte, error) {
			if request.Service != "dms" ||
				request.Method != "get-volte-policy" ||
				!slices.Equal(request.Payload, []byte{0x01}) {
				return nil, fmt.Errorf("unexpected request: %+v", request)
			}
			return []byte{0x01}, nil
		},
	}
	at := &fakeAT{
		command: func(context.Context, string) (string, error) {
			return "", errors.New("AT must not be attempted")
		},
	}

	driver := mustRegistry(t, profile).Resolve(testIdentity, Transports{AT: at, QMI: qmi})
	state, err := driver.Read(context.Background())
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if state.Policy != PolicyEnabled {
		t.Fatalf("unexpected state: %+v", state)
	}
	if qmi.calls != 1 || at.calls != 0 {
		t.Fatalf("unexpected transport calls: qmi=%d at=%d", qmi.calls, at.calls)
	}
}

func TestApplyUsesDeclaredATWriteAndReadBack(t *testing.T) {
	var policy Policy
	at := &fakeAT{
		command: func(_ context.Context, command string) (string, error) {
			switch command {
			case "AT+TESTVOLTE=1":
				policy = PolicyEnabled
				return "OK", nil
			case "AT+TESTVOLTE?":
				if policy == PolicyEnabled {
					return "+TESTVOLTE: 1", nil
				}
				return "+TESTVOLTE: 0", nil
			default:
				return "", fmt.Errorf("unexpected command %q", command)
			}
		},
	}
	driver := mustRegistry(t, writableATProfile(testIdentity)).Resolve(
		testIdentity,
		Transports{AT: at},
	)

	state, err := driver.Apply(context.Background(), PolicyEnabled)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if state.Policy != PolicyEnabled {
		t.Fatalf("unexpected verified state: %+v", state)
	}
	if !slices.Equal(at.commands, []string{"AT+TESTVOLTE=1", "AT+TESTVOLTE?"}) {
		t.Fatalf("unexpected command sequence: %#v", at.commands)
	}
}

func TestApplyUsesDeclaredQMIWriteAndReadBack(t *testing.T) {
	var policy Policy
	qmi := &fakeQMI{
		request: func(_ context.Context, request QMIRequest) ([]byte, error) {
			if request.Service != "nas" {
				return nil, fmt.Errorf("unexpected service %q", request.Service)
			}
			switch request.Method {
			case "set-volte-policy":
				if !slices.Equal(request.Payload, []byte{0x01}) {
					return nil, fmt.Errorf("unexpected write payload %x", request.Payload)
				}
				policy = PolicyEnabled
				return []byte{0x00}, nil
			case "get-volte-policy":
				if policy == PolicyEnabled {
					return []byte{0x01}, nil
				}
				return []byte{0x00}, nil
			default:
				return nil, fmt.Errorf("unexpected method %q", request.Method)
			}
		},
	}
	profile := Profile{
		ID:               "test-qmi",
		Identity:         testIdentity,
		OperationTimeout: time.Second,
		Read: QMIRead("nas", "get-volte-policy", nil, func(response []byte) (State, error) {
			switch {
			case slices.Equal(response, []byte{0x00}):
				return State{Policy: PolicyDisabled}, nil
			case slices.Equal(response, []byte{0x01}):
				return State{Policy: PolicyEnabled}, nil
			default:
				return State{}, fmt.Errorf("unexpected response %x", response)
			}
		}),
		Write: QMIWrite("nas", "set-volte-policy", func(policy Policy) ([]byte, error) {
			switch policy {
			case PolicyDisabled:
				return []byte{0x00}, nil
			case PolicyEnabled:
				return []byte{0x01}, nil
			default:
				return nil, fmt.Errorf("unexpected policy %q", policy)
			}
		}, func(response []byte) error {
			if !slices.Equal(response, []byte{0x00}) {
				return fmt.Errorf("unexpected response %x", response)
			}
			return nil
		}),
	}

	state, err := mustRegistry(t, profile).
		Resolve(testIdentity, Transports{QMI: qmi}).
		Apply(context.Background(), PolicyEnabled)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if state.Policy != PolicyEnabled {
		t.Fatalf("unexpected verified state: %+v", state)
	}
	if qmi.calls != 2 ||
		qmi.requests[0].Method != "set-volte-policy" ||
		qmi.requests[1].Method != "get-volte-policy" {
		t.Fatalf("unexpected QMI request sequence: %+v", qmi.requests)
	}
}

func TestApplyReturnsVerificationErrorOnReadBackMismatch(t *testing.T) {
	at := &fakeAT{
		command: func(_ context.Context, command string) (string, error) {
			switch command {
			case "AT+TESTVOLTE=1":
				return "OK", nil
			case "AT+TESTVOLTE?":
				return "+TESTVOLTE: 0", nil
			default:
				return "", fmt.Errorf("unexpected command %q", command)
			}
		},
	}
	driver := mustRegistry(t, writableATProfile(testIdentity)).Resolve(
		testIdentity,
		Transports{AT: at},
	)

	_, err := driver.Apply(context.Background(), PolicyEnabled)
	assertErrorCode(t, err, ErrorVerification)
	if !slices.Equal(at.commands, []string{"AT+TESTVOLTE=1", "AT+TESTVOLTE?"}) {
		t.Fatalf("unexpected command sequence: %#v", at.commands)
	}
}

func TestApplyReturnsIndeterminateVerificationWhenReadBackFails(t *testing.T) {
	t.Parallel()
	writeCompleted := false
	at := &fakeAT{
		command: func(_ context.Context, command string) (string, error) {
			switch command {
			case "AT+TESTVOLTE=1":
				writeCompleted = true
				return "OK", nil
			case "AT+TESTVOLTE?":
				return "", errors.New("read-back transport failed")
			default:
				return "", fmt.Errorf("unexpected command %q", command)
			}
		},
	}
	driver := mustRegistry(t, writableATProfile(testIdentity)).Resolve(
		testIdentity,
		Transports{AT: at},
	)

	_, err := driver.Apply(context.Background(), PolicyEnabled)
	assertErrorCode(t, err, ErrorVerification)
	typed, ok := AsError(err)
	if !ok || !writeCompleted ||
		typed.Message != "write completed but read-back failed; resulting policy is unknown" {
		t.Fatalf("error = %#v, write completed = %v", err, writeCompleted)
	}
}

func TestApplyHasBoundedDeadlineAndReturnsTypedTimeout(t *testing.T) {
	profile := writableATProfile(testIdentity)
	profile.OperationTimeout = 15 * time.Millisecond
	at := &fakeAT{
		command: func(ctx context.Context, _ string) (string, error) {
			<-ctx.Done()
			return "", ctx.Err()
		},
	}
	driver := mustRegistry(t, profile).Resolve(testIdentity, Transports{AT: at})

	started := time.Now()
	_, err := driver.Apply(context.Background(), PolicyEnabled)
	assertErrorCode(t, err, ErrorTimeout)
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("write was not bounded: %s", elapsed)
	}
	if at.calls != 1 {
		t.Fatalf("timeout triggered an unexpected fallback or read-back: %d calls", at.calls)
	}
}

func TestRegistrationRejectsWildcardAndWriteWithoutRead(t *testing.T) {
	wildcard := readOnlyProfile(testIdentity)
	wildcard.Identity.Firmware = "TEST_*"
	_, err := NewRegistry(wildcard)
	assertErrorCode(t, err, ErrorInvalidProfile)

	writeOnly := writableATProfile(testIdentity)
	writeOnly.Read = nil
	_, err = NewRegistry(writeOnly)
	assertErrorCode(t, err, ErrorInvalidProfile)
}

func readOnlyProfile(identity Identity) Profile {
	return Profile{
		ID:               "test-read-only",
		Identity:         identity,
		OperationTimeout: time.Second,
		Read: ATRead("AT+TESTVOLTE?", func(string) (State, error) {
			return State{Policy: PolicyDisabled}, nil
		}),
	}
}

func writableATProfile(identity Identity) Profile {
	return Profile{
		ID:               "test-at",
		Identity:         identity,
		OperationTimeout: time.Second,
		Read: ATRead("AT+TESTVOLTE?", func(response string) (State, error) {
			switch response {
			case "+TESTVOLTE: 0":
				return State{Policy: PolicyDisabled}, nil
			case "+TESTVOLTE: 1":
				return State{Policy: PolicyEnabled}, nil
			default:
				return State{}, fmt.Errorf("unexpected response %q", response)
			}
		}),
		Write: ATWrite(func(policy Policy) (string, error) {
			switch policy {
			case PolicyDisabled:
				return "AT+TESTVOLTE=0", nil
			case PolicyEnabled:
				return "AT+TESTVOLTE=1", nil
			default:
				return "", fmt.Errorf("unexpected policy %q", policy)
			}
		}, func(response string) error {
			if response != "OK" {
				return fmt.Errorf("unexpected response %q", response)
			}
			return nil
		}),
	}
}

func mustRegistry(t *testing.T, profiles ...Profile) *Registry {
	t.Helper()
	registry, err := NewRegistry(profiles...)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	return registry
}

func assertErrorCode(t *testing.T, err error, want ErrorCode) {
	t.Helper()
	typed, ok := AsError(err)
	if !ok {
		t.Fatalf("expected typed error %q, got %T: %v", want, err, err)
	}
	if typed.Code != want {
		t.Fatalf("unexpected error code: got %q want %q (%v)", typed.Code, want, err)
	}
}

type fakeAT struct {
	command  func(context.Context, string) (string, error)
	commands []string
	calls    int
}

func (f *fakeAT) Command(ctx context.Context, command string) (string, error) {
	f.calls++
	f.commands = append(f.commands, command)
	return f.command(ctx, command)
}

type fakeQMI struct {
	request  func(context.Context, QMIRequest) ([]byte, error)
	requests []QMIRequest
	calls    int
}

func (f *fakeQMI) Request(ctx context.Context, request QMIRequest) ([]byte, error) {
	f.calls++
	f.requests = append(f.requests, cloneQMIRequest(request))
	return f.request(ctx, request)
}
