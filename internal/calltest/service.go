package calltest

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/human-agent65535/modemdeck/internal/callmedia"
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
type session struct {
	status Status
	owner  string
	ctx    context.Context
	cancel context.CancelFunc
	core   *callmedia.Core
	audio  *audioEndpoint
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
		if existing.owner == owner && (existing.ctx.Err() == nil || time.Since(existing.status.AcceptedAt) < 30*time.Second) {
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
	random[6] = random[6]&0x0f | 0x40
	random[8] = random[8]&0x3f | 0x80
	id := fmt.Sprintf("test-%x-%x-%x-%x-%x", random[:4], random[4:6], random[6:8], random[8:10], random[10:])
	audio := newAudioEndpoint()
	core, err := callmedia.New(callmedia.Options{EndpointOpener: audio})
	if err != nil {
		return Status{}, err
	}
	ctx, cancel := context.WithCancel(s.ctx)
	now := time.Now().UTC()
	entry := &session{status: Status{ID: id, AcceptedAt: now, ExpiresAt: now.Add(35 * time.Second), Phase: "scheduled", TestPhase: "scheduled"}, owner: owner, ctx: ctx, cancel: cancel, core: core, audio: audio}
	s.sessions[id] = entry
	s.workers.Add(1)
	go s.run(entry, userID, credentialID)
	return entry.status, nil
}

func (s *Service) run(entry *session, userID, credentialID string) {
	defer s.workers.Done()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := entry.core.Close(ctx); err != nil {
			s.logger.Warn("close test call media", "error", err)
		}
		cancel()
		_ = entry.audio.Close()
		select {
		case <-s.ctx.Done():
		case <-time.After(30 * time.Second):
		}
		s.mu.Lock()
		delete(s.sessions, entry.status.ID)
		s.mu.Unlock()
	}()
	select {
	case <-entry.ctx.Done():
		return
	case <-time.After(5 * time.Second):
	}
	s.mu.Lock()
	if entry.ctx.Err() != nil {
		s.mu.Unlock()
		return
	}
	entry.status.Phase = "ringing"
	entry.status.TestPhase = "sending"
	s.mu.Unlock()
	pushContext, cancel := context.WithTimeout(entry.ctx, 12*time.Second)
	err := s.push.SendAudioTestCall(pushContext, userID, credentialID, entry.status.ID)
	cancel()
	if err != nil {
		s.logger.Warn("send audio test call", "call_id", entry.status.ID, "error", err)
		s.finish(entry, "push_failed")
		return
	}
	s.mu.Lock()
	if entry.status.Phase == "ringing" {
		entry.status.TestPhase = "ringing"
	}
	s.mu.Unlock()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-entry.ctx.Done():
			return
		case <-ticker.C:
			s.mu.Lock()
			expired := !time.Now().Before(entry.status.ExpiresAt)
			s.mu.Unlock()
			if expired {
				s.finish(entry, "")
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
	s.mu.Lock()
	status := entry.status
	s.mu.Unlock()
	if status.Phase == "active" {
		status.TestPhase = entry.audio.Phase()
	}
	return status, nil
}
func (s *Service) Answer(id, owner string) (Status, error) {
	entry, err := s.lookup(id, owner)
	if err != nil {
		return Status{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if entry.ctx.Err() != nil || !time.Now().Before(entry.status.ExpiresAt) {
		return Status{}, ErrEnded
	}
	if entry.status.Phase == "active" {
		return entry.status, nil
	}
	if entry.status.Phase != "ringing" {
		return Status{}, ErrEnded
	}
	entry.status.Phase = "active"
	entry.status.TestPhase = "connecting"
	entry.status.ExpiresAt = time.Now().UTC().Add(60 * time.Second)
	return entry.status, nil
}
func (s *Service) finish(entry *session, failure string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if entry.ctx.Err() != nil {
		return
	}
	entry.status.Phase = "ended"
	entry.status.TestPhase = "completed"
	if failure != "" {
		entry.status.Phase = "failed"
		entry.status.FailureCode = failure
	}
	entry.cancel()
}
func (s *Service) End(id, owner string) error {
	entry, err := s.lookup(id, owner)
	if err != nil {
		return err
	}
	s.finish(entry, "")
	return nil
}
func (s *Service) Configuration(ctx context.Context, id, owner string) (rtcconfig.Configuration, error) {
	status, err := s.Status(id, owner)
	if err != nil {
		return rtcconfig.Configuration{}, err
	}
	if status.Phase != "active" || !time.Now().Before(status.ExpiresAt) {
		return rtcconfig.Configuration{}, ErrEnded
	}
	configuration, err := s.rtc.Generate(ctx)
	if err != nil {
		return rtcconfig.Configuration{}, ErrUnavailable
	}
	configuration.RelayOnly = true
	return configuration, nil
}
func (s *Service) Exchange(ctx context.Context, id, owner, token, offer string) (string, error) {
	entry, err := s.lookup(id, owner)
	if err != nil {
		return "", err
	}
	configuration, err := s.Configuration(ctx, id, owner)
	if err != nil {
		return "", err
	}
	result, err := entry.core.Exchange(ctx, callmedia.Offer{Call: callmedia.ActiveCall{ID: id, State: callmedia.CallStateActive}, OwnerToken: token, SDP: offer, RTCConfiguration: configuration})
	if err != nil {
		s.finish(entry, "media_failed")
		return "", err
	}
	go func() { <-result.Session.Done(); s.finish(entry, "") }()
	return result.AnswerSDP, nil
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
