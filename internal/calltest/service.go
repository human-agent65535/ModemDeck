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
	"github.com/human-agent65535/modemdeck/internal/callmedia"
	"github.com/human-agent65535/modemdeck/internal/mediaapp"
)

var (
	ErrNotFound    = errors.New("test call not found")
	ErrBusy        = errors.New("a test call is already pending or was started recently")
	ErrEnded       = errors.New("test call is no longer active")
	ErrUnavailable = errors.New("call tests are unavailable")
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

const (
	// Push tests keep a per-device cooldown so repeated starts cannot spam a
	// paired phone. Browser tests ring locally and may restart once ended.
	pushTestCooldown = 30 * time.Second
	maxActiveTests   = 16
	maxActivePerUser = 2
	// Ended tests stay readable briefly for final status. Bound that history per
	// account too, so restarting quickly cannot fill the shared limit.
	maxRetainedTests   = 64
	maxRetainedPerUser = 6
	// LegacyAdminUser groups tests from single-administrator sessions, which have
	// no user ID. It cannot collide with UserKey values.
	LegacyAdminUser = "admin"
)

// UserKey groups an account's iOS and browser tests under one limit.
func UserKey(userID string) string {
	return "user\x00" + userID
}

type Service struct {
	mu       sync.Mutex
	sessions map[string]*session
	starting map[string]string
	push     PushSender
	logger   *slog.Logger
	ctx      context.Context
	cancel   context.CancelFunc
	workers  sync.WaitGroup
	// beforeBuild runs after a start reserves capacity; tests use it to hold
	// a start in flight. It is nil in production.
	beforeBuild func()
}

func New(push PushSender, logger *slog.Logger) *Service {
	ctx, cancel := context.WithCancel(context.Background())
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{sessions: make(map[string]*session), starting: make(map[string]string), push: push, logger: logger, ctx: ctx, cancel: cancel}
}

func (s *Service) Start(userID, credentialID string, language ...string) (Status, error) {
	if s == nil || s.push == nil || userID == "" || credentialID == "" {
		return Status{}, ErrUnavailable
	}
	return s.start(userID+"\x00"+credentialID, UserKey(userID), userID, credentialID, language...)
}

// StartBrowser connects a session-scoped test directly, without Apple push or
// modem access. It shares the same leases, audio endpoint and lifetime as iOS.
// The user key (UserKey or LegacyAdminUser) bounds one account's tests.
func (s *Service) StartBrowser(owner, user string, language ...string) (Status, error) {
	if user == "" {
		return Status{}, ErrUnavailable
	}
	status, err := s.start(owner, user, "", "", language...)
	if err != nil {
		return Status{}, err
	}
	answered, err := s.Answer(status.ID, owner)
	if err != nil {
		_ = s.End(status.ID, owner)
	}
	return answered, err
}

// admitLocked reserves capacity before the runtime is built outside s.mu.
func (s *Service) admitLocked(owner, user string) error {
	if s.ctx.Err() != nil {
		return ErrUnavailable
	}
	active, userActive, userRetained := 0, 0, 0
	for reservedOwner, reservedUser := range s.starting {
		if reservedOwner == owner {
			return ErrBusy
		}
		active++
		if reservedUser == user {
			userActive++
			userRetained++
		}
	}
	for _, existing := range s.sessions {
		live := existing.ctx.Err() == nil
		if existing.owner == owner && (live ||
			existing.push && time.Since(existing.snapshot().AcceptedAt) < pushTestCooldown) {
			return ErrBusy
		}
		if existing.user == user {
			userRetained++
		}
		if live {
			active++
			if existing.user == user {
				userActive++
			}
		}
	}
	if active >= maxActiveTests || userActive >= maxActivePerUser ||
		userRetained >= maxRetainedPerUser ||
		len(s.sessions)+len(s.starting) >= maxRetainedTests {
		return ErrBusy
	}
	return nil
}

func (s *Service) start(owner, user, userID, credentialID string, language ...string) (Status, error) {
	if s == nil || owner == "" || user == "" {
		return Status{}, ErrUnavailable
	}
	s.mu.Lock()
	if err := s.admitLocked(owner, user); err != nil {
		s.mu.Unlock()
		return Status{}, err
	}
	s.starting[owner] = user
	// Count the in-flight start as a worker so Close waits for the runtime
	// built outside the lock; run takes over this count on success.
	s.workers.Add(1)
	s.mu.Unlock()

	if s.beforeBuild != nil {
		s.beforeBuild()
	}
	entry, err := s.newSession(owner, user, credentialID, language...)

	s.mu.Lock()
	delete(s.starting, owner)
	if err == nil && s.ctx.Err() != nil {
		entry.close()
		err = ErrUnavailable
	}
	if err != nil {
		s.mu.Unlock()
		s.workers.Done()
		return Status{}, err
	}
	s.sessions[entry.status.ID] = entry
	s.mu.Unlock()
	go s.run(entry, userID, credentialID)
	return entry.snapshot(), nil
}

func (s *Service) newSession(owner, user, credentialID string, language ...string) (*session, error) {
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return nil, err
	}
	random[6], random[8] = random[6]&0x0f|0x40, random[8]&0x3f|0x80
	id := fmt.Sprintf("test-%x-%x-%x-%x-%x", random[:4], random[4:6], random[6:8], random[8:10], random[10:])
	ctx, cancel := context.WithCancel(s.ctx)
	now := time.Now().UTC()
	entry := &session{status: Status{ID: id, AcceptedAt: now, ExpiresAt: now.Add(35 * time.Second), Phase: "scheduled", TestPhase: "scheduled"}, owner: owner, user: user, push: credentialID != "", ctx: ctx, cancel: cancel, audio: newAudioEndpoint(language...)}
	runtime, err := mediaapp.NewRuntime(mediaapp.RuntimeOptions{
		Calls: entry, Refresher: entry, Controller: entry, EndpointOpener: entry.audio,
		OnAudioStats: callmedia.AudioStatsLogger(s.logger),
		LeaseOptions: calllease.Options{Report: func(err error) { s.logger.Warn("test call ownership cleanup failed", "call_id", id, "error", err) }},
	})
	if err != nil {
		cancel()
		_ = entry.audio.Close()
		return nil, err
	}
	entry.runtime = runtime
	entry.lifecycle, err = runtime.Lifecycle()
	if err == nil {
		// Publish the empty pre-call baseline before a new ringing call appears,
		// matching production startup without treating it as an orphaned old call.
		_, err = entry.Refresh(ctx)
	}
	if err == nil && credentialID == "" {
		entry.mu.Lock()
		entry.status.Phase, entry.status.TestPhase = "ringing", "ringing"
		entry.mu.Unlock()
		_, err = entry.Refresh(ctx)
	}
	if err != nil {
		entry.close()
		return nil, err
	}
	return entry, nil
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
	if credentialID != "" {
		if !s.ring(entry, userID, credentialID) {
			return
		}
	}
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

func (s *Service) ring(entry *session, userID, credentialID string) bool {
	select {
	case <-entry.ctx.Done():
		return false
	case <-time.After(5 * time.Second):
	}
	entry.mu.Lock()
	if entry.ctx.Err() != nil {
		entry.mu.Unlock()
		return false
	}
	entry.status.Phase, entry.status.TestPhase = "ringing", "sending"
	entry.mu.Unlock()
	if _, err := entry.Refresh(entry.ctx); err != nil {
		entry.finish("media_failed")
		return false
	}
	pushContext, cancel := context.WithTimeout(entry.ctx, 12*time.Second)
	err := s.push.SendAudioTestCall(pushContext, userID, credentialID, entry.snapshot().ID)
	cancel()
	if err != nil {
		s.logger.Warn("send audio test call", "call_id", entry.snapshot().ID, "error", err)
		entry.finish("push_failed")
		return false
	}
	entry.mu.Lock()
	if entry.status.Phase == "ringing" {
		entry.status.TestPhase = "ringing"
	}
	entry.mu.Unlock()
	return true
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
	status, _, err := s.StatusWithAudio(id, owner)
	return status, err
}

// StatusWithAudio reads the call phase and audio counters from one lookup so a
// status poll reports a consistent snapshot.
func (s *Service) StatusWithAudio(id, owner string) (Status, AudioStatus, error) {
	entry, err := s.lookup(id, owner)
	if err != nil {
		return Status{}, AudioStatus{}, err
	}
	status := entry.snapshot()
	if status.Phase == "active" {
		status.TestPhase = entry.audio.Phase()
		// Build 22 uses authenticated status polling as its control heartbeat.
		// New clients use the same PUT lease endpoint as production calls.
		_, _ = entry.runtime.Leases.Renew(entry.ctx, id, owner)
	}
	return status, entry.audioStatus(), nil
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

func (s *Service) AudioStatus(id, owner string) (AudioStatus, error) {
	entry, err := s.lookup(id, owner)
	if err != nil {
		return AudioStatus{}, err
	}
	return entry.audioStatus(), nil
}

func (s *Service) Renew(ctx context.Context, id, owner string) (calllease.Status, error) {
	entry, err := s.lookup(id, owner)
	if err != nil {
		return calllease.Status{}, err
	}
	return entry.runtime.Leases.Renew(ctx, id, owner)
}

func (s *Service) RequireMedia(ctx context.Context, id, owner string) error {
	entry, err := s.lookup(id, owner)
	if err != nil {
		return err
	}
	return entry.runtime.Leases.Require(ctx, id, owner)
}

func (s *Service) OpenSocket(ctx context.Context, id, owner, token string, transport callmedia.SocketTransport) (*callmedia.Session, error) {
	entry, err := s.lookup(id, owner)
	if err != nil {
		return nil, err
	}
	if err := entry.runtime.Leases.Require(ctx, id, owner); err != nil {
		return nil, err
	}
	return entry.runtime.Media.OpenSocket(ctx, id, token, transport)
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
