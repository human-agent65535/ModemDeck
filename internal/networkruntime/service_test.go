package networkruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/human-agent65535/modemdeck/internal/agentclient"
	platformdb "github.com/human-agent65535/modemdeck/internal/platform/database"
	"github.com/human-agent65535/modemdeck/internal/secretbox"
	"github.com/human-agent65535/modemdeck/internal/store"
)

func waitForAgentCalls(t *testing.T, agent *fakeAgent, putMinimum, getMinimum int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		agent.mu.Lock()
		putCalls := len(agent.received)
		getCalls := agent.networkCalls
		agent.mu.Unlock()
		if putCalls >= putMinimum && getCalls >= getMinimum {
			return
		}
		time.Sleep(time.Millisecond)
	}
	agent.mu.Lock()
	defer agent.mu.Unlock()
	t.Fatalf(
		"agent calls did not reach PUT=%d GET=%d; got PUT=%d GET=%d",
		putMinimum,
		getMinimum,
		len(agent.received),
		agent.networkCalls,
	)
}

type fakeAgent struct {
	mu                   sync.Mutex
	err                  error
	networkErr           error
	putErr               error
	snapshotErr          error
	networkCalls         int
	fullSnapshotCalls    int
	snapshots            []agentclient.NetworkSnapshot
	fullSnapshot         agentclient.Snapshot
	received             [][]agentclient.ProxyConfiguration
	scanResult           agentclient.NetworkScanResult
	scanErr              error
	scanCalls            []networkAgentCall
	selectionErrors      map[string]error
	selectionCalls       []networkAgentCall
	beforeSelectionApply func(string, agentclient.ApplyNetworkSelectionRequest) error
}

type networkAgentCall struct {
	LineID       string
	RequestID    string
	Mode         agentclient.NetworkSelectionMode
	OperatorCode string
	Deadline     time.Time
	HasDeadline  bool
}

type networkTestRepository struct {
	*store.Store
	lines []store.LineSummary
}

func (repository *networkTestRepository) Lines(
	context.Context,
) ([]store.LineSummary, error) {
	return append([]store.LineSummary(nil), repository.lines...), nil
}

func (repository *networkTestRepository) ResolveLineEndpoint(
	_ context.Context,
	lineID string,
) (string, error) {
	for _, line := range repository.lines {
		if line.ID == lineID && strings.TrimSpace(line.EndpointID) != "" {
			return line.EndpointID, nil
		}
	}
	return "", fmt.Errorf("line %q is not attached", lineID)
}

func (agent *fakeAgent) Network(context.Context) (agentclient.NetworkSnapshot, error) {
	agent.mu.Lock()
	defer agent.mu.Unlock()
	agent.networkCalls++
	if agent.networkErr != nil {
		return agentclient.NetworkSnapshot{}, agent.networkErr
	}
	if agent.err != nil {
		return agentclient.NetworkSnapshot{}, agent.err
	}
	return agent.snapshotLocked(), nil
}

func (agent *fakeAgent) PutProxies(
	_ context.Context,
	proxies []agentclient.ProxyConfiguration,
) (agentclient.NetworkSnapshot, error) {
	agent.mu.Lock()
	defer agent.mu.Unlock()
	copied := append([]agentclient.ProxyConfiguration(nil), proxies...)
	agent.received = append(agent.received, copied)
	if agent.putErr != nil {
		return agentclient.NetworkSnapshot{}, agent.putErr
	}
	if agent.err != nil {
		return agentclient.NetworkSnapshot{}, agent.err
	}
	snapshot := agent.snapshotLocked()
	if len(agent.snapshots) == 0 {
		snapshot.Proxies = make([]agentclient.NetworkProxy, len(proxies))
		for index, proxy := range proxies {
			state := agentclient.ProxyStateDisabled
			if proxy.Enabled {
				state = agentclient.ProxyStateWaitingForBearer
			}
			snapshot.Proxies[index] = agentclient.NetworkProxy{
				ID:            proxy.ID,
				LineID:        proxy.LineID,
				State:         state,
				Mode:          proxy.Mode,
				ListenAddress: proxy.ListenAddress,
				ListenPort:    proxy.ListenPort,
			}
		}
	}
	return snapshot, nil
}

