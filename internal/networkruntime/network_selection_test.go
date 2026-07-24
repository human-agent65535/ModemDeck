package networkruntime

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/human-agent65535/modemdeck/internal/agentclient"
	"github.com/human-agent65535/modemdeck/internal/store"
)

func TestNetworkSelectionReturnsSavedPolicyWhenAgentIsUnavailable(t *testing.T) {
	t.Parallel()

	service, repository, agent := newNetworkTestService(t, nil)
	policy, err := repository.EnsureNetworkSelectionPolicy(context.Background(), "line-1")
	if err != nil {
		t.Fatalf("EnsureNetworkSelectionPolicy() error = %v", err)
	}
	if _, err := repository.UpdateNetworkSelectionPolicy(
		context.Background(),
		"line-1",
		"manual",
		"44010",
		policy.Revision,
	); err != nil {
		t.Fatalf("UpdateNetworkSelectionPolicy() error = %v", err)
	}
	agent.snapshotErr = errors.New("dial unix /run/modemdeck/agent.sock: refused")

	selection, err := service.NetworkSelection(context.Background(), "line-1")
	if err != nil {
		t.Fatalf("NetworkSelection() error = %v", err)
	}
	if selection.Mode != agentclient.NetworkSelectionModeManual ||
		selection.OperatorCode != "44010" ||
		selection.Revision != 2 ||
		selection.Registration.Known {
		t.Fatalf("selection = %+v", selection)
	}
}

func TestNetworkSelectionUnknownLineDoesNotCreatePolicy(t *testing.T) {
	t.Parallel()

	service, repository, agent := newNetworkTestService(t, nil)
	agent.fullSnapshot = agentclient.Snapshot{Lines: []agentclient.Line{{
		ID:                   "line-real",
		SIMPresent:           true,
		SavedPolicySupported: true,
	}}}

	_, err := service.NetworkSelection(context.Background(), "line-random")
	assertNetworkRuntimeError(t, err, CodeNotFound)
	if _, err := repository.NetworkSelectionPolicy(
		context.Background(),
		"line-random",
	); !errors.Is(err, store.ErrNetworkSelectionPolicyNotFound) {
		t.Fatalf("random line policy error = %v, want not found", err)
	}
	policies, err := repository.NetworkSelectionPolicies(context.Background())
	if err != nil {
		t.Fatalf("NetworkSelectionPolicies() error = %v", err)
	}
	if len(policies) != 0 {
		t.Fatalf("policies = %+v, want none", policies)
	}
}

func TestNetworkSelectionUnknownLineDoesNotPersistWhenAgentUnavailable(t *testing.T) {
	t.Parallel()

	service, repository, agent := newNetworkTestService(t, nil)
	agent.snapshotErr = errors.New("agent unavailable")

	_, err := service.NetworkSelection(context.Background(), "line-random")
	assertNetworkRuntimeError(t, err, CodeUnavailable)
	if _, err := repository.NetworkSelectionPolicy(
		context.Background(),
		"line-random",
	); !errors.Is(err, store.ErrNetworkSelectionPolicyNotFound) {
		t.Fatalf("random line policy error = %v, want not found", err)
	}
}

func TestReconcileOnlyPersistsSIMBackedStableLines(t *testing.T) {
	t.Parallel()

	service, repository, agent := newNetworkTestService(t, nil)
	agent.fullSnapshot = agentclient.Snapshot{Lines: []agentclient.Line{
		{
			ID:                   "line-no-sim",
			SIMPresent:           false,
			SavedPolicySupported: true,
		},
		{
			ID:                   "line-unstable",
			SIMPresent:           true,
			SavedPolicySupported: false,
		},
		{
			ID:                   "line-stable",
			SIMPresent:           true,
			SavedPolicySupported: true,
		},
	}}

	result := service.Reconcile(context.Background())
	if !result.Applied || result.Status != ApplyStatusApplied {
		t.Fatalf("Reconcile() = %+v", result)
	}
	policies, err := repository.NetworkSelectionPolicies(context.Background())
	if err != nil {
		t.Fatalf("NetworkSelectionPolicies() error = %v", err)
	}
	if len(policies) != 1 || policies[0].LineID != "line-stable" {
		t.Fatalf("policies = %+v", policies)
	}
	agent.mu.Lock()
	defer agent.mu.Unlock()
	if agent.fullSnapshotCalls != 1 {
		t.Fatalf("full Snapshot calls = %d, want 1", agent.fullSnapshotCalls)
	}
	if len(agent.selectionCalls) != 1 ||
		agent.selectionCalls[0].LineID != "line-stable" {
		t.Fatalf("selection calls = %+v", agent.selectionCalls)
	}
}

