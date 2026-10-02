package calltest

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/human-agent65535/modemdeck/internal/calllease"
	"github.com/human-agent65535/modemdeck/internal/mediaapp"
	"github.com/human-agent65535/modemdeck/internal/rtcconfig"
)

var (
	ErrNotFound    = errors.New("test call not found")
	ErrBusy        = errors.New("a test call is already pending or was started recently")
	ErrEnded       = errors.New("test call is no longer active")
	ErrUnavailable = errors.New("test calls require Apple push and TURN")
)

type PushSender interface {
	SendAudioTestCall(context.Context, string, string, string) error
}
type Status struct {
	ID          string    `json:"id"`
	AcceptedAt  time.Time `json:"accepted_at"`
	ExpiresAt   time.Time `json:"expires_at"`
	Phase       string    `json:"phase"`
	TestPhase   string    `json:"test_phase"`
	FailureCode string    `json:"failure_code,omitempty"`
}

type Service struct {
	mu       sync.Mutex
	sessions map[string]*session
	push     PushSender
	rtc      rtcconfig.Provider
	logger   *slog.Logger
	ctx      context.Context
	cancel   context.CancelFunc
	workers  sync.WaitGroup
}

func New(push PushSender, rtc rtcconfig.Provider, logger *slog.Logger) *Service {
	ctx, cancel := context.WithCancel(context.Background())
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{sessions: make(map[string]*session), push: push, rtc: rtc, logger: logger, ctx: ctx, cancel: cancel}
}

func (s *Service) Start(userID, credentialID string) (Status, error) {
	if s == nil || s.push == nil || s.rtc == nil || userID == "" || credentialID == "" {
		return Status{}, ErrUnavailable
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ctx.Err() != nil {
		return Status{}, ErrUnavailable
	}
	owner := userID + "\x00" + credentialID
	for _, existing := range s.sessions {
		if existing.owner == owner && (existing.ctx.Err() == nil || time.Since(existing.snapshot().AcceptedAt) < 30*time.Second) {
			return Status{}, ErrBusy
		}
	}
	if len(s.sessions) >= 16 {
		return Status{}, ErrBusy
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return Status{}, err
	}
	random[6], random[8] = random[6]&0x0f|0x40, random[8]&0x3f|0x80
	id := fmt.Sprintf("test-%x-%x-%x-%x-%x", random[:4], random[4:6], random[6:8], random[8:10], random[10:])
	ctx, cancel := context.WithCancel(s.ctx)
	now := time.Now().UTC()
	entry := &session{status: Status{ID: id, AcceptedAt: now, ExpiresAt: now.Add(35 * time.Second), Phase: "scheduled", TestPhase: "scheduled"}, owner: owner, ctx: ctx, cancel: cancel, audio: newAudioEndpoint()}
	runtime, err := mediaapp.NewRuntime(mediaapp.RuntimeOptions{
		Calls: entry, Refresher: entry, Controller: entry, EndpointOpener: entry.audio, RTCProvider: s.rtc,
		LeaseOptions: calllease.Options{Report: func(err error) { s.logger.Warn("test call ownership cleanup failed", "call_id", id, "error", err) }},
	})
	if err != nil {
		cancel()
		_ = entry.audio.Close()
		return Status{}, err
	}
	entry.runtime = runtime
	entry.lifecycle, err = runtime.Lifecycle()
	if err == nil {
		// Publish the empty pre-call baseline before a new ringing call appears,
		// matching production startup without treating it as an orphaned old call.
		_, err = entry.Refresh(ctx)
	}
	if err != nil {
		cancel()
		_ = runtime.Media.Close(context.Background())
		_ = entry.audio.Close()
		return Status{}, err
	}
	s.sessions[id] = entry
	s.workers.Add(1)
	go s.run(entry, userID, credentialID)
	return entry.snapshot(), nil
}

func (s *Service) run(entry *session, userID, credentialID string) {
	defer s.workers.Done()
	leaseDone := make(chan struct{})
	go func() { defer close(leaseDone); entry.runtime.Leases.Run(entry.ctx) }()
	defer func() {
		entry.cancel()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := entry.runtime.Media.Close(ctx); err != nil {
			s.logger.Warn("close test call media", "error", err)
		}
		cancel()
		_ = entry.audio.Close()
		<-leaseDone
		select {
		case <-s.ctx.Done():
		case <-time.After(30 * time.Second):
		}
		s.mu.Lock()
		delete(s.sessions, entry.snapshot().ID)
		s.mu.Unlock()
	}()
	select {
	case <-entry.ctx.Done():
		return
	case <-time.After(5 * time.Second):
	}
	entry.mu.Lock()
	if entry.ctx.Err() != nil {
		entry.mu.Unlock()
		return
	}
	entry.status.Phase, entry.status.TestPhase = "ringing", "sending"
	entry.mu.Unlock()
	if _, err := entry.Refresh(entry.ctx); err != nil {
		entry.finish("media_failed")
		return
	}
	pushContext, cancel := context.WithTimeout(entry.ctx, 12*time.Second)
	err := s.push.SendAudioTestCall(pushContext, userID, credentialID, entry.snapshot().ID)
	cancel()
	if err != nil {
		s.logger.Warn("send audio test call", "call_id", entry.snapshot().ID, "error", err)
		entry.finish("push_failed")
		return
	}
	entry.mu.Lock()
	if entry.status.Phase == "ringing" {
		entry.status.TestPhase = "ringing"
	}
	entry.mu.Unlock()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-entry.ctx.Done():
			return
		case <-ticker.C:
			if !time.Now().Before(entry.snapshot().ExpiresAt) {
				entry.finish("")
				return
			}
		}
	}
}

