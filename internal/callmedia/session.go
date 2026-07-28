package callmedia

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pion/webrtc/v4"
	"github.com/pion/webrtc/v4/pkg/media"
)

type peerEvents struct {
	track       chan *webrtc.TrackRemote
	failure     chan error
	connected   chan struct{}
	state       chan webrtc.PeerConnectionState
	trackSeen   atomic.Bool
	connectOnce sync.Once
}

func newPeerEvents() *peerEvents {
	return &peerEvents{
		track:     make(chan *webrtc.TrackRemote, 1),
		failure:   make(chan error, 1),
		connected: make(chan struct{}),
		state:     make(chan webrtc.PeerConnectionState, 1),
	}
}

func (e *peerEvents) acceptTrack(track *webrtc.TrackRemote) {
	if track == nil || !e.trackSeen.CompareAndSwap(false, true) {
		e.fail(ErrInvalidRTP)
		return
	}
	select {
	case e.track <- track:
	default:
		e.fail(ErrBackpressure)
	}
}

func (e *peerEvents) updateState(state webrtc.PeerConnectionState) {
	switch state {
	case webrtc.PeerConnectionStateConnected:
		e.connectOnce.Do(func() { close(e.connected) })
		e.publishState(state)
	case webrtc.PeerConnectionStateDisconnected,
		webrtc.PeerConnectionStateFailed:
		e.publishState(state)
	case webrtc.PeerConnectionStateClosed:
		e.fail(ErrTransportClosed)
	}
}

func (e *peerEvents) publishState(state webrtc.PeerConnectionState) {
	select {
	case e.state <- state:
		return
	default:
	}
	select {
	case <-e.state:
	default:
	}
	select {
	case e.state <- state:
	default:
	}
}

func (e *peerEvents) fail(err error) {
	select {
	case e.failure <- err:
	default:
	}
}

func (c *Core) preparePeer() (
	*webrtc.PeerConnection,
	*webrtc.TrackLocalStaticSample,
	*webrtc.RTPSender,
	*peerEvents,
	error,
) {
	peer, err := c.api.NewPeerConnection(clonePeerConfiguration(c.configuration))
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("exchange WebRTC offer: create peer: %w", ErrNegotiation)
	}
	track, err := webrtc.NewTrackLocalStaticSample(
		opusRTPParameters().RTPCodecCapability,
		"audio",
		"modemdeck",
	)
	if err != nil {
		_ = peer.Close()
		return nil, nil, nil, nil, fmt.Errorf("exchange WebRTC offer: create audio track: %w", ErrNegotiation)
	}
	sender, err := peer.AddTrack(track)
	if err != nil {
		_ = peer.Close()
		return nil, nil, nil, nil, fmt.Errorf("exchange WebRTC offer: add audio track: %w", ErrNegotiation)
	}
	events := newPeerEvents()
	peer.OnTrack(func(remote *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		events.acceptTrack(remote)
	})
	peer.OnConnectionStateChange(events.updateState)
	return peer, track, sender, events, nil
}

// Session owns one browser peer and its codec. The per-call media hub owns the
// modem endpoint independently so recording can continue without this peer.
type Session struct {
	callID       string
	format       PCMFormat
	hub          *mediaHub
	subscription *DuplexSubscription
	codec        OpusCodec
	peer         *webrtc.PeerConnection
	local        *webrtc.TrackLocalStaticSample
	sender       *webrtc.RTPSender
	events       *peerEvents
	jitter       *jitterBuffer
	playout      *rtpPlayout
	recoveryTime time.Duration

	ctx    context.Context
	cancel context.CancelFunc

	startOnce sync.Once
	stopOnce  sync.Once
	done      chan struct{}
	workers   sync.WaitGroup

	reasonMu sync.Mutex
	reason   error
}

