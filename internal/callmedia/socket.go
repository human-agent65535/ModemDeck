package callmedia

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/human-agent65535/modemdeck/internal/audiocore"
)

const SocketHeaderBytes = 12
const SocketSampleRate = 16000

var socketPrebuffer = audiocore.Prebuffer
var socketQueueFrames = audiocore.QueueCapacity
var socketLateBudget = audiocore.MaxAge

// SocketTransport is supplied by HTTP, keeping authentication and WebSocket
// framing outside the PCM core. Close must unblock a pending Read or Write.
type SocketTransport interface {
	Read(context.Context) ([]byte, error)
	Write(context.Context, []byte) error
	Finish(error, AudioStatistics)
	InterruptRead()
	Close() error
}

type socketPacket struct {
	payload       []byte
	sequence      uint32
	playAt        time.Time
	generation    uint64
	sourceSamples float64
}

// Future packets stay in this queue until their media slot, so peeking cannot
// exceed the five-frame batch plus two-frame prebuffer capacity. A re-anchor
// invalidates the old timeline atomically.
type socketPacketQueue struct {
	mu         sync.Mutex
	packets    []socketPacket
	generation uint64
}

type socketDrops struct{ overflow, reanchor, playout, rebuffer uint64 }

func (d socketDrops) total() uint64 { return d.overflow + d.reanchor + d.playout + d.rebuffer }
func (s *Session) enqueueSocketPacket(packet socketPacket) uint64 {
	d := s.socket.incoming.push(packet)
	s.stats.recordQueueDrops(d)
	return d.total()
}
func (q *socketPacketQueue) push(packet socketPacket) (dropped socketDrops) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if packet.generation != q.generation {
		dropped.reanchor += uint64(len(q.packets))
		clear(q.packets)
		q.packets = q.packets[:0]
		q.generation = packet.generation
	}
	if len(q.packets) == socketQueueFrames {
		copy(q.packets, q.packets[1:])
		q.packets = q.packets[:len(q.packets)-1]
		dropped.overflow++
	}
	q.packets = append(q.packets, packet)
	return dropped
}

func (q *socketPacketQueue) head(now time.Time, prebuffer time.Duration) (packet socketPacket, queued bool, dropped socketDrops, generation uint64) {
	q.mu.Lock()
	defer q.mu.Unlock()
	generation = q.generation
	for len(q.packets) > 0 {
		packet = q.packets[0]
		// Rebuffer cannot extend a packet's original late deadline.
		if !audiocore.Expired(0, now.Add(prebuffer).Sub(packet.playAt)) {
			return packet, true, dropped, generation
		}
		q.removeHead()
		if audiocore.Expired(0, now.Sub(packet.playAt)) {
			dropped.playout++
		} else {
			dropped.rebuffer++
		}
	}
	return socketPacket{}, false, dropped, generation
}

func (q *socketPacketQueue) removeHead() {
	copy(q.packets, q.packets[1:])
	q.packets[len(q.packets)-1] = socketPacket{}
	q.packets = q.packets[:len(q.packets)-1]
}

// socketHardwareClock projects source slots onto the nearest fixed hardware
// tick. This makes the 10/20ms quantization explicit rather than adding an
// arrival-time epsilon that varies with scheduler latency.
type socketHardwareClock struct {
	anchor time.Time
	period time.Duration
}

func (c socketHardwareClock) slot(source time.Time) time.Time {
	return c.anchor.Add(source.Sub(c.anchor).Round(c.period))
}

func (q *socketPacketQueue) take(slot time.Time, generation uint64, epoch socketPlaybackEpoch, hardware socketHardwareClock) (packet socketPacket, ready, queued bool, dropped socketDrops) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.generation != generation {
		return socketPacket{}, false, len(q.packets) > 0, dropped
	}
	for len(q.packets) > 0 {
		packet = q.packets[0]
		start := hardware.slot(epoch.slot(packet))
		// A lost hardware slot cannot be occupied by old source audio later. TTL
		// above is an independent upper bound, not permission to shift the epoch.
		if !slot.Before(start.Add(defaultFramePeriod)) {
			q.removeHead()
			dropped.playout++
			continue
		}
		if slot.Before(start) {
			return socketPacket{}, false, true, dropped
		}
		q.removeHead()
		return packet, true, true, dropped
	}
	return socketPacket{}, false, false, dropped
}

