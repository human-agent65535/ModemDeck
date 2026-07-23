package agentmedia

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/human-agent65535/modemdeck/internal/callmedia"
	"github.com/human-agent65535/modemdeck/internal/store"
)

const (
	defaultOpenTimeout  = 3 * time.Second
	defaultStartTimeout = 15 * time.Second
	defaultIOTimeout    = 3 * time.Second
	maxErrorBodyBytes   = 64 << 10
)

type CallStore interface {
	CallByID(context.Context, string) (store.Call, error)
}

type Opener struct {
	socketPath   string
	calls        CallStore
	openTimeout  time.Duration
	startTimeout time.Duration
	ioTimeout    time.Duration
	dialer       net.Dialer
}

type Options struct {
	OpenTimeout  time.Duration
	StartTimeout time.Duration
	IOTimeout    time.Duration
}

func New(socketPath string, calls CallStore, options Options) (*Opener, error) {
	socketPath = filepath.Clean(socketPath)
	if !filepath.IsAbs(socketPath) || calls == nil {
		return nil, errors.New("agent media opener requires an absolute socket path and call store")
	}
	if options.OpenTimeout < 0 || options.StartTimeout < 0 || options.IOTimeout < 0 {
		return nil, errors.New("agent media timeouts must not be negative")
	}
	if options.OpenTimeout == 0 {
		options.OpenTimeout = defaultOpenTimeout
	}
	if options.StartTimeout == 0 {
		options.StartTimeout = defaultStartTimeout
	}
	if options.IOTimeout == 0 {
		options.IOTimeout = defaultIOTimeout
	}
	return &Opener{
		socketPath:   socketPath,
		calls:        calls,
		openTimeout:  options.OpenTimeout,
		startTimeout: options.StartTimeout,
		ioTimeout:    options.IOTimeout,
		dialer:       net.Dialer{Timeout: options.OpenTimeout},
	}, nil
}

func (o *Opener) Open(ctx context.Context, active callmedia.ActiveCall) (callmedia.MediaEndpoint, error) {
	if active.State != callmedia.CallStateActive || strings.TrimSpace(active.ID) == "" {
		return nil, callmedia.ErrCallNotActive
	}
	openContext, cancel := context.WithTimeout(normalizeContext(ctx), o.openTimeout)
	defer cancel()
	call, err := o.calls.CallByID(openContext, active.ID)
	if err != nil {
		return nil, fmt.Errorf("resolve media call: %w", err)
	}
	if call.ID != active.ID || call.Phase != "active" {
		return nil, callmedia.ErrCallNotActive
	}
	if !call.MediaAvailable ||
		strings.TrimSpace(call.EndpointCallID) == "" ||
		strings.TrimSpace(call.AudioPort) == "" {
		return nil, callmedia.ErrEndpointUnavailable
	}
	expected, err := callFormat(call)
	if err != nil {
		return nil, err
	}

	connection, err := o.dialer.DialContext(openContext, "unix", o.socketPath)
	if err != nil {
		return nil, fmt.Errorf("dial host media endpoint: %w", err)
	}
	keepConnection := false
	defer func() {
		if !keepConnection {
			_ = connection.Close()
		}
	}()
	if err := connection.SetDeadline(time.Now().Add(o.openTimeout)); err != nil {
		return nil, fmt.Errorf("set host media handshake deadline: %w", err)
	}
	request, err := http.NewRequestWithContext(
		openContext,
		http.MethodGet,
		"http://modemdeck-agent/v1/media/calls/"+url.PathEscape(call.EndpointCallID),
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("create host media request: %w", err)
	}
	request.Header.Set("Connection", "Upgrade")
	request.Header.Set("Upgrade", upgradeProtocol)
	if err := request.Write(connection); err != nil {
		return nil, fmt.Errorf("write host media request: %w", err)
	}
	reader := bufio.NewReader(connection)
	response, err := http.ReadResponse(reader, request)
	if err != nil {
		return nil, fmt.Errorf("read host media handshake: %w", err)
	}
	if response.StatusCode != http.StatusSwitchingProtocols {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, maxErrorBodyBytes))
		_ = response.Body.Close()
		return nil, fmt.Errorf("host media endpoint returned HTTP %d", response.StatusCode)
	}
	if !strings.EqualFold(strings.TrimSpace(response.Header.Get("Upgrade")), upgradeProtocol) ||
		response.Header.Get("ModemDeck-Media-Version") != strconv.Itoa(protocolVersion) {
		return nil, errors.New("host media protocol negotiation failed")
	}
	negotiated, frameBytes, err := parseFormatHeaders(response.Header)
	if err != nil {
		return nil, err
	}
	if negotiated != expected || frameBytes != expected.FrameBytes() {
		return nil, errors.New("host media format does not match the authoritative call metadata")
	}
	if err := connection.SetDeadline(time.Time{}); err != nil {
		return nil, fmt.Errorf("clear host media handshake deadline: %w", err)
	}
	keepConnection = true
	return &endpoint{
		connection:   connection,
		reader:       reader,
		format:       negotiated,
		frameBytes:   frameBytes,
		startTimeout: o.startTimeout,
		ioTimeout:    o.ioTimeout,
	}, nil
}

