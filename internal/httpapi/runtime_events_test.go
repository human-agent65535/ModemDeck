package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/human-agent65535/modemdeck/internal/agentclient"
	"github.com/human-agent65535/modemdeck/internal/auth"
	"github.com/human-agent65535/modemdeck/internal/communication"
	"github.com/human-agent65535/modemdeck/internal/networkruntime"
	"github.com/human-agent65535/modemdeck/internal/runtimeevents"
	"github.com/human-agent65535/modemdeck/internal/store"
	"github.com/human-agent65535/modemdeck/internal/updatecheck"
)

type runtimeStateFlushResponse struct {
	*eventStreamTestResponse
	once    sync.Once
	onFlush func()
}

type countingRuntimeRepository struct {
	*fakeRepository
	linesCalls   int
	devicesCalls int
}

type sequencedRuntimeUpdateManager struct {
	mu         sync.Mutex
	operations []updatecheck.Operation
	statusCall int
}

func (manager *sequencedRuntimeUpdateManager) Apply(
	context.Context,
	updatecheck.ApplyRequest,
) (updatecheck.Operation, error) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	return manager.operations[0], nil
}

func (manager *sequencedRuntimeUpdateManager) Status(
	context.Context,
) (updatecheck.Operation, error) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	index := min(manager.statusCall, len(manager.operations)-1)
	manager.statusCall++
	return manager.operations[index], nil
}

func (repository *countingRuntimeRepository) Lines(
	ctx context.Context,
) ([]store.LineSummary, error) {
	repository.linesCalls++
	return repository.fakeRepository.Lines(ctx)
}

func (repository *countingRuntimeRepository) Devices(
	ctx context.Context,
) ([]store.Device, error) {
	repository.devicesCalls++
	return repository.fakeRepository.Devices(ctx)
}

func (response *runtimeStateFlushResponse) Flush() {
	response.eventStreamTestResponse.Flush()
	response.once.Do(response.onFlush)
}

