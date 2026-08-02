package main

import (
	"context"
	"testing"
	"time"
)

type testRadioReconciler struct {
	events     chan struct{}
	reconciled chan struct{}
}

func (r *testRadioReconciler) SubscribeRadioLifecycle(context.Context) (<-chan struct{}, error) {
	return r.events, nil
}

func (r *testRadioReconciler) ReconcileRadioState(context.Context) error {
	r.reconciled <- struct{}{}
	return nil
}

func TestParseSocketMode(t *testing.T) {
	mode, err := parseSocketMode("0660")
	if err != nil {
		t.Fatalf("parse mode: %v", err)
	}
	if mode.Perm() != 0o660 {
		t.Fatalf("mode = %04o", mode.Perm())
	}

	for _, value := range []string{"", "888", "1000", "-1"} {
		if _, err := parseSocketMode(value); err == nil {
			t.Fatalf("parseSocketMode(%q) succeeded", value)
		}
	}
}

func TestRunRadioReconcilerRunsAtStartupAndOnLifecycleEvent(t *testing.T) {
	reconciler := &testRadioReconciler{
		events:     make(chan struct{}, 1),
		reconciled: make(chan struct{}, 2),
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		runRadioReconciler(ctx, reconciler)
	}()

	waitForReconcile(t, reconciler.reconciled)
	reconciler.events <- struct{}{}
	waitForReconcile(t, reconciler.reconciled)
	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("runRadioReconciler did not stop after cancellation")
	}
}

func waitForReconcile(t *testing.T, reconciled <-chan struct{}) {
	t.Helper()
	select {
	case <-reconciled:
	case <-time.After(time.Second):
		t.Fatal("radio reconcile was not triggered")
	}
}