func (agent *fakeAgent) Snapshot(context.Context) (agentclient.Snapshot, error) {
	agent.mu.Lock()
	defer agent.mu.Unlock()
	agent.fullSnapshotCalls++
	if agent.snapshotErr != nil {
		return agentclient.Snapshot{}, agent.snapshotErr
	}
	if agent.err != nil {
		return agentclient.Snapshot{}, agent.err
	}
	return agent.fullSnapshot, nil
}

func (agent *fakeAgent) ScanNetworks(
	ctx context.Context,
	lineID string,
	request agentclient.NetworkScanRequest,
) (agentclient.NetworkScanResult, error) {
	deadline, hasDeadline := ctx.Deadline()
	agent.mu.Lock()
	defer agent.mu.Unlock()
	agent.scanCalls = append(agent.scanCalls, networkAgentCall{
		LineID:      lineID,
		RequestID:   request.RequestID,
		Deadline:    deadline,
		HasDeadline: hasDeadline,
	})
	if agent.scanErr != nil {
		return agentclient.NetworkScanResult{}, agent.scanErr
	}
	if agent.err != nil {
		return agentclient.NetworkScanResult{}, agent.err
	}
	result := agent.scanResult
	if strings.TrimSpace(result.RequestID) == "" {
		result.RequestID = request.RequestID
	}
	if strings.TrimSpace(result.LineID) == "" {
		result.LineID = lineID
	}
	if result.ObservedAt.IsZero() {
		result.ObservedAt = time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	}
	if result.Networks == nil {
		result.Networks = []agentclient.MobileNetwork{}
	}
	return result, nil
}

func (agent *fakeAgent) SetNetworkSelection(
	ctx context.Context,
	lineID string,
	request agentclient.ApplyNetworkSelectionRequest,
) (agentclient.NetworkSelectionReceipt, error) {
	deadline, hasDeadline := ctx.Deadline()
	agent.mu.Lock()
	agent.selectionCalls = append(agent.selectionCalls, networkAgentCall{
		LineID:       lineID,
		RequestID:    request.RequestID,
		Mode:         request.Mode,
		OperatorCode: request.OperatorCode,
		Deadline:     deadline,
		HasDeadline:  hasDeadline,
	})
	hook := agent.beforeSelectionApply
	err := agent.selectionErrors[lineID]
	if err == nil {
		err = agent.err
	}
	agent.mu.Unlock()
	if hook != nil {
		if hookErr := hook(lineID, request); hookErr != nil {
			return agentclient.NetworkSelectionReceipt{}, hookErr
		}
	}
	if err != nil {
		return agentclient.NetworkSelectionReceipt{}, err
	}
	return agentclient.NetworkSelectionReceipt{
		RequestID:    request.RequestID,
		LineID:       lineID,
		Mode:         request.Mode,
		OperatorCode: request.OperatorCode,
		AppliedAt:    time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC),
	}, nil
}

func (agent *fakeAgent) snapshotLocked() agentclient.NetworkSnapshot {
	if len(agent.snapshots) == 0 {
		return agentclient.NetworkSnapshot{
			BootEpoch:  "boot-1",
			ObservedAt: time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC),
			Lines:      []agentclient.NetworkLine{},
			Proxies:    []agentclient.NetworkProxy{},
		}
	}
	snapshot := agent.snapshots[0]
	if len(agent.snapshots) > 1 {
		agent.snapshots = agent.snapshots[1:]
	}
	return snapshot
}