func TestRuntimeStateStreamStartsWithCurrentStateAndIgnoresReplayCursors(t *testing.T) {
	t.Parallel()

	hub := runtimeevents.NewHub()
	hub.Publish(runtimeevents.Change{Sections: runtimeevents.SectionNetwork})
	current := hub.Publish(runtimeevents.Change{
		Durable:    true,
		ObservedAt: time.Date(2026, time.August, 2, 3, 0, 1, 0, time.UTC),
	})
	api, err := New(&fakeRepository{}, Options{
		RuntimeEvents:         hub,
		disableAuthentication: true,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/runtime/events?after=not-a-cursor",
		nil,
	)
	request.Header.Set("Last-Event-ID", "99")
	ctx, cancel := context.WithCancel(request.Context())
	cancel()
	response := httptest.NewRecorder()

	api.ServeHTTP(response, request.WithContext(ctx))

	body := response.Body.String()
	if response.Code != http.StatusOK ||
		!strings.Contains(body, "event: state") ||
		!strings.Contains(body, `"revision":2`) ||
		!strings.Contains(body, `"data_revision":1`) ||
		!strings.Contains(body, `"observed_at":"2026-08-02T03:00:01Z"`) ||
		!strings.Contains(body, `"epoch":"`+current.Epoch+`"`) ||
		strings.Contains(body, "event: ready") ||
		strings.Contains(body, "event: reset") ||
		strings.Contains(body, "id:") {
		t.Fatalf("status = %d; stream = %q", response.Code, body)
	}
}

func TestRuntimeStateStreamSendsLatestStateAfterNotification(t *testing.T) {
	t.Parallel()

	hub := runtimeevents.NewHub()
	hub.Publish(runtimeevents.Change{Sections: runtimeevents.SectionCommunication})
	api, err := New(&fakeRepository{}, Options{
		RuntimeEvents:         hub,
		disableAuthentication: true,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/api/v1/runtime/events", nil)
	requestContext, cancel := context.WithCancel(request.Context())
	baseResponse := newEventStreamTestResponse()
	response := &runtimeStateFlushResponse{
		eventStreamTestResponse: baseResponse,
		onFlush: func() {
			hub.Publish(runtimeevents.Change{Durable: true})
		},
	}
	done := make(chan struct{})
	go func() {
		api.ServeHTTP(response, request.WithContext(requestContext))
		close(done)
	}()
	waitForMessageEvent(t, baseResponse, `"revision":2`)
	cancel()
	waitForEventStreamClose(t, done, nil)

	body := baseResponse.bodyString()
	if strings.Count(body, "event: state") != 2 ||
		!strings.Contains(body, `"revision":1,"data_revision":0`) ||
		!strings.Contains(body, `"revision":2,"data_revision":1`) {
		t.Fatalf("stream = %q; want initial and latest state", body)
	}
}

func TestRuntimeStateStreamMultiplexesAdminUpdateProgress(t *testing.T) {
	t.Parallel()

	hub := runtimeevents.NewHub()
	manager := &sequencedRuntimeUpdateManager{operations: []updatecheck.Operation{
		{
			ID:            "operation-1",
			State:         updatecheck.OperationRunning,
			TargetVersion: "v1.10.2",
			StartedAt:     "2026-08-02T12:00:00Z",
			Components: []updatecheck.OperationComponent{
				{Name: "api", State: updatecheck.OperationComponentRestarting},
			},
		},
		{
			ID:            "operation-1",
			State:         updatecheck.OperationSucceeded,
			TargetVersion: "v1.10.2",
			StartedAt:     "2026-08-02T12:00:00Z",
			FinishedAt:    "2026-08-02T12:00:05Z",
			Components: []updatecheck.OperationComponent{
				{Name: "api", State: updatecheck.OperationComponentReady},
			},
		},
	}}
	api, err := New(&fakeRepository{}, Options{
		RuntimeEvents:         hub,
		UpdateManager:         manager,
		disableAuthentication: true,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/runtime/events?update_operation=operation-1",
		nil,
	)
	request = request.WithContext(auth.ContextWithPrincipal(
		request.Context(),
		auth.Principal{UserID: "admin-1", Role: auth.RoleAdmin},
	))
	requestContext, cancel := context.WithCancel(request.Context())
	response := newEventStreamTestResponse()
	done := make(chan struct{})
	go func() {
		api.ServeHTTP(response, request.WithContext(requestContext))
		close(done)
	}()
	waitForMessageEvent(t, response, `"state":"succeeded"`)
	cancel()
	waitForEventStreamClose(t, done, nil)

	body := response.bodyString()
	if strings.Count(body, "event: update") != 2 ||
		!strings.Contains(body, `"state":"running"`) ||
		!strings.Contains(body, `"name":"api","state":"restarting"`) ||
		!strings.Contains(body, `"name":"api","state":"ready"`) {
		t.Fatalf("stream = %q; want running and succeeded update events", body)
	}
}

func TestRuntimeStateStreamDoesNotExposeUpdateToMembers(t *testing.T) {
	t.Parallel()

	api, err := New(&fakeRepository{}, Options{
		RuntimeEvents: runtimeevents.NewHub(),
		UpdateManager: fakeUpdateManager{operation: updatecheck.Operation{
			ID:            "operation-1",
			State:         updatecheck.OperationRunning,
			TargetVersion: "v1.10.2",
		}},
		disableAuthentication: true,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/runtime/events?update_operation=operation-1",
		nil,
	)
	request = request.WithContext(auth.ContextWithPrincipal(
		request.Context(),
		auth.Principal{UserID: "member-1", Role: auth.RoleMember},
	))
	requestContext, cancel := context.WithCancel(request.Context())
	cancel()
	response := httptest.NewRecorder()
	api.ServeHTTP(response, request.WithContext(requestContext))

	if response.Code != http.StatusOK ||
		!strings.Contains(response.Body.String(), "event: state") ||
		strings.Contains(response.Body.String(), "event: update") {
		t.Fatalf("status = %d; member stream = %q", response.Code, response.Body.String())
	}
}

func TestRuntimeStateSnapshotIsBuiltOncePerSignal(t *testing.T) {
	repository := &countingRuntimeRepository{fakeRepository: &fakeRepository{}}
	communications := &fakeCommunications{}
	network := &fakeNetworkService{}
	api, err := New(repository, Options{
		Communications:        communications,
		CallLeases:            &fakeCallLeases{},
		Network:               network,
		disableAuthentication: true,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	signal := runtimeevents.Signal{Epoch: "process-a", Revision: 7}
	var wait sync.WaitGroup
	for range 20 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_ = api.currentRuntimeState(signal, runtimeevents.AllSections)
		}()
	}
	wait.Wait()

	if repository.linesCalls != 1 || repository.devicesCalls != 1 ||
		communications.currentStatusCalls != 1 || communications.statusCalls != 0 ||
		network.statusCalls != 1 || network.proxiesCalls != 1 {
		t.Fatalf(
			"same signal reads: lines=%d devices=%d current_status=%d status=%d network=%d proxies=%d",
			repository.linesCalls,
			repository.devicesCalls,
			communications.currentStatusCalls,
			communications.statusCalls,
			network.statusCalls,
			network.proxiesCalls,
		)
	}

	networkSignal := runtimeevents.Signal{Epoch: "process-a", Revision: 8}
	_ = api.currentRuntimeState(networkSignal, runtimeevents.SectionNetwork)
	if repository.linesCalls != 1 || repository.devicesCalls != 1 ||
		communications.currentStatusCalls != 1 ||
		network.statusCalls != 2 || network.proxiesCalls != 2 {
		t.Fatalf("network-only signal rebuilt unrelated sections")
	}

	_ = api.currentRuntimeState(networkSignal, runtimeevents.AllSections)
	if repository.linesCalls != 2 || repository.devicesCalls != 2 ||
		communications.currentStatusCalls != 2 ||
		network.statusCalls != 2 || network.proxiesCalls != 2 {
		t.Fatalf("lazy section fill did not reuse the network snapshot")
	}
}

func TestRuntimeStateWritesOnlyChangedSections(t *testing.T) {
	t.Parallel()

	repository := &countingRuntimeRepository{fakeRepository: &fakeRepository{}}
	communications := &fakeCommunications{}
	network := &fakeNetworkService{status: networkruntime.Status{
		Available:   true,
		State:       "available",
		ApplyStatus: networkruntime.ApplyStatusApplied,
	}}
	api, err := New(repository, Options{
		Communications:        communications,
		CallLeases:            &fakeCallLeases{},
		Network:               network,
		disableAuthentication: true,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/api/v1/runtime/events", nil)
	response := httptest.NewRecorder()
	if !api.writeRuntimeState(
		response,
		response,
		request,
		auth.Principal{},
		false,
		runtimeevents.Signal{Epoch: "process-a", Revision: 1},
		runtimeevents.SectionNetwork,
	) {
		t.Fatal("writeRuntimeState() = false")
	}

	body := response.Body.String()
	if !strings.Contains(body, `"network":`) ||
		strings.Contains(body, `"communication":`) ||
		strings.Contains(body, `"calls":`) ||
		strings.Contains(body, `"recordings":`) {
		t.Fatalf("network-only state = %q", body)
	}
	if repository.linesCalls != 0 || repository.devicesCalls != 0 ||
		communications.currentStatusCalls != 0 ||
		network.statusCalls != 1 || network.proxiesCalls != 1 {
		t.Fatalf(
			"network-only reads: lines=%d devices=%d communication=%d network=%d proxies=%d",
			repository.linesCalls,
			repository.devicesCalls,
			communications.currentStatusCalls,
			network.statusCalls,
			network.proxiesCalls,
		)
	}

	response = httptest.NewRecorder()
	if !api.writeRuntimeState(
		response,
		response,
		request,
		auth.Principal{},
		false,
		runtimeevents.Signal{Epoch: "process-a", Revision: 2},
		runtimeevents.SectionCalls,
	) {
		t.Fatal("writeRuntimeState() = false")
	}
	body = response.Body.String()
	if !strings.Contains(body, `"calls":{"calls":[],"reservations":[]}`) ||
		!strings.Contains(body, `"recordings":[]`) ||
		strings.Contains(body, `"communication":`) ||
		strings.Contains(body, `"network":`) {
		t.Fatalf("calls-only state = %q", body)
	}
}

func TestRuntimeStateCarriesNetworkAndConfiguredProxies(t *testing.T) {
	t.Parallel()

	hub := runtimeevents.NewHub()
	network := &fakeNetworkService{
		status: networkruntime.Status{
			Available:   true,
			State:       "available",
			Lines:       []agentclient.NetworkLine{},
			Proxies:     []agentclient.NetworkProxy{},
			TodayUsage:  []networkruntime.Usage{},
			MonthUsage:  []networkruntime.Usage{},
			ApplyStatus: networkruntime.ApplyStatusApplied,
		},
		proxies: []networkruntime.Proxy{{
			ID:       "proxy-1",
			Name:     "Line proxy",
			LineID:   "line-1",
			Revision: 3,
		}},
	}
	api, err := New(&fakeRepository{}, Options{
		Network:               network,
		RuntimeEvents:         hub,
		disableAuthentication: true,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/runtime/events", nil)
	ctx, cancel := context.WithCancel(request.Context())
	cancel()
	response := httptest.NewRecorder()

	api.ServeHTTP(response, request.WithContext(ctx))

	body := response.Body.String()
	if response.Code != http.StatusOK ||
		!strings.Contains(body, `"network":{"status":{"available":true`) ||
		!strings.Contains(body, `"proxies":[{"id":"proxy-1"`) {
		t.Fatalf("status = %d; stream = %q", response.Code, body)
	}
}

func TestRuntimeStateCacheKeepsPerUserProjectionsIndependent(t *testing.T) {
	t.Parallel()

	repository := &fakeRepository{lines: []store.LineSummary{
		{ID: "line-1", RadioDesiredEnabledKnown: true},
		{ID: "line-2", RadioDesiredEnabledKnown: true},
	}}
	communications := &fakeCommunications{status: communication.Status{
		Connected: true,
		Lines:     append([]store.LineSummary(nil), repository.lines...),
	}}
	network := &fakeNetworkService{
		status: networkruntime.Status{
			Lines: []agentclient.NetworkLine{
				{LineID: "line-1"},
				{LineID: "line-2"},
			},
			Proxies: []agentclient.NetworkProxy{
				{ID: "proxy-1"},
				{ID: "proxy-2"},
			},
		},
		proxies: []networkruntime.Proxy{
			{ID: "proxy-1", LineID: "line-1"},
			{ID: "proxy-2", LineID: "line-2"},
		},
	}
	api, err := New(repository, Options{
		Communications:        communications,
		Network:               network,
		disableAuthentication: true,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	signal := runtimeevents.Signal{Epoch: "process-a", Revision: 1}

	streamFor := func(principal auth.Principal) string {
		request := httptest.NewRequest(http.MethodGet, "/api/v1/runtime/events", nil)
		response := httptest.NewRecorder()
		if !api.writeRuntimeState(
			response,
			response,
			request,
			principal,
			true,
			signal,
			runtimeevents.AllSections,
		) {
			t.Fatal("writeRuntimeState() = false")
		}
		return response.Body.String()
	}
	line1 := streamFor(auth.Principal{
		UserID:         "member-1",
		Role:           auth.RoleMember,
		AllowedLineIDs: []string{"line-1"},
	})
	line2 := streamFor(auth.Principal{
		UserID:         "member-2",
		Role:           auth.RoleMember,
		AllowedLineIDs: []string{"line-2"},
	})

	if !strings.Contains(line1, `"id":"line-1"`) ||
		!strings.Contains(line1, `"id":"proxy-1"`) ||
		strings.Contains(line1, `"id":"line-2"`) ||
		strings.Contains(line1, `"id":"proxy-2"`) {
		t.Fatalf("line-1 projection = %q", line1)
	}
	if !strings.Contains(line2, `"id":"line-2"`) ||
		!strings.Contains(line2, `"id":"proxy-2"`) ||
		strings.Contains(line2, `"id":"line-1"`) ||
		strings.Contains(line2, `"id":"proxy-1"`) {
		t.Fatalf("line-2 projection = %q", line2)
	}
	if communications.currentStatusCalls != 1 ||
		network.statusCalls != 1 || network.proxiesCalls != 1 {
		t.Fatalf(
			"shared snapshot reads: current_status=%d network=%d proxies=%d",
			communications.currentStatusCalls,
			network.statusCalls,
			network.proxiesCalls,
		)
	}
}

func TestMobileRuntimeStateFiltersLinesToPrincipal(t *testing.T) {
	t.Parallel()

	hub := runtimeevents.NewHub()
	repository := &fakeRepository{
		lines: []store.LineSummary{
			{ID: "line-1", RadioDesiredEnabledKnown: true},
			{ID: "line-2", RadioDesiredEnabledKnown: true},
		},
		mobileFound: true,
		mobilePrincipal: auth.Principal{
			UserID:            "member-1",
			Role:              auth.RoleMember,
			IOSPairingEnabled: true,
			AllowedLineIDs:    []string{"line-1"},
		},
	}
	api, err := New(repository, Options{
		RuntimeEvents:         hub,
		disableAuthentication: true,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/runtime/events", nil)
	request = request.WithContext(context.WithValue(
		auth.ContextWithPrincipal(request.Context(), repository.mobilePrincipal),
		mobileAuthenticationContextKey{},
		mobileAuthentication{},
	))
	ctx, cancel := context.WithCancel(request.Context())
	cancel()
	response := httptest.NewRecorder()

	api.ServeHTTP(response, request.WithContext(ctx))

	body := response.Body.String()
	if response.Code != http.StatusOK ||
		!strings.Contains(body, `"lines":[{"id":"line-1"`) ||
		!strings.Contains(body, `"line_catalog":[{"id":"line-1"`) ||
		strings.Contains(body, `"id":"line-2"`) {
		t.Fatalf("status = %d; mobile stream = %q", response.Code, body)
	}
}

func TestRuntimeStateStreamRequiresConfiguredSource(t *testing.T) {
	t.Parallel()

	api, err := New(&fakeRepository{}, Options{disableAuthentication: true})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	response := httptest.NewRecorder()
	api.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/runtime/events", nil))
	if response.Code != http.StatusServiceUnavailable ||
		!strings.Contains(response.Body.String(), `"code":"runtime_events_unavailable"`) {
		t.Fatalf("status = %d; body = %s", response.Code, response.Body.String())
	}
}

func TestEventHeartbeatHasNoReplayCursor(t *testing.T) {
	t.Parallel()

	response := httptest.NewRecorder()
	observedAt := time.Date(2026, time.August, 2, 7, 30, 0, 0, time.UTC)

	if !writeEventHeartbeat(response, response, observedAt) {
		t.Fatal("writeEventHeartbeat() = false")
	}

	body := response.Body.String()
	if !strings.Contains(body, "event: heartbeat") ||
		!strings.Contains(body, `"at":"2026-08-02T07:30:00Z"`) ||
		strings.Contains(body, "id:") {
		t.Fatalf("heartbeat = %q", body)
	}
}
