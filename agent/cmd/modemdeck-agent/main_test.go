package main

import (
	"context"
	"testing"
	"time"

	"github.com/human-agent65535/modemdeck/agent/internal/domain"
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

type testQDC507VoiceProvider struct {
	events      chan struct{}
	changes     chan struct{}
	provisioned chan []domain.Line
	lines       []domain.Line
}

func (p *testQDC507VoiceProvider) SubscribeModemLifecycle(context.Context) (<-chan struct{}, error) {
	return p.events, nil
}

func (p *testQDC507VoiceProvider) QDC507VoiceLines(context.Context) ([]domain.Line, error) {
	return append([]domain.Line(nil), p.lines...), nil
}

func (p *testQDC507VoiceProvider) EnsureQDC507VoiceUSB(
	_ context.Context,
	lines []domain.Line,
) (bool, error) {
	p.provisioned <- append([]domain.Line(nil), lines...)
	return false, nil
}

func (p *testQDC507VoiceProvider) QDC507VoiceRuntimeChanged() {
	p.changes <- struct{}{}
}

type testQDC507VoiceRuntime struct {
	reconciled chan []domain.Line
}

func (r *testQDC507VoiceRuntime) Reconcile(
	_ context.Context,
	lines []domain.Line,
) (bool, error) {
	r.reconciled <- append([]domain.Line(nil), lines...)
	return true, nil
}

func TestRunQDC507VoiceReconcilerRunsAtStartupAndModemLifecycle(t *testing.T) {
	provider := &testQDC507VoiceProvider{
		events:      make(chan struct{}, 1),
		changes:     make(chan struct{}, 2),
		provisioned: make(chan []domain.Line, 2),
		lines:       []domain.Line{{ID: "line-qdc507"}},
	}
	runtime := &testQDC507VoiceRuntime{reconciled: make(chan []domain.Line, 2)}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		runQDC507VoiceReconciler(ctx, provider, runtime)
	}()

	waitForQDC507VoiceReconcile(t, provider.provisioned, runtime.reconciled, provider.changes)
	provider.events <- struct{}{}
	waitForQDC507VoiceReconcile(t, provider.provisioned, runtime.reconciled, provider.changes)
	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("runQDC507VoiceReconciler did not stop after cancellation")
	}
}

func waitForQDC507VoiceReconcile(
	t *testing.T,
	provisioned <-chan []domain.Line,
	reconciled <-chan []domain.Line,
	changes <-chan struct{},
) {
	t.Helper()
	select {
	case lines := <-provisioned:
		if len(lines) != 1 || lines[0].ID != "line-qdc507" {
			t.Fatalf("provisioned lines = %+v", lines)
		}
	case <-time.After(time.Second):
		t.Fatal("QDC507 USB provisioning was not triggered")
	}
	select {
	case lines := <-reconciled:
		if len(lines) != 1 || lines[0].ID != "line-qdc507" {
			t.Fatalf("reconciled lines = %+v", lines)
		}
	case <-time.After(time.Second):
		t.Fatal("QDC507 voice reconcile was not triggered")
	}
	select {
	case <-changes:
	case <-time.After(time.Second):
		t.Fatal("QDC507 voice runtime change was not published")
	}
}
