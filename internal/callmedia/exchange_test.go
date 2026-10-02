package callmedia

import (
	"context"
	"errors"
	"sync"
	"testing"
)

func TestConcurrentOfferRetriesShareOneMediaSession(t *testing.T) {
	core, opener, _ := testCore(t, testFormat(16000))
	authorizeCall(t, core, "retry-call")
	browser := newTestBrowser(t)
	offer := testOffer("retry-call", browser.offer(t))
	var workers sync.WaitGroup
	results := make(chan ExchangeResult, 8)
	failures := make(chan error, 8)
	for range 8 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			result, err := core.Exchange(context.Background(), offer)
			results <- result
			failures <- err
		}()
	}
	workers.Wait()
	first := <-results
	for range 8 {
		if err := <-failures; err != nil {
			t.Fatal(err)
		}
	}
	for range 7 {
		next := <-results
		if next.Session != first.Session || next.AnswerSDP != first.AnswerSDP {
			t.Fatal("retry created another peer or changed the answer")
		}
	}
	if opener.opens.Load() != 1 {
		t.Fatal("duplicate endpoint")
	}
	changed := offer
	changed.SDP = offer.SDP + "a=x-test:changed\r\n"
	if _, err := core.Exchange(context.Background(), changed); !errors.Is(err, ErrCallInUse) {
		t.Fatalf("changed offer reused answer: %v", err)
	}
	if err := core.ReconcileActiveCalls(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := core.Exchange(context.Background(), offer); !errors.Is(err, ErrCallNotActive) {
		t.Fatalf("retry resurrected ended call: %v", err)
	}
}

type gatedOpener struct {
	inner   MediaEndpointOpener
	started chan struct{}
	proceed chan struct{}
}

func (o *gatedOpener) Open(ctx context.Context, call ActiveCall) (MediaEndpoint, error) {
	close(o.started)
	select {
	case <-o.proceed:
		return o.inner.Open(ctx, call)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func TestOfferRetrySurvivesCanceledOriginalRequest(t *testing.T) {
	core, opener, _ := testCore(t, testFormat(16000))
	gate := &gatedOpener{inner: opener, started: make(chan struct{}), proceed: make(chan struct{})}
	core.opener = gate
	authorizeCall(t, core, "abandoned-call")
	browser := newTestBrowser(t)
	offer := testOffer("abandoned-call", browser.offer(t))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := core.Exchange(ctx, offer); done <- err }()
	receive(t, gate.started)
	cancel()
	if err := receive(t, done); !errors.Is(err, ErrCanceled) {
		t.Fatalf("original: %v", err)
	}
	close(gate.proceed)
	result, err := core.Exchange(context.Background(), offer)
	if err != nil {
		t.Fatal(err)
	}
	browser.applyAnswer(t, result.AnswerSDP)
	if opener.opens.Load() != 1 {
		t.Fatal("retry opened a second PCM endpoint")
	}
}