func TestProxyPasswordIsEncryptedAndNeverReturned(t *testing.T) {
	t.Parallel()

	service, repository, agent := newNetworkTestService(t, nil)
	repository.lines[0].EndpointID = "endpoint-1"
	mutation, err := service.Create(context.Background(), CreateInput{
		ID:            "proxy-1",
		Name:          "Primary",
		LineID:        "line-1",
		Enabled:       true,
		Mode:          agentclient.ProxyModeSOCKS5,
		ListenAddress: "127.0.0.1",
		ListenPort:    1080,
		AuthEnabled:   true,
		Username:      "proxy-user",
		Password:      "super-secret-password",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if !mutation.Applied ||
		!mutation.Proxy.HasPassword ||
		mutation.Proxy.ApplyState != ProxyApplyStateApplied ||
		mutation.Proxy.AppliedRevision != mutation.Proxy.Revision {
		t.Fatalf("mutation = %+v", mutation)
	}
	record, err := repository.ProxyInstance(context.Background(), "proxy-1")
	if err != nil {
		t.Fatalf("ProxyInstance() error = %v", err)
	}
	if bytes.Contains(record.PasswordCiphertext, []byte("super-secret-password")) ||
		bytes.Equal(record.PasswordCiphertext, []byte("super-secret-password")) {
		t.Fatal("stored ciphertext contains plaintext")
	}
	encoded, err := json.Marshal(mutation)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	if bytes.Contains(encoded, []byte("super-secret-password")) ||
		bytes.Contains(encoded, []byte("ciphertext")) ||
		bytes.Contains(encoded, []byte("nonce")) {
		t.Fatalf("public response leaks password material: %s", encoded)
	}
	if len(agent.received) != 1 ||
		len(agent.received[0]) != 1 ||
		agent.received[0][0].LineID != "endpoint-1" ||
		agent.received[0][0].Password != "super-secret-password" {
		t.Fatalf("agent desired state = %+v", agent.received)
	}
}

func TestProxyValidationAndRevisionConflict(t *testing.T) {
	t.Parallel()

	service, _, _ := newNetworkTestService(t, nil)
	_, err := service.Create(context.Background(), CreateInput{
		ID:            "proxy-public",
		Name:          "Public",
		LineID:        "line-1",
		Mode:          agentclient.ProxyModeHTTP,
		ListenAddress: "0.0.0.0",
		ListenPort:    8080,
	})
	assertNetworkErrorCode(t, err, CodeInvalidArgument, "auth_enabled")

	created, err := service.Create(context.Background(), CreateInput{
		ID:            "proxy-local",
		Name:          "Local",
		LineID:        "line-1",
		Mode:          agentclient.ProxyModeHTTP,
		ListenAddress: "127.0.0.1",
		ListenPort:    8080,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	newName := "Changed"
	_, err = service.Update(context.Background(), created.Proxy.ID, UpdateInput{
		Revision: created.Proxy.Revision + 1,
		Name:     &newName,
	})
	assertNetworkErrorCode(t, err, CodeConflict, "revision")
}

func TestProxyValidationMatchesHostRuntimeConstraints(t *testing.T) {
	t.Parallel()

	service, _, _ := newNetworkTestService(t, nil)
	_, err := service.Create(context.Background(), CreateInput{
		ID:            strings.Repeat("a", maxProxyIDLength+1),
		Name:          "Too long ID",
		LineID:        "line-1",
		Mode:          agentclient.ProxyModeSOCKS5,
		ListenAddress: "127.0.0.1",
		ListenPort:    1080,
	})
	assertNetworkErrorCode(t, err, CodeInvalidArgument, "id")

	first, err := service.Create(context.Background(), CreateInput{
		ID:            "proxy-1",
		Name:          "First",
		LineID:        "line-1",
		Mode:          agentclient.ProxyModeSOCKS5,
		ListenAddress: "127.0.0.1",
		ListenPort:    1080,
	})
	if err != nil {
		t.Fatalf("Create(first) error = %v", err)
	}
	if first.Proxy.ID != "proxy-1" {
		t.Fatalf("first proxy = %+v", first)
	}
	_, err = service.Create(context.Background(), CreateInput{
		ID:            "proxy-2",
		Name:          "Conflicting listener",
		LineID:        "line-2",
		Mode:          agentclient.ProxyModeHTTP,
		ListenAddress: "127.0.0.1",
		ListenPort:    1080,
	})
	assertNetworkErrorCode(t, err, CodeInvalidArgument, "listen_port")

	_, err = service.Create(context.Background(), CreateInput{
		ID:            "proxy-3",
		Name:          "Control password",
		LineID:        "line-1",
		Mode:          agentclient.ProxyModeSOCKS5,
		ListenAddress: "127.0.0.1",
		ListenPort:    1081,
		AuthEnabled:   true,
		Username:      "user",
		Password:      "line\nbreak",
	})
	assertNetworkErrorCode(t, err, CodeInvalidArgument, "password")

	_, err = service.Create(context.Background(), CreateInput{
		ID:            "proxy-4",
		Name:          "SOCKS password",
		LineID:        "line-1",
		Mode:          agentclient.ProxyModeSOCKS5,
		ListenAddress: "127.0.0.1",
		ListenPort:    1082,
		AuthEnabled:   true,
		Username:      "user",
		Password:      strings.Repeat("p", maxSOCKS5SecretBytes+1),
	})
	assertNetworkErrorCode(t, err, CodeInvalidArgument, "password")

	_, err = service.Create(context.Background(), CreateInput{
		ID:            "proxy-5",
		Name:          "HTTP username",
		LineID:        "line-1",
		Mode:          agentclient.ProxyModeHTTP,
		ListenAddress: "127.0.0.1",
		ListenPort:    8081,
		AuthEnabled:   true,
		Username:      "invalid:user",
		Password:      "password",
	})
	assertNetworkErrorCode(t, err, CodeInvalidArgument, "username")

	httpProxy, err := service.Create(context.Background(), CreateInput{
		ID:            "proxy-6",
		Name:          "HTTP long password",
		LineID:        "line-1",
		Mode:          agentclient.ProxyModeHTTP,
		ListenAddress: "127.0.0.1",
		ListenPort:    8082,
		AuthEnabled:   true,
		Username:      "user",
		Password:      strings.Repeat("p", maxSOCKS5SecretBytes+1),
	})
	if err != nil {
		t.Fatalf("Create(HTTP long password) error = %v", err)
	}
	socksMode := agentclient.ProxyModeSOCKS5
	_, err = service.Update(context.Background(), httpProxy.Proxy.ID, UpdateInput{
		Revision: httpProxy.Proxy.Revision,
		Mode:     &socksMode,
	})
	assertNetworkErrorCode(t, err, CodeInvalidArgument, "password")
}

func TestSavedConfigurationSurvivesUnavailableAgent(t *testing.T) {
	t.Parallel()

	agent := &fakeAgent{err: errors.New("agent socket unavailable")}
	service, _, _ := newNetworkTestService(t, agent)
	mutation, err := service.Create(context.Background(), CreateInput{
		ID:            "proxy-1",
		Name:          "Primary",
		LineID:        "line-1",
		Enabled:       true,
		Mode:          agentclient.ProxyModeSOCKS5,
		ListenAddress: "127.0.0.1",
		ListenPort:    1080,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if mutation.Applied || mutation.Status != ApplyStatusAgentUnavailable {
		t.Fatalf("mutation = %+v", mutation)
	}
	proxies, err := service.Proxies(context.Background())
	if err != nil {
		t.Fatalf("Proxies() error = %v", err)
	}
	if len(proxies) != 1 || proxies[0].ID != "proxy-1" {
		t.Fatalf("saved proxies = %+v", proxies)
	}
	if proxies[0].ApplyState != ProxyApplyStatePendingCreate {
		t.Fatalf("saved proxy apply state = %q", proxies[0].ApplyState)
	}
	status, err := service.Status(context.Background())
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if status.Available || status.State != "unavailable" {
		t.Fatalf("status = %+v", status)
	}
}

func TestReconcileAggregatesResetSafeCounters(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 7, 24, 1, 0, 0, 0, time.UTC)
	agent := &fakeAgent{snapshots: []agentclient.NetworkSnapshot{
		networkFixture(at, 100, 200, 10, 20),
		networkFixture(at.Add(time.Minute), 130, 250, 15, 27),
		networkFixture(at.Add(2*time.Minute), 3, 4, 2, 3),
		networkFixture(at.Add(3*time.Minute), 13, 14, 12, 13),
	}}
	service, _, _ := newNetworkTestService(t, agent)
	if _, err := service.Create(context.Background(), CreateInput{
		ID:            "proxy-1",
		Name:          "Primary",
		LineID:        "line-1",
		Enabled:       true,
		Mode:          agentclient.ProxyModeSOCKS5,
		ListenAddress: "127.0.0.1",
		ListenPort:    1080,
	}); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	service.Reconcile(context.Background())
	service.Reconcile(context.Background())
	service.Reconcile(context.Background())

	status, err := service.Status(context.Background())
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	usage := usageByScope(status.TodayUsage)
	if usage["line:line-1"] != [2]uint64{40, 60} {
		t.Fatalf("line usage = %+v", usage["line:line-1"])
	}
	if usage["proxy:proxy-1"] != [2]uint64{15, 17} {
		t.Fatalf("proxy usage = %+v", usage["proxy:proxy-1"])
	}
}

func TestRunPerformsOneStartupAttemptAndStopsWithContext(t *testing.T) {
	t.Parallel()

	agent := &fakeAgent{err: errors.New("unavailable")}
	service, _, _ := newNetworkTestService(t, agent)
	service.interval = time.Hour
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- service.Run(ctx)
	}()
	deadline := time.After(time.Second)
	for {
		agent.mu.Lock()
		calls := len(agent.received)
		agent.mu.Unlock()
		if calls == 1 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("Run() did not perform startup reconcile")
		case <-time.After(time.Millisecond):
		}
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	agent.mu.Lock()
	defer agent.mu.Unlock()
	if len(agent.received) != 1 {
		t.Fatalf("agent calls = %d, want one bounded startup attempt", len(agent.received))
	}
}

func TestRunUsesGetForNormalPeriodicRefresh(t *testing.T) {
	t.Parallel()

	service, _, agent := newNetworkTestService(t, nil)
	service.interval = 5 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- service.Run(ctx)
	}()
	waitForAgentCalls(t, agent, 1, 1)
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	agent.mu.Lock()
	defer agent.mu.Unlock()
	if len(agent.received) != 1 {
		t.Fatalf("PUT calls = %d, want startup PUT only", len(agent.received))
	}
	if agent.networkCalls < 1 {
		t.Fatalf("GET calls = %d, want at least one periodic refresh", agent.networkCalls)
	}
}

func TestDirtyApplyRetriesAreBoundedThenUseGet(t *testing.T) {
	t.Parallel()

	agent := &fakeAgent{err: errors.New("agent unavailable")}
	service, _, _ := newNetworkTestService(t, agent)
	service.interval = 5 * time.Millisecond
	service.maxApplyAttempts = 2
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- service.Run(ctx)
	}()
	waitForAgentCalls(t, agent, 2, 2)
	time.Sleep(25 * time.Millisecond)
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	agent.mu.Lock()
	defer agent.mu.Unlock()
	if len(agent.received) != 2 {
		t.Fatalf("PUT calls = %d, want bounded maximum 2", len(agent.received))
	}
	if agent.networkCalls <= 2 {
		t.Fatalf("GET calls = %d, want GET-only refresh after exhaustion", agent.networkCalls)
	}
}

func TestFailedPutRefreshesRuntimeAndKeepsPendingConfiguration(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 7, 24, 5, 0, 0, 0, time.UTC)
	agent := &fakeAgent{
		putErr:    errors.New("PUT unavailable"),
		snapshots: []agentclient.NetworkSnapshot{networkFixture(at, 100, 200, 10, 20)},
	}
	service, _, _ := newNetworkTestService(t, agent)
	mutation, err := service.Create(context.Background(), CreateInput{
		ID:            "proxy-1",
		Name:          "Pending",
		LineID:        "line-1",
		Enabled:       true,
		Mode:          agentclient.ProxyModeSOCKS5,
		ListenAddress: "127.0.0.1",
		ListenPort:    1080,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if mutation.Applied ||
		mutation.Status != ApplyStatusAgentUnavailable ||
		mutation.Proxy.ApplyState != ProxyApplyStatePendingCreate {
		t.Fatalf("mutation = %+v", mutation)
	}
	status, err := service.Status(context.Background())
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if !status.Available ||
		status.Stale ||
		!status.ApplyPending ||
		len(status.Proxies) != 1 ||
		status.Proxies[0].ID != "proxy-1" {
		t.Fatalf("status = %+v", status)
	}
}

func TestCorruptStoredPasswordDoesNotBlockGetStatus(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 7, 24, 6, 0, 0, 0, time.UTC)
	agent := &fakeAgent{snapshots: []agentclient.NetworkSnapshot{
		networkFixture(at, 100, 200, 10, 20),
	}}
	service, repository, _ := newNetworkTestService(t, agent)
	if _, err := repository.CreateProxyInstance(
		context.Background(),
		store.ProxyInstanceRecord{
			ID:                 "proxy-corrupt",
			Name:               "Corrupt",
			LineID:             "line-1",
			Enabled:            true,
			Mode:               string(agentclient.ProxyModeSOCKS5),
			ListenAddress:      "127.0.0.1",
			ListenPort:         1080,
			AuthEnabled:        true,
			Username:           "user",
			PasswordNonce:      []byte("invalid"),
			PasswordCiphertext: []byte("invalid"),
		},
	); err != nil {
		t.Fatalf("CreateProxyInstance() error = %v", err)
	}
	result := service.Reconcile(context.Background())
	if result.Applied || result.Status != ApplyStatusRuntimeUnavailable {
		t.Fatalf("Reconcile() = %+v", result)
	}
	agent.mu.Lock()
	putCalls := len(agent.received)
	getCalls := agent.networkCalls
	agent.mu.Unlock()
	if putCalls != 0 || getCalls != 1 {
		t.Fatalf("PUT calls=%d GET calls=%d, want 0/1", putCalls, getCalls)
	}
	status, err := service.Status(context.Background())
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if !status.Available || len(status.Lines) != 1 {
		t.Fatalf("status = %+v", status)
	}
}

func TestFailedDeleteRemainsVisibleAsPendingWithLastRuntime(t *testing.T) {
	t.Parallel()

	service, _, agent := newNetworkTestService(t, nil)
	created, err := service.Create(context.Background(), CreateInput{
		ID:            "proxy-1",
		Name:          "Primary",
		LineID:        "line-1",
		Enabled:       true,
		Mode:          agentclient.ProxyModeSOCKS5,
		ListenAddress: "127.0.0.1",
		ListenPort:    1080,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	at := time.Date(2026, 7, 24, 7, 0, 0, 0, time.UTC)
	agent.mu.Lock()
	agent.putErr = errors.New("apply unavailable")
	agent.snapshots = []agentclient.NetworkSnapshot{
		networkFixture(at, 100, 200, 10, 20),
	}
	agent.mu.Unlock()

	deleted, err := service.Delete(
		context.Background(),
		created.Proxy.ID,
		created.Proxy.Revision,
	)
	if err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if deleted.Applied || deleted.Status != ApplyStatusAgentUnavailable {
		t.Fatalf("Delete() = %+v", deleted)
	}
	proxies, err := service.Proxies(context.Background())
	if err != nil {
		t.Fatalf("Proxies() error = %v", err)
	}
	if len(proxies) != 1 ||
		proxies[0].ID != "proxy-1" ||
		proxies[0].ApplyState != ProxyApplyStatePendingDelete {
		t.Fatalf("proxies = %+v", proxies)
	}
	status, err := service.Status(context.Background())
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if !status.ApplyPending ||
		len(status.Proxies) != 1 ||
		status.Proxies[0].ID != "proxy-1" {
		t.Fatalf("status = %+v", status)
	}
}

func TestEnabledProxyCannotReconcileAsDisabled(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 7, 24, 8, 0, 0, 0, time.UTC)
	disabled := disabledProxySnapshot(at)
	agent := &fakeAgent{snapshots: []agentclient.NetworkSnapshot{disabled, disabled}}
	service, _, _ := newNetworkTestService(t, agent)
	mutation, err := service.Create(context.Background(), CreateInput{
		ID:            "proxy-1",
		Name:          "Primary",
		LineID:        "line-1",
		Enabled:       true,
		Mode:          agentclient.ProxyModeSOCKS5,
		ListenAddress: "127.0.0.1",
		ListenPort:    1080,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if mutation.Applied ||
		mutation.Status != ApplyStatusAgentRejected ||
		mutation.Proxy.ApplyState != ProxyApplyStatePendingCreate {
		t.Fatalf("mutation = %+v", mutation)
	}
}

func TestProtocolErrorIsAgentRejected(t *testing.T) {
	t.Parallel()

	agent := &fakeAgent{putErr: fmt.Errorf("%w: invalid response", agentclient.ErrProtocol)}
	service, _, _ := newNetworkTestService(t, agent)
	mutation, err := service.Create(context.Background(), CreateInput{
		ID:            "proxy-1",
		Name:          "Primary",
		LineID:        "line-1",
		Mode:          agentclient.ProxyModeHTTP,
		ListenAddress: "127.0.0.1",
		ListenPort:    8080,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if mutation.Status != ApplyStatusAgentRejected {
		t.Fatalf("mutation status = %q, want %q", mutation.Status, ApplyStatusAgentRejected)
	}
}

func TestUnavailableRefreshPreservesLastSnapshot(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 7, 24, 9, 0, 0, 0, time.UTC)
	agent := &fakeAgent{snapshots: []agentclient.NetworkSnapshot{
		networkFixture(at, 100, 200, 10, 20),
	}}
	service, _, _ := newNetworkTestService(t, agent)
	if err := service.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	agent.mu.Lock()
	agent.networkErr = errors.New("agent unavailable")
	agent.mu.Unlock()
	if err := service.Refresh(context.Background()); err == nil {
		t.Fatal("Refresh() error = nil, want unavailable")
	}
	status, err := service.Status(context.Background())
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if status.Available ||
		!status.Stale ||
		len(status.Lines) != 1 ||
		len(status.Proxies) != 1 {
		t.Fatalf("status = %+v", status)
	}
}

func TestDisconnectedStaleInterfaceDoesNotHideConnectedLineUsage(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 7, 24, 10, 0, 0, 0, time.UTC)
	agent := &fakeAgent{snapshots: []agentclient.NetworkSnapshot{
		lineSnapshot(at, networkLine("line-a", true, "wwan0", 100)),
		lineSnapshot(
			at.Add(time.Minute),
			networkLine("line-a", false, "wwan0", 150),
			networkLine("line-b", true, "wwan0", 150),
		),
		lineSnapshot(at.Add(2*time.Minute), networkLine("line-b", true, "wwan0", 200)),
		lineSnapshot(at.Add(3*time.Minute), networkLine("line-b", true, "wwan0", 220)),
	}}
	service, repository, _ := newNetworkTestService(t, agent)
	repository.lines = []store.LineSummary{
		{ID: "line-a", EndpointID: "line-a"},
		{ID: "line-b", EndpointID: "line-b"},
	}
	for range 4 {
		if err := service.Refresh(context.Background()); err != nil {
			t.Fatalf("Refresh() error = %v", err)
		}
	}
	status, err := service.Status(context.Background())
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	usage := usageByScope(status.TodayUsage)
	if _, exists := usage["line:line-a"]; exists {
		t.Fatalf("line-a usage must be reset: %+v", usage)
	}
	if usage["line:line-b"] != [2]uint64{70, 70} {
		t.Fatalf("line-b usage = %+v, want 70/70", usage["line:line-b"])
	}
}

func TestStatusUsesConfiguredAccountingLocation(t *testing.T) {
	t.Parallel()

	location := time.FixedZone("UTC+9", 9*60*60)
	first := time.Date(2026, 7, 24, 23, 59, 30, 0, location)
	agent := &fakeAgent{snapshots: []agentclient.NetworkSnapshot{
		lineSnapshot(first, agentclient.NetworkLine{
			LineID:    "line-1",
			Connected: true,
			Interface: "wwan0",
			DNS:       []string{},
			RXBytes:   10,
			TXBytes:   20,
		}),
		lineSnapshot(first.Add(time.Minute), agentclient.NetworkLine{
			LineID:    "line-1",
			Connected: true,
			Interface: "wwan0",
			DNS:       []string{},
			RXBytes:   20,
			TXBytes:   40,
		}),
	}}
	service, _, _ := newNetworkTestService(t, agent)
	service.location = location
	service.now = func() time.Time {
		return time.Date(2026, 7, 25, 0, 30, 0, 0, location)
	}
	for range 2 {
		if err := service.Refresh(context.Background()); err != nil {
			t.Fatalf("Refresh() error = %v", err)
		}
	}
	status, err := service.Status(context.Background())
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if status.TodayTotal != (UsageTotal{RXBytes: 5, TXBytes: 10}) {
		t.Fatalf("today total = %+v", status.TodayTotal)
	}
	if status.MonthTotal != (UsageTotal{RXBytes: 10, TXBytes: 20}) {
		t.Fatalf("month total = %+v", status.MonthTotal)
	}
}

func newNetworkTestService(
	t *testing.T,
	agent *fakeAgent,
) (*Service, *networkTestRepository, *fakeAgent) {
	t.Helper()
	database, err := platformdb.Open(context.Background(), platformdb.Config{
		TargetPath: filepath.Join(t.TempDir(), "modemdeck.db"),
	})
	if err != nil {
		t.Fatalf("database.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	persistentStore, err := store.New(database)
	if err != nil {
		t.Fatalf("store.New() error = %v", err)
	}
	repository := &networkTestRepository{
		Store: persistentStore,
		lines: []store.LineSummary{{
			ID:         "line-1",
			EndpointID: "line-1",
		}},
	}
	secrets, err := secretbox.New([]byte(strings.Repeat("k", secretbox.KeySize)))
	if err != nil {
		t.Fatalf("secretbox.New() error = %v", err)
	}
	if agent == nil {
		agent = &fakeAgent{}
	}
	service, err := New(repository, secrets, agent, Options{
		Location: time.UTC,
		Now: func() time.Time {
			return time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
		},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return service, repository, agent
}

func disabledProxySnapshot(observedAt time.Time) agentclient.NetworkSnapshot {
	return agentclient.NetworkSnapshot{
		BootEpoch:  "boot-1",
		ObservedAt: observedAt,
		Lines:      []agentclient.NetworkLine{},
		Proxies: []agentclient.NetworkProxy{{
			ID:            "proxy-1",
			LineID:        "line-1",
			State:         agentclient.ProxyStateDisabled,
			Mode:          agentclient.ProxyModeSOCKS5,
			ListenAddress: "127.0.0.1",
			ListenPort:    1080,
		}},
	}
}

func lineSnapshot(
	observedAt time.Time,
	lines ...agentclient.NetworkLine,
) agentclient.NetworkSnapshot {
	return agentclient.NetworkSnapshot{
		BootEpoch:  "boot-1",
		ObservedAt: observedAt,
		Lines:      lines,
		Proxies:    []agentclient.NetworkProxy{},
	}
}

func networkLine(
	id string,
	connected bool,
	interfaceName string,
	counter uint64,
) agentclient.NetworkLine {
	return agentclient.NetworkLine{
		LineID:    id,
		Connected: connected,
		Interface: interfaceName,
		DNS:       []string{},
		RXBytes:   counter,
		TXBytes:   counter,
	}
}

func networkFixture(
	observedAt time.Time,
	lineRX, lineTX, proxyDown, proxyUp uint64,
) agentclient.NetworkSnapshot {
	startedAt := observedAt.Add(-time.Hour)
	return agentclient.NetworkSnapshot{
		BootEpoch:  "boot-1",
		ObservedAt: observedAt,
		Lines: []agentclient.NetworkLine{{
			LineID:    "line-1",
			Connected: true,
			Interface: "wwan0",
			DNS:       []string{"1.1.1.1"},
			RXBytes:   lineRX,
			TXBytes:   lineTX,
		}},
		Proxies: []agentclient.NetworkProxy{{
			ID:            "proxy-1",
			LineID:        "line-1",
			State:         agentclient.ProxyStateRunning,
			Running:       true,
			Mode:          agentclient.ProxyModeSOCKS5,
			ListenAddress: "127.0.0.1",
			ListenPort:    1080,
			Interface:     "wwan0",
			RuntimeEpoch:  "proxy-runtime-1",
			StartedAt:     &startedAt,
			BytesDown:     proxyDown,
			BytesUp:       proxyUp,
		}},
	}
}

func usageByScope(usage []Usage) map[string][2]uint64 {
	result := make(map[string][2]uint64, len(usage))
	for _, item := range usage {
		result[string(item.ScopeKind)+":"+item.ScopeID] = [2]uint64{
			item.RXBytes,
			item.TXBytes,
		}
	}
	return result
}

func assertNetworkErrorCode(t *testing.T, err error, code ErrorCode, field string) {
	t.Helper()
	var operationError *Error
	if !errors.As(err, &operationError) ||
		operationError.Code != code ||
		operationError.Field != field {
		t.Fatalf("error = %#v, want code=%q field=%q", err, code, field)
	}
}
