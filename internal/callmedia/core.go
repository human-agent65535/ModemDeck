package callmedia

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/pion/webrtc/v4"
)

const (
	defaultGatheringTimeout = 5 * time.Second
	defaultRecoveryTimeout  = 15 * time.Second
)

type Options struct {
	EndpointOpener    MediaEndpointOpener
	CodecFactory      OpusCodecFactory
	PeerConfiguration webrtc.Configuration
	GatheringTimeout  time.Duration
	RecoveryTimeout   time.Duration
	Jitter            JitterConfig
}

type Core struct {
	opener        MediaEndpointOpener
	codecs        OpusCodecFactory
	api           *webrtc.API
	configuration webrtc.Configuration
	gatherTimeout time.Duration
	recoveryTime  time.Duration
	jitter        JitterConfig

	ctx    context.Context
	cancel context.CancelFunc

	mu             sync.Mutex
	closed         bool
	owners         map[string]*mediaOwner
	hubs           map[string]*hubEntry
	lifetimes      map[string]*callLifetime
	authority      map[string]struct{}
	authorityKnown bool
	prepares       sync.WaitGroup
	openings       sync.WaitGroup
}

type callLifetime struct {
	ctx    context.Context
	cancel context.CancelFunc
}

type mediaOwner struct {
	token   string
	ctx     context.Context
	cancel  context.CancelFunc
	session *Session
	done    chan struct{}
	once    sync.Once
}

func (o *mediaOwner) finish() {
	if o == nil {
		return
	}
	o.once.Do(func() { close(o.done) })
}

type hubEntry struct {
	ready chan struct{}
	hub   *mediaHub
	err   error
}

