package httpapi

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/human-agent65535/modemdeck/agent/internal/domain"
)

type fakeDeviceConfigurationProvider struct {
	configuration domain.DeviceConfiguration
	readLineID    string
	applyRequest  domain.ApplyDeviceConfigurationRequest
	err           error
}

func (provider *fakeDeviceConfigurationProvider) DeviceConfiguration(
	_ context.Context,
	lineID string,
) (domain.DeviceConfiguration, error) {
	provider.readLineID = lineID
	return provider.configuration, provider.err
}

func (provider *fakeDeviceConfigurationProvider) ApplyDeviceConfiguration(
	_ context.Context,
	request domain.ApplyDeviceConfigurationRequest,
) (domain.DeviceConfiguration, error) {
	provider.applyRequest = request
	return provider.configuration, provider.err
}

func TestDeviceConfigurationRoutesExposeTypedReadAndApply(t *testing.T) {
	t.Parallel()
	configurations := &fakeDeviceConfigurationProvider{
		configuration: domain.DeviceConfiguration{
			LineID:          "line-1",
			Revision:        "sha256:fixture",
			ObservedAt:      time.Date(2026, time.July, 23, 0, 0, 0, 0, time.UTC),
			DataConnections: []domain.DataConnection{},
		},
	}
	handler := NewWithOptions(&fakeProvider{}, "test", Options{
		DeviceConfigurations: configurations,
	})
	recorder := performRequest(
		handler,
		http.MethodGet,
		"/v1/lines/line-1/configuration",
		nil,
	)
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var read domain.DeviceConfiguration
	decodeResponse(t, recorder, &read)
	if configurations.readLineID != "line-1" ||
		read.Revision != "sha256:fixture" ||
		read.DataConnections == nil {
		t.Fatalf("GET configuration = %+v, line id = %q", read, configurations.readLineID)
	}

	recorder = performRequest(
		handler,
		http.MethodPatch,
		"/v1/lines/line-1/configuration",
		[]byte(`{
			"request_id":"config-request-1",
			"expected_revision":"sha256:fixture",
			"operation":"set_radio_enabled",
			"radio_enabled":false
		}`),
	)
	if recorder.Code != http.StatusOK {
		t.Fatalf("PATCH status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if configurations.applyRequest.LineID != "line-1" ||
		configurations.applyRequest.RequestID != "config-request-1" ||
		configurations.applyRequest.RadioEnabled == nil ||
		*configurations.applyRequest.RadioEnabled {
		t.Fatalf("PATCH request = %+v", configurations.applyRequest)
	}
}

func TestDeviceConfigurationVerificationFailureIsTypedBadGateway(t *testing.T) {
	t.Parallel()
	configurations := &fakeDeviceConfigurationProvider{
		err: domain.VerificationFailed(
			"apply_device_configuration",
			"read-back did not match",
			nil,
		),
	}
	handler := NewWithOptions(&fakeProvider{}, "test", Options{
		DeviceConfigurations: configurations,
	})
	recorder := performRequest(
		handler,
		http.MethodPatch,
		"/v1/lines/line-1/configuration",
		[]byte(`{
			"request_id":"config-request-2",
			"expected_revision":"sha256:fixture",
			"operation":"disconnect_data"
		}`),
	)
	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var body errorBody
	decodeResponse(t, recorder, &body)
	if body.Error.Code != domain.ErrorVerification {
		t.Fatalf("error = %+v", body.Error)
	}
}

func TestHealthAdvertisesDeviceConfigurationOnlyWhenWired(t *testing.T) {
	t.Parallel()
	provider := &fakeProvider{health: domain.ProviderHealth{
		Available: true,
		Capabilities: domain.AgentCapabilities{
			DeviceConfiguration: true,
		},
	}}
	without := performRequest(New(provider, "test"), http.MethodGet, "/v1/health", nil)
	var withoutBody healthResponse
	decodeResponse(t, without, &withoutBody)
	if withoutBody.Provider.Capabilities.DeviceConfiguration {
		t.Fatal("health advertised an unwired device configuration endpoint")
	}
	with := performRequest(NewWithOptions(provider, "test", Options{
		DeviceConfigurations: &fakeDeviceConfigurationProvider{},
	}), http.MethodGet, "/v1/health", nil)
	var withBody healthResponse
	decodeResponse(t, with, &withBody)
	if !withBody.Provider.Capabilities.DeviceConfiguration {
		t.Fatal("health did not advertise wired device configuration")
	}
}
