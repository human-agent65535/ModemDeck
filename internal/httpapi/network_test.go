package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/human-agent65535/modemdeck/internal/agentclient"
	"github.com/human-agent65535/modemdeck/internal/networkruntime"
)

type fakeNetworkService struct {
	status       networkruntime.Status
	statusError  error
	proxies      []networkruntime.Proxy
	proxiesError error
	createInput  networkruntime.CreateInput
	createResult networkruntime.ProxyMutation
	createError  error
	updateID     string
	updateInput  networkruntime.UpdateInput
	updateResult networkruntime.ProxyMutation
	updateError  error
	deleteID     string
	deleteRev    int64
	deleteResult networkruntime.DeleteResult
	deleteError  error
}

func (service *fakeNetworkService) Status(context.Context) (networkruntime.Status, error) {
	return service.status, service.statusError
}

func (service *fakeNetworkService) Proxies(
	context.Context,
) ([]networkruntime.Proxy, error) {
	return service.proxies, service.proxiesError
}

func (service *fakeNetworkService) Create(
	_ context.Context,
	input networkruntime.CreateInput,
) (networkruntime.ProxyMutation, error) {
	service.createInput = input
	return service.createResult, service.createError
}

func (service *fakeNetworkService) Update(
	_ context.Context,
	id string,
	input networkruntime.UpdateInput,
) (networkruntime.ProxyMutation, error) {
	service.updateID = id
	service.updateInput = input
	return service.updateResult, service.updateError
}

func (service *fakeNetworkService) Delete(
	_ context.Context,
	id string,
	revision int64,
) (networkruntime.DeleteResult, error) {
	service.deleteID = id
	service.deleteRev = revision
	return service.deleteResult, service.deleteError
}

func TestNetworkStatusIncludesCurrentAndAggregatedUsage(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC)
	network := &fakeNetworkService{status: networkruntime.Status{
		Available:  true,
		State:      "available",
		BootEpoch:  "boot-1",
		ObservedAt: &observedAt,
		Lines: []agentclient.NetworkLine{{
			LineID:    "line-1",
			Connected: true,
			Interface: "wwan0",
			DNS:       []string{"1.1.1.1"},
			RXBytes:   100,
			TXBytes:   200,
		}},
		Proxies:        []agentclient.NetworkProxy{},
		TodayTotal:     networkruntime.UsageTotal{RXBytes: 10, TXBytes: 20},
		TodayUsage:     []networkruntime.Usage{{ScopeID: "line-1", RXBytes: 10, TXBytes: 20}},
		MonthTotal:     networkruntime.UsageTotal{RXBytes: 30, TXBytes: 40},
		MonthUsage:     []networkruntime.Usage{{ScopeID: "line-1", RXBytes: 30, TXBytes: 40}},
		ApplyPending:   true,
		ApplyStatus:    networkruntime.ApplyStatusAgentUnavailable,
		ApplyAttempts:  2,
		ApplyExhausted: true,
	}}
	api := newNetworkTestAPI(t, network)
	response := httptest.NewRecorder()
	api.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/network", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", response.Code, response.Body)
	}
	var body networkruntime.Status
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !body.Available ||
		body.BootEpoch != "boot-1" ||
		body.TodayTotal.RXBytes != 10 ||
		body.MonthTotal.TXBytes != 40 ||
		len(body.TodayUsage) != 1 ||
		len(body.MonthUsage) != 1 ||
		!body.ApplyPending ||
		body.ApplyStatus != networkruntime.ApplyStatusAgentUnavailable ||
		body.ApplyAttempts != 2 ||
		!body.ApplyExhausted {
		t.Fatalf("body = %+v", body)
	}
}