func TestUpdateNetworkSelectionPersistsDesiredStateBeforeAgentCall(t *testing.T) {
	t.Parallel()

	service, repository, agent := newNetworkTestService(t, nil)
	if _, err := repository.EnsureNetworkSelectionPolicy(
		context.Background(),
		"line-1",
	); err != nil {
		t.Fatalf("EnsureNetworkSelectionPolicy() error = %v", err)
	}
	service.setSnapshot(agentclient.NetworkSnapshot{
		BootEpoch:  "boot-1",
		ObservedAt: time.Now().UTC(),
		Lines:      []agentclient.NetworkLine{},
		Proxies:    []agentclient.NetworkProxy{},
	})
	agent.beforeSelectionApply = func(
		lineID string,
		request agentclient.ApplyNetworkSelectionRequest,
	) error {
		policy, err := repository.NetworkSelectionPolicy(context.Background(), lineID)
		if err != nil {
			return err
		}
		if policy.Mode != "manual" ||
			policy.OperatorCode != "44010" ||
			policy.Revision != 2 ||
			policy.AppliedRevision != 0 ||
			policy.LastError != "" {
			return errors.New("desired policy was not persisted before agent call")
		}
		if request.Mode != agentclient.NetworkSelectionModeManual ||
			request.OperatorCode != "44010" {
			return errors.New("agent request does not match desired policy")
		}
		return &agentclient.OperationError{
			Status:  422,
			Code:    "network_rejected",
			Message: "Operator rejected manual registration",
		}
	}

	_, err := service.UpdateNetworkSelection(
		context.Background(),
		"line-1",
		UpdateNetworkSelectionInput{
			ExpectedRevision: 1,
			Mode:             agentclient.NetworkSelectionModeManual,
			OperatorCode:     "44010",
		},
	)
	assertNetworkRuntimeError(t, err, CodeNetworkRejected)
	policy, err := repository.NetworkSelectionPolicy(context.Background(), "line-1")
	if err != nil {
		t.Fatalf("NetworkSelectionPolicy() error = %v", err)
	}
	if policy.Mode != "manual" ||
		policy.OperatorCode != "44010" ||
		policy.Revision != 2 ||
		policy.AppliedRevision != 0 ||
		policy.LastError != "Operator rejected manual registration" {
		t.Fatalf("policy = %+v", policy)
	}
}

func TestUpdateNetworkSelectionHidesUntypedAgentFailure(t *testing.T) {
	t.Parallel()

	service, repository, agent := newNetworkTestService(t, nil)
	if _, err := repository.EnsureNetworkSelectionPolicy(
		context.Background(),
		"line-1",
	); err != nil {
		t.Fatalf("EnsureNetworkSelectionPolicy() error = %v", err)
	}
	service.setSnapshot(agentclient.NetworkSnapshot{
		BootEpoch:  "boot-1",
		ObservedAt: time.Now().UTC(),
		Lines:      []agentclient.NetworkLine{},
		Proxies:    []agentclient.NetworkProxy{},
	})
	agent.selectionErrors = map[string]error{
		"line-1": errors.New("dial unix /private/internal.sock: permission denied"),
	}

	_, err := service.UpdateNetworkSelection(
		context.Background(),
		"line-1",
		UpdateNetworkSelectionInput{
			ExpectedRevision: 1,
			Mode:             agentclient.NetworkSelectionModeManual,
			OperatorCode:     "44010",
		},
	)
	assertNetworkRuntimeError(t, err, CodeUnavailable)
	policy, err := repository.NetworkSelectionPolicy(context.Background(), "line-1")
	if err != nil {
		t.Fatalf("NetworkSelectionPolicy() error = %v", err)
	}
	if policy.LastError != "Host agent is unavailable" ||
		strings.Contains(policy.LastError, "internal.sock") {
		t.Fatalf("last_error = %q", policy.LastError)
	}
}

