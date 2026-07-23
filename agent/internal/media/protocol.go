package media

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"sync"
	"time"
)

const (
	UpgradeProtocol  = "modemdeck-media-v2"
	ProtocolVersion  = 2
	FrameHeaderBytes = 16
	maxControlBytes  = 64
)

type FrameType uint8

const (
	FramePlayback FrameType = 1
	FrameCapture  FrameType = 2
	FramePing     FrameType = 3
	FramePong     FrameType = 4
	FrameClose    FrameType = 5
	FrameStart    FrameType = 6
	FrameStarted  FrameType = 7
	FrameError    FrameType = 8
)

var frameMagic = [4]byte{'M', 'D', 'M', '2'}

type Frame struct {
	Type     FrameType
	Sequence uint32
	Payload  []byte
}

func ReadFrame(reader io.Reader, maxPayload int) (Frame, error) {
	if maxPayload < 0 {
		return Frame{}, NewError(ErrorInvalidArgument, "read_media_frame", "invalid payload limit", nil)
	}

	var header [FrameHeaderBytes]byte
	if _, err := io.ReadFull(reader, header[:]); err != nil {
		return Frame{}, err
	}
	if !bytes.Equal(header[:4], frameMagic[:]) ||
		header[5] != 0 ||
		header[6] != 0 ||
		header[7] != 0 {
		return Frame{}, NewError(ErrorProtocol, "read_media_frame", "invalid media frame header", nil)
	}

	payloadLength := binary.BigEndian.Uint32(header[12:16])
	if uint64(payloadLength) > uint64(maxPayload) {
		return Frame{}, NewError(ErrorProtocol, "read_media_frame", "media frame payload is too large", nil)
	}
	payload := make([]byte, int(payloadLength))
	if _, err := io.ReadFull(reader, payload); err != nil {
		return Frame{}, err
	}
	return Frame{
		Type:     FrameType(header[4]),
		Sequence: binary.BigEndian.Uint32(header[8:12]),
		Payload:  payload,
	}, nil
}

func WriteFrame(writer io.Writer, frame Frame) error {
	if uint64(len(frame.Payload)) > uint64(^uint32(0)) {
		return NewError(ErrorInvalidArgument, "write_media_frame", "media frame payload is too large", nil)
	}

	var header [FrameHeaderBytes]byte
	copy(header[:4], frameMagic[:])
	header[4] = byte(frame.Type)
	binary.BigEndian.PutUint32(header[8:12], frame.Sequence)
	binary.BigEndian.PutUint32(header[12:16], uint32(len(frame.Payload)))
	if err := writeFull(writer, header[:]); err != nil {
		return err
	}
	return writeFull(writer, frame.Payload)
}

func (s *Session) Serve(ctx context.Context, connection net.Conn) error {
	const operation = "serve_media"

	if connection == nil {
		return NewError(ErrorInvalidArgument, operation, "media connection is required", nil)
	}
	if s.closed.Load() {
		return NewError(ErrorConflict, operation, "media session is closed", nil)
	}
	if !s.running.CompareAndSwap(false, true) {
		return NewError(ErrorConflict, operation, "media session is already running", nil)
	}
	if ctx == nil {
		ctx = context.Background()
	}

	sessionContext, cancel := context.WithCancel(ctx)
	s.lifecycleMu.Lock()
	if s.closed.Load() {
		s.lifecycleMu.Unlock()
		cancel()
		return NewError(ErrorConflict, operation, "media session is closed", nil)
	}
	s.connection = connection
	s.serveCancel = cancel
	s.lifecycleMu.Unlock()
	defer func() {
		s.lifecycleMu.Lock()
		if s.connection == connection {
			s.connection = nil
			s.serveCancel = nil
		}
		s.lifecycleMu.Unlock()
		cancel()
	}()
	defer connection.Close()
	defer s.Close()

	if err := s.awaitStart(sessionContext, connection); err != nil {
		if errors.Is(err, errPeerClosed) {
			return nil
		}
		if errors.Is(err, context.Canceled) && ctx.Err() != nil {
			return ctx.Err()
		}
		return err
	}

	outbound := make(chan Frame, s.options.OutboundFrames)
	results := make(chan error, 3)
	var workers sync.WaitGroup
	run := func(worker func(context.Context) error) {
		workers.Add(1)
		go func() {
			defer workers.Done()
			results <- worker(sessionContext)
		}()
	}

	run(func(workerContext context.Context) error {
		return s.readPlayback(workerContext, connection, outbound)
	})
	run(func(workerContext context.Context) error {
		return s.readCapture(workerContext, outbound)
	})
	run(func(workerContext context.Context) error {
		return s.writeOutbound(workerContext, connection, outbound)
	})

	firstError := <-results
	cancel()
	now := time.Now()
	_ = connection.SetDeadline(now)
	_ = s.device.SetReadDeadline(now)
	_ = s.device.SetWriteDeadline(now)
	_ = connection.Close()
	_ = s.Close()

	done := make(chan struct{})
	go func() {
		workers.Wait()
		close(done)
	}()
	timer := time.NewTimer(s.options.ShutdownTimeout)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
		return NewError(ErrorTimeout, operation, "media workers did not stop before the shutdown deadline", nil)
	}

	if errors.Is(firstError, errPeerClosed) {
		return nil
	}
	if errors.Is(firstError, context.Canceled) && ctx.Err() != nil {
		return ctx.Err()
	}
	return firstError
}

