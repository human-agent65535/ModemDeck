package recording

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/human-agent65535/modemdeck/internal/callmedia"
	platformdb "github.com/human-agent65535/modemdeck/internal/platform/database"
	"github.com/human-agent65535/modemdeck/internal/store"
)

const recordingTestTimeout = 5 * time.Second

func TestServiceFollowsIncomingDefaultAndCreatesToggleSegments(t *testing.T) {
	fixture := newServiceFixture(t, nil)
	settings, err := fixture.repository.RecordingSettings(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.repository.UpdateRecordingSettings(
		context.Background(),
		true,
		settings.Revision,
	); err != nil {
		t.Fatal(err)
	}
	applyServiceTestCall(t, fixture.repository, "call-default", "", "incoming", true)

	if err := fixture.reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	firstWriter := receiveWriter(t, fixture.writers.created)
	if got := fixture.endpoint.startCalls.Load(); got != 1 {
		t.Fatalf("endpoint starts = %d, want 1", got)
	}
	frame := make([]byte, fixture.endpoint.format.FrameBytes())
	frame[0] = 0x23
	fixture.endpoint.capture <- frame
	select {
	case <-firstWriter.writes:
	case <-time.After(recordingTestTimeout):
		t.Fatal("first recording frame was not written")
	}

	state, err := fixture.service.SetEnabled(context.Background(), "call-default", false)
	if err != nil {
		t.Fatal(err)
	}
	if state.Enabled || state.Status != store.RecordingStateOff {
		t.Fatalf("disabled state = %+v", state)
	}
	segments, err := fixture.repository.RecordingSegments(context.Background(), "call-default")
	if err != nil {
		t.Fatal(err)
	}
	if len(segments) != 1 ||
		segments[0].Status != store.RecordingSegmentReady ||
		segments[0].DurationMS != 20 ||
		segments[0].SizeBytes != 128 {
		t.Fatalf("first segments = %+v", segments)
	}

	state, err = fixture.service.SetEnabled(context.Background(), "call-default", true)
	if err != nil {
		t.Fatal(err)
	}
	if !state.Enabled || state.Status != store.RecordingStateRecording {
		t.Fatalf("re-enabled state = %+v", state)
	}
	receiveWriter(t, fixture.writers.created)
	if got := fixture.endpoint.startCalls.Load(); got != 1 {
		t.Fatalf("endpoint starts after second segment = %d, want 1", got)
	}
	if err := fixture.service.FinalizeCall(context.Background(), "call-default"); err != nil {
		t.Fatal(err)
	}
	segments, err = fixture.repository.RecordingSegments(context.Background(), "call-default")
	if err != nil {
		t.Fatal(err)
	}
	if len(segments) != 2 ||
		segments[0].SegmentIndex != 1 ||
		segments[1].SegmentIndex != 2 ||
		segments[1].Status != store.RecordingSegmentReady {
		t.Fatalf("toggle segments = %+v", segments)
	}
}

func TestServiceOutgoingOverrideCanDisableEnabledDefault(t *testing.T) {
	fixture := newServiceFixture(t, nil)
	settings, err := fixture.repository.RecordingSettings(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.repository.UpdateRecordingSettings(
		context.Background(),
		true,
		settings.Revision,
	); err != nil {
		t.Fatal(err)
	}
	requestID, err := fixture.service.PrepareOutgoing(
		context.Background(),
		"request-no-recording",
		false,
	)
	if err != nil {
		t.Fatal(err)
	}
	applyServiceTestCall(
		t,
		fixture.repository,
		"call-override",
		requestID,
		"outgoing",
		true,
	)
	if err := fixture.reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case writer := <-fixture.writers.created:
		t.Fatalf("unexpected writer = %+v", writer)
	case <-time.After(50 * time.Millisecond):
	}
	state, err := fixture.repository.CallRecordingState(context.Background(), "call-override")
	if err != nil {
		t.Fatal(err)
	}
	if state.Preference != store.RecordingPreferenceOverride || state.Enabled {
		t.Fatalf("override state = %+v", state)
	}
	if got := fixture.opener.opens.Load(); got != 0 {
		t.Fatalf("media opens = %d, want 0", got)
	}
}

func TestCallRecordingsDoesNotCreateStateForHistoricalCall(t *testing.T) {
	fixture := newServiceFixture(t, nil)
	applyServiceTestCall(t, fixture.repository, "call-history-only", "", "incoming", false)
	if err := fixture.repository.ApplyHardwareSnapshot(context.Background(), store.HardwareSnapshot{
		BootEpoch:  "boot-service-recording",
		Revision:   "snapshot-history-ended",
		ObservedAt: time.Date(2026, time.July, 23, 16, 1, 0, 0, time.UTC),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.database.ExecContext(
		context.Background(),
		"DELETE FROM modemdeck_call_recording_state WHERE call_id = ?",
		"call-history-only",
	); err != nil {
		t.Fatal(err)
	}
	recordings, err := fixture.service.CallRecordings(
		context.Background(),
		"call-history-only",
	)
	if err != nil {
		t.Fatal(err)
	}
	if recordings.State.Status != store.RecordingStateOff ||
		recordings.State.Enabled ||
		len(recordings.Segments) != 0 {
		t.Fatalf("historical recordings = %+v", recordings)
	}
	if _, err := fixture.repository.CallRecordingState(
		context.Background(),
		"call-history-only",
	); !errors.Is(err, store.ErrRecordingNotFound) {
		t.Fatalf("CallRecordingState() error = %v, want ErrRecordingNotFound", err)
	}
}

func TestServiceMediaUnavailableRemainsPendingUntilCallEnds(t *testing.T) {
	fixture := newServiceFixture(t, nil)
	applyServiceTestCall(t, fixture.repository, "call-no-media", "", "incoming", false)
	state, err := fixture.service.SetEnabled(context.Background(), "call-no-media", true)
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != store.RecordingStatePending || state.LastErrorCode != "" {
		t.Fatalf("pending state = %+v", state)
	}
	segments, listErr := fixture.repository.RecordingSegments(
		context.Background(),
		"call-no-media",
	)
	if listErr != nil {
		t.Fatal(listErr)
	}
	if len(segments) != 0 {
		t.Fatalf("segments before media becomes available = %+v", segments)
	}
	if err := fixture.publish(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	state, err = fixture.repository.CallRecordingState(context.Background(), "call-no-media")
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != store.RecordingStateFailed ||
		state.LastErrorCode != "call_ended_before_recording" {
		t.Fatalf("terminal state = %+v", state)
	}
}

func TestServiceWriterBackpressureFailsSegmentWithoutRetry(t *testing.T) {
	fixture := newServiceFixture(t, ErrBackpressure)
	applyServiceTestCall(t, fixture.repository, "call-backpressure", "", "incoming", true)
	if err := fixture.reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.service.SetEnabled(
		context.Background(),
		"call-backpressure",
		true,
	); err != nil {
		t.Fatal(err)
	}
	receiveWriter(t, fixture.writers.created)
	frame := make([]byte, fixture.endpoint.format.FrameBytes())
	fixture.endpoint.capture <- frame

	eventuallyRecording(t, func() bool {
		segments, err := fixture.repository.RecordingSegments(
			context.Background(),
			"call-backpressure",
		)
		return err == nil &&
			len(segments) == 1 &&
			segments[0].Status == store.RecordingSegmentFailed &&
			segments[0].FailureCode == "backpressure"
	})
	select {
	case workerErr := <-fixture.reports:
		if !errors.Is(workerErr, ErrBackpressure) {
			t.Fatalf("worker report = %v, want ErrBackpressure", workerErr)
		}
	case <-time.After(recordingTestTimeout):
		t.Fatal("recording worker error was not reported")
	}
	if err := fixture.reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := fixture.writers.count(); got != 1 {
		t.Fatalf("writer attempts = %d, want 1", got)
	}
}

func TestServiceDoesNotRetryInterruptedGeneration(t *testing.T) {
	fixture := newServiceFixture(t, nil)
	applyServiceTestCall(t, fixture.repository, "call-interrupted", "", "incoming", true)
	if err := fixture.reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	state, err := fixture.repository.SetCallRecordingEnabled(
		context.Background(),
		"call-interrupted",
		true,
	)
	if err != nil {
		t.Fatal(err)
	}
	segment, err := fixture.repository.CreateRecordingSegment(
		context.Background(),
		store.RecordingSegment{
			ID:           "segment-interrupted",
			CallID:       "call-interrupted",
			RelativePath: "call-interrupted/segment-interrupted.ogg",
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.repository.MarkRecordingSegmentStarted(
		context.Background(),
		segment.CallID,
		segment.ID,
		time.Now().UTC(),
	); err != nil {
		t.Fatal(err)
	}
	if err := fixture.repository.FailInterruptedRecordingSegments(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := fixture.reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	returned, err := fixture.service.SetEnabled(
		context.Background(),
		"call-interrupted",
		true,
	)
	if err != nil {
		t.Fatal(err)
	}
	if returned.Generation != state.Generation ||
		returned.Status != store.RecordingStateFailed {
		t.Fatalf("same generation state = %+v", returned)
	}
	select {
	case writer := <-fixture.writers.created:
		t.Fatalf("interrupted generation retried with writer %+v", writer)
	case <-time.After(50 * time.Millisecond):
	}
	if got := fixture.opener.opens.Load(); got != 0 {
		t.Fatalf("media opens for interrupted generation = %d, want 0", got)
	}

	if _, err := fixture.service.SetEnabled(
		context.Background(),
		"call-interrupted",
		false,
	); err != nil {
		t.Fatal(err)
	}
	retried, err := fixture.service.SetEnabled(
		context.Background(),
		"call-interrupted",
		true,
	)
	if err != nil {
		t.Fatal(err)
	}
	if retried.Generation != state.Generation+2 ||
		retried.Status != store.RecordingStateRecording {
		t.Fatalf("explicit retry state = %+v", retried)
	}
	receiveWriter(t, fixture.writers.created)
}

func TestServiceRemoteCallRemovalFinalizesRecording(t *testing.T) {
	fixture := newServiceFixture(t, nil)
	applyServiceTestCall(t, fixture.repository, "call-remote-end", "", "incoming", true)
	if err := fixture.reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.service.SetEnabled(
		context.Background(),
		"call-remote-end",
		true,
	); err != nil {
		t.Fatal(err)
	}
	receiveWriter(t, fixture.writers.created)
	if err := fixture.publish(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	eventuallyRecording(t, func() bool {
		segments, err := fixture.repository.RecordingSegments(
			context.Background(),
			"call-remote-end",
		)
		return err == nil && len(segments) == 1 &&
			segments[0].Status == store.RecordingSegmentReady
	})
	if got := fixture.endpoint.closeCalls.Load(); got != 1 {
		t.Fatalf("endpoint closes = %d, want 1", got)
	}
}

func TestServiceRequestCleanupUsesBoundedBatches(t *testing.T) {
	fixture := newServiceFixture(t, nil)
	ctx := context.Background()
	transaction, err := fixture.database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < requestCleanupBatch+1; index++ {
		if _, err := transaction.ExecContext(
			ctx,
			`INSERT INTO modemdeck_call_recording_requests (request_id, enabled, created_at)
			 VALUES (?, 1, '2000-01-01 00:00:00')`,
			fmt.Sprintf("expired-request-%03d", index),
		); err != nil {
			_ = transaction.Rollback()
			t.Fatal(err)
		}
	}
	if _, err := transaction.ExecContext(
		ctx,
		`INSERT INTO modemdeck_call_recording_requests (request_id, enabled, created_at)
		 VALUES ('recent-request', 0, CURRENT_TIMESTAMP)`,
	); err != nil {
		_ = transaction.Rollback()
		t.Fatal(err)
	}
	if err := transaction.Commit(); err != nil {
		t.Fatal(err)
	}

	if err := fixture.service.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	if got := countServiceRequests(t, fixture.database, "created_at < '2020-01-01'"); got != 1 {
		t.Fatalf("expired rows after first bounded cleanup = %d, want 1", got)
	}
	if err := fixture.reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	if got := countServiceRequests(t, fixture.database, "created_at < '2020-01-01'"); got != 0 {
		t.Fatalf("expired rows after backlog cleanup = %d, want 0", got)
	}
	if got := countServiceRequests(t, fixture.database, "request_id = 'recent-request'"); got != 1 {
		t.Fatalf("recent request rows = %d, want 1", got)
	}
}

func TestRecoverRemovesPublishedFileForInterruptedSegment(t *testing.T) {
	fixture := newServiceFixture(t, nil)
	ctx := context.Background()
	applyServiceTestCall(t, fixture.repository, "call-recover-file", "", "incoming", true)
	if _, err := fixture.repository.EnsureCallRecordingState(ctx, "call-recover-file"); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.repository.SetCallRecordingEnabled(ctx, "call-recover-file", true); err != nil {
		t.Fatal(err)
	}
	segment, err := fixture.repository.CreateRecordingSegment(ctx, store.RecordingSegment{
		ID:           "segment-recover-file",
		CallID:       "call-recover-file",
		RelativePath: "call-recover-file/segment-recover-file.ogg",
	})
	if err != nil {
		t.Fatal(err)
	}
	callDirectory := filepath.Join(fixture.service.files.root, segment.CallID)
	if err := os.Mkdir(callDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	final := filepath.Join(callDirectory, segment.ID+".ogg")
	if err := os.WriteFile(final, []byte("published-before-crash"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := fixture.service.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(final); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("interrupted published file still exists: %v", err)
	}
	stored, err := fixture.repository.RecordingSegment(ctx, segment.CallID, segment.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != store.RecordingSegmentFailed ||
		stored.FailureCode != "interrupted" {
		t.Fatalf("recovered segment = %+v", stored)
	}
}

type serviceFixture struct {
	database   *sql.DB
	repository *store.Store
	service    *Service
	core       *callmedia.Core
	opener     *recordingEndpointOpener
	endpoint   *recordingEndpoint
	writers    *memoryWriterFactory
	reports    chan error
}

func newServiceFixture(t *testing.T, writerError error) *serviceFixture {
	t.Helper()
	directory := t.TempDir()
	database, err := platformdb.Open(context.Background(), platformdb.Config{
		TargetPath: filepath.Join(directory, "modemdeck.db"),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	repository, err := store.New(database)
	if err != nil {
		t.Fatal(err)
	}
	format := callmedia.PCMFormat{
		Encoding:      callmedia.PCMEncodingS16LE,
		SampleRate:    8000,
		Channels:      1,
		FrameDuration: 20 * time.Millisecond,
	}
	endpoint := newRecordingEndpoint(format)
	opener := &recordingEndpointOpener{endpoint: endpoint}
	core, err := callmedia.New(callmedia.Options{
		EndpointOpener: opener,
		CodecFactory:   unusedCodecFactory{},
	})
	if err != nil {
		t.Fatal(err)
	}
	writers := &memoryWriterFactory{
		created:  make(chan *memoryWriter, 8),
		writeErr: writerError,
	}
	reports := make(chan error, 8)
	randomBytes := make([]byte, 256)
	for index := range randomBytes {
		randomBytes[index] = byte(index)
	}
	service, err := New(repository, core, Options{
		RootDirectory: filepath.Join(directory, "recordings"),
		CodecFactory:  unusedCodecFactory{},
		Random:        bytes.NewReader(randomBytes),
		Report: func(err error) {
			reports <- err
		},
		writerFactory: writers,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), recordingTestTimeout)
		defer cancel()
		if err := service.Close(ctx); err != nil {
			t.Errorf("close recording service: %v", err)
		}
		if err := core.Close(ctx); err != nil {
			t.Errorf("close media core: %v", err)
		}
	})
	return &serviceFixture{
		database:   database,
		repository: repository,
		service:    service,
		core:       core,
		opener:     opener,
		endpoint:   endpoint,
		writers:    writers,
		reports:    reports,
	}
}

func (f *serviceFixture) reconcile(ctx context.Context) error {
	calls, err := f.repository.ActiveCalls(ctx)
	if err != nil {
		return err
	}
	return f.publish(ctx, calls)
}

func (f *serviceFixture) publish(ctx context.Context, calls []store.Call) error {
	callIDs := make([]string, 0, len(calls))
	for _, call := range calls {
		callIDs = append(callIDs, call.ID)
	}
	if err := f.core.ReconcileActiveCalls(ctx, callIDs); err != nil {
		return err
	}
	return f.service.ReconcileAuthoritativeCalls(ctx, calls)
}

func countServiceRequests(t *testing.T, database *sql.DB, predicate string) int {
	t.Helper()
	var count int
	if err := database.QueryRowContext(
		context.Background(),
		"SELECT COUNT(*) FROM modemdeck_call_recording_requests WHERE "+predicate,
	).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

type recordingEndpointOpener struct {
	endpoint *recordingEndpoint
	opens    atomic.Int32
}

func (o *recordingEndpointOpener) Open(
	context.Context,
	callmedia.ActiveCall,
) (callmedia.MediaEndpoint, error) {
	o.opens.Add(1)
	return o.endpoint, nil
}

type recordingEndpoint struct {
	format  callmedia.PCMFormat
	capture chan []byte
	started chan struct{}
	closed  chan struct{}

	startOnce  sync.Once
	closeOnce  sync.Once
	startCalls atomic.Int32
	closeCalls atomic.Int32
}

func newRecordingEndpoint(format callmedia.PCMFormat) *recordingEndpoint {
	return &recordingEndpoint{
		format:  format,
		capture: make(chan []byte, 8),
		started: make(chan struct{}),
		closed:  make(chan struct{}),
	}
}

func (e *recordingEndpoint) Format() callmedia.PCMFormat {
	return e.format
}

func (e *recordingEndpoint) Start(context.Context) error {
	e.startCalls.Add(1)
	e.startOnce.Do(func() { close(e.started) })
	return nil
}

func (e *recordingEndpoint) ReadPCM(ctx context.Context, destination []byte) error {
	select {
	case <-e.started:
	case <-ctx.Done():
		return ctx.Err()
	case <-e.closed:
		return io.EOF
	}
	select {
	case frame := <-e.capture:
		copy(destination, frame)
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-e.closed:
		return io.EOF
	}
}

func (e *recordingEndpoint) WritePCM(context.Context, []byte) error {
	return nil
}

func (e *recordingEndpoint) Close() error {
	e.closeCalls.Add(1)
	e.closeOnce.Do(func() { close(e.closed) })
	return nil
}

type unusedCodecFactory struct{}

func (unusedCodecFactory) New(callmedia.PCMFormat) (callmedia.OpusCodec, error) {
	return nil, errors.New("unused test codec")
}

type memoryWriterFactory struct {
	mu       sync.Mutex
	writers  []*memoryWriter
	created  chan *memoryWriter
	writeErr error
}

func (f *memoryWriterFactory) New(
	_ *fileStore,
	callID, segmentID string,
	_ callmedia.PCMFormat,
	_ callmedia.OpusCodecFactory,
) (segmentWriter, string, error) {
	writer := &memoryWriter{
		writes:   make(chan struct{}, 8),
		writeErr: f.writeErr,
	}
	f.mu.Lock()
	f.writers = append(f.writers, writer)
	f.mu.Unlock()
	f.created <- writer
	return writer, callID + "/" + segmentID + ".ogg", nil
}

func (f *memoryWriterFactory) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.writers)
}

type memoryWriter struct {
	mu       sync.Mutex
	frames   int64
	finished bool
	writes   chan struct{}
	writeErr error
}

func (w *memoryWriter) Write(callmedia.DuplexFrame) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.finished {
		return ErrClosed
	}
	if w.writeErr != nil {
		return w.writeErr
	}
	w.frames++
	w.writes <- struct{}{}
	return nil
}

func (w *memoryWriter) Finalize() (time.Duration, int64, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.finished {
		return 0, 0, ErrClosed
	}
	w.finished = true
	return time.Duration(w.frames) * 20 * time.Millisecond, 128, nil
}

func (w *memoryWriter) Abort() error {
	w.mu.Lock()
	w.finished = true
	w.mu.Unlock()
	return nil
}

func receiveWriter(t *testing.T, writers <-chan *memoryWriter) *memoryWriter {
	t.Helper()
	select {
	case writer := <-writers:
		return writer
	case <-time.After(recordingTestTimeout):
		t.Fatal("timed out waiting for recording writer")
		return nil
	}
}

func eventuallyRecording(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(recordingTestTimeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("recording condition was not satisfied before timeout")
}

func applyServiceTestCall(
	t *testing.T,
	repository *store.Store,
	callID, requestID, direction string,
	mediaAvailable bool,
) {
	t.Helper()
	observed := time.Date(2026, time.July, 23, 16, 0, 0, 0, time.UTC)
	line := store.HardwareLine{
		ID:                  "line-service-recording",
		Model:               "Fixture modem",
		EquipmentIdentifier: "990000000000088",
		State:               "connected",
		ICCID:               "8901000000000000088",
		IMSI:                "440500000000088",
	}
	audioPort := ""
	if mediaAvailable {
		audioPort = "hw:8,0"
	}
	if err := repository.ApplyHardwareSnapshot(context.Background(), store.HardwareSnapshot{
		BootEpoch:  "boot-service-recording",
		Revision:   "snapshot-" + callID,
		ObservedAt: observed,
		Lines:      []store.HardwareLine{line},
		Calls: []store.HardwareCall{{
			AppID:           callID,
			RequestID:       requestID,
			LineID:          line.ID,
			EndpointCallID:  "endpoint-" + callID,
			Number:          "+818000000088",
			Direction:       direction,
			Phase:           "active",
			AudioPort:       audioPort,
			AudioEncoding:   "pcm",
			AudioResolution: "s16le",
			AudioRate:       8000,
			MediaAvailable:  mediaAvailable,
			Revision:        1,
			ObservedAt:      observed,
		}},
	}); err != nil {
		t.Fatalf("ApplyHardwareSnapshot() error = %v", err)
	}
}
