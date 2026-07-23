package callmedia

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
)

const (
	defaultBrowserQueueCapacity   = 16
	defaultRecordingQueueCapacity = 32
	maxSubscriberQueueCapacity    = 256
	uplinkQueueCapacity           = 16
)

// DuplexFrame is one fixed-duration modem frame. DownlinkPCM is remote audio
// read from the modem; UplinkPCM is audio successfully written to the modem
// during the corresponding interval, or silence when no uplink was available.
type DuplexFrame struct {
	Sequence    uint64
	DownlinkPCM []byte
	UplinkPCM   []byte
}

// DuplexSubscription is a bounded, lossless view of a call's PCM frames.
// Falling behind closes only that subscriber with ErrBackpressure.
type DuplexSubscription struct {
	hub       *mediaHub
	id        uint64
	frames    chan DuplexFrame
	closeOnce sync.Once

	errMu sync.Mutex
	err   error
}

// Start begins the shared host PCM stream. All subscribers of a call share the
// same start result and the endpoint is never started more than once.
func (s *DuplexSubscription) Start(ctx context.Context) error {
	if s == nil || s.hub == nil {
		return ErrEndpointUnavailable
	}
	return s.hub.Start(ctx)
}

func (s *DuplexSubscription) Next(ctx context.Context) (DuplexFrame, error) {
	if s == nil {
		return DuplexFrame{}, io.EOF
	}
	ctx = normalizeContext(ctx)
	select {
	case frame, ok := <-s.frames:
		if ok {
			return frame, nil
		}
		if err := s.Err(); err != nil {
			return DuplexFrame{}, err
		}
		return DuplexFrame{}, io.EOF
	case <-ctx.Done():
		return DuplexFrame{}, ctx.Err()
	}
}

func (s *DuplexSubscription) Format() PCMFormat {
	if s == nil || s.hub == nil {
		return PCMFormat{}
	}
	return s.hub.format
}

func (s *DuplexSubscription) Err() error {
	if s == nil {
		return nil
	}
	s.errMu.Lock()
	defer s.errMu.Unlock()
	return s.err
}

func (s *DuplexSubscription) Close() error {
	if s == nil {
		return nil
	}
	s.closeOnce.Do(func() {
		if s.hub != nil {
			s.hub.removeSubscription(s.id, nil)
		}
	})
	return nil
}

func (s *DuplexSubscription) fail(err error) {
	s.errMu.Lock()
	if s.err == nil {
		s.err = err
	}
	s.errMu.Unlock()
}

type mediaHub struct {
	callID   string
	format   PCMFormat
	endpoint MediaEndpoint

	ctx    context.Context
	cancel context.CancelFunc

	writeMu sync.Mutex
	uplink  chan []byte

	mu            sync.Mutex
	nextID        uint64
	subscriptions map[uint64]*DuplexSubscription
	reason        error

	stopOnce sync.Once
	done     chan struct{}
	capture  sync.WaitGroup

	startMu        sync.Mutex
	startInitiated bool
	startClosed    bool
	startDone      chan struct{}
	startErr       error
	started        bool
}

func newMediaHub(parent context.Context, callID string, endpoint MediaEndpoint) (*mediaHub, error) {
	if endpoint == nil {
		return nil, ErrEndpointUnavailable
	}
	format := endpoint.Format()
	if err := format.Validate(); err != nil {
		return nil, fmt.Errorf("create media hub: endpoint format: %w", err)
	}
	ctx, cancel := context.WithCancel(parent)
	hub := &mediaHub{
		callID:        callID,
		format:        format,
		endpoint:      endpoint,
		ctx:           ctx,
		cancel:        cancel,
		uplink:        make(chan []byte, uplinkQueueCapacity),
		subscriptions: make(map[uint64]*DuplexSubscription),
		done:          make(chan struct{}),
		startDone:     make(chan struct{}),
	}
	go hub.cleanup()
	context.AfterFunc(parent, func() {
		hub.initiate(nil)
	})
	return hub, nil
}