func (q *socketPacketQueue) current(generation uint64) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.generation == generation
}

type socketAudio struct {
	transport    SocketTransport
	incoming     socketPacketQueue
	firstPacket  chan struct{}
	codecFactory OpusCodecFactory
}

func SocketFrame(sequence, timestamp uint32, opus []byte) []byte {
	frame := make([]byte, SocketHeaderBytes+len(opus))
	copy(frame, "MD")
	frame[2] = 1
	binary.BigEndian.PutUint32(frame[4:8], sequence)
	binary.BigEndian.PutUint32(frame[8:12], timestamp)
	copy(frame[12:], opus)
	return frame
}

func ParseSocketFrame(frame []byte) (sequence, timestamp uint32, opus []byte, err error) {
	if len(frame) <= SocketHeaderBytes || len(frame) > SocketHeaderBytes+maxOpusPayloadBytes ||
		string(frame[:2]) != "MD" || frame[2] != 1 || frame[3] != 0 {
		return 0, 0, nil, ErrInvalidAudio
	}
	return binary.BigEndian.Uint32(frame[4:8]), binary.BigEndian.Uint32(frame[8:12]), frame[12:], nil
}

// OpenSocket attaches an authenticated WSS client to the shared call lifetime
// and PCM hub. A failed prepare never leaves a reserved media owner behind.
func (c *Core) OpenSocket(ctx context.Context, active ActiveCall, token string, transport SocketTransport) (*Session, error) {
	if c == nil {
		return nil, ErrCoreClosed
	}
	if !audiocore.Available {
		return nil, &UnsupportedError{Feature: "portable audio core CGO"}
	}
	ctx = normalizeContext(ctx)
	call, err := normalizeActiveCall(active)
	if err != nil {
		return nil, err
	}
	token, err = normalizeOwnerToken(token)
	if err != nil || transport == nil {
		return nil, ErrInvalidArgument
	}
	lifetime, owner, err := c.reserve(call.ID, token)
	if err != nil {
		return nil, err
	}
	defer c.prepares.Done()
	committed := false
	defer func() {
		if !committed {
			c.releaseReservation(call.ID, owner)
		}
	}()
	prepare, cancel := context.WithCancel(ctx)
	stopCall := context.AfterFunc(lifetime.ctx, cancel)
	stopOwner := context.AfterFunc(owner.ctx, cancel)
	defer func() { stopCall(); stopOwner(); cancel() }()
	hub, err := c.acquireHub(prepare, call, lifetime)
	if err != nil {
		return nil, err
	}
	format := PCMFormat{Encoding: PCMEncodingS16LE, SampleRate: SocketSampleRate, Channels: 1, FrameDuration: defaultFramePeriod}
	codec, err := c.codecs.New(format)
	if err != nil {
		if codec != nil {
			_ = codec.Close()
		}
		return nil, fmt.Errorf("create socket codec: %w", errors.Join(ErrCodec, err))
	}
	if codec == nil {
		return nil, ErrCodec
	}
	if codec.Format() != format {
		_ = codec.Close()
		return nil, ErrCodec
	}
	defer func() {
		if !committed {
			_ = codec.Close()
		}
	}()
	subscription, err := hub.Subscribe(socketQueueFrames * int(defaultFramePeriod/hub.format.FrameDuration))
	if err != nil {
		return nil, err
	}
	defer func() {
		if !committed {
			_ = subscription.Close()
		}
	}()
	session := newSession(owner.ctx, call.ID, format, hub, subscription, codec)
	session.socket = &socketAudio{transport: transport, firstPacket: make(chan struct{}), codecFactory: c.codecs}
	session.reportStats = c.onAudioStats
	if err := prepare.Err(); err != nil {
		return nil, ErrCanceled
	}
	if err := c.commit(call.ID, lifetime, owner, session); err != nil {
		return nil, err
	}
	committed = true
	session.start()
	go c.releaseWhenDone(call.ID, owner, session)
	return session, nil
}

