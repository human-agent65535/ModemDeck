package callmedia

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pion/interceptor"
	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"
)

const testTimeout = 8 * time.Second

type fakeCodecFactory struct {
	mu     sync.Mutex
	codecs []*fakeCodec
}

func (f *fakeCodecFactory) New(format PCMFormat) (OpusCodec, error) {
	if err := format.Validate(); err != nil {
		return nil, err
	}
	codec := &fakeCodec{format: format}
	f.mu.Lock()
	f.codecs = append(f.codecs, codec)
	f.mu.Unlock()
	return codec, nil
}

func (f *fakeCodecFactory) latest(t *testing.T) *fakeCodec {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.codecs) == 0 {
		t.Fatal("no fake codec was created")
	}
	return f.codecs[len(f.codecs)-1]
}

type fakeCodec struct {
	format       PCMFormat
	concealCalls atomic.Int32
	closeCalls   atomic.Int32
}

func (c *fakeCodec) Format() PCMFormat {
	return c.format
}

func (c *fakeCodec) Encode(pcm []byte) ([]byte, error) {
	if err := validateEncodedFrame(c.format, pcm); err != nil {
		return nil, err
	}
	return []byte{byte(c.format.FrameDuration / time.Millisecond), pcm[0]}, nil
}

func (c *fakeCodec) Decode(payload []byte) (DecodedAudio, error) {
	duration, err := c.PacketDuration(payload)
	if err != nil {
		return DecodedAudio{}, err
	}
	size, err := pcmBytesForDuration(c.format, duration)
	if err != nil {
		return DecodedAudio{}, err
	}
	pcm := make([]byte, size)
	value := byte(0)
	if len(payload) > 1 {
		value = payload[1]
	}
	for index := range pcm {
		pcm[index] = value
	}
	return DecodedAudio{PCM: pcm, Duration: duration}, nil
}

func (c *fakeCodec) PacketDuration(payload []byte) (time.Duration, error) {
	if len(payload) == 0 {
		return 0, ErrInvalidRTP
	}
	duration := time.Duration(payload[0]) * time.Millisecond
	if !validOpusPacketDuration(duration) {
		return 0, ErrInvalidRTP
	}
	return duration, nil
}

func (c *fakeCodec) Conceal(duration time.Duration) ([]byte, error) {
	if !validOpusFrameDuration(duration) {
		return nil, ErrInvalidRTP
	}
	size, err := pcmBytesForDuration(c.format, duration)
	if err != nil {
		return nil, err
	}
	c.concealCalls.Add(1)
	pcm := make([]byte, size)
	for index := range pcm {
		pcm[index] = 0xee
	}
	return pcm, nil
}

func (c *fakeCodec) Close() error {
	c.closeCalls.Add(1)
	return nil
}

type fakeEndpoint struct {
	format  PCMFormat
	read    chan []byte
	writes  chan []byte
	started chan struct{}
	closed  chan struct{}

	startOnce  sync.Once
	closeOnce  sync.Once
	startCalls atomic.Int32
	closeCalls atomic.Int32
	startErr   error
}

func newFakeEndpoint(format PCMFormat) *fakeEndpoint {
	return &fakeEndpoint{
		format:  format,
		read:    make(chan []byte, 8),
		writes:  make(chan []byte, 16),
		started: make(chan struct{}),
		closed:  make(chan struct{}),
	}
}

func (e *fakeEndpoint) Format() PCMFormat {
	return e.format
}

func (e *fakeEndpoint) Start(context.Context) error {
	e.startCalls.Add(1)
	if e.startErr != nil {
		return e.startErr
	}
	e.startOnce.Do(func() { close(e.started) })
	return nil
}

func (e *fakeEndpoint) ReadPCM(ctx context.Context, dst []byte) error {
	select {
	case <-e.started:
	case <-ctx.Done():
		return ctx.Err()
	case <-e.closed:
		return errors.New("test endpoint closed")
	}
	select {
	case frame := <-e.read:
		if len(frame) != len(dst) {
			return errors.New("unexpected test PCM frame size")
		}
		copy(dst, frame)
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-e.closed:
		return errors.New("test endpoint closed")
	}
}

