package callmedia

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	"github.com/human-agent65535/modemdeck/internal/audiocore"
)

const SocketHeaderBytes = 12
const SocketSampleRate = 16000

// SocketTransport is supplied by HTTP, keeping authentication and WebSocket
// framing outside the PCM core. Close must unblock a pending Read or Write.
type SocketTransport interface {
	Read(context.Context) ([]byte, error)
	Write(context.Context, []byte) error
	Finish(error, AudioStatistics)
	InterruptRead()
	Close() error
}

type socketAudio struct {
	transport   SocketTransport
	firstPacket chan struct{}
	receiver    *audiocore.Receiver
	epoch       time.Time
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
	format := PCMFormat{Encoding: PCMEncodingS16LE, SampleRate: hub.format.SampleRate, Channels: 1, FrameDuration: defaultFramePeriod}
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
	subscription, err := hub.Subscribe(audiocore.SendCapacity * int(defaultFramePeriod/hub.format.FrameDuration))
	if err != nil {
		return nil, err
	}
	defer func() {
		if !committed {
			_ = subscription.Close()
		}
	}()
	session := newSession(owner.ctx, call.ID, format, hub, subscription, codec)
	receiver, err := audiocore.NewReceiver(hub.format.SampleRate)
	if err != nil {
		return nil, fmt.Errorf("create uplink NetEq: %w", errors.Join(ErrCodec, err))
	}
	defer func() {
		if !committed {
			receiver.Close()
		}
	}()
	session.socket = &socketAudio{transport: transport, firstPacket: make(chan struct{}), receiver: receiver, epoch: time.Now()}
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
	initialized := false
	var previousSequence, previousTimestamp uint32
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
		if initialized {
			step := sequence - previousSequence
			if step == 0 || step >= 1<<31 || timestamp-previousTimestamp != step*320 {
				return ErrInvalidAudio
			}
		}
		previousSequence, previousTimestamp = sequence, timestamp
		if err := s.socket.receiver.Enqueue(payload, uint16(sequence), timestamp*3, time.Since(s.socket.epoch)); err != nil {
			if errors.Is(err, audiocore.ErrInvalidPacket) {
				return ErrInvalidAudio
			}
			if errors.Is(err, audiocore.ErrBackpressure) {
				return ErrBackpressure
			}
			return fmt.Errorf("insert socket Opus: %w", errors.Join(ErrCodec, err))
		}
		s.stats.receivedPackets.Add(1)
		s.stats.receivedBytes.Add(uint64(len(payload)))
		if !initialized {
			initialized = true
			s.events.markConnected()
			close(s.socket.firstPacket)
		}
	}
}

// The hub is the sole playout owner. This worker only attaches the receiver
// after the first valid packet and waits for cancellation; it has no timer.
func (s *Session) socketPlaybackLoop() error {
	if err := s.waitSocketAudio(); err != nil {
		return err
	}
	if err := s.hub.attachReceiver(s); err != nil {
		return err
	}
	if err := s.subscription.Start(s.ctx); err != nil {
		return err
	}
	<-s.ctx.Done()
	return nil
}

// pullPCM10 is called only by the hub's one10ms consumption clock. A20ms
// endpoint receives two adjacent chunks; no pull advances a clock into future.
func (s *Session) pullPCM10(now time.Time) ([]byte, error) {
	rate := s.hub.format.SampleRate
	pcm := make([]byte, rate/100*2)
	if s.ctx.Err() != nil {
		return pcm, nil
	}
	samples := make([]int16, rate/100)
	if err := s.socket.receiver.Pull(now.Sub(s.socket.epoch), samples); err != nil {
		return nil, fmt.Errorf("pull uplink NetEq: %w", errors.Join(ErrCodec, err))
	}
	for i, v := range samples {
		binary.LittleEndian.PutUint16(pcm[i*2:], uint16(v))
	}
	level, peak := PCMLevels(pcm)
	s.stats.inputDBFS.Store(int64(level))
	s.stats.inputPeakDBFS.Store(int64(peak))
	return pcm, nil
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
		pcm = append(pcm, frame.DownlinkPCM...)
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