func callFormat(call store.Call) (callmedia.PCMFormat, error) {
	if !strings.EqualFold(call.AudioEncoding, "pcm") ||
		!strings.EqualFold(call.AudioResolution, string(callmedia.PCMEncodingS16LE)) {
		return callmedia.PCMFormat{}, callmedia.ErrUnsupported
	}
	format := callmedia.PCMFormat{
		Encoding:      callmedia.PCMEncodingS16LE,
		SampleRate:    int(call.AudioRate),
		Channels:      1,
		FrameDuration: 20 * time.Millisecond,
	}
	if err := format.Validate(); err != nil {
		return callmedia.PCMFormat{}, err
	}
	return format, nil
}

func parseFormatHeaders(headers http.Header) (callmedia.PCMFormat, int, error) {
	rate, err := strconv.Atoi(headers.Get("ModemDeck-Media-Rate"))
	if err != nil {
		return callmedia.PCMFormat{}, 0, errors.New("host media rate header is invalid")
	}
	channels, err := strconv.Atoi(headers.Get("ModemDeck-Media-Channels"))
	if err != nil {
		return callmedia.PCMFormat{}, 0, errors.New("host media channels header is invalid")
	}
	durationMilliseconds, err := strconv.Atoi(headers.Get("ModemDeck-Media-Frame-Duration-Ms"))
	if err != nil {
		return callmedia.PCMFormat{}, 0, errors.New("host media frame duration header is invalid")
	}
	frameBytes, err := strconv.Atoi(headers.Get("ModemDeck-Media-Frame-Bytes"))
	if err != nil || frameBytes <= 0 {
		return callmedia.PCMFormat{}, 0, errors.New("host media frame size header is invalid")
	}
	if !strings.EqualFold(headers.Get("ModemDeck-Media-Encoding"), "pcm") ||
		!strings.EqualFold(headers.Get("ModemDeck-Media-Resolution"), "s16le") {
		return callmedia.PCMFormat{}, 0, callmedia.ErrUnsupported
	}
	format := callmedia.PCMFormat{
		Encoding:      callmedia.PCMEncodingS16LE,
		SampleRate:    rate,
		Channels:      channels,
		FrameDuration: time.Duration(durationMilliseconds) * time.Millisecond,
	}
	if err := format.Validate(); err != nil {
		return callmedia.PCMFormat{}, 0, err
	}
	if frameBytes != format.FrameBytes() {
		return callmedia.PCMFormat{}, 0, errors.New("host media frame size is inconsistent")
	}
	return format, frameBytes, nil
}

type endpoint struct {
	connection   net.Conn
	reader       *bufio.Reader
	format       callmedia.PCMFormat
	frameBytes   int
	startTimeout time.Duration
	ioTimeout    time.Duration

	readMu         sync.Mutex
	writeMu        sync.Mutex
	closeOnce      sync.Once
	startAttempted bool
	started        bool
	closed         bool
	stateMu        sync.Mutex
	readSequence   uint32
	writeSequence  uint32
	closeErr       error
}

func (e *endpoint) Format() callmedia.PCMFormat {
	return e.format
}

func (e *endpoint) Start(ctx context.Context) error {
	const operation = "start_media"

	e.stateMu.Lock()
	if e.closed {
		e.stateMu.Unlock()
		return newError(ErrorConflict, operation, "host media endpoint is closed", nil)
	}
	if e.startAttempted {
		e.stateMu.Unlock()
		return newError(
			ErrorProtocol,
			operation,
			"host media endpoint START may be attempted exactly once",
			nil,
		)
	}
	e.startAttempted = true
	e.stateMu.Unlock()

	startContext, cancel := withBoundedTimeout(ctx, e.startTimeout)
	defer cancel()

	e.writeMu.Lock()
	stopWrite, err := setContextDeadline(
		startContext,
		e.startTimeout,
		e.connection.SetWriteDeadline,
	)
	if err == nil {
		err = writeFrame(e.connection, frame{Type: frameStart})
	}
	if stopWrite != nil {
		stopWrite()
	}
	e.writeMu.Unlock()
	if err != nil {
		_ = e.Close()
		return classifyEndpointError(startContext, operation, err)
	}

	e.readMu.Lock()
	stopRead, err := setContextDeadline(
		startContext,
		e.startTimeout,
		e.connection.SetReadDeadline,
	)
	var response frame
	if err == nil {
		response, err = readFrame(e.reader, maxControlBytes)
	}
	if stopRead != nil {
		stopRead()
	}
	e.readMu.Unlock()
	if err != nil {
		_ = e.Close()
		return classifyEndpointError(startContext, operation, err)
	}

	switch response.Type {
	case frameStarted:
		if response.Sequence != 0 || len(response.Payload) != 0 {
			_ = e.Close()
			return newError(
				ErrorProtocol,
				operation,
				"host STARTED response is malformed",
				nil,
			)
		}
	case frameError:
		_ = e.Close()
		return decodeHostStartError(response)
	default:
		_ = e.Close()
		return newError(
			ErrorProtocol,
			operation,
			"host did not acknowledge media START",
			nil,
		)
	}

	e.stateMu.Lock()
	if e.closed {
		e.stateMu.Unlock()
		return newError(ErrorConflict, operation, "host media endpoint closed during START", nil)
	}
	e.started = true
	e.stateMu.Unlock()
	return nil
}