func (h *mediaHub) Subscribe(queueCapacity int) (*DuplexSubscription, error) {
	if h == nil || queueCapacity <= 0 || queueCapacity > maxSubscriberQueueCapacity {
		return nil, ErrInvalidArgument
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.ctx.Err() != nil {
		if h.reason != nil {
			return nil, h.reason
		}
		return nil, ErrEndpointUnavailable
	}
	h.nextID++
	subscription := &DuplexSubscription{
		hub:    h,
		id:     h.nextID,
		frames: make(chan DuplexFrame, queueCapacity),
	}
	h.subscriptions[subscription.id] = subscription
	return subscription, nil
}

func (h *mediaHub) Start(ctx context.Context) error {
	if h == nil {
		return ErrEndpointUnavailable
	}
	ctx = normalizeContext(ctx)
	h.startMu.Lock()
	if h.startClosed {
		h.startMu.Unlock()
		if err := h.Error(); err != nil {
			return err
		}
		return ErrEndpointUnavailable
	}
	if !h.startInitiated {
		h.startInitiated = true
		h.capture.Add(1)
		go h.startAndCapture()
	}
	startDone := h.startDone
	h.startMu.Unlock()

	select {
	case <-startDone:
		h.startMu.Lock()
		err := h.startErr
		h.startMu.Unlock()
		return err
	case <-ctx.Done():
		return ErrCanceled
	case <-h.ctx.Done():
		if err := h.Error(); err != nil {
			return err
		}
		return ErrCanceled
	}
}

func (h *mediaHub) WritePCM(ctx context.Context, frame []byte) error {
	if h == nil || len(frame) != h.format.FrameBytes() {
		return ErrInvalidArgument
	}
	h.startMu.Lock()
	started := h.started
	h.startMu.Unlock()
	if !started {
		return ErrEndpointNotStarted
	}
	ctx = normalizeContext(ctx)
	owned := append([]byte(nil), frame...)
	h.writeMu.Lock()
	defer h.writeMu.Unlock()
	if h.ctx.Err() != nil {
		if err := h.Error(); err != nil {
			return err
		}
		return ErrEndpointUnavailable
	}
	if err := h.endpoint.WritePCM(ctx, owned); err != nil {
		if h.ctx.Err() != nil || ctx.Err() != nil {
			return ErrCanceled
		}
		h.initiate(fmt.Errorf("write PCM endpoint: %w", ErrEndpointIO))
		return ErrEndpointIO
	}
	select {
	case h.uplink <- owned:
		return nil
	default:
		h.initiate(ErrBackpressure)
		return ErrBackpressure
	}
}

func (h *mediaHub) Close(ctx context.Context) error {
	if h == nil {
		return nil
	}
	if err := h.shutdown(ctx); err != nil {
		return err
	}
	return h.Error()
}

func (h *mediaHub) shutdown(ctx context.Context) error {
	h.initiate(nil)
	ctx = normalizeContext(ctx)
	select {
	case <-h.done:
		return nil
	case <-ctx.Done():
		return ErrCanceled
	}
}

func (h *mediaHub) Done() <-chan struct{} {
	if h == nil {
		done := make(chan struct{})
		close(done)
		return done
	}
	return h.done
}

func (h *mediaHub) Error() error {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.reason
}

func (h *mediaHub) startAndCapture() {
	defer h.capture.Done()
	err := h.endpoint.Start(h.ctx)
	h.startMu.Lock()
	if err != nil {
		switch {
		case h.ctx.Err() != nil:
			h.startErr = ErrCanceled
		default:
			h.startErr = fmt.Errorf("start PCM endpoint: %w", ErrEndpointIO)
		}
		close(h.startDone)
		h.startMu.Unlock()
		if h.ctx.Err() == nil {
			h.initiate(h.startErr)
		}
		return
	}
	h.started = true
	close(h.startDone)
	h.startMu.Unlock()
	h.captureWorker()
}

func (h *mediaHub) captureWorker() {
	frame := make([]byte, h.format.FrameBytes())
	var sequence uint64
	for {
		if err := h.endpoint.ReadPCM(h.ctx, frame); err != nil {
			if h.ctx.Err() != nil {
				return
			}
			h.initiate(fmt.Errorf("read PCM endpoint: %w", ErrEndpointIO))
			return
		}
		sequence++
		uplink := make([]byte, h.format.FrameBytes())
		select {
		case pending := <-h.uplink:
			copy(uplink, pending)
		default:
		}
		h.broadcast(DuplexFrame{
			Sequence:    sequence,
			DownlinkPCM: append([]byte(nil), frame...),
			UplinkPCM:   uplink,
		})
	}
}

func (h *mediaHub) broadcast(frame DuplexFrame) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for id, subscription := range h.subscriptions {
		cloned := DuplexFrame{
			Sequence:    frame.Sequence,
			DownlinkPCM: append([]byte(nil), frame.DownlinkPCM...),
			UplinkPCM:   append([]byte(nil), frame.UplinkPCM...),
		}
		select {
		case subscription.frames <- cloned:
		default:
			subscription.fail(ErrBackpressure)
			close(subscription.frames)
			delete(h.subscriptions, id)
		}
	}
}

func (h *mediaHub) removeSubscription(id uint64, reason error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	subscription, exists := h.subscriptions[id]
	if !exists {
		return
	}
	if reason != nil {
		subscription.fail(reason)
	}
	close(subscription.frames)
	delete(h.subscriptions, id)
}

func (h *mediaHub) initiate(reason error) {
	h.stopOnce.Do(func() {
		h.mu.Lock()
		h.reason = reason
		h.mu.Unlock()
		h.cancel()
		if err := h.endpoint.Close(); err != nil {
			h.mu.Lock()
			h.reason = errors.Join(h.reason, fmt.Errorf("close PCM endpoint: %w", err))
			h.mu.Unlock()
		}
	})
}

func (h *mediaHub) cleanup() {
	<-h.ctx.Done()
	h.startMu.Lock()
	h.startClosed = true
	h.startMu.Unlock()
	h.capture.Wait()
	h.mu.Lock()
	for id, subscription := range h.subscriptions {
		if h.reason != nil {
			subscription.fail(h.reason)
		}
		close(subscription.frames)
		delete(h.subscriptions, id)
	}
	h.mu.Unlock()
	close(h.done)
}
