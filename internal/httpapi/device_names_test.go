package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/human-agent65535/modemdeck/internal/store"
)

type deviceNameRepository struct {
	*fakeRepository
	imei          string
	requestedName string
}

func (repository *deviceNameRepository) RenameDevice(
	_ context.Context,
	imei,
	name string,
) (store.Device, error) {
	repository.imei = imei
	repository.requestedName = name
	return store.Device{IMEI: imei, Name: strings.TrimSpace(name), Model: "QDC507"}, nil
}

func TestDeviceResourceRenamesPersistedDeviceName(t *testing.T) {
	t.Parallel()

	repository := &deviceNameRepository{fakeRepository: &fakeRepository{}}
	api, err := New(repository, Options{disableAuthentication: true})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	request := httptest.NewRequest(
		http.MethodPatch,
		"/api/v1/devices/860000000000001",
		bytes.NewBufferString(`{"name":"  机房模组  "}`),
	)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	api.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", response.Code, response.Body.String())
	}
	if repository.imei != "860000000000001" || repository.requestedName != "  机房模组  " {
		t.Fatalf(
			"repository rename = (%q, %q), want device-bound name",
			repository.imei,
			repository.requestedName,
		)
	}
	var payload deviceResponse
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Device.Name != "机房模组" || payload.Device.Model != "QDC507" {
		t.Fatalf("device = %+v", payload.Device)
	}
}

func TestDeviceResourceRequiresNameInsteadOfAlias(t *testing.T) {
	t.Parallel()

	repository := &deviceNameRepository{fakeRepository: &fakeRepository{}}
	api, err := New(repository, Options{disableAuthentication: true})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	request := httptest.NewRequest(
		http.MethodPatch,
		"/api/v1/devices/860000000000001",
		bytes.NewBufferString(`{"alias":"legacy"}`),
	)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	api.ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", response.Code, response.Body.String())
	}
}