func TestReconcileNetworkSelectionsContinuesAfterPerLineFailure(t *testing.T) {
	t.Parallel()

	service, repository, agent := newNetworkTestService(t, nil)
	for _, lineID := range []string{"line-a", "line-b"} {
		if _, err := repository.EnsureNetworkSelectionPolicy(
			context.Background(),
			lineID,
		); err != nil {
			t.Fatalf("EnsureNetworkSelectionPolicy(%q) error = %v", lineID, err)
		}
	}
	agent.fullSnapshot = agentclient.Snapshot{Lines: []agentclient.Line{
		{ID: "line-a", SIMPresent: true, SavedPolicySupported: true},
		{ID: "line-b", SIMPresent: true, SavedPolicySupported: true},
	}}
	agent.selectionErrors = map[string]error{
		"line-a": &agentclient.OperationError{
			Status:  409,
			Code:    "conflict",
			Message: "Another network operation is running",
		},
	}

	result := service.Reconcile(context.Background())
	if result.Applied || result.Status == ApplyStatusApplied {
		t.Fatalf("Reconcile() = %+v, want pending failure", result)
	}
	agent.mu.Lock()
	calls := append([]networkAgentCall(nil), agent.selectionCalls...)
	agent.mu.Unlock()
	if len(calls) != 2 ||
		calls[0].LineID != "line-a" ||
		calls[1].LineID != "line-b" {
		t.Fatalf("selection calls = %+v", calls)
	}
	lineA, err := repository.NetworkSelectionPolicy(context.Background(), "line-a")
	if err != nil {
		t.Fatalf("load line-a policy: %v", err)
	}
	lineB, err := repository.NetworkSelectionPolicy(context.Background(), "line-b")
	if err != nil {
		t.Fatalf("load line-b policy: %v", err)
	}
	if lineA.LastError != "Another network operation is running" ||
		lineA.AppliedRevision != 0 {
		t.Fatalf("line-a policy = %+v", lineA)
	}
	if lineB.AppliedRevision != lineB.Revision ||
		lineB.AppliedBootEpoch != "boot-1" ||
		lineB.LastError != "" {
		t.Fatalf("line-b policy = %+v", lineB)
	}
}

func TestNetworkOperationContextsCoverHostLimits(t *testing.T) {
	t.Parallel()

	service, repository, agent := newNetworkTestService(t, nil)
	if _, err := repository.EnsureNetworkSelectionPolicy(
		context.Background(),
		"line-1",
	); err != nil {
		t.Fatalf("EnsureNetworkSelectionPolicy() error = %v", err)
	}
	service.setSnapshot(agentclient.NetworkSnapshot{
		BootEpoch:  "boot-1",
		ObservedAt: time.Now().UTC(),
		Lines:      []agentclient.NetworkLine{},
		Proxies:    []agentclient.NetworkProxy{},
	})

	updateStarted := time.Now()
	if _, err := service.UpdateNetworkSelection(
		context.Background(),
		"line-1",
		UpdateNetworkSelectionInput{
			ExpectedRevision: 1,
			Mode:             agentclient.NetworkSelectionModeManual,
			OperatorCode:     "44010",
		},
	); err != nil {
		t.Fatalf("UpdateNetworkSelection() error = %v", err)
	}
	scanStarted := time.Now()
	if _, err := service.ScanNetworks(context.Background(), "line-1"); err != nil {
		t.Fatalf("ScanNetworks() error = %v", err)
	}

	agent.mu.Lock()
	selectionCalls := append([]networkAgentCall(nil), agent.selectionCalls...)
	scanCalls := append([]networkAgentCall(nil), agent.scanCalls...)
	agent.mu.Unlock()
	if len(selectionCalls) != 1 ||
		!selectionCalls[0].HasDeadline ||
		selectionCalls[0].Deadline.Sub(updateStarted) < 49*time.Second ||
		selectionCalls[0].Deadline.Sub(updateStarted) > 51*time.Second {
		t.Fatalf("selection calls = %+v", selectionCalls)
	}
	if len(scanCalls) != 1 ||
		!scanCalls[0].HasDeadline ||
		scanCalls[0].Deadline.Sub(scanStarted) < 124*time.Second ||
		scanCalls[0].Deadline.Sub(scanStarted) > 126*time.Second {
		t.Fatalf("scan calls = %+v", scanCalls)
	}
}

func TestNetworkSelectionAgentConflictIsNotRevisionConflict(t *testing.T) {
	t.Parallel()

	translated := translateNetworkSelectionAgentError(&agentclient.OperationError{
		Status:  409,
		Code:    "conflict",
		Message: "Network scan is already running",
	})
	assertNetworkRuntimeError(t, translated, CodeOperationConflict)
	assertNetworkRuntimeError(
		t,
		classifyNetworkSelectionStoreError(store.ErrNetworkSelectionRevisionConflict),
		CodeConflict,
	)
}

func assertNetworkRuntimeError(t *testing.T, err error, code ErrorCode) {
	t.Helper()
	var runtimeError *Error
	if !errors.As(err, &runtimeError) {
		t.Fatalf("error = %v, want network runtime error", err)
	}
	if runtimeError.Code != code {
		t.Fatalf("error code = %q, want %q (error: %v)", runtimeError.Code, code, err)
	}
}
