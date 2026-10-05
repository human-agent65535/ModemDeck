package callmedia

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	"github.com/pion/webrtc/v4"
)

const SocketHeaderBytes = 12
const SocketSampleRate = 16000
const socketQueueFrames = 5

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
	payload  []byte
	received time.Time
}
type socketAudio struct {
	transport   SocketTransport
	incoming    chan socketPacket
	firstPacket chan struct{}
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
		return 0, 0, nil, ErrInvalidRTP
	}
	return binary.BigEndian.Uint32(frame[4:8]), binary.BigEndian.Uint32(frame[8:12]), frame[12:], nil
}

// OpenSocket uses precisely the same ownership, call lifetime and modem hub as
// legacy peers. A failed prepare never leaves a reserved media owner behind.
func (c *Core) OpenSocket(ctx context.Context, active ActiveCall, token string, transport SocketTransport) (*Session, error) {
	if c == nil {
		return nil, ErrCoreClosed
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
		return nil, fmt.Errorf("create socket codec: %w", err)
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
	events := newPeerEvents()
	session := newSession(owner.ctx, call.ID, format, hub, subscription, codec, nil, nil, nil, events, c.jitter, c.recoveryTime)
	session.socket = &socketAudio{transport: transport, incoming: make(chan socketPacket, socketQueueFrames), firstPacket: make(chan struct{})}
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
			return ErrInvalidRTP
		}
		now := time.Now()
		current, err := clock.accept(sequence, timestamp, now)
		if err != nil {
			return err
		}
		s.stats.receivedPackets.Add(1)
		s.stats.receivedBytes.Add(uint64(len(payload)))
		if !current {
			s.stats.droppedPackets.Add(1)
			continue
		}
		if !initialized {
			initialized = true
			s.events.updateState(webrtc.PeerConnectionStateConnected)
			close(s.socket.firstPacket)
		}
		packet := socketPacket{payload: append([]byte(nil), payload...), received: now}
		select {
		case s.socket.incoming <- packet:
		default:
			select {
			case <-s.socket.incoming:
				s.stats.droppedPackets.Add(1)
			default:
			}
			select {
			case s.socket.incoming <- packet:
			default:
			}
		}
	}
}

func (s *Session) socketPlaybackLoop() error {
	if err := s.waitSocketAudio(); err != nil {
		return err
	}
	if err := s.subscription.Start(s.ctx); err != nil {
		return err
	}
	ticker := time.NewTicker(s.hub.format.FrameDuration)
	defer ticker.Stop()
	var pending []byte
	for {
		select {
		case <-s.ctx.Done():
			return nil
		case <-ticker.C:
		}
		if len(pending) == 0 {
			var packet socketPacket
			fresh := false
			for {
				select {
				case packet = <-s.socket.incoming:
					if time.Since(packet.received) <= 100*time.Millisecond {
						fresh = true
					} else {
						s.stats.droppedPackets.Add(1)
					}
				default:
					goto selected
				}
				if fresh {
					break
				}
			}
		selected:
			if fresh {
				duration, err := s.codec.PacketDuration(packet.payload)
				if err != nil || duration != defaultFramePeriod {
					return ErrInvalidRTP
				}
				decoded, err := s.codec.Decode(packet.payload)
				if err != nil {
					return err
				}
				if decoded.Duration != defaultFramePeriod || len(decoded.PCM) != s.format.FrameBytes() {
					return ErrCodec
				}
				level, peak := PCMLevels(decoded.PCM)
				s.stats.inputDBFS.Store(int64(level))
				s.stats.inputPeakDBFS.Store(int64(peak))
				pending = socketResample(decoded.PCM, SocketSampleRate, s.hub.format.SampleRate)
			} else {
				pending = make([]byte, s.hub.format.FrameBytes())
				s.stats.inputDBFS.Store(-96)
				s.stats.inputPeakDBFS.Store(-96)
			}
		}
		frameBytes := s.hub.format.FrameBytes()
		if len(pending) < frameBytes {
			return ErrCodec
		}
		if err := s.hub.WritePCM(s.ctx, pending[:frameBytes]); err != nil {
			return err
		}
		pending = pending[frameBytes:]
	}
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
	timer := time.NewTimer(s.recoveryTime)
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

// socketReceiveClock measures capture age from the sender's sample ticks, not
// just time in our local queue: TCP backlog does not become fresh on receipt.
// Re-anchor a changed route only after three arrivals regain real-time cadence.
type socketReceiveClock struct {
	initialized       bool
	previousSequence  uint32
	previousTimestamp uint32
	previousArrival   time.Time
	anchorTimestamp   uint32
	anchorArrival     time.Time
	recoveryCadence   int
}

func (c *socketReceiveClock) accept(sequence, timestamp uint32, now time.Time) (bool, error) {
	stable := false
	if c.initialized {
		advance := sequence - c.previousSequence
		if int32(advance) <= 0 || timestamp-c.previousTimestamp != advance*320 {
			return false, ErrInvalidRTP
		}
		gap := now.Sub(c.previousArrival)
		stable = advance == 1 && gap >= 12*time.Millisecond && gap <= 28*time.Millisecond
	} else {
		c.initialized = true
		c.anchorTimestamp = timestamp
		c.anchorArrival = now
	}
	c.previousSequence = sequence
	c.previousTimestamp = timestamp
	c.previousArrival = now
	elapsed := time.Duration(timestamp-c.anchorTimestamp) * time.Second / SocketSampleRate
	age := now.Sub(c.anchorArrival) - elapsed
	if age > 100*time.Millisecond || age < -100*time.Millisecond {
		if stable {
			c.recoveryCadence++
		} else {
			c.recoveryCadence = 0
		}
		if c.recoveryCadence < 3 {
			return false, nil
		}
		c.anchorTimestamp = timestamp
		c.anchorArrival = now
		c.recoveryCadence = 0
		return true, nil
	}
	c.recoveryCadence = 0
	predicted := c.anchorArrival.Add(elapsed + 200*time.Microsecond)
	c.anchorTimestamp = timestamp
	if now.Before(predicted) {
		c.anchorArrival = now
	} else {
		c.anchorArrival = predicted
	}
	return true, nil
}