func (e *endpoint) ReadPCM(ctx context.Context, destination []byte) error {
	if len(destination) != e.frameBytes {
		return callmedia.ErrInvalidArgument
	}
	if err := e.requireStarted("read_pcm"); err != nil {
		return err
	}
	e.readMu.Lock()
	defer e.readMu.Unlock()
	stop, err := setContextDeadline(ctx, e.ioTimeout, e.connection.SetReadDeadline)
	if err != nil {
		return err
	}
	defer stop()
	value, err := readFrame(e.reader, e.frameBytes)
	if err != nil {
		return contextOrError(ctx, err)
	}
	if value.Type != frameCapture ||
		value.Sequence != e.readSequence ||
		len(value.Payload) != e.frameBytes {
		return newError(
			ErrorProtocol,
			"read_pcm",
			"host media capture frame is invalid",
			nil,
		)
	}
	e.readSequence++
	copy(destination, value.Payload)
	return nil
}

func (e *endpoint) WritePCM(ctx context.Context, source []byte) error {
	if len(source) != e.frameBytes {
		return callmedia.ErrInvalidArgument
	}
	if err := e.requireStarted("write_pcm"); err != nil {
		return err
	}
	e.writeMu.Lock()
	defer e.writeMu.Unlock()
	stop, err := setContextDeadline(ctx, e.ioTimeout, e.connection.SetWriteDeadline)
	if err != nil {
		return err
	}
	defer stop()
	err = writeFrame(e.connection, frame{
		Type:     framePlayback,
		Sequence: e.writeSequence,
		Payload:  source,
	})
	if err != nil {
		return contextOrError(ctx, err)
	}
	e.writeSequence++
	return nil
}

func (e *endpoint) Close() error {
	e.closeOnce.Do(func() {
		e.stateMu.Lock()
		e.closed = true
		e.stateMu.Unlock()
		e.writeMu.Lock()
		_ = e.connection.SetWriteDeadline(time.Now().Add(250 * time.Millisecond))
		_ = writeFrame(e.connection, frame{Type: frameClose, Sequence: e.writeSequence})
		e.writeMu.Unlock()
		e.closeErr = e.connection.Close()
	})
	return e.closeErr
}

func (e *endpoint) requireStarted(operation string) error {
	e.stateMu.Lock()
	defer e.stateMu.Unlock()
	if e.closed {
		return newError(ErrorConflict, operation, "host media endpoint is closed", nil)
	}
	if !e.started {
		return newError(
			ErrorProtocol,
			operation,
			"host media endpoint must be started before PCM I/O",
			nil,
		)
	}
	return nil
}

func decodeHostStartError(value frame) error {
	if value.Sequence != 0 || len(value.Payload) == 0 || len(value.Payload) > maxControlBytes {
		return newError(ErrorProtocol, "start_media", "host START error is malformed", nil)
	}
	code := ErrorCode(value.Payload)
	switch code {
	case ErrorInvalidArgument,
		ErrorNotFound,
		ErrorProtocol,
		ErrorTimeout,
		ErrorConflict,
		ErrorNotActive,
		ErrorBackendUnavailable,
		ErrorMediaUnavailable,
		ErrorUnsupportedFormat,
		ErrorUnboundAudioPort,
		ErrorBackpressure,
		ErrorInternal:
		return newError(code, "start_media", "host rejected media START", nil)
	default:
		return newError(ErrorProtocol, "start_media", "host returned an unknown START error", nil)
	}
}

func setContextDeadline(
	ctx context.Context,
	timeout time.Duration,
	set func(time.Time) error,
) (func(), error) {
	ctx = normalizeContext(ctx)
	deadline := time.Now().Add(timeout)
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
		deadline = contextDeadline
	}
	if err := set(deadline); err != nil {
		return nil, err
	}
	stop := context.AfterFunc(ctx, func() {
		_ = set(time.Now())
	})
	return func() {
		stop()
		_ = set(time.Time{})
	}, nil
}

func withBoundedTimeout(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	ctx = normalizeContext(ctx)
	if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) <= timeout {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, timeout)
}

func classifyEndpointError(ctx context.Context, operation string, err error) error {
	if err == nil {
		return nil
	}
	if mediaError, ok := AsError(err); ok {
		return mediaError
	}
	if ctx != nil && ctx.Err() != nil {
		return newError(ErrorTimeout, operation, "host media operation exceeded its deadline", ctx.Err())
	}
	var networkError net.Error
	if errors.As(err, &networkError) && networkError.Timeout() {
		return newError(ErrorTimeout, operation, "host media operation exceeded its deadline", err)
	}
	return newError(ErrorInternal, operation, "host media operation failed", err)
}

func contextOrError(ctx context.Context, err error) error {
	if ctx != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}

func normalizeContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}
