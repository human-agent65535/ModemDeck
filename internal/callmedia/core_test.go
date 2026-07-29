package callmedia

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"
)

func TestExchangeRejectsInactiveCallBeforeOpeningEndpoint(t *testing.T) {
	format := testFormat(8000)
	core, opener, _ := testCore(t, format)
	_, err := core.Exchange(context.Background(), Offer{
		Call: ActiveCall{ID: "call-inactive"},
		SDP:  minimalSDP(opusMedia("sendrecv")),
	})
	assertErrorIs(t, err, ErrCallNotActive)
	if got := opener.opens.Load(); got != 0 {
		t.Fatalf("endpoint opens = %d, want 0", got)
	}
}

func TestExchangeRejectsUnsupportedMediaBeforeOpeningEndpoint(t *testing.T) {
	format := testFormat(8000)
	core, opener, _ := testCore(t, format)
	video := "m=video 9 UDP/TLS/RTP/SAVPF 96\r\n" +
		"c=IN IP4 0.0.0.0\r\n" +
		"a=sendrecv\r\n" +
		"a=rtpmap:96 VP8/90000\r\n"
	_, err := core.Exchange(
		context.Background(),
		testOffer("call-video", minimalSDP(video)),
	)
	assertErrorIs(t, err, ErrUnsupportedMedia)
	if got := opener.opens.Load(); got != 0 {
		t.Fatalf("endpoint opens = %d, want 0", got)
	}
}

func TestPionLoopbackBridgesVariableOpusPacketsAndNativePCM(t *testing.T) {
	format := testFormat(16000)
	core, opener, _ := testCore(t, format)
	authorizeCall(t, core, "call-loopback")
	browser := newTestBrowser(t)
	result, err := core.Exchange(
		context.Background(),
		testOffer("call-loopback", browser.offer(t)),
	)
	if err != nil {
		t.Fatal(err)
	}
	browser.applyAnswer(t, result.AnswerSDP)

	browser.send(t, []byte{60, 0x5a}, 60*time.Millisecond)
	for index := 0; index < 3; index++ {
		frame := receiveNonSilentPCM(t, opener.endpoint.writes)
		if len(frame) != format.FrameBytes() || !allBytes(frame, 0x5a) {
			t.Fatalf("browser PCM frame %d is invalid", index)
		}
	}

	firstCapture := make([]byte, format.FrameBytes())
	secondCapture := make([]byte, format.FrameBytes())
	for index := range firstCapture {
		firstCapture[index] = 0x6b
		secondCapture[index] = 0x7c
	}
	opener.endpoint.read <- firstCapture
	opener.endpoint.read <- secondCapture
	remote := receive(t, browser.remoteTrack)
	type packetResult struct {
		packets []*rtp.Packet
		err     error
	}
	packet := make(chan packetResult, 1)
	go func() {
		packets := make([]*rtp.Packet, 0, 2)
		for range 2 {
			rtpPacket, _, readErr := remote.ReadRTP()
			if readErr != nil {
				packet <- packetResult{err: readErr}
				return
			}
			packets = append(packets, rtpPacket)
		}
		packet <- packetResult{packets: packets}
	}()
	got := receive(t, packet)
	if got.err != nil {
		t.Fatal(got.err)
	}
	if len(got.packets) != 2 {
		t.Fatalf("phone-to-browser RTP packets = %d", len(got.packets))
	}
	if payload := got.packets[0].Payload; len(payload) != 2 || payload[0] != 20 || payload[1] != 0x6b {
		t.Fatalf("first phone-to-browser payload = %v", payload)
	}
	if payload := got.packets[1].Payload; len(payload) != 2 || payload[0] != 20 || payload[1] != 0x7c {
		t.Fatalf("second phone-to-browser payload = %v", payload)
	}
	if delta := got.packets[1].Timestamp - got.packets[0].Timestamp; delta != 960 {
		t.Fatalf("RTP timestamp delta = %d, want 960 at 48 kHz", delta)
	}
	if codec := remote.Codec(); codec.ClockRate != RTPClockRate || codec.Channels != 2 {
		t.Fatalf("remote Opus RTP parameters = %+v", codec)
	}

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	if err := result.Session.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if err := result.Session.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if err := core.CloseCall(ctx, "call-loopback"); err != nil {
		t.Fatal(err)
	}
	if got := opener.endpoint.closeCalls.Load(); got != 1 {
		t.Fatalf("endpoint close calls = %d, want 1", got)
	}
}

