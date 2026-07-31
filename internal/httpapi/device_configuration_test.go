package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/human-agent65535/modemdeck/internal/agentclient"
	"github.com/human-agent65535/modemdeck/internal/auth"
	"github.com/human-agent65535/modemdeck/internal/communication"
	"github.com/human-agent65535/modemdeck/internal/store"
)

type fakeDeviceConfigurations struct {
	configuration agentclient.DeviceConfiguration
	lineID        string
	applyRequest  agentclient.ApplyDeviceConfigurationRequest
	err           error
}

func (service *fakeDeviceConfigurations) DeviceConfiguration(
	_ context.Context,
	lineID string,
) (agentclient.DeviceConfiguration, error) {
	service.lineID = lineID
	return service.configuration, service.err
}

func (service *fakeDeviceConfigurations) ApplyDeviceConfiguration(
	_ context.Context,
	lineID string,
	request agentclient.ApplyDeviceConfigurationRequest,
) (agentclient.DeviceConfiguration, error) {
	service.lineID = lineID
	service.applyRequest = request
	return service.configuration, service.err
}

type fakeCallPolicies struct {
	global               store.GlobalCallSettings
	line                 store.LineCallPolicy
	effective            store.EffectiveCallPolicy
	latest               *store.IncomingCallAction
	globalReceiveUpdate  bool
	globalRevisionUpdate int64
	linePolicyUpdate     store.LineCallPolicyValue
	lineRevisionUpdate   int64
}

type fakeMessagePolicies struct {
	policy         store.MessageDeliveryPolicy
	enabledUpdate  bool
	revisionUpdate int64
}

func (service *fakeMessagePolicies) MessageDeliveryPolicy(
	context.Context,
	string,
) (store.MessageDeliveryPolicy, error) {
	return service.policy, nil
}

func (service *fakeMessagePolicies) UpdateMessageDeliveryPolicy(
	_ context.Context,
	_ string,
	enabled bool,
	revision int64,
) (store.MessageDeliveryPolicy, error) {
	service.enabledUpdate = enabled
	service.revisionUpdate = revision
	service.policy.DeliveryReportsEnabled = enabled
	if enabled {
		service.policy.DeliveryReportsSupport = store.MessageDeliveryReportSupportUnknown
	}
	service.policy.Revision++
	return service.policy, nil
}

func (service *fakeCallPolicies) GlobalCallSettings(
	context.Context,
) (store.GlobalCallSettings, error) {
	return service.global, nil
}

func (service *fakeCallPolicies) UpdateGlobalCallSettings(
	_ context.Context,
	receiveCalls bool,
	revision int64,
) (store.GlobalCallSettings, error) {
	service.globalReceiveUpdate = receiveCalls
	service.globalRevisionUpdate = revision
	service.global.ReceiveCalls = receiveCalls
	service.global.Revision++
	return service.global, nil
}

func (service *fakeCallPolicies) LineCallPolicy(
	context.Context,
	string,
) (store.LineCallPolicy, error) {
	return service.line, nil
}

func (service *fakeCallPolicies) UpdateLineCallPolicy(
	_ context.Context,
	_ string,
	policy store.LineCallPolicyValue,
	revision int64,
) (store.LineCallPolicy, error) {
	service.linePolicyUpdate = policy
	service.lineRevisionUpdate = revision
	service.line.Policy = policy
	service.line.Revision++
	switch policy {
	case store.LineCallPolicyReceive:
		service.effective.Policy = store.EffectiveCallPolicyReceive
	case store.LineCallPolicyDND:
		service.effective.Policy = store.EffectiveCallPolicyDND
	case store.LineCallPolicyFollowGlobal:
		service.effective.Policy = store.EffectiveCallPolicyReceive
		if !service.global.ReceiveCalls {
			service.effective.Policy = store.EffectiveCallPolicyDND
		}
	}
	return service.line, nil
}

func (service *fakeCallPolicies) EffectiveCallPolicy(
	context.Context,
	string,
) (store.EffectiveCallPolicy, error) {
	return service.effective, nil
}