func TestProxyCreateRequiresZeroRevisionAndRedactsPassword(t *testing.T) {
	t.Parallel()

	network := &fakeNetworkService{createResult: networkruntime.ProxyMutation{
		Proxy: networkruntime.Proxy{
			ID:          "proxy-1",
			Name:        "Primary",
			HasPassword: true,
			Revision:    1,
			ApplyState:  networkruntime.ProxyApplyStatePendingCreate,
		},
		ApplyResult: networkruntime.ApplyResult{
			Applied: false,
			Status:  networkruntime.ApplyStatusAgentUnavailable,
		},
	}}
	api := newNetworkTestAPI(t, network)
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/proxies",
		bytes.NewBufferString(`{
			"revision":0,
			"id":"proxy-1",
			"name":"Primary",
			"line_id":"line-1",
			"enabled":true,
			"mode":"socks5",
			"listen_address":"127.0.0.1",
			"listen_port":1080,
			"auth_enabled":true,
			"username":"user",
			"password":"do-not-return"
		}`),
	)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	api.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d body = %s", response.Code, response.Body)
	}
	if network.createInput.Password != "do-not-return" {
		t.Fatalf("create input = %+v", network.createInput)
	}
	if bytes.Contains(response.Body.Bytes(), []byte("do-not-return")) {
		t.Fatalf("response leaks password: %s", response.Body)
	}
	var publicBody struct {
		Proxy map[string]json.RawMessage `json:"proxy"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &publicBody); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	for _, forbidden := range []string{"password", "password_nonce", "password_ciphertext"} {
		if _, exists := publicBody.Proxy[forbidden]; exists {
			t.Fatalf("response contains write-only field %q: %s", forbidden, response.Body)
		}
	}
	if !bytes.Contains(response.Body.Bytes(), []byte(`"has_password":true`)) ||
		!bytes.Contains(response.Body.Bytes(), []byte(`"status":"agent_unavailable"`)) ||
		!bytes.Contains(response.Body.Bytes(), []byte(`"apply_state":"pending_create"`)) {
		t.Fatalf("response = %s", response.Body)
	}

	missingRevision := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/proxies",
		bytes.NewBufferString(`{
			"name":"Primary",
			"line_id":"line-1",
			"mode":"socks5",
			"listen_address":"127.0.0.1",
			"listen_port":1080
		}`),
	)
	missingRevision.Header.Set("Content-Type", "application/json")
	missingResponse := httptest.NewRecorder()
	api.ServeHTTP(missingResponse, missingRevision)
	assertAPIError(t, missingResponse, http.StatusBadRequest, "invalid_argument")
}

func TestProxyCollectionKeepsPendingDeleteVisible(t *testing.T) {
	t.Parallel()

	network := &fakeNetworkService{proxies: []networkruntime.Proxy{{
		ID:              "proxy-1",
		Name:            "Removing",
		Revision:        3,
		AppliedRevision: 2,
		ApplyState:      networkruntime.ProxyApplyStatePendingDelete,
	}}}
	api := newNetworkTestAPI(t, network)
	response := httptest.NewRecorder()
	api.ServeHTTP(
		response,
		httptest.NewRequest(http.MethodGet, "/api/v1/proxies", nil),
	)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", response.Code, response.Body)
	}
	if !bytes.Contains(response.Body.Bytes(), []byte(`"apply_state":"pending_delete"`)) ||
		!bytes.Contains(response.Body.Bytes(), []byte(`"applied_revision":2`)) {
		t.Fatalf("response = %s", response.Body)
	}
}

func TestProxyPatchAndDeleteRequireRevision(t *testing.T) {
	t.Parallel()

	network := &fakeNetworkService{
		updateResult: networkruntime.ProxyMutation{
			Proxy: networkruntime.Proxy{ID: "proxy-1", Revision: 3},
			ApplyResult: networkruntime.ApplyResult{
				Applied: true,
				Status:  networkruntime.ApplyStatusApplied,
			},
		},
		deleteResult: networkruntime.DeleteResult{
			ID: "proxy-1",
			ApplyResult: networkruntime.ApplyResult{
				Applied: true,
				Status:  networkruntime.ApplyStatusApplied,
			},
		},
	}
	api := newNetworkTestAPI(t, network)
	name := "Updated"
	request := httptest.NewRequest(
		http.MethodPatch,
		"/api/v1/proxies/proxy-1",
		bytes.NewBufferString(`{"revision":2,"name":"Updated"}`),
	)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	api.ServeHTTP(response, request)
	if response.Code != http.StatusOK ||
		network.updateID != "proxy-1" ||
		network.updateInput.Revision != 2 ||
		network.updateInput.Name == nil ||
		*network.updateInput.Name != name {
		t.Fatalf(
			"status=%d update id=%q input=%+v body=%s",
			response.Code,
			network.updateID,
			network.updateInput,
			response.Body,
		)
	}

	deleteRequest := httptest.NewRequest(
		http.MethodDelete,
		"/api/v1/proxies/proxy-1?revision=3",
		nil,
	)
	deleteResponse := httptest.NewRecorder()
	api.ServeHTTP(deleteResponse, deleteRequest)
	if deleteResponse.Code != http.StatusOK ||
		network.deleteID != "proxy-1" ||
		network.deleteRev != 3 ||
		!bytes.Contains(deleteResponse.Body.Bytes(), []byte(`"applied":true`)) {
		t.Fatalf(
			"delete status=%d id=%q rev=%d body=%s",
			deleteResponse.Code,
			network.deleteID,
			network.deleteRev,
			deleteResponse.Body,
		)
	}
}

func TestProxyErrorsMapWithoutLeakingInternalDetails(t *testing.T) {
	t.Parallel()

	network := &fakeNetworkService{
		updateError: &networkruntime.Error{
			Code:    networkruntime.CodeConflict,
			Field:   "revision",
			Message: "Proxy changed since it was loaded",
			Cause:   errors.New("database detail"),
		},
	}
	api := newNetworkTestAPI(t, network)
	request := httptest.NewRequest(
		http.MethodPatch,
		"/api/v1/proxies/proxy-1",
		bytes.NewBufferString(`{"revision":1,"enabled":true}`),
	)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	api.ServeHTTP(response, request)
	assertAPIError(t, response, http.StatusConflict, "revision_conflict")
	if bytes.Contains(response.Body.Bytes(), []byte("database detail")) {
		t.Fatalf("response leaks internal error: %s", response.Body)
	}
}

func newNetworkTestAPI(t *testing.T, network NetworkService) *API {
	t.Helper()
	api, err := New(&fakeRepository{}, Options{
		Network:               network,
		disableAuthentication: true,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return api
}