func New(options Options) (*Core, error) {
	if options.EndpointOpener == nil ||
		options.GatheringTimeout < 0 ||
		options.RecoveryTimeout < 0 {
		return nil, fmt.Errorf("create call media core: %w", ErrInvalidArgument)
	}
	codecs := options.CodecFactory
	if codecs == nil {
		var err error
		codecs, err = NewProductionOpusFactory()
		if err != nil {
			return nil, err
		}
	}
	jitter, err := options.Jitter.normalized()
	if err != nil {
		return nil, fmt.Errorf("create call media core: jitter: %w", err)
	}
	api, err := newWebRTCAPI()
	if err != nil {
		return nil, fmt.Errorf("create call media core: Pion: %w", ErrNegotiation)
	}
	timeout := options.GatheringTimeout
	if timeout == 0 {
		timeout = defaultGatheringTimeout
	}
	recoveryTime := options.RecoveryTimeout
	if recoveryTime == 0 {
		recoveryTime = defaultRecoveryTimeout
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Core{
		opener:        options.EndpointOpener,
		codecs:        codecs,
		api:           api,
		configuration: clonePeerConfiguration(options.PeerConfiguration),
		gatherTimeout: timeout,
		recoveryTime:  recoveryTime,
		jitter:        jitter,
		ctx:           ctx,
		cancel:        cancel,
		owners:        make(map[string]*mediaOwner),
		hubs:          make(map[string]*hubEntry),
		lifetimes:     make(map[string]*callLifetime),
		authority:     make(map[string]struct{}),
	}, nil
}

func (c *Core) Exchange(ctx context.Context, offer Offer) (ExchangeResult, error) {
	if c == nil {
		return ExchangeResult{}, ErrCoreClosed
	}
	ctx = normalizeContext(ctx)
	call, err := normalizeActiveCall(offer.Call)
	if err != nil {
		return ExchangeResult{}, fmt.Errorf("exchange WebRTC offer: %w", err)
	}
	ownerToken, err := normalizeOwnerToken(offer.OwnerToken)
	if err != nil {
		return ExchangeResult{}, fmt.Errorf("exchange WebRTC offer: owner token: %w", err)
	}
	if err := validateOfferSDP(offer.SDP); err != nil {
		return ExchangeResult{}, fmt.Errorf("exchange WebRTC offer: %w", err)
	}
	lifetime, owner, err := c.reserve(call.ID, ownerToken)
	if err != nil {
		return ExchangeResult{}, fmt.Errorf("exchange WebRTC offer: %w", err)
	}
	defer c.prepares.Done()
	committed := false
	defer func() {
		if !committed {
			c.releaseReservation(call.ID, owner)
		}
	}()

	prepareContext, cancel := context.WithCancel(ctx)
	stopCallCancel := context.AfterFunc(lifetime.ctx, cancel)
	stopOwnerCancel := context.AfterFunc(owner.ctx, cancel)
	defer func() {
		stopCallCancel()
		stopOwnerCancel()
		cancel()
	}()

	peer, localTrack, sender, events, err := c.preparePeer(
		offer.RTCConfiguration,
	)
	if err != nil {
		return ExchangeResult{}, err
	}
	keepPeer := false
	defer func() {
		if !keepPeer {
			_ = peer.Close()
		}
	}()

	if err := peer.SetRemoteDescription(webrtc.SessionDescription{
		Type: webrtc.SDPTypeOffer,
		SDP:  offer.SDP,
	}); err != nil {
		return ExchangeResult{}, fmt.Errorf("exchange WebRTC offer: set remote description: %w", ErrNegotiation)
	}

	hub, err := c.acquireHub(prepareContext, call, lifetime)
	if err != nil {
		return ExchangeResult{}, fmt.Errorf("exchange WebRTC offer: %w", err)
	}
	format := hub.format

	codec, codecErr := c.codecs.New(format)
	keepCodec := false
	defer func() {
		if codec != nil && !keepCodec {
			_ = codec.Close()
		}
	}()
	if codecErr != nil {
		return ExchangeResult{}, fmt.Errorf(
			"exchange WebRTC offer: create Opus codec: %w",
			errors.Join(ErrCodec, codecErr),
		)
	}
	if codec == nil {
		return ExchangeResult{}, fmt.Errorf("exchange WebRTC offer: create Opus codec: %w", ErrCodec)
	}
	if codec.Format() != format {
		return ExchangeResult{}, fmt.Errorf("exchange WebRTC offer: codec PCM format: %w", ErrCodec)
	}
	subscription, err := hub.Subscribe(defaultBrowserQueueCapacity)
	if err != nil {
		return ExchangeResult{}, fmt.Errorf("exchange WebRTC offer: subscribe PCM hub: %w", err)
	}
	keepSubscription := false
	defer func() {
		if !keepSubscription {
			_ = subscription.Close()
		}
	}()

	answer, err := peer.CreateAnswer(nil)
	if err != nil {
		return ExchangeResult{}, fmt.Errorf("exchange WebRTC offer: create answer: %w", ErrNegotiation)
	}
	gatheringComplete := webrtc.GatheringCompletePromise(peer)
	if err := peer.SetLocalDescription(answer); err != nil {
		return ExchangeResult{}, fmt.Errorf(
			"exchange WebRTC offer: set local description: %v: %w",
			err,
			ErrNegotiation,
		)
	}
	timer := time.NewTimer(c.gatherTimeout)
	defer timer.Stop()
	select {
	case <-gatheringComplete:
	case <-prepareContext.Done():
		return ExchangeResult{}, fmt.Errorf("exchange WebRTC offer: %w", ErrCanceled)
	case <-timer.C:
		return ExchangeResult{}, fmt.Errorf("exchange WebRTC offer: %w", ErrGatheringTimeout)
	}
	localDescription := peer.LocalDescription()
	if localDescription == nil || localDescription.Type != webrtc.SDPTypeAnswer || localDescription.SDP == "" {
		return ExchangeResult{}, fmt.Errorf("exchange WebRTC offer: local description: %w", ErrNegotiation)
	}
	browserAnswer, err := setAnswerPacketTime(*localDescription, format.FrameDuration)
	if err != nil {
		return ExchangeResult{}, fmt.Errorf("exchange WebRTC offer: packet time: %w", err)
	}
	if err := validateAnswerSDP(browserAnswer.SDP); err != nil {
		return ExchangeResult{}, fmt.Errorf("exchange WebRTC offer: %w", err)
	}
	if err := prepareContext.Err(); err != nil {
		return ExchangeResult{}, fmt.Errorf("exchange WebRTC offer: %w", ErrCanceled)
	}

	session := newSession(
		owner.ctx,
		call.ID,
		format,
		hub,
		subscription,
		codec,
		peer,
		localTrack,
		sender,
		events,
		c.jitter,
		c.recoveryTime,
	)
	if err := c.commit(call.ID, lifetime, owner, session); err != nil {
		return ExchangeResult{}, fmt.Errorf("exchange WebRTC offer: %w", err)
	}
	committed = true
	keepPeer = true
	keepCodec = true
	keepSubscription = true
	session.start()
	go c.releaseWhenDone(call.ID, session)
	return ExchangeResult{
		AnswerSDP: browserAnswer.SDP,
		Session:   session,
	}, nil
}

func (c *Core) CloseCall(ctx context.Context, callID string) error {
	if c == nil {
		return nil
	}
	ctx = normalizeContext(ctx)
	callID, err := normalizeCallID(callID)
	if err != nil {
		return err
	}
	c.mu.Lock()
	owner := c.owners[callID]
	entry := c.hubs[callID]
	lifetime := c.lifetimes[callID]
	if lifetime != nil {
		lifetime.cancel()
	}
	var hub *mediaHub
	if entry != nil {
		select {
		case <-entry.ready:
			hub = entry.hub
		default:
		}
		if c.hubs[callID] == entry {
			delete(c.hubs, callID)
		}
	}
	c.mu.Unlock()
	var closeErr error
	if owner != nil {
		owner.cancel()
		if owner.session != nil {
			closeErr = owner.session.shutdown(ctx)
		}
	}
	if hub != nil {
		closeErr = errors.Join(closeErr, hub.shutdown(ctx))
	}
	c.mu.Lock()
	if c.owners[callID] == owner {
		delete(c.owners, callID)
		owner.finish()
	}
	c.mu.Unlock()
	return closeErr
}

// ReleaseOwner releases only the matching browser media owner. The call
// lifetime and shared PCM hub remain available to recording and a later owner.
func (c *Core) ReleaseOwner(ctx context.Context, callID, ownerToken string) error {
	if c == nil {
		return nil
	}
	ctx = normalizeContext(ctx)
	callID, err := normalizeCallID(callID)
	if err != nil {
		return err
	}
	ownerToken, err = normalizeOwnerToken(ownerToken)
	if err != nil {
		return err
	}

	c.mu.Lock()
	owner := c.owners[callID]
	if owner == nil {
		c.mu.Unlock()
		return nil
	}
	if owner.token != ownerToken {
		c.mu.Unlock()
		return ErrNotMediaOwner
	}
	owner.cancel()
	session := owner.session
	done := owner.done
	c.mu.Unlock()

	if session != nil {
		if err := session.shutdown(ctx); err != nil {
			return err
		}
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ErrCanceled
	}
}

// ReconcileActiveCalls closes every call media lifetime no longer present in
// the authoritative call set. It is intentionally driven by communication
// snapshots rather than endpoint I/O or local call-control outcomes.
func (c *Core) ReconcileActiveCalls(ctx context.Context, activeCallIDs []string) error {
	if c == nil {
		return nil
	}
	ctx = normalizeContext(ctx)
	active := make(map[string]struct{}, len(activeCallIDs))
	for _, callID := range activeCallIDs {
		normalized, err := normalizeCallID(callID)
		if err != nil {
			return err
		}
		active[normalized] = struct{}{}
	}
	c.mu.Lock()
	c.authority = active
	c.authorityKnown = true
	stale := make(map[string]struct{})
	for callID := range c.lifetimes {
		if _, exists := active[callID]; !exists {
			stale[callID] = struct{}{}
		}
	}
	for callID := range c.owners {
		if _, exists := active[callID]; !exists {
			stale[callID] = struct{}{}
		}
	}
	for callID := range c.hubs {
		if _, exists := active[callID]; !exists {
			stale[callID] = struct{}{}
		}
	}
	c.mu.Unlock()

	var result error
	for callID := range stale {
		if err := c.CloseCall(ctx, callID); err != nil {
			result = errors.Join(result, err)
		}
	}
	c.mu.Lock()
	for callID := range stale {
		delete(c.lifetimes, callID)
	}
	c.mu.Unlock()
	return result
}

func (c *Core) Close(ctx context.Context) error {
	if c == nil {
		return nil
	}
	ctx = normalizeContext(ctx)
	c.mu.Lock()
	if !c.closed {
		c.closed = true
		c.cancel()
	}
	c.mu.Unlock()

	prepared := make(chan struct{})
	go func() {
		c.prepares.Wait()
		close(prepared)
	}()
	select {
	case <-prepared:
	case <-ctx.Done():
		return ErrCanceled
	}
	opened := make(chan struct{})
	go func() {
		c.openings.Wait()
		close(opened)
	}()
	select {
	case <-opened:
	case <-ctx.Done():
		return ErrCanceled
	}

	c.mu.Lock()
	sessions := make([]*Session, 0, len(c.owners))
	for _, owner := range c.owners {
		if owner != nil {
			owner.cancel()
			if owner.session != nil {
				sessions = append(sessions, owner.session)
			}
		}
	}
	hubs := make([]*mediaHub, 0, len(c.hubs))
	for _, entry := range c.hubs {
		if entry != nil && entry.hub != nil {
			hubs = append(hubs, entry.hub)
		}
	}
	c.mu.Unlock()
	for _, session := range sessions {
		session.stop(nil)
	}
	for _, session := range sessions {
		select {
		case <-session.Done():
		case <-ctx.Done():
			return ErrCanceled
		}
	}
	for _, hub := range hubs {
		if err := hub.shutdown(ctx); err != nil {
			return err
		}
	}
	c.mu.Lock()
	clear(c.owners)
	clear(c.hubs)
	clear(c.lifetimes)
	clear(c.authority)
	c.mu.Unlock()
	return nil
}

// SubscribeDuplex opens the one host PCM endpoint for an active call, or
// reuses the existing hub, and returns a bounded duplex subscription.
func (c *Core) SubscribeDuplex(
	ctx context.Context,
	active ActiveCall,
) (*DuplexSubscription, error) {
	if c == nil {
		return nil, ErrCoreClosed
	}
	call, err := normalizeActiveCall(active)
	if err != nil {
		return nil, err
	}
	lifetime, err := c.activeLifetime(call.ID)
	if err != nil {
		return nil, err
	}
	hub, err := c.acquireHub(normalizeContext(ctx), call, lifetime)
	if err != nil {
		return nil, err
	}
	return hub.Subscribe(defaultRecordingQueueCapacity)
}

func (c *Core) acquireHub(
	ctx context.Context,
	call ActiveCall,
	lifetime *callLifetime,
) (*mediaHub, error) {
	if lifetime == nil || lifetime.ctx.Err() != nil || ctx.Err() != nil {
		return nil, ErrCanceled
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, ErrCoreClosed
	}
	if entry := c.hubs[call.ID]; entry != nil {
		ready := entry.ready
		c.mu.Unlock()
		select {
		case <-ready:
			if entry.err != nil {
				return nil, entry.err
			}
			if entry.hub == nil {
				return nil, ErrEndpointUnavailable
			}
			return entry.hub, nil
		case <-ctx.Done():
			return nil, ErrCanceled
		case <-lifetime.ctx.Done():
			return nil, ErrCanceled
		case <-c.ctx.Done():
			return nil, ErrCoreClosed
		}
	}
	entry := &hubEntry{ready: make(chan struct{})}
	c.hubs[call.ID] = entry
	c.openings.Add(1)
	c.mu.Unlock()

	go c.openHub(call, lifetime, entry)
	select {
	case <-entry.ready:
		if entry.err != nil {
			return nil, entry.err
		}
		if entry.hub == nil {
			return nil, ErrEndpointUnavailable
		}
		return entry.hub, nil
	case <-ctx.Done():
		return nil, ErrCanceled
	case <-lifetime.ctx.Done():
		return nil, ErrCanceled
	case <-c.ctx.Done():
		return nil, ErrCoreClosed
	}
}

func (c *Core) openHub(call ActiveCall, lifetime *callLifetime, entry *hubEntry) {
	defer c.openings.Done()
	endpoint, openErr := c.opener.Open(lifetime.ctx, call)
	var (
		hub    *mediaHub
		hubErr error
	)
	switch {
	case lifetime.ctx.Err() != nil:
		hubErr = ErrCanceled
	case c.ctx.Err() != nil:
		hubErr = ErrCoreClosed
	case openErr != nil || endpoint == nil:
		hubErr = errors.Join(ErrEndpointUnavailable, openErr)
	default:
		hub, hubErr = newMediaHub(lifetime.ctx, call.ID, endpoint)
	}
	if hub == nil && endpoint != nil {
		_ = endpoint.Close()
	}

	c.mu.Lock()
	if c.hubs[call.ID] != entry || c.closed || lifetime.ctx.Err() != nil {
		if hubErr == nil {
			if c.closed {
				hubErr = ErrCoreClosed
			} else {
				hubErr = ErrCanceled
			}
		}
	} else {
		entry.hub = hub
		entry.err = hubErr
		if hubErr != nil {
			delete(c.hubs, call.ID)
		}
	}
	if entry.hub == nil && entry.err == nil {
		entry.err = hubErr
	}
	close(entry.ready)
	c.mu.Unlock()

	if entry.hub != hub && hub != nil {
		_ = hub.Close(context.Background())
		return
	}
	if hub != nil {
		go c.releaseHubWhenDone(call.ID, entry, hub)
	}
}

func (c *Core) releaseHubWhenDone(callID string, entry *hubEntry, hub *mediaHub) {
	<-hub.Done()
	c.mu.Lock()
	if c.hubs[callID] == entry && entry.hub == hub {
		delete(c.hubs, callID)
	}
	c.mu.Unlock()
}

func (c *Core) reserve(
	callID string,
	ownerToken string,
) (*callLifetime, *mediaOwner, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil, nil, ErrCoreClosed
	}
	if _, exists := c.owners[callID]; exists {
		return nil, nil, ErrCallInUse
	}
	lifetime, err := c.activeLifetimeLocked(callID)
	if err != nil {
		return nil, nil, err
	}
	ownerContext, ownerCancel := context.WithCancel(c.ctx)
	owner := &mediaOwner{
		token:  ownerToken,
		ctx:    ownerContext,
		cancel: ownerCancel,
		done:   make(chan struct{}),
	}
	c.owners[callID] = owner
	c.prepares.Add(1)
	return lifetime, owner, nil
}

