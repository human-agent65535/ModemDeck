package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/human-agent65535/modemdeck/internal/auth"
	"github.com/human-agent65535/modemdeck/internal/calllease"
	"github.com/human-agent65535/modemdeck/internal/communication"
	"github.com/human-agent65535/modemdeck/internal/store"
)

func TestConcurrentPreparedCommitRetainsOriginalOwnerAndDispatchesOnce(t *testing.T) {
	f := outgoingHTTP(t)
	recording := &blockingOutgoingRecording{
		fakeRecordingService: fakeRecordingService{settings: store.RecordingSettings{DefaultEnabled: true}},
		entered:              make(chan struct{}),
		release:              make(chan struct{}),
	}
	f.api.recordings = recording
	id := f.prepare(t, "concurrent-request")
	f.readyPreparation(t, id)
	path := "/api/v1/calls/preparations/" + id + "/commit"
	commit := func(done chan<- *httptest.ResponseRecorder) {
		r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"audio_running":true}`))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		f.api.ServeHTTP(w, r)
		done <- w
	}
	first, retry := make(chan *httptest.ResponseRecorder, 1), make(chan *httptest.ResponseRecorder, 1)
	go commit(first)
	select {
	case <-recording.entered:
	case <-time.After(time.Second):
		t.Fatal("commit never reached the recording barrier")
	}
	go commit(retry)
	reservations, err := f.leases.OutgoingReservations(developmentCallLeaseHolderID)
	if err != nil || len(reservations) != 1 || reservations[0].ID != id {
		t.Fatalf("concurrent retry changed the original reservation: %+v, %v", reservations, err)
	}
	close(recording.release)
	for _, done := range []<-chan *httptest.ResponseRecorder{first, retry} {
		select {
		case response := <-done:
			if response.Code != http.StatusCreated {
				t.Fatalf("commit status %d: %s", response.Code, response.Body.String())
			}
		case <-time.After(time.Second):
			t.Fatal("concurrent commit did not complete")
		}
	}
	if f.communications.startCalls != 1 {
		t.Fatalf("concurrent commit dispatched %d calls", f.communications.startCalls)
	}
	state, err := f.leases.ControlState(context.Background(), "actual-call", developmentCallLeaseHolderID)
	if err != nil || state != calllease.ControlOwned {
		t.Fatalf("retry lost the original owner: %s, %v", state, err)
	}
}

func TestIndeterminatePreparedCommitRetainsOwnerUntilSnapshotResolution(t *testing.T) {
	for _, found := range []bool{false, true} {
		t.Run(map[bool]string{false: "no-call", true: "late-call"}[found], func(t *testing.T) {
			f := outgoingHTTP(t)
			id := f.prepare(t, "indeterminate-request")
			f.readyPreparation(t, id)
			f.communications.onStart = nil
			f.communications.startError = errors.Join(communication.ErrUnavailable, communication.ErrStartCallOutcomeIndeterminate)
			f.request(t, http.MethodPost, "/api/v1/calls/preparations/"+id+"/commit", `{"audio_running":true}`, http.StatusServiceUnavailable)
			reservations, err := f.leases.OutgoingReservations(developmentCallLeaseHolderID)
			if err != nil || len(reservations) != 1 || reservations[0].ID != id {
				t.Fatalf("indeterminate dispatch released its owner: %+v, %v", reservations, err)
			}
			var calls []store.Call
			if found {
				calls = []store.Call{f.communications.call}
				f.calls.mu.Lock()
				f.calls.call = f.communications.call
				f.calls.mu.Unlock()
			}
			if err := f.leases.ReconcileAuthoritativeCalls(context.Background(), calls); err != nil {
				t.Fatal(err)
			}
			if found {
				if err := f.leases.Require(context.Background(), "actual-call", developmentCallLeaseHolderID); err != nil {
					t.Fatalf("late call lost its original owner: %v", err)
				}
			} else {
				remaining, err := f.leases.OutgoingReservations(developmentCallLeaseHolderID)
				if err != nil || len(remaining) != 0 {
					t.Fatalf("complete empty snapshot did not resolve reservation: %+v, %v", remaining, err)
				}
			}
			if f.communications.startCalls != 1 {
				t.Fatal("snapshot resolution dispatched another call")
			}
		})
	}
}

func TestPreparedCommitReplayDoesNotNeedEphemeralReservation(t *testing.T) {
	f := outgoingHTTP(t)
	id := f.prepare(t, "completed-request")
	f.readyPreparation(t, id)
	path := "/api/v1/calls/preparations/" + id + "/commit"
	f.request(t, http.MethodPost, path, `{"audio_running":true}`, http.StatusCreated)
	if err := f.leases.ReconcileAuthoritativeCalls(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	f.communications.startReplay = true
	f.communications.call.Phase = "ended"
	response := f.request(t, http.MethodPost, path, `{"audio_running":true}`, http.StatusCreated)
	if !strings.Contains(response.Body.String(), `"phase":"ended"`) || f.communications.startCalls != 1 {
		t.Fatalf("completed replay required a new reservation or redialed: %s", response.Body.String())
	}
}

func TestPreparedCallOwnershipExcludesAnotherRequestAndBrowser(t *testing.T) {
	f := outgoingHTTP(t)
	id := f.prepare(t, "owned-request")
	f.request(t, http.MethodPost, "/api/v1/calls/prepare", `{"request_id":"second","line_id":"line","number":"+81000000002"}`, http.StatusConflict)
	foreignContext := contextWithCallLeaseSession(context.Background(), auth.SessionToken("different-browser"))
	for _, operation := range []struct{ method, suffix, body string }{
		{http.MethodPost, "/commit", `{"audio_running":true}`},
		{http.MethodPut, "/lease", `{}`},
		{http.MethodDelete, "", ""},
	} {
		r := httptest.NewRequest(operation.method, "/api/v1/calls/preparations/"+id+operation.suffix, strings.NewReader(operation.body))
		r = r.WithContext(foreignContext)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		f.api.outgoingPreparationResource(w, r)
		if w.Code != http.StatusNotFound || strings.Contains(w.Body.String(), id) {
			t.Fatalf("foreign browser accessed preparation: %d %s", w.Code, w.Body.String())
		}
	}
	if err := f.leases.RequireOutgoing(id, developmentCallLeaseHolderID); err != nil {
		t.Fatalf("foreign request changed the original reservation: %v", err)
	}
	f.readyPreparation(t, id)
	f.request(t, http.MethodPost, "/api/v1/calls/preparations/"+id+"/commit", `{"audio_running":true}`, http.StatusCreated)
	f.request(t, http.MethodPost, "/api/v1/calls/prepare", `{"request_id":"third","line_id":"line","number":"+81000000003"}`, http.StatusConflict)
}

func TestPreparedRecordingOmissionUsesEnabledDefault(t *testing.T) {
	f := outgoingHTTP(t)
	recordings := &fakeRecordingService{
		settings: store.RecordingSettings{DefaultEnabled: true},
	}
	f.api.recordings = recordings
	id := f.prepare(t, "default-recording-request")
	if recordings.prepareCalls != 0 {
		t.Fatal("media preparation mutated recording state")
	}
	f.readyPreparation(t, id)
	f.request(t, http.MethodPost, "/api/v1/calls/preparations/"+id+"/commit", `{"audio_running":true}`, http.StatusCreated)
	if recordings.prepareCalls != 1 || !recordings.preparedEnabled {
		t.Fatal("omitted recording override lost the enabled default")
	}
}

func TestPreparationFreezesEffectiveRecordingChoiceWithoutMutation(t *testing.T) {
	for _, tc := range []struct {
		name           string
		defaultEnabled bool
		override       string
		want           bool
	}{
		{"default-on", true, "", true}, {"default-off", false, "", false},
		{"explicit-off", true, `,"recording_enabled":false`, false},
		{"explicit-on", false, `,"recording_enabled":true`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := outgoingHTTP(t)
			recordings := &fakeRecordingService{settings: store.RecordingSettings{DefaultEnabled: tc.defaultEnabled}}
			f.api.recordings = recordings
			body := `{"request_id":"fixed-recording","line_id":"line","number":"+81000000001"` + tc.override + `}`
			response := f.request(t, http.MethodPost, "/api/v1/calls/prepare", body, http.StatusCreated)
			var result struct {
				ID      string `json:"preparation_id"`
				Enabled bool   `json:"recording_enabled"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if result.Enabled != tc.want || recordings.prepareCalls != 0 || f.communications.startCalls != 0 {
				t.Fatalf("prepare choice/state mismatch: %s", response.Body.String())
			}
			recordings.settings.DefaultEnabled = !tc.defaultEnabled
			replay := f.request(t, http.MethodPost, "/api/v1/calls/prepare", body, http.StatusCreated)
			if replay.Body.String() != response.Body.String() {
				t.Fatalf("prepare replay changed fixed choice: %s", replay.Body.String())
			}
			changed := `{"request_id":"fixed-recording","line_id":"line","number":"+81000000001"`
			if tc.override == "" {
				changed += `,"recording_enabled":` + strconv.FormatBool(tc.want)
			}
			f.request(t, http.MethodPost, "/api/v1/calls/prepare", changed+`}`, http.StatusConflict)
			f.readyPreparation(t, result.ID)
			f.request(t, http.MethodPost, "/api/v1/calls/preparations/"+result.ID+"/commit", `{"audio_running":true}`, http.StatusCreated)
			expectedCalls := 0
			if tc.override != "" || tc.want {
				expectedCalls = 1
			}
			if recordings.prepareCalls != expectedCalls || (expectedCalls == 1 && recordings.preparedEnabled != tc.want) {
				t.Fatalf("commit did not use preparation choice: calls=%d enabled=%v", recordings.prepareCalls, recordings.preparedEnabled)
			}
		})
	}
}
