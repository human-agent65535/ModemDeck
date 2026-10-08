package callmedia

import (
	"context"
	"errors"
	"io"
	"testing"
)

func TestCoreCancellationStopsEndpointPreparation(t *testing.T) {
	opener := &blockingEndpointOpener{started: make(chan struct{})}
	core, err := New(Options{
		EndpointOpener: opener,
		CodecFactory:   &fakeCodecFactory{},
	})
	if err != nil {
		t.Fatal(err)
	}
	authorizeCall(t, core, "call-canceled")
	exchanged := make(chan error, 1)
	go func() {
		_, exchangeErr := core.OpenSocket(context.Background(), ActiveCall{ID: "call-canceled", State: CallStateActive}, "owner", newFakeSocket())
		exchanged <- exchangeErr
	}()
	receive(t, opener.started)

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	if err := core.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if err := receive(t, exchanged); !errors.Is(err, ErrCoreClosed) {
		t.Fatalf("closed-core exchange error = %v", err)
	}
}

func TestAuthoritativeCallRemovalCancelsEndpointPreparation(t *testing.T) {
	opener := &blockingEndpointOpener{started: make(chan struct{})}
	core, err := New(Options{
		EndpointOpener: opener,
		CodecFactory:   &fakeCodecFactory{},
	})
	if err != nil {
		t.Fatal(err)
	}
	authorizeCall(t, core, "call-removed-opening")
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
		defer cancel()
		if err := core.Close(ctx); err != nil {
			t.Errorf("close core: %v", err)
		}
	})

	subscribed := make(chan error, 1)
	go func() {
		_, subscribeErr := core.SubscribeDuplex(context.Background(), ActiveCall{
			ID:    "call-removed-opening",
			State: CallStateActive,
		})
		subscribed <- subscribeErr
	}()
	receive(t, opener.started)

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	if err := core.ReconcileActiveCalls(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if err := receive(t, subscribed); !errors.Is(err, ErrCanceled) {
		t.Fatalf("SubscribeDuplex() error = %v, want ErrCanceled", err)
	}
	core.mu.Lock()
	_, hubRetained := core.hubs["call-removed-opening"]
	_, lifetimeRetained := core.lifetimes["call-removed-opening"]
	core.mu.Unlock()
	if hubRetained || lifetimeRetained {
		t.Fatal("removed call retained its hub or lifetime")
	}
}