func (c *Core) releaseReservation(callID string, owner *mediaOwner) {
	c.mu.Lock()
	if c.owners[callID] == owner && owner != nil && owner.session == nil {
		delete(c.owners, callID)
		owner.cancel()
		owner.finish()
	}
	c.mu.Unlock()
}

func (c *Core) commit(
	callID string,
	lifetime *callLifetime,
	owner *mediaOwner,
	session *Session,
) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return ErrCoreClosed
	}
	if lifetime == nil ||
		lifetime.ctx.Err() != nil ||
		c.lifetimes[callID] != lifetime {
		return ErrCanceled
	}
	current, exists := c.owners[callID]
	if !exists || current != owner || owner == nil || owner.session != nil {
		return ErrCallInUse
	}
	owner.session = session
	return nil
}

func (c *Core) activeLifetime(callID string) (*callLifetime, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.activeLifetimeLocked(callID)
}

func (c *Core) activeLifetimeLocked(callID string) (*callLifetime, error) {
	if c.closed {
		return nil, ErrCoreClosed
	}
	if !c.authorityKnown {
		return nil, ErrCallNotActive
	}
	if _, active := c.authority[callID]; !active {
		return nil, ErrCallNotActive
	}
	if lifetime := c.lifetimes[callID]; lifetime != nil {
		if lifetime.ctx.Err() != nil {
			return nil, ErrCallNotActive
		}
		return lifetime, nil
	}
	ctx, cancel := context.WithCancel(c.ctx)
	lifetime := &callLifetime{ctx: ctx, cancel: cancel}
	c.lifetimes[callID] = lifetime
	return lifetime, nil
}

func (c *Core) releaseWhenDone(callID string, session *Session) {
	<-session.Done()
	c.mu.Lock()
	owner := c.owners[callID]
	if owner != nil && owner.session == session {
		delete(c.owners, callID)
		owner.cancel()
		owner.finish()
	}
	c.mu.Unlock()
}

func normalizeCallID(value string) (string, error) {
	call, err := normalizeActiveCall(ActiveCall{ID: value, State: CallStateActive})
	if err != nil {
		return "", ErrInvalidArgument
	}
	return call.ID, nil
}

func clonePeerConfiguration(configuration webrtc.Configuration) webrtc.Configuration {
	cloned := configuration
	cloned.ICEServers = append([]webrtc.ICEServer(nil), configuration.ICEServers...)
	cloned.Certificates = append([]webrtc.Certificate(nil), configuration.Certificates...)
	return cloned
}