func newSession(
	parent context.Context,
	callID string,
	format PCMFormat,
	hub *mediaHub,
	subscription *DuplexSubscription,
	codec OpusCodec,
	peer *webrtc.PeerConnection,
	local *webrtc.TrackLocalStaticSample,
	sender *webrtc.RTPSender,
	events *peerEvents,
	jitterConfig JitterConfig,
	recoveryTime time.Duration,
) *Session {
	ctx, cancel := context.WithCancel(parent)
	jitter := newJitterBuffer(jitterConfig.PacketCapacity)
	return &Session{
		callID:       callID,
		format:       format,
		hub:          hub,
		subscription: subscription,
		codec:        codec,
		peer:         peer,
		local:        local,
		sender:       sender,
		events:       events,
		jitter:       jitter,
		playout:      newRTPPlayout(format, codec, jitter, jitterConfig),
		recoveryTime: recoveryTime,
		ctx:          ctx,
		cancel:       cancel,
		done:         make(chan struct{}),
	}
}

func (s *Session) start() {
	s.startOnce.Do(func() {
		s.workers.Add(5)
		go s.runWorker(s.captureLoop)
		go s.runWorker(s.receiveLoop)
		go s.runWorker(s.playbackLoop)
		go s.runWorker(s.rtcpLoop)
		go s.runWorker(s.connectionLoop)
		go s.cleanup()
	})
}

func (s *Session) runWorker(run func() error) {
	defer s.workers.Done()
	if err := run(); err != nil && s.ctx.Err() == nil {
		s.stop(err)
	}
}

func (s *Session) stop(reason error) {
	s.stopOnce.Do(func() {
		s.reasonMu.Lock()
		s.reason = reason
		s.reasonMu.Unlock()
		s.cancel()
	})
}

func (s *Session) cleanup() {
	<-s.ctx.Done()
	var cleanupError error
	if err := s.peer.Close(); err != nil {
		cleanupError = errors.Join(cleanupError, fmt.Errorf("close WebRTC peer: %w", err))
	}
	if err := s.subscription.Close(); err != nil {
		cleanupError = errors.Join(cleanupError, fmt.Errorf("close PCM subscription: %w", err))
	}
	s.workers.Wait()
	if err := s.codec.Close(); err != nil {
		cleanupError = errors.Join(cleanupError, fmt.Errorf("close Opus codec: %w", err))
	}
	if cleanupError != nil {
		s.setReasonIfNil(cleanupError)
	}
	close(s.done)
}

func (s *Session) Close(ctx context.Context) error {
	if s == nil {
		return nil
	}
	if err := s.shutdown(ctx); err != nil {
		return err
	}
	return s.Err()
}

func (s *Session) shutdown(ctx context.Context) error {
	ctx = normalizeContext(ctx)
	s.stop(nil)
	select {
	case <-s.done:
		return nil
	case <-ctx.Done():
		return ErrCanceled
	}
}

func (s *Session) Done() <-chan struct{} {
	if s == nil {
		done := make(chan struct{})
		close(done)
		return done
	}
	return s.done
}

func (s *Session) Err() error {
	if s == nil {
		return nil
	}
	s.reasonMu.Lock()
	defer s.reasonMu.Unlock()
	return s.reason
}

func (s *Session) Format() PCMFormat {
	if s == nil {
		return PCMFormat{}
	}
	return s.format
}

func (s *Session) CallID() string {
	if s == nil {
		return ""
	}
	return s.callID
}

func (s *Session) captureLoop() error {
	if err := s.waitConnected(); err != nil {
		return err
	}
	if err := s.subscription.Start(s.ctx); err != nil {
		if s.ctx.Err() != nil {
			return nil
		}
		return fmt.Errorf("start PCM hub: %w", err)
	}
	for {
		frame, err := s.subscription.Next(s.ctx)
		if err != nil {
			if s.ctx.Err() != nil || errors.Is(err, context.Canceled) {
				return nil
			}
			return fmt.Errorf("read PCM subscription: %w", err)
		}
		encoded, err := s.codec.Encode(frame.DownlinkPCM)
		if err != nil {
			return fmt.Errorf("encode endpoint PCM: %w", err)
		}
		if len(encoded) == 0 || len(encoded) > maxOpusPayloadBytes {
			return fmt.Errorf("encode endpoint PCM: payload size: %w", ErrCodec)
		}
		if err := s.local.WriteSample(media.Sample{
			Data:     encoded,
			Duration: s.format.FrameDuration,
		}); err != nil {
			if s.ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("write browser RTP: %w", ErrTransportClosed)
		}
	}
}