func TestAuthoritativeCallRemovalClosesStartedHub(t *testing.T) {
	format := testFormat(8000)
	core, opener, _ := testCore(t, format)
	authorizeCall(t, core, "call-remote-hangup")
	subscription, err := core.SubscribeDuplex(context.Background(), ActiveCall{
		ID:    "call-remote-hangup",
		State: CallStateActive,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := subscription.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := core.ReconcileActiveCalls(context.Background(), []string{"call-remote-hangup"}); err != nil {
		t.Fatal(err)
	}
	if got := opener.endpoint.closeCalls.Load(); got != 0 {
		t.Fatalf("active endpoint close calls = %d, want 0", got)
	}
	if err := core.ReconcileActiveCalls(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if got := opener.endpoint.closeCalls.Load(); got != 1 {
		t.Fatalf("removed endpoint close calls = %d, want 1", got)
	}
	if _, err := subscription.Next(context.Background()); !errors.Is(err, io.EOF) {
		t.Fatalf("closed subscription error = %v, want EOF", err)
	}
}

func TestAuthoritativeCallRemovalPreventsLateHubRecreation(t *testing.T) {
	format := testFormat(8000)
	core, opener, _ := testCore(t, format)
	if err := core.ReconcileActiveCalls(
		context.Background(),
		[]string{"call-authoritative"},
	); err != nil {
		t.Fatal(err)
	}
	if err := core.ReconcileActiveCalls(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	_, err := core.SubscribeDuplex(context.Background(), ActiveCall{
		ID:    "call-authoritative",
		State: CallStateActive,
	})
	if !errors.Is(err, ErrCallNotActive) {
		t.Fatalf("late SubscribeDuplex() error = %v, want ErrCallNotActive", err)
	}
	if got := opener.opens.Load(); got != 0 {
		t.Fatalf("late endpoint opens = %d, want 0", got)
	}
}

func TestPreparationClosesPartialEndpointAndCodecResultsOnce(t *testing.T) {
	format := testFormat(8000)
	endpoint := newFakeEndpoint(format)
	core, err := New(Options{
		EndpointOpener: &endpointAndErrorOpener{endpoint: endpoint},
		CodecFactory:   &fakeCodecFactory{},
	})
	if err != nil {
		t.Fatal(err)
	}
	authorizeCall(t, core, "call-partial-endpoint")
	_, err = core.OpenSocket(context.Background(), ActiveCall{ID: "call-partial-endpoint", State: CallStateActive}, "owner", newFakeSocket())
	assertErrorIs(t, err, ErrEndpointUnavailable)
	if got := endpoint.closeCalls.Load(); got != 1 {
		t.Fatalf("partial endpoint close calls = %d, want 1", got)
	}
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	if err := core.Close(ctx); err != nil {
		t.Fatal(err)
	}

	endpoint = newFakeEndpoint(format)
	codecs := &codecAndErrorFactory{}
	core, err = New(Options{
		EndpointOpener: &fakeEndpointOpener{endpoint: endpoint},
		CodecFactory:   codecs,
	})
	if err != nil {
		t.Fatal(err)
	}
	authorizeCall(t, core, "call-partial-codec")
	_, err = core.OpenSocket(context.Background(), ActiveCall{ID: "call-partial-codec", State: CallStateActive}, "owner", newFakeSocket())
	assertErrorIs(t, err, ErrCodec)
	if got := endpoint.closeCalls.Load(); got != 0 {
		t.Fatalf("codec-failure endpoint close calls = %d, want shared hub retained", got)
	}
	if got := codecs.codec.closeCalls.Load(); got != 1 {
		t.Fatalf("partial codec close calls = %d, want 1", got)
	}
	subscription, err := core.SubscribeDuplex(context.Background(), ActiveCall{
		ID:    "call-partial-codec",
		State: CallStateActive,
	})
	if err != nil {
		t.Fatalf("recording subscription after browser codec failure: %v", err)
	}
	if err := subscription.Close(); err != nil {
		t.Fatal(err)
	}
	if err := core.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if got := endpoint.closeCalls.Load(); got != 1 {
		t.Fatalf("core-close endpoint close calls = %d, want 1", got)
	}
}

func TestFailedEndpointOpenCanBeRetriedForActiveCall(t *testing.T) {
	format := testFormat(8000)
	opener := &retryEndpointOpener{endpoint: newFakeEndpoint(format)}
	core, err := New(Options{
		EndpointOpener: opener,
		CodecFactory:   &fakeCodecFactory{},
	})
	if err != nil {
		t.Fatal(err)
	}
	authorizeCall(t, core, "call-open-retry")
	if _, err := core.SubscribeDuplex(context.Background(), ActiveCall{
		ID:    "call-open-retry",
		State: CallStateActive,
	}); !errors.Is(err, ErrEndpointUnavailable) {
		t.Fatalf("first SubscribeDuplex() error = %v", err)
	}
	subscription, err := core.SubscribeDuplex(context.Background(), ActiveCall{
		ID:    "call-open-retry",
		State: CallStateActive,
	})
	if err != nil {
		t.Fatalf("retry SubscribeDuplex() error = %v", err)
	}
	if err := subscription.Close(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	if err := core.Close(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestLateEndpointFromCanceledCallIsClosed(t *testing.T) {
	format := testFormat(8000)
	endpoint := newFakeEndpoint(format)
	opener := &delayedEndpointOpener{
		started:  make(chan struct{}),
		release:  make(chan struct{}),
		endpoint: endpoint,
	}
	core, err := New(Options{
		EndpointOpener: opener,
		CodecFactory:   &fakeCodecFactory{},
	})
	if err != nil {
		t.Fatal(err)
	}
	authorizeCall(t, core, "call-late-open")
	result := make(chan error, 1)
	go func() {
		_, subscribeErr := core.SubscribeDuplex(context.Background(), ActiveCall{
			ID:    "call-late-open",
			State: CallStateActive,
		})
		result <- subscribeErr
	}()
	receive(t, opener.started)
	if err := core.ReconcileActiveCalls(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	close(opener.release)
	if err := receive(t, result); !errors.Is(err, ErrCanceled) {
		t.Fatalf("SubscribeDuplex() error = %v, want ErrCanceled", err)
	}
	eventually(t, func() bool {
		return endpoint.closeCalls.Load() == 1
	})
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	if err := core.Close(ctx); err != nil {
		t.Fatal(err)
	}
}
