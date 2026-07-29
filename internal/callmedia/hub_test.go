package callmedia

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestMediaHubStartsEndpointOnlyWhenConsumerStarts(t *testing.T) {
	format := testFormat(8000)
	endpoint := newFakeEndpoint(format)
	hub, err := newMediaHub(context.Background(), "call-start", endpoint)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
		defer cancel()
		if err := hub.Close(ctx); err != nil {
			t.Errorf("close hub: %v", err)
		}
	})
	first, err := hub.Subscribe(2)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := hub.Subscribe(2)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()

	time.Sleep(20 * time.Millisecond)
	if got := endpoint.startCalls.Load(); got != 0 {
		t.Fatalf("endpoint starts after open = %d, want 0", got)
	}
	if err := first.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := second.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := endpoint.startCalls.Load(); got != 1 {
		t.Fatalf("endpoint starts = %d, want 1", got)
	}
	silence := receive(t, endpoint.writes)
	if !allBytes(silence, 0) {
		t.Fatal("endpoint did not receive idle playback silence")
	}

	downlink := make([]byte, format.FrameBytes())
	downlink[0] = 0x42
	endpoint.read <- downlink
	frame, err := first.Next(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if frame.Sequence != 1 || frame.DownlinkPCM[0] != 0x42 {
		t.Fatalf("duplex frame = %+v", frame)
	}
}

func TestMediaHubDoesNotQueueFramesBeforeSubscriptionStarts(t *testing.T) {
	format := testFormat(8000)
	endpoint := newFakeEndpoint(format)
	hub, err := newMediaHub(context.Background(), "call-late-browser", endpoint)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
		defer cancel()
		if err := hub.Close(ctx); err != nil {
			t.Errorf("close hub: %v", err)
		}
	})
	recording, err := hub.Subscribe(8)
	if err != nil {
		t.Fatal(err)
	}
	defer recording.Close()
	if err := recording.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	browser, err := hub.Subscribe(2)
	if err != nil {
		t.Fatal(err)
	}
	defer browser.Close()

	for sequence := byte(1); sequence <= 3; sequence++ {
		downlink := make([]byte, format.FrameBytes())
		downlink[0] = sequence
		endpoint.read <- downlink
		if _, err := recording.Next(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if err := browser.Err(); err != nil {
		t.Fatalf("unstarted browser subscription failed: %v", err)
	}

	if err := browser.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	downlink := make([]byte, format.FrameBytes())
	downlink[0] = 4
	endpoint.read <- downlink
	if _, err := recording.Next(context.Background()); err != nil {
		t.Fatal(err)
	}
	frame, err := browser.Next(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if frame.Sequence != 4 || frame.DownlinkPCM[0] != 4 {
		t.Fatalf("first browser frame = %+v, want live sequence 4", frame)
	}
}

func TestMediaHubStartFailureIsTerminal(t *testing.T) {
	endpoint := newFakeEndpoint(testFormat(8000))
	endpoint.startErr = errors.New("start failed")
	hub, err := newMediaHub(context.Background(), "call-start-failed", endpoint)
	if err != nil {
		t.Fatal(err)
	}
	subscription, err := hub.Subscribe(2)
	if err != nil {
		t.Fatal(err)
	}
	if err := subscription.Start(context.Background()); !errors.Is(err, ErrEndpointIO) {
		t.Fatalf("Start() error = %v, want ErrEndpointIO", err)
	}
	select {
	case <-hub.Done():
	case <-time.After(testTimeout):
		t.Fatal("failed hub did not close")
	}
	if got := endpoint.startCalls.Load(); got != 1 {
		t.Fatalf("endpoint starts = %d, want 1", got)
	}
	if got := endpoint.closeCalls.Load(); got != 1 {
		t.Fatalf("endpoint closes = %d, want 1", got)
	}
}

func TestMediaHubKeepsPlayedFramesInOrderForBurstCapture(t *testing.T) {
	format := testFormat(8000)
	hub := &mediaHub{
		format: format,
		played: make(chan []byte, uplinkQueueCapacity),
	}
	first := make([]byte, format.FrameBytes())
	first[0] = 0x11
	second := make([]byte, format.FrameBytes())
	second[0] = 0x22

	hub.rememberPlayed(first)
	hub.rememberPlayed(second)

	if got := <-hub.played; got[0] != 0x11 {
		t.Fatalf("first played frame starts with %#x", got[0])
	}
	if got := <-hub.played; got[0] != 0x22 {
		t.Fatalf("second played frame starts with %#x", got[0])
	}
}

func TestMediaHubStartIsOwnedByCallLifetime(t *testing.T) {
	format := testFormat(8000)
	endpoint := &blockingStartEndpoint{
		fakeEndpoint: newFakeEndpoint(format),
		started:      make(chan struct{}),
		release:      make(chan struct{}),
	}
	hub, err := newMediaHub(context.Background(), "call-shared-start", endpoint)
	if err != nil {
		t.Fatal(err)
	}
	first, err := hub.Subscribe(2)
	if err != nil {
		t.Fatal(err)
	}
	second, err := hub.Subscribe(2)
	if err != nil {
		t.Fatal(err)
	}
	firstContext, cancelFirst := context.WithCancel(context.Background())
	firstResult := make(chan error, 1)
	go func() {
		firstResult <- first.Start(firstContext)
	}()
	receive(t, endpoint.started)
	cancelFirst()
	if err := receive(t, firstResult); !errors.Is(err, ErrCanceled) {
		t.Fatalf("first Start() error = %v, want ErrCanceled", err)
	}
	close(endpoint.release)
	if err := second.Start(context.Background()); err != nil {
		t.Fatalf("second Start() error = %v", err)
	}
	if got := endpoint.calls.Load(); got != 1 {
		t.Fatalf("endpoint Start() calls = %d, want 1", got)
	}
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	if err := hub.Close(ctx); err != nil {
		t.Fatal(err)
	}
}

type blockingStartEndpoint struct {
	*fakeEndpoint
	started chan struct{}
	release chan struct{}
	calls   atomic.Int32
}

func (e *blockingStartEndpoint) Start(ctx context.Context) error {
	e.calls.Add(1)
	close(e.started)
	select {
	case <-e.release:
		return e.fakeEndpoint.Start(ctx)
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestMediaHubUplinkBackpressureFailsAllConsumers(t *testing.T) {
	format := testFormat(8000)
	endpoint := newFakeEndpoint(format)
	endpoint.writes = make(chan []byte, uplinkQueueCapacity+1)
	hub, err := newMediaHub(context.Background(), "call-backpressure", endpoint)
	if err != nil {
		t.Fatal(err)
	}
	subscription, err := hub.Subscribe(2)
	if err != nil {
		t.Fatal(err)
	}
	if err := subscription.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	frame := make([]byte, format.FrameBytes())
	var overflow error
	for index := 0; index < uplinkQueueCapacity*4; index++ {
		if err := hub.WritePCM(context.Background(), frame); err != nil {
			overflow = err
			break
		}
	}
	if !errors.Is(overflow, ErrBackpressure) {
		t.Fatalf("overflow WritePCM() error = %v, want ErrBackpressure", overflow)
	}
	if _, err := subscription.Next(context.Background()); !errors.Is(err, ErrBackpressure) {
		t.Fatalf("subscription error = %v, want ErrBackpressure", err)
	}
}
