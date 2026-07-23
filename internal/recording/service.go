package recording

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/human-agent65535/modemdeck/internal/callmedia"
	"github.com/human-agent65535/modemdeck/internal/store"
)

const (
	workerFinishTimeout = 10 * time.Second
	randomIDBytes       = 16
	pendingRequestTTL   = 24 * time.Hour
	requestCleanupEvery = time.Hour
	requestCleanupBatch = 256
)

type Service struct {
	repository Repository
	media      Media
	files      *fileStore
	codecs     callmedia.OpusCodecFactory
	writers    segmentWriterFactory
	now        func() time.Time
	random     io.Reader
	report     func(error)

	ctx    context.Context
	cancel context.CancelFunc

	reconcileMu sync.Mutex
	cleanupMu   sync.Mutex
	nextCleanup time.Time
	mu          sync.Mutex
	closed      bool
	workers     map[string]*recordingWorker
	knownCalls  map[string]struct{}
}

type recordingWorker struct {
	callID       string
	segmentID    string
	relativePath string
	generation   int64
	subscription *callmedia.DuplexSubscription
	writer       segmentWriter
	cancel       context.CancelFunc
	done         chan struct{}
	result       error
}

func New(repository Repository, media Media, options Options) (*Service, error) {
	if repository == nil || media == nil {
		return nil, ErrInvalidArgument
	}
	files, err := newFileStore(options.RootDirectory)
	if err != nil {
		return nil, err
	}
	codecs := options.CodecFactory
	if codecs == nil {
		codecs, err = callmedia.NewProductionOpusFactory()
		if err != nil {
			_ = files.close()
			return nil, fmt.Errorf("create recording codec factory: %w", errors.Join(ErrCodec, err))
		}
	}
	writers := options.writerFactory
	if writers == nil {
		writers = productionWriterFactory{}
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	randomSource := options.Random
	if randomSource == nil {
		randomSource = rand.Reader
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Service{
		repository: repository,
		media:      media,
		files:      files,
		codecs:     codecs,
		writers:    writers,
		now:        now,
		random:     randomSource,
		report:     options.Report,
		ctx:        ctx,
		cancel:     cancel,
		workers:    make(map[string]*recordingWorker),
		knownCalls: make(map[string]struct{}),
	}, nil
}

func (s *Service) Settings(ctx context.Context) (store.RecordingSettings, error) {
	if s == nil {
		return store.RecordingSettings{}, ErrClosed
	}
	settings, err := s.repository.RecordingSettings(normalizeContext(ctx))
	return settings, translateStoreError(err)
}

func (s *Service) UpdateSettings(
	ctx context.Context,
	defaultEnabled bool,
	revision int64,
) (store.RecordingSettings, error) {
	if s == nil || revision <= 0 {
		return store.RecordingSettings{}, ErrInvalidArgument
	}
	settings, err := s.repository.UpdateRecordingSettings(
		normalizeContext(ctx),
		defaultEnabled,
		revision,
	)
	return settings, translateStoreError(err)
}

func (s *Service) PrepareOutgoing(
	ctx context.Context,
	requestID string,
	enabled bool,
) (string, error) {
	if s == nil {
		return "", ErrClosed
	}
	if requestID == "" {
		var err error
		requestID, err = s.randomID("recording_request")
		if err != nil {
			return "", err
		}
	}
	if err := s.repository.PrepareCallRecordingRequest(
		normalizeContext(ctx),
		requestID,
		enabled,
	); err != nil {
		return "", translateStoreError(err)
	}
	return requestID, nil
}

func (s *Service) CallRecordings(ctx context.Context, callID string) (CallRecordings, error) {
	if s == nil || !validOpaqueID(callID) {
		return CallRecordings{}, ErrInvalidArgument
	}
	state, err := s.repository.CallRecordingState(normalizeContext(ctx), callID)
	if errors.Is(err, store.ErrRecordingNotFound) {
		if _, callErr := s.repository.CallByID(normalizeContext(ctx), callID); callErr != nil {
			return CallRecordings{}, translateStoreError(callErr)
		}
		state = store.CallRecordingState{
			CallID:     callID,
			Preference: store.RecordingPreferenceDefault,
			Status:     store.RecordingStateOff,
		}
	} else if err != nil {
		return CallRecordings{}, translateStoreError(err)
	}
	segments, err := s.repository.RecordingSegments(normalizeContext(ctx), callID)
	if err != nil {
		return CallRecordings{}, translateStoreError(err)
	}
	return CallRecordings{State: state, Segments: segments}, nil
}

func (s *Service) SetEnabled(
	ctx context.Context,
	callID string,
	enabled bool,
) (store.CallRecordingState, error) {
	if s == nil || !validOpaqueID(callID) {
		return store.CallRecordingState{}, ErrInvalidArgument
	}
	s.reconcileMu.Lock()
	defer s.reconcileMu.Unlock()

	call, err := s.repository.CallByID(normalizeContext(ctx), callID)
	if err != nil {
		return store.CallRecordingState{}, translateStoreError(err)
	}
	if call.Phase != "active" || call.EndedAt != "" {
		return store.CallRecordingState{}, ErrCallNotActive
	}
	s.mu.Lock()
	s.knownCalls[call.ID] = struct{}{}
	s.mu.Unlock()
	state, err := s.repository.SetCallRecordingEnabled(normalizeContext(ctx), callID, enabled)
	if err != nil {
		return store.CallRecordingState{}, translateStoreError(err)
	}
	if !enabled {
		stopErr := s.stopWorker(normalizeContext(ctx), callID)
		authoritative, stateErr := s.repository.CallRecordingState(
			normalizeContext(ctx),
			callID,
		)
		return authoritative, errors.Join(stopErr, translateStoreError(stateErr))
	}
	target := store.RecordingTarget{
		CallID:         call.ID,
		Phase:          call.Phase,
		MediaAvailable: call.MediaAvailable,
		State:          state,
	}
	if state.Status != store.RecordingStatePending {
		return state, nil
	}
	if !call.MediaAvailable {
		return state, nil
	}
	startErr := s.startTarget(target)
	authoritative, stateErr := s.repository.CallRecordingState(normalizeContext(ctx), callID)
	if stateErr != nil {
		return state, translateStoreError(stateErr)
	}
	return authoritative, startErr
}

func (s *Service) Download(
	ctx context.Context,
	callID, segmentID string,
) (Download, error) {
	if s == nil || !validOpaqueID(callID) || !validOpaqueID(segmentID) {
		return Download{}, ErrInvalidArgument
	}
	segment, err := s.repository.RecordingSegment(normalizeContext(ctx), callID, segmentID)
	if err != nil {
		return Download{}, translateStoreError(err)
	}
	if segment.Status != store.RecordingSegmentReady || segment.RelativePath == "" {
		return Download{}, errors.Join(ErrConflict, ErrNotReady)
	}
	file, err := s.files.open(segment.RelativePath)
	if err != nil {
		return Download{}, err
	}
	return Download{Segment: segment, File: file}, nil
}

func (s *Service) Recover(ctx context.Context) error {
	if s == nil {
		return ErrClosed
	}
	if err := s.files.cleanupPartialFiles(); err != nil {
		return err
	}
	interrupted, err := s.repository.InterruptedRecordingSegments(normalizeContext(ctx))
	if err != nil {
		return translateStoreError(err)
	}
	for _, segment := range interrupted {
		if err := s.files.remove(segment.RelativePath); err != nil {
			return err
		}
	}
	if err := s.repository.FailInterruptedRecordingSegments(normalizeContext(ctx)); err != nil {
		return translateStoreError(err)
	}
	return s.cleanupExpiredRequests(normalizeContext(ctx), true)
}

func (s *Service) ReconcileAuthoritativeCalls(
	ctx context.Context,
	calls []store.Call,
) error {
	if s == nil {
		return ErrClosed
	}
	s.reconcileMu.Lock()
	defer s.reconcileMu.Unlock()
	ctx = normalizeContext(ctx)
	result := s.cleanupExpiredRequests(ctx, false)
	active := make(map[string]struct{}, len(calls))
	for _, call := range calls {
		active[call.ID] = struct{}{}
		s.mu.Lock()
		s.knownCalls[call.ID] = struct{}{}
		s.mu.Unlock()
		state, stateErr := s.repository.EnsureCallRecordingState(ctx, call.ID)
		if stateErr != nil {
			result = errors.Join(result, translateStoreError(stateErr))
			continue
		}
		target := store.RecordingTarget{
			CallID:         call.ID,
			Phase:          call.Phase,
			MediaAvailable: call.MediaAvailable,
			State:          state,
		}
		if call.Phase == "active" &&
			state.Enabled &&
			state.Status == store.RecordingStatePending &&
			call.MediaAvailable {
			if startErr := s.startTarget(target); startErr != nil {
				result = errors.Join(result, startErr)
			}
			continue
		}
		if stopErr := s.stopWorker(ctx, call.ID); stopErr != nil {
			result = errors.Join(result, stopErr)
		}
	}
	s.mu.Lock()
	knownIDs := make([]string, 0, len(s.knownCalls))
	for callID := range s.knownCalls {
		knownIDs = append(knownIDs, callID)
	}
	s.mu.Unlock()
	for _, callID := range knownIDs {
		if _, exists := active[callID]; exists {
			continue
		}
		if stopErr := s.stopWorker(ctx, callID); stopErr != nil {
			result = errors.Join(result, stopErr)
		}
		if finalizeErr := s.repository.FinalizePendingCallRecording(
			ctx,
			callID,
			"call_ended_before_recording",
		); finalizeErr != nil {
			result = errors.Join(result, translateStoreError(finalizeErr))
		}
		s.mu.Lock()
		delete(s.knownCalls, callID)
		s.mu.Unlock()
	}
	return result
}

func (s *Service) cleanupExpiredRequests(ctx context.Context, force bool) error {
	s.cleanupMu.Lock()
	defer s.cleanupMu.Unlock()

	now := s.now().UTC()
	if !force && !s.nextCleanup.IsZero() && now.Before(s.nextCleanup) {
		return nil
	}
	deleted, err := s.repository.DeleteExpiredCallRecordingRequests(
		ctx,
		now.Add(-pendingRequestTTL),
		requestCleanupBatch,
	)
	if err != nil {
		s.nextCleanup = now.Add(requestCleanupEvery)
		return translateStoreError(err)
	}
	if deleted == requestCleanupBatch {
		s.nextCleanup = now
	} else {
		s.nextCleanup = now.Add(requestCleanupEvery)
	}
	return nil
}

func (s *Service) FinalizeCall(ctx context.Context, callID string) error {
	if s == nil || !validOpaqueID(callID) {
		return ErrInvalidArgument
	}
	s.reconcileMu.Lock()
	defer s.reconcileMu.Unlock()
	ctx = normalizeContext(ctx)
	return errors.Join(
		s.stopWorker(ctx, callID),
		translateStoreError(s.repository.FinalizePendingCallRecording(
			ctx,
			callID,
			"call_ended_before_recording",
		)),
	)
}

func (s *Service) Close(ctx context.Context) error {
	if s == nil {
		return nil
	}
	ctx = normalizeContext(ctx)
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	workers := make([]*recordingWorker, 0, len(s.workers))
	for _, worker := range s.workers {
		workers = append(workers, worker)
		worker.cancel()
	}
	s.mu.Unlock()
	var result error
	for _, worker := range workers {
		select {
		case <-worker.done:
		case <-ctx.Done():
			result = errors.Join(result, ErrClosed)
		}
	}
	s.cancel()
	if err := s.files.close(); err != nil {
		result = errors.Join(result, err)
	}
	return result
}

func (s *Service) startTarget(target store.RecordingTarget) error {
	if !target.MediaAvailable {
		return ErrMediaUnavailable
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return ErrClosed
	}
	if _, exists := s.workers[target.CallID]; exists {
		s.mu.Unlock()
		return nil
	}
	s.mu.Unlock()

	segmentID, err := s.randomID("recording")
	if err != nil {
		return err
	}
	relativePath := target.CallID + "/" + segmentID + ".ogg"
	segment, err := s.repository.CreateRecordingSegment(s.ctx, store.RecordingSegment{
		ID:           segmentID,
		CallID:       target.CallID,
		RelativePath: relativePath,
	})
	if err != nil {
		return translateStoreError(err)
	}
	fail := func(code string, cause error) error {
		failContext, cancel := context.WithTimeout(
			context.Background(),
			workerFinishTimeout,
		)
		defer cancel()
		failErr := s.repository.FailRecordingSegment(
			failContext,
			target.CallID,
			segment.ID,
			code,
			s.now().UTC(),
		)
		return errors.Join(cause, translateStoreError(failErr))
	}
	subscription, err := s.media.SubscribeDuplex(s.ctx, callmedia.ActiveCall{
		ID:    target.CallID,
		State: callmedia.CallStateActive,
	})
	if err != nil {
		translated := translateMediaError(err)
		return fail(recordingFailureCode(translated), translated)
	}
	writer, writerPath, err := s.writers.New(
		s.files,
		target.CallID,
		segment.ID,
		subscription.Format(),
		s.codecs,
	)
	if err != nil {
		_ = subscription.Close()
		return fail(recordingFailureCode(err), err)
	}
	if writerPath != relativePath {
		_ = writer.Abort()
		_ = subscription.Close()
		return fail("storage_io", ErrStorage)
	}
	if err := subscription.Start(s.ctx); err != nil {
		_ = writer.Abort()
		_ = subscription.Close()
		translated := translateMediaError(err)
		return fail(recordingFailureCode(translated), translated)
	}
	startedAt := s.now().UTC()
	if err := s.repository.MarkRecordingSegmentStarted(
		s.ctx,
		target.CallID,
		segment.ID,
		startedAt,
	); err != nil {
		_ = writer.Abort()
		_ = subscription.Close()
		return fail("database_write", translateStoreError(err))
	}
	workerContext, cancel := context.WithCancel(s.ctx)
	worker := &recordingWorker{
		callID:       target.CallID,
		segmentID:    segment.ID,
		relativePath: relativePath,
		generation:   target.State.Generation,
		subscription: subscription,
		writer:       writer,
		cancel:       cancel,
		done:         make(chan struct{}),
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		cancel()
		_ = writer.Abort()
		_ = subscription.Close()
		return fail("service_closed", ErrClosed)
	}
	s.workers[target.CallID] = worker
	s.mu.Unlock()
	go s.runWorker(workerContext, worker)
	return nil
}

func (s *Service) runWorker(ctx context.Context, worker *recordingWorker) {
	defer close(worker.done)
	defer worker.subscription.Close()
	var runErr error
	for {
		frame, err := worker.subscription.Next(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, io.EOF) {
				break
			}
			runErr = translateMediaError(err)
			break
		}
		if err := worker.writer.Write(frame); err != nil {
			runErr = err
			break
		}
	}
	finishContext, cancel := context.WithTimeout(context.Background(), workerFinishTimeout)
	defer cancel()
	if runErr == nil {
		duration, size, err := worker.writer.Finalize()
		if err == nil {
			err = s.repository.CompleteRecordingSegment(
				finishContext,
				worker.callID,
				worker.segmentID,
				s.now().UTC(),
				duration.Milliseconds(),
				size,
			)
		}
		if err != nil {
			_ = s.files.remove(worker.relativePath)
			runErr = err
		}
	} else {
		_ = worker.writer.Abort()
	}
	if runErr != nil {
		failErr := s.repository.FailRecordingSegment(
			finishContext,
			worker.callID,
			worker.segmentID,
			recordingFailureCode(runErr),
			s.now().UTC(),
		)
		runErr = errors.Join(runErr, translateStoreError(failErr))
	}
	worker.result = runErr
	s.mu.Lock()
	if s.workers[worker.callID] == worker {
		delete(s.workers, worker.callID)
	}
	s.mu.Unlock()
	if runErr != nil {
		s.reportWorkerError(fmt.Errorf(
			"record call %s segment %s: %w",
			worker.callID,
			worker.segmentID,
			runErr,
		))
	}
}

func (s *Service) stopWorker(ctx context.Context, callID string) error {
	s.mu.Lock()
	worker := s.workers[callID]
	if worker != nil {
		worker.cancel()
	}
	s.mu.Unlock()
	if worker == nil {
		return nil
	}
	select {
	case <-worker.done:
		return worker.result
	case <-normalizeContext(ctx).Done():
		return ErrClosed
	}
}

func (s *Service) reportWorkerError(err error) {
	if err == nil {
		return
	}
	if s.report != nil {
		s.report(err)
	}
}

func (s *Service) randomID(prefix string) (string, error) {
	buffer := make([]byte, randomIDBytes)
	if _, err := io.ReadFull(s.random, buffer); err != nil {
		return "", fmt.Errorf("generate recording identifier: %w", ErrStorage)
	}
	return prefix + "_" + hex.EncodeToString(buffer), nil
}

func translateStoreError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, store.ErrCallNotFound),
		errors.Is(err, store.ErrRecordingNotFound):
		return ErrNotFound
	case errors.Is(err, store.ErrRecordingRevisionConflict),
		errors.Is(err, store.ErrRecordingRequestConflict):
		if errors.Is(err, store.ErrRecordingRevisionConflict) {
			return errors.Join(ErrConflict, ErrRevisionConflict)
		}
		return errors.Join(ErrConflict, ErrRequestConflict)
	case errors.Is(err, store.ErrRecordingValidation):
		return ErrInvalidArgument
	default:
		return err
	}
}

func translateMediaError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, callmedia.ErrBackpressure):
		return ErrBackpressure
	case errors.Is(err, callmedia.ErrEndpointUnavailable),
		errors.Is(err, callmedia.ErrEndpointIO),
		errors.Is(err, callmedia.ErrCallNotActive):
		return ErrMediaUnavailable
	case errors.Is(err, callmedia.ErrCodec),
		errors.Is(err, callmedia.ErrUnsupported):
		return ErrCodec
	default:
		return err
	}
}

func recordingFailureCode(err error) string {
	switch {
	case errors.Is(err, ErrMediaUnavailable):
		return "media_unavailable"
	case errors.Is(err, ErrBackpressure):
		return "backpressure"
	case errors.Is(err, ErrCodec):
		return "codec_failure"
	case errors.Is(err, ErrNoAudio):
		return "no_audio"
	case errors.Is(err, ErrStorage):
		return "storage_io"
	case errors.Is(err, ErrClosed):
		return "service_closed"
	default:
		return "recording_failed"
	}
}

func normalizeContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}
