package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/human-agent65535/modemdeck/internal/calllease"
	"github.com/human-agent65535/modemdeck/internal/recording"
	"github.com/human-agent65535/modemdeck/internal/store"
)

func TestRecordingSettingsAndToggleReturnAuthoritativeState(t *testing.T) {
	recordings := &fakeRecordingService{
		settings: store.RecordingSettings{
			DefaultEnabled: false,
			Revision:       3,
		},
		updatedSettings: store.RecordingSettings{
			DefaultEnabled: true,
			Revision:       4,
		},
		toggleState: store.CallRecordingState{
			CallID:     "call-1",
			Enabled:    true,
			Generation: 2,
			Status:     store.RecordingStateRecording,
		},
	}
	api, err := New(&fakeRepository{}, Options{
		Recording:             recordings,
		CallLeases:            &fakeCallLeases{},
		disableAuthentication: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	update := httptest.NewRequest(
		http.MethodPut,
		"/api/v1/settings/recording",
		bytes.NewBufferString(`{"default_enabled":true,"revision":3}`),
	)
	update.Header.Set("Content-Type", "application/json")
	updateResponse := httptest.NewRecorder()
	api.ServeHTTP(updateResponse, update)
	if updateResponse.Code != http.StatusOK {
		t.Fatalf("settings status = %d; body = %s", updateResponse.Code, updateResponse.Body.String())
	}
	if !recordings.updatedEnabled || recordings.updatedRevision != 3 {
		t.Fatalf(
			"settings update = enabled %v revision %d",
			recordings.updatedEnabled,
			recordings.updatedRevision,
		)
	}

	toggle := httptest.NewRequest(
		http.MethodPut,
		"/api/v1/calls/call-1/recording",
		bytes.NewBufferString(`{"enabled":true}`),
	)
	toggle.Header.Set("Content-Type", "application/json")
	toggleResponse := httptest.NewRecorder()
	api.ServeHTTP(toggleResponse, toggle)
	if toggleResponse.Code != http.StatusOK {
		t.Fatalf("toggle status = %d; body = %s", toggleResponse.Code, toggleResponse.Body.String())
	}
	var payload recordingStateResponse
	if err := json.Unmarshal(toggleResponse.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.State.CallID != "call-1" ||
		payload.State.Status != store.RecordingStateRecording ||
		recordings.toggleCallID != "call-1" ||
		!recordings.toggleEnabled {
		t.Fatalf("toggle payload = %+v, service = %+v", payload, recordings)
	}
}

func TestRecordingToggleRejectsNonOwner(t *testing.T) {
	t.Parallel()

	recordings := &fakeRecordingService{}
	api, err := New(&fakeRepository{}, Options{
		Recording:             recordings,
		CallLeases:            &fakeCallLeases{err: calllease.ErrNotOwner},
		disableAuthentication: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(
		http.MethodPut,
		"/api/v1/calls/call-1/recording",
		bytes.NewBufferString(`{"enabled":true}`),
	)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	api.ServeHTTP(response, request)

	assertAPIError(t, response, http.StatusConflict, "call_not_owned")
	if recordings.toggleCallID != "" {
		t.Fatalf("non-owner toggled recording for call %q", recordings.toggleCallID)
	}
}

func TestRecordingMetadataDoesNotLeakPathAndDownloadIsOgg(t *testing.T) {
	endedAt := "2026-07-23T17:00:00Z"
	segment := store.RecordingSegment{
		ID:           "segment-1",
		CallID:       "call-1",
		SegmentIndex: 1,
		Status:       store.RecordingSegmentReady,
		EndedAt:      &endedAt,
		DurationMS:   1000,
		SizeBytes:    7,
		RelativePath: "call-1/segment-1.ogg",
	}
	recordings := &fakeRecordingService{
		callRecordings: recording.CallRecordings{
			State:    store.CallRecordingState{CallID: "call-1"},
			Segments: []store.RecordingSegment{segment},
		},
		download: recording.Download{
			Segment: segment,
			File:    &recordingReadSeekCloser{Reader: bytes.NewReader([]byte("OggS123"))},
		},
	}
	api, err := New(&fakeRepository{}, Options{
		Recording:             recordings,
		CallLeases:            &fakeCallLeases{},
		disableAuthentication: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	listResponse := httptest.NewRecorder()
	api.ServeHTTP(
		listResponse,
		httptest.NewRequest(http.MethodGet, "/api/v1/calls/call-1/recordings", nil),
	)
	if listResponse.Code != http.StatusOK {
		t.Fatalf("list status = %d; body = %s", listResponse.Code, listResponse.Body.String())
	}
	if strings.Contains(listResponse.Body.String(), "RelativePath") ||
		strings.Contains(listResponse.Body.String(), "call-1/segment-1.ogg") ||
		strings.Contains(listResponse.Body.String(), "/data/") {
		t.Fatalf("recording path leaked: %s", listResponse.Body.String())
	}

	downloadResponse := httptest.NewRecorder()
	api.ServeHTTP(
		downloadResponse,
		httptest.NewRequest(
			http.MethodGet,
			"/api/v1/calls/call-1/recordings/segment-1/download",
			nil,
		),
	)
	if downloadResponse.Code != http.StatusOK {
		t.Fatalf(
			"download status = %d; body = %s",
			downloadResponse.Code,
			downloadResponse.Body.String(),
		)
	}
	if downloadResponse.Header().Get("Content-Type") != "audio/ogg" ||
		downloadResponse.Body.String() != "OggS123" {
		t.Fatalf(
			"download headers = %#v; body = %q",
			downloadResponse.Header(),
			downloadResponse.Body.String(),
		)
	}
}

func TestRecordingDeleteUsesExactCallAndSegment(t *testing.T) {
	t.Parallel()

	recordings := &fakeRecordingService{}
	api, err := New(&fakeRepository{}, Options{
		Recording:             recordings,
		CallLeases:            &fakeCallLeases{},
		disableAuthentication: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	api.ServeHTTP(
		response,
		httptest.NewRequest(
			http.MethodDelete,
			"/api/v1/calls/call-1/recordings/segment-1",
			nil,
		),
	)
	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d; body = %s", response.Code, response.Body.String())
	}
	if recordings.deleteRecordingCallID != "call-1" ||
		recordings.deleteRecordingSegmentID != "segment-1" {
		t.Fatalf(
			"deleted recording = call %q segment %q",
			recordings.deleteRecordingCallID,
			recordings.deleteRecordingSegmentID,
		)
	}
}

func TestRecordingBatchDeleteUsesExactCallAndSegment(t *testing.T) {
	t.Parallel()

	recordings := &fakeRecordingService{}
	api, err := New(&fakeRepository{}, Options{
		Recording:             recordings,
		disableAuthentication: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(
		http.MethodPatch,
		"/api/v1/recordings/batch",
		bytes.NewBufferString(
			`{"action":"delete","recordings":[{"call_id":" call-1 ","id":" segment-1 "}]}`,
		),
	)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	api.ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d; body = %s", response.Code, response.Body.String())
	}
	if recordings.deleteRecordingCallID != "call-1" ||
		recordings.deleteRecordingSegmentID != "segment-1" {
		t.Fatalf(
			"deleted recording = call %q segment %q",
			recordings.deleteRecordingCallID,
			recordings.deleteRecordingSegmentID,
		)
	}
}

func TestRecordingBatchFavoriteUsesEntryIdentityWithoutRecordingService(t *testing.T) {
	t.Parallel()

	repository := &fakeRepository{}
	api, err := New(repository, Options{disableAuthentication: true})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(
		http.MethodPatch,
		"/api/v1/recordings/batch",
		bytes.NewBufferString(
			`{"action":"favorite","recordings":[{"call_id":" call-1 ","id":" segment-1 "},{"call_id":"call-1","id":"segment-1"}]}`,
		),
	)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	api.ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d; body = %s", response.Code, response.Body.String())
	}
	want := []store.RecordingIdentity{{CallID: "call-1", ID: "segment-1"}}
	if !repository.recordingFavorite ||
		len(repository.recordingFavorites) != len(want) ||
		repository.recordingFavorites[0] != want[0] {
		t.Fatalf(
			"recording favorite update = favorite %v recordings %+v",
			repository.recordingFavorite,
			repository.recordingFavorites,
		)
	}
}

func TestRecordingCollectionIsBoundedSearchableAndDoesNotLeakPaths(t *testing.T) {
	repository := &fakeRepository{
		recordingEntries: []store.RecordingEntry{{
			Segment: store.RecordingSegment{
				ID:           "segment-1",
				CallID:       "call-1",
				SegmentIndex: 1,
				Status:       store.RecordingSegmentReady,
				RelativePath: "call-1/segment-1.ogg",
			},
			Call: store.RecordingCall{
				ID:           "call-1",
				RemoteNumber: "+818000000001",
				ContactName:  "Alice",
			},
			Playable: true,
		}},
	}
	api, err := New(repository, Options{disableAuthentication: true})
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	api.ServeHTTP(
		response,
		httptest.NewRequest(
			http.MethodGet,
			"/api/v1/recordings?q=Alice&limit=9999",
			nil,
		),
	)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", response.Code, response.Body.String())
	}
	if repository.recordingQuery.Search != "Alice" ||
		repository.recordingQuery.Limit != store.MaxQueryLimit {
		t.Fatalf("recording query = %+v", repository.recordingQuery)
	}
	var payload recordingEntriesResponse
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Recordings) != 1 ||
		payload.Recordings[0].Segment.ID != "segment-1" ||
		payload.Meta.Limit != store.MaxQueryLimit {
		t.Fatalf("recording response = %+v", payload)
	}
	if strings.Contains(response.Body.String(), "relative_path") ||
		strings.Contains(response.Body.String(), "segment-1.ogg") {
		t.Fatalf("recording path leaked: %s", response.Body.String())
	}
}

func TestRecordingErrorsAreTypedAndToggleIncludesState(t *testing.T) {
	recordings := &fakeRecordingService{
		updateError: errors.Join(recording.ErrConflict, recording.ErrRevisionConflict),
		toggleState: store.CallRecordingState{
			CallID:  "call-1",
			Enabled: true,
			Status:  store.RecordingStateFailed,
		},
		toggleError: recording.ErrMediaUnavailable,
	}
	api, err := New(&fakeRepository{}, Options{
		Recording:             recordings,
		CallLeases:            &fakeCallLeases{},
		disableAuthentication: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	update := httptest.NewRequest(
		http.MethodPut,
		"/api/v1/settings/recording",
		bytes.NewBufferString(`{"default_enabled":true,"revision":2}`),
	)
	update.Header.Set("Content-Type", "application/json")
	updateResponse := httptest.NewRecorder()
	api.ServeHTTP(updateResponse, update)
	assertAPIError(t, updateResponse, http.StatusConflict, "revision_conflict")

	toggle := httptest.NewRequest(
		http.MethodPut,
		"/api/v1/calls/call-1/recording",
		bytes.NewBufferString(`{"enabled":true}`),
	)
	toggle.Header.Set("Content-Type", "application/json")
	toggleResponse := httptest.NewRecorder()
	api.ServeHTTP(toggleResponse, toggle)
	if toggleResponse.Code != http.StatusServiceUnavailable {
		t.Fatalf("toggle status = %d; body = %s", toggleResponse.Code, toggleResponse.Body.String())
	}
	var payload recordingMutationErrorResponse
	if err := json.Unmarshal(toggleResponse.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Error.Code != "media_unavailable" ||
		payload.State.CallID != "call-1" ||
		payload.State.Status != store.RecordingStateFailed {
		t.Fatalf("toggle error payload = %+v", payload)
	}
}

func TestRecordingRoutesRejectPathTraversalBeforeService(t *testing.T) {
	recordings := &fakeRecordingService{}
	api, err := New(&fakeRepository{}, Options{
		Recording:             recordings,
		disableAuthentication: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	api.ServeHTTP(
		response,
		httptest.NewRequest(
			http.MethodGet,
			"/api/v1/calls/call-1/recordings/%2e%2e/download",
			nil,
		),
	)
	assertAPIError(t, response, http.StatusNotFound, "not_found")
	if recordings.downloadCallID != "" {
		t.Fatalf("download service called with %q", recordings.downloadCallID)
	}
}

func TestStartCallRecordingOverrideIsOptional(t *testing.T) {
	t.Run("omitted follows default", func(t *testing.T) {
		communications := &fakeCommunications{call: store.Call{ID: "call-default"}}
		recordings := &fakeRecordingService{}
		api, err := New(&fakeRepository{}, Options{
			Communications:        communications,
			Recording:             recordings,
			CallLeases:            &fakeCallLeases{},
			disableAuthentication: true,
		})
		if err != nil {
			t.Fatal(err)
		}
		request := httptest.NewRequest(
			http.MethodPost,
			"/api/v1/calls",
			bytes.NewBufferString(
				`{"request_id":"request-default","line_id":"line-1","number":"+818000000077"}`,
			),
		)
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		api.ServeHTTP(response, request)
		if response.Code != http.StatusCreated {
			t.Fatalf("status = %d; body = %s", response.Code, response.Body.String())
		}
		if recordings.prepareCalls != 0 ||
			communications.startInput.RequestID != "request-default" {
			t.Fatalf(
				"prepare calls = %d, start input = %+v",
				recordings.prepareCalls,
				communications.startInput,
			)
		}
	})

	t.Run("explicit false is prepared", func(t *testing.T) {
		communications := &fakeCommunications{call: store.Call{ID: "call-override"}}
		recordings := &fakeRecordingService{preparedRequestID: "request-recording-off"}
		api, err := New(&fakeRepository{}, Options{
			Communications:        communications,
			Recording:             recordings,
			CallLeases:            &fakeCallLeases{},
			disableAuthentication: true,
		})
		if err != nil {
			t.Fatal(err)
		}
		request := httptest.NewRequest(
			http.MethodPost,
			"/api/v1/calls",
			bytes.NewBufferString(
				`{"line_id":"line-1","number":"+818000000077","recording_enabled":false}`,
			),
		)
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		api.ServeHTTP(response, request)
		if response.Code != http.StatusCreated {
			t.Fatalf("status = %d; body = %s", response.Code, response.Body.String())
		}
		if recordings.prepareCalls != 1 ||
			recordings.preparedEnabled ||
			communications.startInput.RequestID != "request-recording-off" {
			t.Fatalf("recording service = %+v, start input = %+v", recordings, communications.startInput)
		}
	})
}

func TestIncomingAnswerPersistsRecordingChoiceBeforeModemAnswer(t *testing.T) {
	recordings := &fakeRecordingService{
		settings: store.RecordingSettings{DefaultEnabled: true, Revision: 1},
		toggleState: store.CallRecordingState{
			CallID: "call-incoming",
			Status: store.RecordingStateOff,
		},
	}
	answerSawPreparedRecording := false
	communications := &fakeCommunications{onAction: func() {
		answerSawPreparedRecording =
			recordings.toggleCallID == "call-incoming" && !recordings.toggleEnabled
	}}
	api, err := New(&fakeRepository{}, Options{
		Communications:        communications,
		Recording:             recordings,
		CallLeases:            &fakeCallLeases{},
		disableAuthentication: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/calls/call-incoming/answer",
		bytes.NewBufferString(
			`{"request_id":"request-answer","recording_enabled":false}`,
		),
	)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	api.ServeHTTP(response, request)

	if response.Code != http.StatusAccepted {
		t.Fatalf("status = %d; body = %s", response.Code, response.Body.String())
	}
	if recordings.toggleCallID != "call-incoming" || recordings.toggleEnabled {
		t.Fatalf("recording preparation = %+v", recordings)
	}
	if communications.actionCalls != 1 || communications.actionInput.Action != "answer" {
		t.Fatalf("answer action = %+v", communications.actionInput)
	}
	if !answerSawPreparedRecording {
		t.Fatal("modem answer ran before the recording choice was persisted")
	}
}

type fakeRecordingService struct {
	settings                 store.RecordingSettings
	updatedSettings          store.RecordingSettings
	updateError              error
	updatedEnabled           bool
	updatedRevision          int64
	preparedRequestID        string
	preparedEnabled          bool
	prepareCalls             int
	prepareError             error
	callRecordings           recording.CallRecordings
	callRecordingsErr        error
	toggleState              store.CallRecordingState
	toggleCallID             string
	toggleEnabled            bool
	toggleError              error
	download                 recording.Download
	downloadCallID           string
	downloadSegmentID        string
	downloadError            error
	deleteCallID             string
	deleteCallIDs            []string
	deleteCallError          error
	deleteRecordingCallID    string
	deleteRecordingSegmentID string
	deleteRecordingError     error
	finalizedCallID          string
}

func (f *fakeRecordingService) Settings(context.Context) (store.RecordingSettings, error) {
	return f.settings, nil
}

func (f *fakeRecordingService) UpdateSettings(
	_ context.Context,
	enabled bool,
	revision int64,
) (store.RecordingSettings, error) {
	f.updatedEnabled = enabled
	f.updatedRevision = revision
	return f.updatedSettings, f.updateError
}

func (f *fakeRecordingService) PrepareOutgoing(
	_ context.Context,
	_ string,
	enabled bool,
) (string, error) {
	f.prepareCalls++
	f.preparedEnabled = enabled
	return f.preparedRequestID, f.prepareError
}

func (f *fakeRecordingService) CallRecordings(
	context.Context,
	string,
) (recording.CallRecordings, error) {
	return f.callRecordings, f.callRecordingsErr
}

func (f *fakeRecordingService) SetEnabled(
	_ context.Context,
	callID string,
	enabled bool,
) (store.CallRecordingState, error) {
	f.toggleCallID = callID
	f.toggleEnabled = enabled
	return f.toggleState, f.toggleError
}

func (f *fakeRecordingService) Download(
	_ context.Context,
	callID, segmentID string,
) (recording.Download, error) {
	f.downloadCallID = callID
	f.downloadSegmentID = segmentID
	return f.download, f.downloadError
}

func (f *fakeRecordingService) DeleteRecording(
	_ context.Context,
	callID, segmentID string,
) error {
	f.deleteRecordingCallID = callID
	f.deleteRecordingSegmentID = segmentID
	return f.deleteRecordingError
}

func (f *fakeRecordingService) DeleteCall(_ context.Context, callID string) error {
	f.deleteCallID = callID
	f.deleteCallIDs = append(f.deleteCallIDs, callID)
	return f.deleteCallError
}

func (f *fakeRecordingService) FinalizeCall(_ context.Context, callID string) error {
	f.finalizedCallID = callID
	return nil
}

type recordingReadSeekCloser struct {
	*bytes.Reader
}

func (recordingReadSeekCloser) Close() error {
	return nil
}