var errPeerClosed = errors.New("media peer closed")

func (s *Session) awaitStart(ctx context.Context, connection net.Conn) error {
	const operation = "start_media"

	startContext, cancel := context.WithTimeout(ctx, s.options.StartTimeout)
	defer cancel()
	deadline, _ := startContext.Deadline()
	if err := connection.SetDeadline(deadline); err != nil {
		return NewError(ErrorTimeout, operation, "media start deadline is unavailable", err)
	}
	stop := context.AfterFunc(startContext, func() {
		_ = connection.SetDeadline(time.Now())
	})
	defer stop()

	frame, err := ReadFrame(connection, maxControlBytes)
	if err != nil {
		if errors.Is(startContext.Err(), context.DeadlineExceeded) {
			return NewError(ErrorTimeout, operation, "media START was not received before the deadline", startContext.Err())
		}
		if startContext.Err() != nil {
			return startContext.Err()
		}
		return classifyConnectionIOError(operation, err)
	}
	if frame.Type == FrameClose {
		if frame.Sequence != 0 || len(frame.Payload) != 0 {
			return s.rejectStart(connection, "close frame must be empty")
		}
		return errPeerClosed
	}
	if frame.Type != FrameStart || frame.Sequence != 0 || len(frame.Payload) != 0 {
		return s.rejectStart(connection, "first client frame must be an empty START with sequence zero")
	}
	if s.validateStart != nil {
		if err := s.validateStart(startContext); err != nil {
			if _, typed := AsError(err); !typed {
				err = NewError(ErrorBackendUnavailable, operation, "call state revalidation failed", err)
			}
			_ = writeStartError(connection, err)
			return err
		}
	}
	if starter, ok := s.device.(DeviceStarter); ok {
		if err := starter.Start(startContext); err != nil {
			if _, typed := AsError(err); !typed {
				if startContext.Err() != nil {
					err = NewError(ErrorTimeout, operation, "media device start timed out", startContext.Err())
				} else {
					err = NewError(ErrorBackendUnavailable, operation, "media device start failed", err)
				}
			}
			_ = writeStartError(connection, err)
			return err
		}
	}
	s.started.Store(true)
	if err := WriteFrame(connection, Frame{Type: FrameStarted}); err != nil {
		s.started.Store(false)
		return classifyConnectionIOError(operation, err)
	}
	if err := connection.SetDeadline(time.Time{}); err != nil {
		s.started.Store(false)
		return NewError(ErrorTimeout, operation, "media start deadline could not be cleared", err)
	}
	return nil
}

func (s *Session) rejectStart(connection net.Conn, message string) error {
	err := NewError(ErrorProtocol, "start_media", message, nil)
	_ = writeStartError(connection, err)
	return err
}

func writeStartError(connection net.Conn, err error) error {
	code := ErrorInternal
	if mediaError, ok := AsError(err); ok {
		code = mediaError.Code
	}
	return WriteFrame(connection, Frame{
		Type:    FrameError,
		Payload: []byte(code),
	})
}