func (s *Session) socketReceiveLoop() error {
	clock := socketReceiveClock{}
	initialized := false
	for {
		frame, err := s.socket.transport.Read(s.ctx)
		if err != nil {
			if s.ctx.Err() != nil {
				return nil
			}
			if errors.Is(err, ErrCanceled) {
				s.stop(nil)
				return nil
			}
			return fmt.Errorf("read socket audio: %w", errors.Join(ErrTransportClosed, err))
		}
		sequence, timestamp, payload, err := ParseSocketFrame(frame)
		if err != nil {
			return err
		}
		duration, err := s.codec.PacketDuration(payload)
		if err != nil || duration != defaultFramePeriod {
			return ErrInvalidAudio
		}
		now := time.Now()
		previousGeneration := clock.generation
		current, err := clock.accept(sequence, timestamp, now)
		if err != nil {
			return err
		}
		s.stats.receivedPackets.Add(1)
		s.stats.receivedBytes.Add(uint64(len(payload)))
		if previousGeneration != 0 && previousGeneration != clock.generation {
			s.stats.clockReanchors.Add(1)
		}
		if !current {
			s.stats.recordSourceDrop(clock.age)
			continue
		}
		packet := socketPacket{payload: append([]byte(nil), payload...), sequence: sequence, playAt: clock.playAt, generation: clock.generation, sourceSamples: clock.sourceSamples}
		s.enqueueSocketPacket(packet)
		if !initialized {
			initialized = true
			s.events.markConnected()
			close(s.socket.firstPacket)
		}
	}
}

// socketPlayout owns the uplink decoder and the unplayed part of a 20ms packet.
// A new timeline must not retain either old decoded samples or predictive state.
type socketPlayout struct {
	session              *Session
	decoder              OpusCodec
	ownedDecoder         bool
	generation           uint64
	previousSequence     uint32
	decoded              bool
	pending              []byte
	pendingAt            time.Time
	pendingSlot          time.Time
	pendingDropped       bool
	epochGeneration      uint64
	offset               time.Duration
	starving             bool
	decoderDiscontinuous bool
	hardware             socketHardwareClock
	epoch                socketPlaybackEpoch
}

func (p *socketPlayout) close() {
	if p.ownedDecoder {
		_ = p.decoder.Close()
	}
}

func (p *socketPlayout) resetDecoder() error {
	decoder, err := p.session.socket.codecFactory.New(p.session.format)
	if err != nil || decoder == nil {
		if decoder != nil {
			_ = decoder.Close()
		}
		return fmt.Errorf("reset socket decoder: %w", errors.Join(ErrCodec, err))
	}
	if decoder.Format() != p.session.format {
		_ = decoder.Close()
		return ErrCodec
	}
	p.close()
	p.decoder, p.ownedDecoder = decoder, true
	return nil
}

// A device epoch is fixed to unwrapped source samples. Healthy oscillator
// drift updates source-age estimates, never the spacing of already playing PCM.
type socketPlaybackEpoch struct {
	initialized   bool
	sourceSamples float64
	start         time.Time
}

func (e socketPlaybackEpoch) slot(packet socketPacket) time.Time {
	return e.start.Add(audiocore.SourceSlot(0, packet.sourceSamples-e.sourceSamples))
}

func (p *socketPlayout) recordPendingDrop(d socketDrops) {
	if !p.pendingDropped {
		p.session.stats.recordQueueDrops(d)
		p.pendingDropped = true
	}
}

func (p *socketPlayout) skipMissedPending(slot time.Time) {
	if len(p.pending) == 0 || !slot.After(p.pendingSlot) {
		return
	}
	period := p.session.hub.format.FrameDuration
	frames := min(len(p.pending)/p.session.hub.format.FrameBytes(), int(slot.Sub(p.pendingSlot)/period))
	if frames == 0 {
		return
	}
	p.recordPendingDrop(socketDrops{playout: 1})
	p.pending = p.pending[frames*p.session.hub.format.FrameBytes():]
	p.pendingSlot = p.pendingSlot.Add(time.Duration(frames) * period)
}

