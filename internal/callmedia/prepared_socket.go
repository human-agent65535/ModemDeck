package callmedia

import (
	"context"
	"time"

	"github.com/human-agent65535/modemdeck/internal/audiocore"
)

// PrepareSocket creates the ordinary Session and socket codec before hardware
// exists. Its sole NetEq receiver is created once the hardware rate is known.
// Valid pre-dial packets prove ingress but are never queued for a future callee.
func (c *Core) PrepareSocket(ctx context.Context, id, token string, transport SocketTransport) (*Session, error) {
	if c == nil {
		return nil, ErrCoreClosed
	}
	ctx = normalizeContext(ctx)
	if ctx.Err() != nil {
		return nil, ErrCanceled
	}
	if !audiocore.Available {
		return nil, ErrUnsupported
	}
	var err error
	id, err = normalizeCallID(id)
	if err != nil {
		return nil, err
	}
	token, err = normalizeOwnerToken(token)
	if err != nil || transport == nil {
		return nil, ErrInvalidArgument
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil, ErrCoreClosed
	}
	if c.owners[id] != nil {
		c.mu.Unlock()
		return nil, ErrCallInUse
	}
	lifetimeContext, lifetimeCancel := context.WithCancel(c.ctx)
	lifetime := &callLifetime{ctx: lifetimeContext, cancel: lifetimeCancel, prepared: true}
	c.lifetimes[id] = lifetime
	ownerContext, ownerCancel := context.WithCancel(c.ctx)
	owner := &mediaOwner{token: token, ctx: ownerContext, cancel: ownerCancel, done: make(chan struct{})}
	c.owners[id] = owner
	c.prepares.Add(1)
	c.mu.Unlock()
	defer c.prepares.Done()
	committed := false
	defer func() {
		if committed {
			return
		}
		c.releaseReservation(id, owner)
		lifetimeCancel()
		c.mu.Lock()
		if c.lifetimes[id] == lifetime {
			delete(c.lifetimes, id)
		}
		c.mu.Unlock()
	}()
	format := PCMFormat{Encoding: PCMEncodingS16LE, SampleRate: 16000, Channels: 1, FrameDuration: defaultFramePeriod}
	codec, err := c.codecs.New(format)
	if err != nil {
		return nil, err
	}
	if codec == nil || codec.Format() != format {
		if codec != nil {
			_ = codec.Close()
		}
		return nil, ErrCodec
	}
	session := newSession(ownerContext, id, format, nil, nil, codec)
	session.bound = make(chan struct{})
	session.socket = &socketAudio{transport: transport, firstPacket: make(chan struct{}), epoch: time.Now()}
	session.reportStats = c.onAudioStats
	if ctx.Err() != nil {
		_ = codec.Close()
		return nil, ErrCanceled
	}
	if err := c.commit(id, lifetime, owner, session); err != nil {
		_ = codec.Close()
		return nil, err
	}
	committed = true
	session.start()
	go c.releaseWhenDone(id, owner, session)
	return session, nil
}

// ActivatePreparedSocket migrates the same owner before hardware PCM exists.
// Its liveness callbacks remain serialized with owner replacement/removal.
func (c *Core) ActivatePreparedSocket(ctx context.Context, id string, call ActiveCall, session *Session) error {
	if c == nil {
		return ErrCoreClosed
	}
	if session == nil {
		return ErrInvalidArgument
	}
	if normalizeContext(ctx).Err() != nil {
		return ErrCanceled
	}
	call, err := normalizeActiveCall(call)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return ErrCoreClosed
	}
	if existing := c.owners[call.ID]; existing != nil && existing.session == session && session.TransportReady() {
		return nil
	}
	owner, lifetime := c.owners[id], c.lifetimes[id]
	if owner == nil || owner.session != session || lifetime == nil || !lifetime.prepared || !session.TransportReady() {
		return ErrCanceled
	}
	if c.owners[call.ID] != nil {
		return ErrCallInUse
	}
	if _, ok := c.authority[call.ID]; !ok {
		return ErrCallNotActive
	}
	actualLifetime := c.lifetimes[call.ID]
	if actualLifetime != nil && actualLifetime.ctx.Err() != nil {
		return ErrCallNotActive
	}
	delete(c.owners, id)
	delete(c.lifetimes, id)
	if actualLifetime != nil {
		lifetime.cancel()
		lifetime = actualLifetime
	}
	c.owners[call.ID], c.lifetimes[call.ID] = owner, lifetime
	lifetime.prepared = false
	session.identityMu.Lock()
	session.callID = call.ID
	session.identityMu.Unlock()
	if owner.connected {
		c.notifyOwnerState(call.ID, true)
	}
	return nil
}

// BindPreparedSocket attaches hardware to the same activated socket/session.
// The existing call hub is reused when recording acquired the endpoint first.
func (c *Core) BindPreparedSocket(ctx context.Context, id string, call ActiveCall, session *Session) error {
	if c == nil || session == nil || session.bound == nil {
		return ErrInvalidArgument
	}
	session.bindingMu.Lock()
	defer session.bindingMu.Unlock()
	bindContext, cancel := context.WithCancel(normalizeContext(ctx))
	stop := context.AfterFunc(session.ctx, cancel)
	defer func() { stop(); cancel() }()
	call, err := normalizeActiveCall(call)
	if err != nil {
		return err
	}
	if err := c.ActivatePreparedSocket(bindContext, id, call, session); err != nil {
		return err
	}
	c.mu.Lock()
	owner, lifetime := c.owners[call.ID], c.lifetimes[call.ID]
	if owner == nil || owner.session != session || lifetime == nil || !session.TransportReady() {
		c.mu.Unlock()
		return ErrCanceled
	}
	select {
	case <-session.bound:
		c.mu.Unlock()
		return ErrCallInUse
	default:
	}
	c.mu.Unlock()
	hub, err := c.acquireHub(bindContext, call, lifetime)
	if err != nil {
		session.stop(err)
		return err
	}
	format := PCMFormat{Encoding: PCMEncodingS16LE, SampleRate: hub.format.SampleRate, Channels: 1, FrameDuration: defaultFramePeriod}
	codec, err := c.codecs.New(format)
	if err != nil {
		session.stop(err)
		return err
	}
	if codec == nil || codec.Format() != format {
		if codec != nil {
			_ = codec.Close()
		}
		session.stop(ErrCodec)
		return ErrCodec
	}
	receiver, err := audiocore.NewReceiver(format.SampleRate)
	if err != nil {
		_ = codec.Close()
		session.stop(err)
		return err
	}
	session.codecMu.Lock()
	_ = session.codec.Close()
	session.codec, session.format, session.socket.receiver = codec, format, receiver
	session.codecMu.Unlock()
	subscription, err := hub.Subscribe(audiocore.SendCapacity * int(defaultFramePeriod/hub.format.FrameDuration))
	if err != nil {
		session.stop(err)
		return err
	}
	session.hub, session.subscription = hub, subscription
	c.mu.Lock()
	if owner.ctx.Err() != nil || bindContext.Err() != nil || !session.TransportReady() || c.owners[call.ID] != owner {
		c.mu.Unlock()
		_ = subscription.Close()
		return ErrCanceled
	}
	close(session.bound)
	c.mu.Unlock()
	if err := subscription.Start(bindContext); err != nil {
		session.stop(err)
		return err
	}
	return nil
}