func (s *Session) readPlayback(
	ctx context.Context,
	connection net.Conn,
	outbound chan<- Frame,
) error {
	var expectedSequence uint32
	maxPayload := s.format.FrameBytes
	if maxPayload < maxControlBytes {
		maxPayload = maxControlBytes
	}

	for {
		if err := connection.SetReadDeadline(time.Now().Add(s.options.FrameIOTimeout)); err != nil {
			return NewError(ErrorTimeout, "read_playback", "connection read deadline is unavailable", err)
		}
		frame, err := ReadFrame(connection, maxPayload)
		if err != nil {
			return classifyConnectionIOError("read_playback", err)
		}

		switch frame.Type {
		case FramePlayback:
			if len(frame.Payload) != s.format.FrameBytes {
				return NewError(
					ErrorProtocol,
					"read_playback",
					"playback frame has the wrong duration",
					nil,
				)
			}
			if frame.Sequence != expectedSequence {
				return NewError(
					ErrorProtocol,
					"read_playback",
					"playback frame sequence is not contiguous",
					nil,
				)
			}
			expectedSequence++
			if err := s.device.SetWriteDeadline(time.Now().Add(s.options.FrameIOTimeout)); err != nil {
				return NewError(ErrorTimeout, "write_playback", "device write deadline is unavailable", err)
			}
			if err := writeFull(s.device, frame.Payload); err != nil {
				return classifyDeviceIOError("write_playback", err)
			}
		case FramePing:
			if len(frame.Payload) > maxControlBytes {
				return NewError(ErrorProtocol, "read_playback", "ping payload is too large", nil)
			}
			payload := append([]byte(nil), frame.Payload...)
			if err := s.enqueue(ctx, outbound, Frame{
				Type:     FramePong,
				Sequence: frame.Sequence,
				Payload:  payload,
			}); err != nil {
				return err
			}
		case FrameClose:
			if len(frame.Payload) != 0 {
				return NewError(ErrorProtocol, "read_playback", "close frame must be empty", nil)
			}
			return errPeerClosed
		case FrameStart:
			return NewError(
				ErrorProtocol,
				"read_playback",
				"START may be sent exactly once before media frames",
				nil,
			)
		default:
			return NewError(ErrorProtocol, "read_playback", "client frame type is not allowed", nil)
		}
	}
}

func (s *Session) readCapture(ctx context.Context, outbound chan<- Frame) error {
	var sequence uint32
	for {
		payload := make([]byte, s.format.FrameBytes)
		if err := s.device.SetReadDeadline(time.Now().Add(s.options.FrameIOTimeout)); err != nil {
			return NewError(ErrorTimeout, "read_capture", "device read deadline is unavailable", err)
		}
		if _, err := io.ReadFull(s.device, payload); err != nil {
			return classifyDeviceIOError("read_capture", err)
		}
		if err := s.enqueue(ctx, outbound, Frame{
			Type:     FrameCapture,
			Sequence: sequence,
			Payload:  payload,
		}); err != nil {
			return err
		}
		sequence++
	}
}

func (s *Session) writeOutbound(
	ctx context.Context,
	connection net.Conn,
	outbound <-chan Frame,
) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case frame := <-outbound:
			if err := connection.SetWriteDeadline(time.Now().Add(s.options.FrameIOTimeout)); err != nil {
				return NewError(ErrorTimeout, "write_capture", "connection write deadline is unavailable", err)
			}
			if err := WriteFrame(connection, frame); err != nil {
				return classifyConnectionIOError("write_capture", err)
			}
		}
	}
}

func (s *Session) enqueue(ctx context.Context, outbound chan<- Frame, frame Frame) error {
	timer := time.NewTimer(s.options.BackpressureTimeout)
	defer timer.Stop()
	select {
	case outbound <- frame:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return NewError(ErrorBackpressure, "queue_media_frame", "media output is backpressured", nil)
	}
}

func classifyConnectionIOError(operation string, err error) error {
	if err == nil {
		return nil
	}
	if mediaError, ok := AsError(err); ok {
		return mediaError
	}
	var networkError net.Error
	if errors.As(err, &networkError) && networkError.Timeout() {
		return NewError(ErrorTimeout, operation, "media I/O deadline exceeded", err)
	}
	if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
		return errPeerClosed
	}
	return NewError(ErrorInternal, operation, "media I/O failed", err)
}

func classifyDeviceIOError(operation string, err error) error {
	if err == nil {
		return nil
	}
	if mediaError, ok := AsError(err); ok {
		return mediaError
	}
	var networkError net.Error
	if errors.As(err, &networkError) && networkError.Timeout() {
		return NewError(ErrorTimeout, operation, "media I/O deadline exceeded", err)
	}
	return NewError(ErrorInternal, operation, "PCM device I/O failed", err)
}

func writeFull(writer io.Writer, payload []byte) error {
	for len(payload) > 0 {
		written, err := writer.Write(payload)
		if err != nil {
			return err
		}
		if written <= 0 {
			return io.ErrShortWrite
		}
		payload = payload[written:]
	}
	return nil
}