func (e *fakeEndpoint) WritePCM(ctx context.Context, src []byte) error {
	select {
	case <-e.started:
	case <-ctx.Done():
		return ctx.Err()
	case <-e.closed:
		return errors.New("test endpoint closed")
	}
	frame := append([]byte(nil), src...)
	select {
	case e.writes <- frame:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-e.closed:
		return errors.New("test endpoint closed")
	}
}

func (e *fakeEndpoint) Close() error {
	e.closeCalls.Add(1)
	e.closeOnce.Do(func() { close(e.closed) })
	return nil
}

type fakeEndpointOpener struct {
	endpoint *fakeEndpoint
	opens    atomic.Int32
}

func (o *fakeEndpointOpener) Open(
	_ context.Context,
	call ActiveCall,
) (MediaEndpoint, error) {
	if call.State != CallStateActive {
		return nil, errors.New("inactive call reached endpoint opener")
	}
	o.opens.Add(1)
	return o.endpoint, nil
}

type blockingEndpointOpener struct {
	started chan struct{}
	once    sync.Once
}

func (o *blockingEndpointOpener) Open(
	ctx context.Context,
	_ ActiveCall,
) (MediaEndpoint, error) {
	o.once.Do(func() { close(o.started) })
	<-ctx.Done()
	return nil, ctx.Err()
}

type endpointAndErrorOpener struct {
	endpoint *fakeEndpoint
}

func (o *endpointAndErrorOpener) Open(
	_ context.Context,
	_ ActiveCall,
) (MediaEndpoint, error) {
	return o.endpoint, errors.New("test endpoint open failure")
}

type retryEndpointOpener struct {
	mu       sync.Mutex
	endpoint *fakeEndpoint
	opens    int
}

func (o *retryEndpointOpener) Open(
	_ context.Context,
	_ ActiveCall,
) (MediaEndpoint, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.opens++
	if o.opens == 1 {
		return nil, errors.New("first open failed")
	}
	return o.endpoint, nil
}

type delayedEndpointOpener struct {
	started  chan struct{}
	release  chan struct{}
	endpoint *fakeEndpoint
	once     sync.Once
}

func (o *delayedEndpointOpener) Open(
	_ context.Context,
	_ ActiveCall,
) (MediaEndpoint, error) {
	o.once.Do(func() { close(o.started) })
	<-o.release
	return o.endpoint, nil
}

type codecAndErrorFactory struct {
	codec *fakeCodec
}

func (f *codecAndErrorFactory) New(format PCMFormat) (OpusCodec, error) {
	f.codec = &fakeCodec{format: format}
	return f.codec, errors.New("test codec construction failure")
}

type testBrowser struct {
	pc          *webrtc.PeerConnection
	local       *webrtc.TrackLocalStaticSample
	remoteTrack chan *webrtc.TrackRemote
}

func newTestBrowser(t *testing.T) *testBrowser {
	t.Helper()
	engine := &webrtc.MediaEngine{}
	if err := engine.RegisterCodec(opusRTPParameters(), webrtc.RTPCodecTypeAudio); err != nil {
		t.Fatal(err)
	}
	interceptors := &interceptor.Registry{}
	if err := webrtc.RegisterDefaultInterceptors(engine, interceptors); err != nil {
		t.Fatal(err)
	}
	api := webrtc.NewAPI(
		webrtc.WithMediaEngine(engine),
		webrtc.WithInterceptorRegistry(interceptors),
	)
	peer, err := api.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	local, err := webrtc.NewTrackLocalStaticSample(
		opusRTPParameters().RTPCodecCapability,
		"microphone",
		"browser",
	)
	if err != nil {
		t.Fatal(err)
	}
	sender, err := peer.AddTrack(local)
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		buffer := make([]byte, 1500)
		for {
			if _, _, readErr := sender.Read(buffer); readErr != nil {
				return
			}
		}
	}()
	browser := &testBrowser{
		pc:          peer,
		local:       local,
		remoteTrack: make(chan *webrtc.TrackRemote, 1),
	}
	peer.OnTrack(func(track *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		select {
		case browser.remoteTrack <- track:
		default:
		}
	})
	t.Cleanup(func() { _ = peer.Close() })
	return browser
}