func TestOnePeerOwnsConsumerCallUntilSessionCloses(t *testing.T) {
	format := testFormat(8000)
	core, opener, codecs := testCore(t, format)
	authorizeCall(t, core, "call-exclusive")
	browser := newTestBrowser(t)
	offer := testOffer("call-exclusive", browser.offer(t))
	result, err := core.Exchange(context.Background(), offer)
	if err != nil {
		t.Fatal(err)
	}
	browser.applyAnswer(t, result.AnswerSDP)

	_, err = core.Exchange(context.Background(), offer)
	if !errors.Is(err, ErrCallInUse) {
		t.Fatalf("second peer error = %v", err)
	}
	if got := opener.opens.Load(); got != 1 {
		t.Fatalf("endpoint opens = %d, want 1", got)
	}

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	if err := result.Session.Close(ctx); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(testTimeout)
	for {
		core.mu.Lock()
		_, retained := core.owners["call-exclusive"]
		core.mu.Unlock()
		if !retained {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("closed media session remained retained by the core")
		}
		time.Sleep(time.Millisecond)
	}
	if got := codecs.latest(t).closeCalls.Load(); got != 1 {
		t.Fatalf("codec close calls = %d, want 1", got)
	}
}

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
	browser := newTestBrowser(t)
	offer := browser.offer(t)
	exchanged := make(chan error, 1)
	go func() {
		_, exchangeErr := core.Exchange(
			context.Background(),
			testOffer("call-canceled", offer),
		)
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

func TestWebRTCDisconnectedRecoversWithoutReopeningEndpoint(t *testing.T) {
	format := testFormat(8000)
	core, opener, _ := testCore(t, format)
	authorizeCall(t, core, "call-disconnected")
	browser := newTestBrowser(t)
	result, err := core.Exchange(
		context.Background(),
		testOffer("call-disconnected", browser.offer(t)),
	)
	if err != nil {
		t.Fatal(err)
	}
	browser.applyAnswer(t, result.AnswerSDP)
	eventually(t, func() bool {
		return opener.endpoint.startCalls.Load() == 1
	})

	result.Session.events.updateState(webrtc.PeerConnectionStateDisconnected)
	select {
	case <-result.Session.Done():
		t.Fatal("transient disconnect closed the media session")
	case <-time.After(25 * time.Millisecond):
	}
	result.Session.events.updateState(webrtc.PeerConnectionStateConnected)
	select {
	case <-result.Session.Done():
		t.Fatal("reconnected media session closed")
	case <-time.After(25 * time.Millisecond):
	}
	if got := opener.opens.Load(); got != 1 {
		t.Fatalf("endpoint opens after disconnect = %d, want 1", got)
	}
	if got := opener.endpoint.closeCalls.Load(); got != 0 {
		t.Fatalf("hub closed with browser peer = %d, want recording-capable hub retained", got)
	}
	if err := core.ReconcileActiveCalls(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if got := opener.endpoint.closeCalls.Load(); got != 1 {
		t.Fatalf("endpoint closes after call removal = %d, want 1", got)
	}
}

func TestPreparationClosesPartialEndpointAndCodecResultsOnce(t *testing.T) {
	format := testFormat(8000)
	browser := newTestBrowser(t)
	offer := testOffer("call-partial-endpoint", browser.offer(t))
	endpoint := newFakeEndpoint(format)
	core, err := New(Options{
		EndpointOpener: &endpointAndErrorOpener{endpoint: endpoint},
		CodecFactory:   &fakeCodecFactory{},
	})
	if err != nil {
		t.Fatal(err)
	}
	authorizeCall(t, core, "call-partial-endpoint")
	_, err = core.Exchange(context.Background(), offer)
	assertErrorIs(t, err, ErrEndpointUnavailable)
	if got := endpoint.closeCalls.Load(); got != 1 {
		t.Fatalf("partial endpoint close calls = %d, want 1", got)
	}
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	if err := core.Close(ctx); err != nil {
		t.Fatal(err)
	}

	browser = newTestBrowser(t)
	offer = testOffer("call-partial-codec", browser.offer(t))
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
	_, err = core.Exchange(context.Background(), offer)
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