func (p *socketPlayout) next(slot, now time.Time) ([]byte, error) {
	s := p.session
	frameBytes := s.hub.format.FrameBytes()
	if p.hardware.period == 0 {
		p.hardware = socketHardwareClock{anchor: slot, period: s.hub.format.FrameDuration}
	}
	if len(p.pending) > 0 && (!s.socket.incoming.current(p.generation) || audiocore.Expired(0, now.Sub(p.pendingAt))) {
		if !s.socket.incoming.current(p.generation) {
			p.recordPendingDrop(socketDrops{reanchor: 1})
		} else {
			p.recordPendingDrop(socketDrops{playout: 1})
		}
		p.pending = nil
	}
	p.skipMissedPending(slot)
	if len(p.pending) == 0 {
		prebuffer := time.Duration(0)
		if p.starving {
			prebuffer = socketPrebuffer
		}
		head, queued, dropped, generation := s.socket.incoming.head(now, prebuffer)
		s.stats.recordQueueDrops(dropped)
		if generation != p.epochGeneration {
			p.epochGeneration, p.offset, p.starving = generation, 0, false
			p.epoch = socketPlaybackEpoch{}
		}
		if queued && (!p.epoch.initialized || p.starving) {
			// Replace the epoch after a genuine underrun; original packet deadlines
			// remain independent and the offset never accumulates across episodes.
			if p.starving {
				p.offset = audiocore.PlaybackStart(0, now.Sub(head.playAt))
			}
			p.epoch = socketPlaybackEpoch{initialized: true, sourceSamples: head.sourceSamples, start: head.playAt.Add(p.offset)}
			p.starving = false
		}
		// Expiry above uses actual now, independent of hardware quantization.
		packet, ready, queued, slotDrops := s.socket.incoming.take(slot, generation, p.epoch, p.hardware)
		s.stats.recordQueueDrops(slotDrops)
		if !queued && p.decoded {
			if !p.starving {
				s.stats.playoutUnderruns.Add(1)
			}
			p.starving, p.decoderDiscontinuous = true, true
		}
		if ready {
			if p.decoded && (packet.generation != p.generation || packet.sequence != p.previousSequence+1 || p.decoderDiscontinuous) {
				if err := p.resetDecoder(); err != nil {
					return nil, err
				}
			}
			decoded, err := p.decoder.Decode(packet.payload)
			if err != nil {
				return nil, err
			}
			if decoded.Duration != defaultFramePeriod || len(decoded.PCM) != s.format.FrameBytes() {
				return nil, ErrCodec
			}
			p.generation, p.previousSequence, p.decoded = packet.generation, packet.sequence, true
			p.decoderDiscontinuous = false
			p.pendingAt = packet.playAt
			p.pendingSlot = p.hardware.slot(p.epoch.slot(packet))
			p.pendingDropped = false
			p.pending = socketResample(decoded.PCM, SocketSampleRate, s.hub.format.SampleRate)
			// Receipt may have re-anchored while Decode was running.
			if !s.socket.incoming.current(p.generation) {
				p.pending = nil
				p.recordPendingDrop(socketDrops{reanchor: 1})
			}
		}
	}
	p.skipMissedPending(slot)
	if len(p.pending) == 0 {
		s.stats.playoutSilenceFrames.Add(1)
		s.stats.inputDBFS.Store(-96)
		s.stats.inputPeakDBFS.Store(-96)
		return make([]byte, frameBytes), nil
	}
	if len(p.pending) < frameBytes {
		return nil, ErrCodec
	}
	frame := p.pending[:frameBytes]
	p.pending = p.pending[frameBytes:]
	p.pendingSlot = p.pendingSlot.Add(s.hub.format.FrameDuration)
	level, peak := PCMLevels(frame)
	s.stats.inputDBFS.Store(int64(level))
	s.stats.inputPeakDBFS.Store(int64(peak))
	return frame, nil
}

func (s *Session) socketPlaybackLoop() error {
	if err := s.waitSocketAudio(); err != nil {
		return err
	}
	if err := s.subscription.Start(s.ctx); err != nil {
		return err
	}
	// Logical hardware slots are independent of scheduler wake times. A late
	// wake skips elapsed slots once; it never shifts the grid or bursts catch-up
	// audio into the hub. Freshness always uses the actual monotonic wall time.
	cadence := socketPlaybackCadence{next: time.Now(), period: s.hub.format.FrameDuration}
	timer := time.NewTimer(0)
	defer timer.Stop()
	playout := socketPlayout{session: s, decoder: s.codec}
	defer playout.close()
	for {
		select {
		case <-s.ctx.Done():
			return nil
		case <-timer.C:
		}
		now := time.Now()
		slot, skipped := cadence.advance(now)
		s.stats.playoutMissedTicks.Add(skipped)
		frame, err := playout.next(slot, now)
		if err != nil {
			return err
		}
		if err := s.hub.WritePCM(s.ctx, frame); err != nil {
			return err
		}
		timer.Reset(max(0, time.Until(cadence.next)))
	}
}

