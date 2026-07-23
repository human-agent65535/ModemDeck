package callmedia

import (
	"fmt"
	"sync"
	"time"

	"github.com/pion/rtp"
)

const rtpTimestampQuantum = 120

type JitterConfig struct {
	PacketCapacity   int
	StartupDelay     time.Duration
	MaxConcealment   time.Duration
	PCMQueueDuration time.Duration
}

func (c JitterConfig) normalized() (JitterConfig, error) {
	if c.PacketCapacity == 0 {
		c.PacketCapacity = 64
	}
	if c.StartupDelay == 0 {
		c.StartupDelay = 60 * time.Millisecond
	}
	if c.MaxConcealment == 0 {
		c.MaxConcealment = 120 * time.Millisecond
	}
	if c.PCMQueueDuration == 0 {
		c.PCMQueueDuration = 160 * time.Millisecond
	}
	if c.PacketCapacity < 2 ||
		c.StartupDelay < 0 ||
		c.MaxConcealment < defaultFramePeriod ||
		c.PCMQueueDuration < maxOpusPacketTime+defaultFramePeriod {
		return JitterConfig{}, ErrInvalidArgument
	}
	return c, nil
}

type bufferedPacket struct {
	sequence  int64
	timestamp uint32
	payload   []byte
}

type jitterBuffer struct {
	mu       sync.Mutex
	capacity int
	packets  map[int64]bufferedPacket
	unwrap   sequenceUnwrapper
	started  bool
	expected int64
	ready    chan struct{}
	readyOne sync.Once
}

func newJitterBuffer(capacity int) *jitterBuffer {
	return &jitterBuffer{
		capacity: capacity,
		packets:  make(map[int64]bufferedPacket, capacity),
		ready:    make(chan struct{}),
	}
}

func (b *jitterBuffer) push(packet *rtp.Packet) error {
	if b == nil || packet == nil || len(packet.Payload) == 0 || len(packet.Payload) > maxOpusPayloadBytes {
		return ErrInvalidRTP
	}
	b.mu.Lock()
	sequence := b.unwrap.unwrap(packet.SequenceNumber)
	if b.started && sequence < b.expected {
		b.mu.Unlock()
		return nil
	}
	if _, duplicate := b.packets[sequence]; duplicate {
		b.mu.Unlock()
		return nil
	}
	if len(b.packets) >= b.capacity {
		b.mu.Unlock()
		return ErrBackpressure
	}
	b.packets[sequence] = bufferedPacket{
		sequence:  sequence,
		timestamp: packet.Timestamp,
		payload:   append([]byte(nil), packet.Payload...),
	}
	b.readyOne.Do(func() { close(b.ready) })
	b.mu.Unlock()
	return nil
}

func (b *jitterBuffer) start() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.started {
		return true
	}
	sequence, ok := b.minimumLocked()
	if !ok {
		return false
	}
	b.started = true
	b.expected = sequence
	return true
}

func (b *jitterBuffer) candidate() (bufferedPacket, bool, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.started {
		return bufferedPacket{}, false, false
	}
	sequence, ok := b.minimumLocked()
	if !ok {
		return bufferedPacket{}, false, false
	}
	return b.packets[sequence], sequence == b.expected, true
}

func (b *jitterBuffer) consume(sequence int64) {
	b.mu.Lock()
	delete(b.packets, sequence)
	if sequence >= b.expected {
		b.expected = sequence + 1
	}
	b.mu.Unlock()
}

func (b *jitterBuffer) rebase(sequence int64) {
	b.mu.Lock()
	if sequence > b.expected {
		b.expected = sequence
	}
	for existing := range b.packets {
		if existing < b.expected {
			delete(b.packets, existing)
		}
	}
	b.mu.Unlock()
}

func (b *jitterBuffer) minimumLocked() (int64, bool) {
	var minimum int64
	found := false
	for sequence := range b.packets {
		if b.started && sequence < b.expected {
			continue
		}
		if !found || sequence < minimum {
			minimum = sequence
			found = true
		}
	}
	return minimum, found
}

type sequenceUnwrapper struct {
	initialized bool
	highest     int64
}

func (u *sequenceUnwrapper) unwrap(sequence uint16) int64 {
	if !u.initialized {
		u.initialized = true
		u.highest = int64(sequence)
		return u.highest
	}
	candidate := (u.highest &^ 0xffff) | int64(sequence)
	if candidate-u.highest > 1<<15 {
		candidate -= 1 << 16
	} else if u.highest-candidate > 1<<15 {
		candidate += 1 << 16
	}
	if candidate > u.highest {
		u.highest = candidate
	}
	return candidate
}

type rtpPlayout struct {
	format PCMFormat
	codec  OpusCodec
	jitter *jitterBuffer
	config JitterConfig

	pcmQueue          []byte
	maxPCMBytes       int
	timestampSet      bool
	expectedTimestamp uint32
	concealed         time.Duration
}