func (s *Session) receiveLoop() error {
	if err := s.waitConnected(); err != nil {
		return err
	}
	var track *webrtc.TrackRemote
	select {
	case track = <-s.events.track:
	case <-s.ctx.Done():
		return nil
	}
	codec := track.Codec()
	if !strings.EqualFold(codec.MimeType, webrtc.MimeTypeOpus) ||
		codec.ClockRate != RTPClockRate ||
		codec.Channels != 2 {
		return ErrUnsupportedCodec
	}
	for {
		packet, _, err := track.ReadRTP()
		if err != nil {
			if s.ctx.Err() != nil {
				return nil
			}
			return ErrTransportClosed
		}
		if err := s.jitter.push(packet); err != nil {
			return err
		}
	}
}

func (s *Session) playbackLoop() error {
	if err := s.waitConnected(); err != nil {
		return err
	}
	if err := s.subscription.Start(s.ctx); err != nil {
		if s.ctx.Err() != nil {
			return nil
		}
		return fmt.Errorf("start PCM hub: %w", err)
	}
	select {
	case <-s.jitter.ready:
	case <-s.ctx.Done():
		return nil
	}
	startup := time.NewTimer(s.playout.config.StartupDelay)
	defer startup.Stop()
	select {
	case <-startup.C:
	case <-s.ctx.Done():
		return nil
	}
	if !s.jitter.start() {
		return ErrInvalidRTP
	}

	frame := make([]byte, s.format.FrameBytes())
	ticker := time.NewTicker(s.format.FrameDuration)
	defer ticker.Stop()
	for {
		if err := s.playout.nextFrame(frame); err != nil {
			return err
		}
		if err := s.hub.WritePCM(s.ctx, frame); err != nil {
			if s.ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("write PCM hub: %w", err)
		}
		select {
		case <-ticker.C:
		case <-s.ctx.Done():
			return nil
		}
	}
}

func (s *Session) rtcpLoop() error {
	if err := s.waitConnected(); err != nil {
		return err
	}
	buffer := make([]byte, 1500)
	for {
		if _, _, err := s.sender.Read(buffer); err != nil {
			if s.ctx.Err() != nil {
				return nil
			}
			return ErrTransportClosed
		}
	}
}

func (s *Session) connectionLoop() error {
	var (
		recoveryTimer *time.Timer
		recovery      <-chan time.Time
	)
	stopRecovery := func() {
		if recoveryTimer == nil {
			return
		}
		if !recoveryTimer.Stop() {
			select {
			case <-recoveryTimer.C:
			default:
			}
		}
		recoveryTimer = nil
		recovery = nil
	}
	defer stopRecovery()

	for {
		select {
		case state := <-s.events.state:
			switch state {
			case webrtc.PeerConnectionStateConnected:
				stopRecovery()
			case webrtc.PeerConnectionStateDisconnected,
				webrtc.PeerConnectionStateFailed:
				if recoveryTimer == nil {
					recoveryTimer = time.NewTimer(s.recoveryTime)
					recovery = recoveryTimer.C
				}
			}
		case <-recovery:
			return ErrTransportClosed
		case err := <-s.events.failure:
			return err
		case <-s.ctx.Done():
			return nil
		}
	}
}

func (s *Session) waitConnected() error {
	select {
	case <-s.events.connected:
		return nil
	case err := <-s.events.failure:
		return err
	case <-s.ctx.Done():
		return nil
	}
}

func (s *Session) setReasonIfNil(err error) {
	s.reasonMu.Lock()
	if s.reason == nil {
		s.reason = err
	}
	s.reasonMu.Unlock()
}