// advance returns a position on the original hardware grid, not a new
// anchor derived from a delayed wake. Missed slots produce no catch-up burst.
type socketPlaybackCadence struct {
	next   time.Time
	period time.Duration
}

func (c *socketPlaybackCadence) advance(now time.Time) (time.Time, uint64) {
	skipped := time.Duration(0)
	if now.After(c.next) {
		skipped = now.Sub(c.next) / c.period
	}
	slot := c.next.Add(skipped * c.period)
	c.next = slot.Add(c.period)
	return slot, uint64(skipped)
}

func (s *Session) socketCaptureLoop() error {
	if err := s.waitSocketAudio(); err != nil {
		return err
	}
	if err := s.subscription.Start(s.ctx); err != nil {
		return err
	}
	var pcm []byte
	var sequence, timestamp uint32
	for {
		frame, err := s.subscription.Next(s.ctx)
		if err != nil {
			return err
		}
		pcm = append(pcm, socketResample(frame.DownlinkPCM, s.hub.format.SampleRate, SocketSampleRate)...)
		if err := s.subscription.Err(); err != nil {
			return err
		}
		if len(pcm) < s.format.FrameBytes() {
			continue
		}
		encoded, err := s.codec.Encode(pcm[:s.format.FrameBytes()])
		if err != nil {
			return err
		}
		if len(encoded) == 0 || len(encoded) > maxOpusPayloadBytes {
			return ErrCodec
		}
		if err := s.socket.transport.Write(s.ctx, SocketFrame(sequence, timestamp, encoded)); err != nil {
			return err
		}
		level, _ := PCMLevels(pcm[:s.format.FrameBytes()])
		s.stats.outputDBFS.Store(int64(level))
		s.stats.sentPackets.Add(1)
		sequence++
		timestamp += 320
		pcm = pcm[s.format.FrameBytes():]
	}
}

// Only the existing 8/16 kHz mono PCM boundary is supported. Downsampling uses
// a two-sample low-pass average; interpolation avoids nearest-neighbor steps.
func socketResample(pcm []byte, source, target int) []byte {
	if source == target {
		return append([]byte(nil), pcm...)
	}
	samples := len(pcm) / 2
	output := make([]byte, samples*target/source*2)
	sample := func(i int) int32 {
		if i >= samples {
			i = samples - 1
		}
		return int32(int16(binary.LittleEndian.Uint16(pcm[i*2:])))
	}
	if source == 8000 && target == 16000 {
		for i := 0; i < samples; i++ {
			binary.LittleEndian.PutUint16(output[i*4:], uint16(int16(sample(i))))
			binary.LittleEndian.PutUint16(output[i*4+2:], uint16(int16((sample(i)+sample(i+1))/2)))
		}
	} else {
		for i := 0; i < samples/2; i++ {
			binary.LittleEndian.PutUint16(output[i*2:], uint16(int16((sample(i*2)+sample(i*2+1))/2)))
		}
	}
	return output
}

func (s *Session) waitSocketAudio() error {
	timer := time.NewTimer(15 * time.Second)
	defer timer.Stop()
	select {
	case <-s.socket.firstPacket:
		return nil
	case <-s.ctx.Done():
		return s.ctx.Err()
	case <-timer.C:
		return ErrTransportTimeout
	}
}

// socketReceiveClock maps unwrapped source progress onto monotonic playback
// slots plus a fixed 40ms prebuffer. Five packets received together still
// occupy five distinct 20ms slots.
// Arrival age rejects TCP backlog; queue expiry is relative to each source slot.
type socketReceiveClock struct {
	core          audiocore.Clock
	origin        time.Time
	initialized   bool
	playAt        time.Time
	generation    uint64
	age           time.Duration
	sourceSamples float64
}

func (c *socketReceiveClock) accept(sequence, timestamp uint32, now time.Time) (bool, error) {
	if !c.initialized {
		c.origin = now
	}
	result, err := c.core.Accept(sequence, timestamp, now.Sub(c.origin))
	if err != nil {
		return false, ErrInvalidAudio
	}
	c.initialized = true
	c.playAt, c.generation, c.age, c.sourceSamples = c.origin.Add(result.PlayAt), result.Generation, result.Age, result.SourceSamples
	return result.Accepted, nil
}