func (s *Service) lookup(id, owner string) (*session, error) {
	if s == nil {
		return nil, ErrUnavailable
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entry := s.sessions[id]
	if entry == nil || owner == "" || entry.owner != owner {
		return nil, ErrNotFound
	}
	return entry, nil
}

func (s *Service) Status(id, owner string) (Status, error) {
	entry, err := s.lookup(id, owner)
	if err != nil {
		return Status{}, err
	}
	status := entry.snapshot()
	if status.Phase == "active" {
		status.TestPhase = entry.audio.Phase()
		// Build 22 uses authenticated status polling as its control heartbeat.
		// New clients use the same PUT lease endpoint as production calls.
		_, _ = entry.runtime.Leases.Renew(entry.ctx, id, owner)
	}
	return status, nil
}

func (s *Service) Answer(id, owner string) (Status, error) {
	entry, err := s.lookup(id, owner)
	if err != nil {
		return Status{}, err
	}
	entry.controlMu.Lock()
	defer entry.controlMu.Unlock()
	status := entry.snapshot()
	if entry.ctx.Err() != nil || !time.Now().Before(status.ExpiresAt) {
		return Status{}, ErrEnded
	}
	if status.Phase == "active" {
		return status, nil
	}
	if status.Phase != "ringing" {
		return Status{}, ErrEnded
	}
	if _, err := entry.runtime.Leases.Claim(entry.ctx, id, owner); err != nil {
		return Status{}, err
	}
	entry.mu.Lock()
	entry.status.Phase, entry.status.TestPhase = "active", "connecting"
	entry.status.ExpiresAt = time.Now().UTC().Add(60 * time.Second)
	entry.mu.Unlock()
	if err := entry.reconcile(entry.ctx); err != nil {
		return Status{}, err
	}
	return entry.snapshot(), nil
}

func (s *Service) End(id, owner string) error {
	entry, err := s.lookup(id, owner)
	if err != nil {
		return err
	}
	entry.finish("")
	return nil
}

func (s *Service) Active(ctx context.Context, id, owner string) (calllease.ActiveProjection, string, error) {
	entry, err := s.lookup(id, owner)
	if err != nil {
		return calllease.ActiveProjection{}, "", err
	}
	if _, err := entry.Refresh(ctx); err != nil {
		return calllease.ActiveProjection{}, "", err
	}
	calls, err := entry.ActiveCalls(ctx)
	if err != nil {
		return calllease.ActiveProjection{}, "", err
	}
	projection, err := entry.runtime.Leases.ProjectActive(calls, owner)
	return projection, entry.audio.Phase(), err
}

func (s *Service) Renew(ctx context.Context, id, owner string) (calllease.Status, error) {
	entry, err := s.lookup(id, owner)
	if err != nil {
		return calllease.Status{}, err
	}
	return entry.runtime.Leases.Renew(ctx, id, owner)
}

func (s *Service) Configuration(ctx context.Context, id, owner string) (rtcconfig.Configuration, error) {
	entry, err := s.lookup(id, owner)
	if err != nil {
		return rtcconfig.Configuration{}, err
	}
	if err := entry.runtime.Leases.Require(ctx, id, owner); err != nil {
		return rtcconfig.Configuration{}, err
	}
	return entry.runtime.Media.Configuration(ctx, true)
}

func (s *Service) Exchange(ctx context.Context, id, owner, token, offer string) (string, error) {
	entry, err := s.lookup(id, owner)
	if err != nil {
		return "", err
	}
	if err := entry.runtime.Leases.Require(ctx, id, owner); err != nil {
		return "", err
	}
	return entry.runtime.Media.Exchange(ctx, id, token, offer, true)
}

func (s *Service) Release(ctx context.Context, id, owner, token string) error {
	entry, err := s.lookup(id, owner)
	if err != nil {
		return err
	}
	if err := entry.runtime.Leases.Require(ctx, id, owner); err != nil {
		return err
	}
	return entry.runtime.Media.ReleaseOwner(ctx, id, token)
}

func (s *Service) Close() {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.cancel()
	s.mu.Unlock()
	s.workers.Wait()
}
