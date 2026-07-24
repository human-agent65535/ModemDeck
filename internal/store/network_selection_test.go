package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestNetworkSelectionPolicyReadDoesNotCreateDefault(t *testing.T) {
	t.Parallel()

	repository := newNetworkTestStore(t)
	if _, err := repository.NetworkSelectionPolicy(
		context.Background(),
		"line-random",
	); !errors.Is(err, ErrNetworkSelectionPolicyNotFound) {
		t.Fatalf("NetworkSelectionPolicy() error = %v, want not found", err)
	}
	policies, err := repository.NetworkSelectionPolicies(context.Background())
	if err != nil {
		t.Fatalf("NetworkSelectionPolicies() error = %v", err)
	}
	if len(policies) != 0 {
		t.Fatalf("policies = %+v, want none", policies)
	}
}

func TestNetworkSelectionDesiredStateApplyLifecycle(t *testing.T) {
	t.Parallel()

	repository := newNetworkTestStore(t)
	policy, err := repository.EnsureNetworkSelectionPolicy(
		context.Background(),
		"line-1",
	)
	if err != nil {
		t.Fatalf("EnsureNetworkSelectionPolicy() error = %v", err)
	}
	if policy.Mode != "auto" ||
		policy.OperatorCode != "" ||
		policy.Revision != 1 ||
		policy.AppliedRevision != 0 {
		t.Fatalf("default policy = %+v", policy)
	}
	firstAppliedAt := time.Date(2026, 7, 24, 1, 2, 3, 0, time.UTC)
	if stale, err := repository.MarkNetworkSelectionApplied(
		context.Background(),
		"line-1",
		1,
		"boot-1",
		firstAppliedAt,
	); err != nil || stale {
		t.Fatalf("MarkNetworkSelectionApplied() stale=%t error=%v", stale, err)
	}
	if err := repository.MarkNetworkSelectionApplyFailed(
		context.Background(),
		"line-1",
		1,
		"old failure",
	); err != nil {
		t.Fatalf("MarkNetworkSelectionApplyFailed() error = %v", err)
	}

	desired, err := repository.UpdateNetworkSelectionPolicy(
		context.Background(),
		"line-1",
		"manual",
		"44010",
		1,
	)
	if err != nil {
		t.Fatalf("UpdateNetworkSelectionPolicy() error = %v", err)
	}
	if desired.Mode != "manual" ||
		desired.OperatorCode != "44010" ||
		desired.Revision != 2 ||
		desired.AppliedRevision != 1 ||
		desired.AppliedBootEpoch != "boot-1" ||
		desired.AppliedAt == "" ||
		desired.LastError != "" {
		t.Fatalf("desired policy = %+v", desired)
	}
	if _, err := repository.UpdateNetworkSelectionPolicy(
		context.Background(),
		"line-1",
		"auto",
		"",
		1,
	); !errors.Is(err, ErrNetworkSelectionRevisionConflict) {
		t.Fatalf("stale update error = %v, want revision conflict", err)
	}
	if err := repository.MarkNetworkSelectionApplyFailed(
		context.Background(),
		"line-1",
		2,
		"operator rejected registration",
	); err != nil {
		t.Fatalf("MarkNetworkSelectionApplyFailed() error = %v", err)
	}
	failed, err := repository.NetworkSelectionPolicy(context.Background(), "line-1")
	if err != nil {
		t.Fatalf("NetworkSelectionPolicy() error = %v", err)
	}
	if failed.LastError != "operator rejected registration" ||
		failed.AppliedRevision != 1 {
		t.Fatalf("failed policy = %+v", failed)
	}
	secondAppliedAt := firstAppliedAt.Add(time.Hour)
	if stale, err := repository.MarkNetworkSelectionApplied(
		context.Background(),
		"line-1",
		2,
		"boot-2",
		secondAppliedAt,
	); err != nil || stale {
		t.Fatalf("MarkNetworkSelectionApplied() stale=%t error=%v", stale, err)
	}
	applied, err := repository.NetworkSelectionPolicy(context.Background(), "line-1")
	if err != nil {
		t.Fatalf("NetworkSelectionPolicy() error = %v", err)
	}
	if applied.AppliedRevision != 2 ||
		applied.AppliedBootEpoch != "boot-2" ||
		applied.LastError != "" {
		t.Fatalf("applied policy = %+v", applied)
	}
}