func (b *testBrowser) offer(t *testing.T) string {
	t.Helper()
	offer, err := b.pc.CreateOffer(nil)
	if err != nil {
		t.Fatal(err)
	}
	gathered := webrtc.GatheringCompletePromise(b.pc)
	if err := b.pc.SetLocalDescription(offer); err != nil {
		t.Fatal(err)
	}
	select {
	case <-gathered:
	case <-time.After(testTimeout):
		t.Fatal("browser ICE gathering timed out")
	}
	description := b.pc.LocalDescription()
	if description == nil {
		t.Fatal("browser local description is nil")
	}
	return description.SDP
}

func (b *testBrowser) applyAnswer(t *testing.T, value string) {
	t.Helper()
	if err := b.pc.SetRemoteDescription(webrtc.SessionDescription{
		Type: webrtc.SDPTypeAnswer,
		SDP:  value,
	}); err != nil {
		t.Fatal(err)
	}
	eventually(t, func() bool {
		return b.pc.ConnectionState() == webrtc.PeerConnectionStateConnected
	})
}

func (b *testBrowser) send(t *testing.T, payload []byte, duration time.Duration) {
	t.Helper()
	if err := b.local.WriteSample(media.Sample{Data: payload, Duration: duration}); err != nil {
		t.Fatal(err)
	}
}

func eventually(t *testing.T, predicate func() bool) {
	t.Helper()
	deadline := time.Now().Add(testTimeout)
	for time.Now().Before(deadline) {
		if predicate() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition was not satisfied before timeout")
}

func receive[T any](t *testing.T, values <-chan T) T {
	t.Helper()
	select {
	case value := <-values:
		return value
	case <-time.After(testTimeout):
		var zero T
		t.Fatal("timed out waiting for test value")
		return zero
	}
}

func testFormat(sampleRate int) PCMFormat {
	return PCMFormat{
		Encoding:      PCMEncodingS16LE,
		SampleRate:    sampleRate,
		Channels:      1,
		FrameDuration: 20 * time.Millisecond,
	}
}

func testCore(
	t *testing.T,
	format PCMFormat,
) (*Core, *fakeEndpointOpener, *fakeCodecFactory) {
	t.Helper()
	opener := &fakeEndpointOpener{endpoint: newFakeEndpoint(format)}
	codecs := &fakeCodecFactory{}
	core, err := New(Options{
		EndpointOpener: opener,
		CodecFactory:   codecs,
		Jitter: JitterConfig{
			StartupDelay: 1 * time.Millisecond,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
		defer cancel()
		if err := core.Close(ctx); err != nil {
			t.Errorf("close core: %v", err)
		}
	})
	return core, opener, codecs
}

func testOffer(callID, sdp string) Offer {
	return Offer{
		Call: ActiveCall{ID: callID, State: CallStateActive},
		SDP:  sdp,
	}
}

func authorizeCall(t *testing.T, core *Core, callIDs ...string) {
	t.Helper()
	if err := core.ReconcileActiveCalls(context.Background(), callIDs); err != nil {
		t.Fatal(err)
	}
}

func assertErrorIs(t *testing.T, err, target error) {
	t.Helper()
	if !errors.Is(err, target) {
		t.Fatalf("expected error %v, got %v", target, err)
	}
}

func minimalSDP(media ...string) string {
	return fmt.Sprintf(
		"v=0\r\n"+
			"o=- 0 0 IN IP4 127.0.0.1\r\n"+
			"s=-\r\n"+
			"t=0 0\r\n%s",
		stringsJoin(media, ""),
	)
}

func stringsJoin(values []string, separator string) string {
	if len(values) == 0 {
		return ""
	}
	result := values[0]
	for _, value := range values[1:] {
		result += separator + value
	}
	return result
}

func opusMedia(direction string) string {
	return "m=audio 9 UDP/TLS/RTP/SAVPF 111\r\n" +
		"c=IN IP4 0.0.0.0\r\n" +
		"a=" + direction + "\r\n" +
		"a=rtpmap:111 opus/48000/2\r\n"
}
