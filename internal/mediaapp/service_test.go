package mediaapp

import (
	"context"
	"errors"
	"testing"

	"github.com/human-agent65535/modemdeck/internal/callmedia"
	"github.com/human-agent65535/modemdeck/internal/communication"
	"github.com/human-agent65535/modemdeck/internal/store"
)

func TestExchangeRequiresAuthoritativeActiveMediaCall(t *testing.T) {
	t.Parallel()

	core := &fakeCore{}
	service, err := New(
		fakeRefresher{},
		fakeCallStore{call: store.Call{ID: "call-1", Phase: "ringing", MediaAvailable: true}},
		core,
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if _, err := service.Exchange(context.Background(), "call-1", "offer"); !errors.Is(err, ErrNotActive) {
		t.Fatalf("Exchange() error = %v, want ErrNotActive", err)
	}
	if core.exchanges != 0 {
		t.Fatalf("core exchanges = %d, want 0", core.exchanges)
	}
}

func TestExchangeReturnsCoreAnswerForActiveMediaCall(t *testing.T) {
	t.Parallel()

	core := &fakeCore{answer: "answer"}
	service, err := New(
		fakeRefresher{},
		fakeCallStore{call: store.Call{ID: "call-1", Phase: "active", MediaAvailable: true}},
		core,
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	answer, err := service.Exchange(context.Background(), "call-1", "offer")
	if err != nil {
		t.Fatalf("Exchange() error = %v", err)
	}
	if answer != "answer" || core.exchanges != 1 {
		t.Fatalf("answer = %q, exchanges = %d", answer, core.exchanges)
	}
}

func TestReconcileAuthoritativeCallsForwardsOnlyOpaqueIDs(t *testing.T) {
	t.Parallel()

	core := &fakeCore{}
	service, err := New(fakeRefresher{}, fakeCallStore{}, core)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := service.ReconcileAuthoritativeCalls(context.Background(), []store.Call{
		{ID: "call-1", RemoteNumber: "+818012345678"},
		{ID: "call-2", RemoteNumber: "+819012345678"},
	}); err != nil {
		t.Fatalf("ReconcileAuthoritativeCalls() error = %v", err)
	}
	if len(core.reconciled) != 2 ||
		core.reconciled[0] != "call-1" ||
		core.reconciled[1] != "call-2" {
		t.Fatalf("reconciled IDs = %v", core.reconciled)
	}
}

type fakeRefresher struct {
	err error
}

func (f fakeRefresher) Refresh(context.Context) (communication.Status, error) {
	return communication.Status{}, f.err
}

type fakeCallStore struct {
	call store.Call
	err  error
}

func (f fakeCallStore) CallByID(context.Context, string) (store.Call, error) {
	return f.call, f.err
}

type fakeCore struct {
	answer     string
	err        error
	exchanges  int
	reconciled []string
}

func (f *fakeCore) Exchange(context.Context, callmedia.Offer) (callmedia.ExchangeResult, error) {
	f.exchanges++
	return callmedia.ExchangeResult{AnswerSDP: f.answer}, f.err
}

func (*fakeCore) CloseCall(context.Context, string) error {
	return nil
}

func (f *fakeCore) ReconcileActiveCalls(_ context.Context, callIDs []string) error {
	f.reconciled = append([]string(nil), callIDs...)
	return nil
}

func (*fakeCore) Close(context.Context) error {
	return nil
}
