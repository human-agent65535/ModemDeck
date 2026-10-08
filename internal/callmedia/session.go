package callmedia

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

type sessionEvents struct {
	connected   chan struct{}
	connectOnce sync.Once
}

func newSessionEvents() *sessionEvents  { return &sessionEvents{connected: make(chan struct{})} }
func (e *sessionEvents) markConnected() { e.connectOnce.Do(func() { close(e.connected) }) }

// Session owns one authenticated WSS client and its codec. The shared PCM hub
// owns the host endpoint independently so recording can outlive the client.
type Session struct {
	callID       string
	format       PCMFormat
	hub          *mediaHub
	subscription *DuplexSubscription
	codec        OpusCodec
	events       *sessionEvents
	stats        audioCounters
	baseStats    AudioStatistics
	reportStats  func(string, AudioStatistics)
	socket       *socketAudio
	ctx          context.Context
	cancel       context.CancelFunc
	startOnce    sync.Once
	stopOnce     sync.Once
	done         chan struct{}
	workers      sync.WaitGroup
	reasonMu     sync.Mutex
	reason       error
}

func newSession(parent context.Context, callID string, format PCMFormat, hub *mediaHub, subscription *DuplexSubscription, codec OpusCodec) *Session {
	ctx, cancel := context.WithCancel(parent)
	return &Session{callID: callID, format: format, hub: hub, subscription: subscription, codec: codec, events: newSessionEvents(), ctx: ctx, cancel: cancel, done: make(chan struct{})}
}
func (s *Session) start() {
	s.startOnce.Do(func() {
		s.stats.inputDBFS.Store(-96)
		s.stats.inputPeakDBFS.Store(-96)
		s.stats.outputDBFS.Store(-96)
		s.workers.Add(3)
		go s.runWorker(s.socketCaptureLoop)
		go s.runWorker(s.socketReceiveLoop)
		go s.runWorker(s.socketPlaybackLoop)
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
	s.socket.transport.InterruptRead()
	s.hub.detachReceiver(s)
	if err := s.subscription.Close(); err != nil {
		cleanupError = errors.Join(cleanupError, fmt.Errorf("close PCM subscription: %w", err))
	}
	s.workers.Wait()
	s.socket.transport.Finish(s.Err(), s.Statistics())
	_ = s.socket.transport.Close()
	if s.reportStats != nil {
		s.reportStats(s.callID, s.Statistics())
	}
	s.socket.receiver.Close()
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

func (s *Session) setReasonIfNil(err error) {
	s.reasonMu.Lock()
	if s.reason == nil {
		s.reason = err
	}
	s.reasonMu.Unlock()
}