func newRTPPlayout(
	format PCMFormat,
	codec OpusCodec,
	jitter *jitterBuffer,
	config JitterConfig,
) *rtpPlayout {
	maxPCMBytes := format.samples(config.PCMQueueDuration) * format.Channels * bytesPerPCMSample
	return &rtpPlayout{
		format:      format,
		codec:       codec,
		jitter:      jitter,
		config:      config,
		pcmQueue:    make([]byte, 0, maxPCMBytes),
		maxPCMBytes: maxPCMBytes,
	}
}

func (p *rtpPlayout) nextFrame(dst []byte) error {
	if p == nil || p.codec == nil || p.jitter == nil || len(dst) != p.format.FrameBytes() {
		return ErrInvalidArgument
	}
	for len(p.pcmQueue) < len(dst) {
		if err := p.fill(); err != nil {
			return err
		}
	}
	copy(dst, p.pcmQueue[:len(dst)])
	copy(p.pcmQueue, p.pcmQueue[len(dst):])
	p.pcmQueue = p.pcmQueue[:len(p.pcmQueue)-len(dst)]
	return nil
}

func (p *rtpPlayout) fill() error {
	packet, exact, available := p.jitter.candidate()
	if !available {
		if !p.timestampSet {
			return ErrInvalidRTP
		}
		return p.appendConcealment(p.format.FrameDuration)
	}

	if !p.timestampSet {
		p.jitter.rebase(packet.sequence)
		p.expectedTimestamp = packet.timestamp
		p.timestampSet = true
		exact = true
	}

	delta := int64(int32(packet.timestamp - p.expectedTimestamp))
	if delta < 0 {
		p.jitter.consume(packet.sequence)
		return nil
	}
	if delta > 0 {
		if delta%rtpTimestampQuantum != 0 {
			return fmt.Errorf("play Opus packet: timestamp quantum: %w", ErrInvalidRTP)
		}
		gapDuration := time.Duration(delta) * time.Second / RTPClockRate
		if p.concealed+gapDuration > p.config.MaxConcealment {
			return ErrMediaGap
		}
		duration := concealmentChunk(
			gapDuration,
			p.format.FrameDuration,
		)
		if duration == 0 {
			return fmt.Errorf("play Opus packet: timestamp gap: %w", ErrInvalidRTP)
		}
		return p.appendConcealment(duration)
	}
	if !exact {
		p.jitter.rebase(packet.sequence)
	}
	p.jitter.consume(packet.sequence)

	duration, err := p.codec.PacketDuration(packet.payload)
	if err != nil {
		return fmt.Errorf("play Opus packet: inspect duration: %w", err)
	}
	decoded, err := p.codec.Decode(packet.payload)
	if err != nil {
		return fmt.Errorf("play Opus packet: decode: %w", err)
	}
	if decoded.Duration != duration {
		return fmt.Errorf("play Opus packet: inconsistent duration: %w", ErrCodec)
	}
	expectedBytes, err := pcmBytesForDuration(p.format, duration)
	if err != nil || len(decoded.PCM) != expectedBytes {
		return fmt.Errorf("play Opus packet: decoded PCM size: %w", ErrCodec)
	}
	if err := p.appendPCM(decoded.PCM); err != nil {
		return err
	}
	p.expectedTimestamp += uint32(int64(RTPClockRate) * int64(duration) / int64(time.Second))
	p.concealed = 0
	return nil
}

func (p *rtpPlayout) appendConcealment(duration time.Duration) error {
	if duration <= 0 || duration > p.format.FrameDuration || !validOpusFrameDuration(duration) {
		return fmt.Errorf("conceal missing Opus packet: duration: %w", ErrInvalidRTP)
	}
	if p.concealed+duration > p.config.MaxConcealment {
		return ErrMediaGap
	}
	pcm, err := p.codec.Conceal(duration)
	if err != nil {
		return fmt.Errorf("conceal missing Opus packet: %w", err)
	}
	expectedBytes, err := pcmBytesForDuration(p.format, duration)
	if err != nil || len(pcm) != expectedBytes {
		return fmt.Errorf("conceal missing Opus packet: PCM size: %w", ErrCodec)
	}
	if err := p.appendPCM(pcm); err != nil {
		return err
	}
	p.expectedTimestamp += uint32(int64(RTPClockRate) * int64(duration) / int64(time.Second))
	p.concealed += duration
	return nil
}

func (p *rtpPlayout) appendPCM(pcm []byte) error {
	if len(pcm) == 0 || len(p.pcmQueue)+len(pcm) > p.maxPCMBytes {
		return ErrBackpressure
	}
	p.pcmQueue = append(p.pcmQueue, pcm...)
	return nil
}

func concealmentChunk(remaining, maximum time.Duration) time.Duration {
	if remaining <= 0 || remaining%(2500*time.Microsecond) != 0 {
		return 0
	}
	for _, candidate := range []time.Duration{
		60 * time.Millisecond,
		40 * time.Millisecond,
		20 * time.Millisecond,
		10 * time.Millisecond,
		5 * time.Millisecond,
		2500 * time.Microsecond,
	} {
		if candidate <= remaining && candidate <= maximum {
			return candidate
		}
	}
	return 0
}
