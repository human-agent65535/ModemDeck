package callmedia

import (
	"context"
	"errors"
	"testing"
)

func TestMediaOwnerTokenIsExclusiveAndTransferableAfterRelease(t *testing.T) {
	format := testFormat(8000)
	core, opener, _ := testCore(t, format)
	authorizeCall(t, core, "call-owner")

	firstBrowser := newTestBrowser(t)
	firstOffer := testOffer("call-owner", firstBrowser.offer(t))
	firstOffer.OwnerToken = "owner-first"
	first, err := core.Exchange(context.Background(), firstOffer)
	if err != nil {
		t.Fatal(err)
	}
	firstBrowser.applyAnswer(t, first.AnswerSDP)

	conflicting := firstOffer
	conflicting.OwnerToken = "owner-second"
	if _, err := core.Exchange(context.Background(), conflicting); !errors.Is(err, ErrCallInUse) {
		t.Fatalf("conflicting owner error = %v, want ErrCallInUse", err)
	}
	if err := core.ReleaseOwner(
		context.Background(),
		"call-owner",
		"owner-second",
	); !errors.Is(err, ErrNotMediaOwner) {
		t.Fatalf("wrong owner release error = %v, want ErrNotMediaOwner", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	if err := core.ReleaseOwner(ctx, "call-owner", "owner-first"); err != nil {
		t.Fatal(err)
	}
	if got := opener.endpoint.closeCalls.Load(); got != 0 {
		t.Fatalf("owner release closed shared endpoint %d times", got)
	}

	secondBrowser := newTestBrowser(t)
	secondOffer := testOffer("call-owner", secondBrowser.offer(t))
	secondOffer.OwnerToken = "owner-second"
	second, err := core.Exchange(context.Background(), secondOffer)
	if err != nil {
		t.Fatalf("replacement owner exchange: %v", err)
	}
	secondBrowser.applyAnswer(t, second.AnswerSDP)
	if got := opener.opens.Load(); got != 1 {
		t.Fatalf("endpoint opens across owner transfer = %d, want 1", got)
	}
	if err := core.ReleaseOwner(ctx, "call-owner", "owner-second"); err != nil {
		t.Fatal(err)
	}
}

func TestReleaseCancelsOwnerPreparation(t *testing.T) {
	opener := &blockingEndpointOpener{started: make(chan struct{})}
	core, err := New(Options{
		EndpointOpener: opener,
		CodecFactory:   &fakeCodecFactory{},
	})
	if err != nil {
		t.Fatal(err)
	}
	authorizeCall(t, core, "call-owner-preparing")
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
		defer cancel()
		if err := core.Close(ctx); err != nil {
			t.Errorf("close core: %v", err)
		}
	})

	browser := newTestBrowser(t)
	offer := testOffer("call-owner-preparing", browser.offer(t))
	exchanged := make(chan error, 1)
	go func() {
		_, exchangeErr := core.Exchange(context.Background(), offer)
		exchanged <- exchangeErr
	}()
	receive(t, opener.started)

	if err := core.ReleaseOwner(
		context.Background(),
		"call-owner-preparing",
		offer.OwnerToken,
	); err != nil {
		t.Fatal(err)
	}
	if err := receive(t, exchanged); !errors.Is(err, ErrCanceled) {
		t.Fatalf("canceled exchange error = %v, want ErrCanceled", err)
	}

	core.mu.Lock()
	_, retained := core.owners["call-owner-preparing"]
	core.mu.Unlock()
	if retained {
		t.Fatal("released preparation retained its owner")
	}
}