func (service *fakeCallPolicies) CallPolicyConfiguration(
	context.Context,
	string,
) (store.CallPolicyConfiguration, error) {
	return store.CallPolicyConfiguration{
		Global:    service.global,
		Line:      service.line,
		Effective: service.effective,
	}, nil
}

func (service *fakeCallPolicies) LatestIncomingCallAction(
	context.Context,
	string,
) (*store.IncomingCallAction, error) {
	return service.latest, nil
}

func TestAppDeviceConfigurationReturnsServerComputedPolicyAndVisibleDNDOutcome(t *testing.T) {
	t.Parallel()
	configurations := &fakeDeviceConfigurations{
		configuration: agentclient.DeviceConfiguration{
			LineID:          "line-1",
			Revision:        "sha256:fixture",
			ObservedAt:      time.Date(2026, time.July, 23, 0, 0, 0, 0, time.UTC),
			DataConnections: []agentclient.DataConnection{},
		},
	}
	policies := &fakeCallPolicies{
		global: store.GlobalCallSettings{
			ReceiveCalls: false,
			Revision:     4,
		},
		line: store.LineCallPolicy{
			LineID:   "line-1",
			Policy:   store.LineCallPolicyFollowGlobal,
			Revision: 7,
		},
		effective: store.EffectiveCallPolicy{
			LineID:         "line-1",
			Policy:         store.EffectiveCallPolicyDND,
			GlobalRevision: 4,
			LineRevision:   7,
		},
		latest: &store.IncomingCallAction{
			CallID:          "call-policy-1",
			EffectivePolicy: store.EffectiveCallPolicyDND,
			Status:          store.IncomingCallActionFailed,
			ErrorCode:       "agent_not_supported",
			CreatedAt:       "2026-07-23 00:00:00",
			UpdatedAt:       "2026-07-23 00:00:01",
		},
	}
	communications := &fakeCommunications{status: communicationStatusWithReject("line-1")}
	messagePolicies := &fakeMessagePolicies{policy: store.MessageDeliveryPolicy{
		LineID:                 "line-1",
		DeliveryReportsEnabled: false,
		DeliveryReportsSupport: store.MessageDeliveryReportSupportUnsupported,
		Revision:               3,
	}}
	api, err := New(&fakeRepository{}, Options{
		Communications:        communications,
		DeviceConfigurations:  configurations,
		CallPolicies:          policies,
		MessagePolicies:       messagePolicies,
		disableAuthentication: true,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/devices/line-1/configuration",
		nil,
	)
	response := httptest.NewRecorder()
	api.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var body deviceConfigurationResponse
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Hardware == nil || body.Hardware.Revision != "sha256:fixture" ||
		body.IncomingCalls == nil ||
		body.IncomingCalls.Policy != store.LineCallPolicyFollowGlobal ||
		body.IncomingCalls.EffectivePolicy != store.EffectiveCallPolicyDND ||
		!body.IncomingCalls.Enforcement.Available ||
		body.IncomingCalls.Enforcement.ConfigOnly ||
		body.IncomingCalls.Enforcement.MaxSubmissionsPerCall != 1 ||
		body.IncomingCalls.LastAction == nil ||
		body.IncomingCalls.LastAction.Status != store.IncomingCallActionFailed ||
		body.IncomingCalls.LastAction.ErrorCode != "agent_not_supported" ||
		body.Messaging == nil ||
		body.Messaging.DeliveryReportsEnabled ||
		body.Messaging.DeliveryReportsSupport !=
			store.MessageDeliveryReportSupportUnsupported ||
		body.Messaging.Revision != 3 {
		t.Fatalf("configuration response = %+v", body)
	}
}

func TestAppMessageDeliveryPolicyUsesRevisionedWrite(t *testing.T) {
	t.Parallel()
	configurations := &fakeDeviceConfigurations{configuration: agentclient.DeviceConfiguration{
		LineID:     "line-1",
		Revision:   "sha256:fixture",
		ObservedAt: time.Date(2026, time.July, 29, 0, 0, 0, 0, time.UTC),
	}}
	messagePolicies := &fakeMessagePolicies{policy: store.MessageDeliveryPolicy{
		LineID:                 "line-1",
		DeliveryReportsSupport: store.MessageDeliveryReportSupportUnsupported,
		Revision:               5,
	}}
	api, err := New(&fakeRepository{}, Options{
		DeviceConfigurations:  configurations,
		CallPolicies:          &fakeCallPolicies{},
		MessagePolicies:       messagePolicies,
		disableAuthentication: true,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	request := httptest.NewRequest(
		http.MethodPatch,
		"/api/v1/devices/line-1/configuration",
		bytes.NewBufferString(`{
			"operation":"set_delivery_reports_enabled",
			"expected_message_policy_revision":5,
			"delivery_reports_enabled":true
		}`),
	)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	api.ServeHTTP(response, request)
	if response.Code != http.StatusOK ||
		!messagePolicies.enabledUpdate ||
		messagePolicies.revisionUpdate != 5 ||
		configurations.applyRequest.Operation != "" {
		t.Fatalf(
			"status=%d update=(%t,%d) hardware=%+v body=%s",
			response.Code,
			messagePolicies.enabledUpdate,
			messagePolicies.revisionUpdate,
			configurations.applyRequest,
			response.Body.String(),
		)
	}
	var body deviceConfigurationResponse
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Messaging == nil ||
		!body.Messaging.DeliveryReportsEnabled ||
		body.Messaging.DeliveryReportsSupport !=
			store.MessageDeliveryReportSupportUnknown ||
		body.Messaging.Revision != 6 {
		t.Fatalf("messaging response = %+v", body.Messaging)
	}
}

func TestAppCallSettingsAndLinePolicyUseRevisionedWrites(t *testing.T) {
	t.Parallel()
	configurations := &fakeDeviceConfigurations{configuration: agentclient.DeviceConfiguration{
		LineID:     "line-1",
		Revision:   "sha256:fixture",
		ObservedAt: time.Date(2026, time.July, 23, 0, 0, 0, 0, time.UTC),
	}}
	policies := &fakeCallPolicies{
		global: store.GlobalCallSettings{ReceiveCalls: true, Revision: 2},
		line: store.LineCallPolicy{
			LineID:   "line-1",
			Policy:   store.LineCallPolicyFollowGlobal,
			Revision: 3,
		},
		effective: store.EffectiveCallPolicy{
			LineID:         "line-1",
			Policy:         store.EffectiveCallPolicyReceive,
			GlobalRevision: 2,
			LineRevision:   3,
		},
	}
	api, err := New(&fakeRepository{}, Options{
		Communications:        &fakeCommunications{status: communicationStatusWithReject("line-1")},
		DeviceConfigurations:  configurations,
		CallPolicies:          policies,
		disableAuthentication: true,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	globalRequest := httptest.NewRequest(
		http.MethodPatch,
		"/api/v1/settings/calls",
		bytes.NewBufferString(`{"receive_calls":false,"expected_revision":2}`),
	)
	globalRequest.Header.Set("Content-Type", "application/json")
	globalResponse := httptest.NewRecorder()
	api.ServeHTTP(globalResponse, globalRequest)
	if globalResponse.Code != http.StatusOK ||
		policies.globalReceiveUpdate ||
		policies.globalRevisionUpdate != 2 {
		t.Fatalf(
			"global status=%d update=(%t,%d) body=%s",
			globalResponse.Code,
			policies.globalReceiveUpdate,
			policies.globalRevisionUpdate,
			globalResponse.Body.String(),
		)
	}
	var globalBody map[string]any
	if err := json.NewDecoder(globalResponse.Body).Decode(&globalBody); err != nil {
		t.Fatalf("decode global response: %v", err)
	}
	if len(globalBody) != 2 ||
		globalBody["receive_calls"] != false ||
		globalBody["revision"] != float64(3) {
		t.Fatalf("global response contract = %#v", globalBody)
	}

	lineRequest := httptest.NewRequest(
		http.MethodPatch,
		"/api/v1/devices/line-1/configuration",
		bytes.NewBufferString(`{
			"operation":"set_incoming_call_policy",
			"expected_policy_revision":3,
			"incoming_call_policy":"do_not_disturb"
		}`),
	)
	lineRequest.Header.Set("Content-Type", "application/json")
	lineResponse := httptest.NewRecorder()
	api.ServeHTTP(lineResponse, lineRequest)
	if lineResponse.Code != http.StatusOK ||
		policies.linePolicyUpdate != store.LineCallPolicyDND ||
		policies.lineRevisionUpdate != 3 ||
		configurations.applyRequest.Operation != "" {
		t.Fatalf(
			"line status=%d policy=%q revision=%d hardware=%+v body=%s",
			lineResponse.Code,
			policies.linePolicyUpdate,
			policies.lineRevisionUpdate,
			configurations.applyRequest,
			lineResponse.Body.String(),
		)
	}
}

func TestAppHardwareConfigurationForwardsOpaqueDeviceRevision(t *testing.T) {
	t.Parallel()
	accessTechnologies := uint32(1 << 14)
	snr := 8.5
	configurations := &fakeDeviceConfigurations{configuration: agentclient.DeviceConfiguration{
		LineID:     "line-1",
		Revision:   "sha256:updated",
		ObservedAt: time.Date(2026, time.July, 23, 0, 0, 0, 0, time.UTC),
		Details: agentclient.DeviceHardwareDetails{
			HardwareRevision:   "fixture-hw-1",
			PrimaryPort:        "cdc-wdm0",
			AccessTechnologies: &accessTechnologies,
			SNR:                &snr,
			Ports: []agentclient.ModemPort{{
				Name:     "cdc-wdm0",
				Type:     "qmi",
				TypeCode: 6,
			}},
		},
	}}
	api, err := New(&fakeRepository{}, Options{
		DeviceConfigurations:  configurations,
		CallPolicies:          &fakeCallPolicies{},
		disableAuthentication: true,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	request := httptest.NewRequest(
		http.MethodPatch,
		"/api/v1/devices/line-1/configuration",
		bytes.NewBufferString(`{
			"request_id":"hardware-config-1",
			"operation":"disconnect_data",
			"expected_device_revision":"sha256:current"
		}`),
	)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	api.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if configurations.lineID != "line-1" ||
		configurations.applyRequest.RequestID != "hardware-config-1" ||
		configurations.applyRequest.ExpectedRevision != "sha256:current" ||
		configurations.applyRequest.Operation != agentclient.DeviceConfigurationDisconnectData {
		t.Fatalf("hardware request = %+v, line = %q", configurations.applyRequest, configurations.lineID)
	}
	var body deviceConfigurationResponse
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Hardware == nil ||
		body.Hardware.Details.HardwareRevision != "fixture-hw-1" ||
		body.Hardware.Details.AccessTechnologies == nil ||
		*body.Hardware.Details.AccessTechnologies != accessTechnologies ||
		body.Hardware.Details.SNR == nil ||
		*body.Hardware.Details.SNR != snr ||
		len(body.Hardware.Details.Ports) != 1 ||
		body.Hardware.Details.Ports[0].Type != "qmi" {
		t.Fatalf("hardware response details = %+v", body.Hardware)
	}
}

func TestAppHardwareConfigurationAcceptsControlledUSBReset(t *testing.T) {
	t.Parallel()
	configurations := &fakeDeviceConfigurations{configuration: agentclient.DeviceConfiguration{
		LineID:     "line-1",
		Revision:   "sha256:updated",
		ObservedAt: time.Date(2026, time.July, 27, 0, 0, 0, 0, time.UTC),
	}}
	api, err := New(&fakeRepository{}, Options{
		DeviceConfigurations:  configurations,
		CallPolicies:          &fakeCallPolicies{},
		disableAuthentication: true,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	request := httptest.NewRequest(
		http.MethodPatch,
		"/api/v1/devices/line-1/configuration",
		bytes.NewBufferString(`{
			"request_id":"usb-reset-1",
			"operation":"reset_usb",
			"expected_device_revision":"sha256:current"
		}`),
	)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	api.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if configurations.applyRequest.Operation != agentclient.DeviceConfigurationResetUSB ||
		configurations.applyRequest.RequestID != "usb-reset-1" ||
		configurations.applyRequest.ExpectedRevision != "sha256:current" {
		t.Fatalf("USB reset request = %+v", configurations.applyRequest)
	}
}

func TestAdministratorDiagnosticsOperateOutsideAssignedLines(t *testing.T) {
	t.Parallel()

	configurations := &fakeDeviceConfigurations{configuration: agentclient.DeviceConfiguration{
		LineID:     "line-other",
		Revision:   "sha256:updated",
		ObservedAt: time.Date(2026, time.July, 31, 0, 0, 0, 0, time.UTC),
	}}
	api, err := New(&fakeRepository{}, Options{
		DeviceConfigurations:  configurations,
		CallPolicies:          &fakeCallPolicies{},
		disableAuthentication: true,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	adminContext := auth.ContextWithPrincipal(context.Background(), auth.Principal{
		UserID:         auth.InitialAdminUserID,
		Role:           auth.RoleAdmin,
		AllowedLineIDs: []string{"line-owned"},
	})

	regularRequest := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/devices/line-other/configuration",
		nil,
	).WithContext(adminContext)
	regularResponse := httptest.NewRecorder()
	api.ServeHTTP(regularResponse, regularRequest)
	if regularResponse.Code != http.StatusNotFound {
		t.Fatalf(
			"regular configuration status = %d, want %d",
			regularResponse.Code,
			http.StatusNotFound,
		)
	}

	diagnosticRequest := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/diagnostics/devices/line-other/configuration",
		nil,
	).WithContext(adminContext)
	diagnosticResponse := httptest.NewRecorder()
	api.ServeHTTP(diagnosticResponse, diagnosticRequest)
	if diagnosticResponse.Code != http.StatusOK {
		t.Fatalf(
			"diagnostic configuration status = %d; body = %s",
			diagnosticResponse.Code,
			diagnosticResponse.Body.String(),
		)
	}
	if configurations.lineID != "line-other" {
		t.Fatalf("diagnostic configuration line = %q", configurations.lineID)
	}

	resetRequest := httptest.NewRequest(
		http.MethodPatch,
		"/api/v1/diagnostics/devices/line-other/configuration",
		bytes.NewBufferString(`{
			"request_id":"diagnostic-usb-reset-1",
			"operation":"reset_usb",
			"expected_device_revision":"sha256:current"
		}`),
	).WithContext(adminContext)
	resetRequest.Header.Set("Content-Type", "application/json")
	resetResponse := httptest.NewRecorder()
	api.ServeHTTP(resetResponse, resetRequest)
	if resetResponse.Code != http.StatusOK {
		t.Fatalf(
			"diagnostic reset status = %d; body = %s",
			resetResponse.Code,
			resetResponse.Body.String(),
		)
	}
	if configurations.lineID != "line-other" ||
		configurations.applyRequest.Operation != agentclient.DeviceConfigurationResetUSB ||
		configurations.applyRequest.RequestID != "diagnostic-usb-reset-1" ||
		configurations.applyRequest.ExpectedRevision != "sha256:current" {
		t.Fatalf(
			"diagnostic USB reset request = %+v, line = %q",
			configurations.applyRequest,
			configurations.lineID,
		)
	}
}

func TestMemberCannotUseSystemDiagnosticDeviceConfiguration(t *testing.T) {
	t.Parallel()

	api, err := New(&fakeRepository{}, Options{
		DeviceConfigurations:  &fakeDeviceConfigurations{},
		disableAuthentication: true,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	memberContext := auth.ContextWithPrincipal(context.Background(), auth.Principal{
		UserID:         "member-1",
		Role:           auth.RoleMember,
		AllowedLineIDs: []string{"line-1"},
	})
	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/diagnostics/devices/line-1/configuration",
		nil,
	).WithContext(memberContext)
	response := httptest.NewRecorder()
	api.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("member diagnostic status = %d, want %d", response.Code, http.StatusForbidden)
	}
}

func communicationStatusWithReject(lineID string) communication.Status {
	return communication.Status{
		Connected:  true,
		ObservedAt: time.Now().UTC(),
		Lines: []store.LineSummary{{
			ID: lineID,
			Capabilities: store.LineCapabilities{
				RejectCall: true,
			},
		}},
	}
}
