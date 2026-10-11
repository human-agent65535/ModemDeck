package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/human-agent65535/modemdeck/internal/calllease"
	"github.com/human-agent65535/modemdeck/internal/callmedia"
	"github.com/human-agent65535/modemdeck/internal/communication"
	"github.com/human-agent65535/modemdeck/internal/store"
)

type outgoingTestCalls struct {
	mu   sync.Mutex
	call store.Call
}

func (s *outgoingTestCalls) CallByID(_ context.Context, id string) (store.Call, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.call.ID != id {
		return store.Call{}, store.ErrCallNotFound
	}
	return s.call, nil
}

func (s *outgoingTestCalls) ActiveCalls(context.Context) ([]store.Call, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.call.ID == "" {
		return nil, nil
	}
	return []store.Call{s.call}, nil
}

type outgoingTestCommunication struct {
	fakeCommunications
	endMu sync.Mutex
	ended []string
}

func (s *outgoingTestCommunication) StartCall(ctx context.Context, input communication.StartCallInput) (store.Call, error) {
	if ctx.Err() != nil || input.ReadyToDial == nil || !input.ReadyToDial() {
		return store.Call{}, communication.ErrUnavailable
	}
	return s.fakeCommunications.StartCall(ctx, input)
}

func (s *outgoingTestCommunication) EndCall(_ context.Context, id string) error {
	s.endMu.Lock()
	defer s.endMu.Unlock()
	s.ended = append(s.ended, id)
	return nil
}

func (s *httpSocketMedia) PrepareSocket(ctx context.Context, id, token string, transport callmedia.SocketTransport) (*callmedia.Session, error) {
	return s.core.PrepareSocket(ctx, id, token, transport)
}

func (s *httpSocketMedia) ActivatePreparedSocket(ctx context.Context, id, call string, session *callmedia.Session) error {
	return s.core.ActivatePreparedSocket(ctx, id, callmedia.ActiveCall{ID: call, State: callmedia.CallStateActive}, session)
}

func (s *httpSocketMedia) BindPreparedSocket(ctx context.Context, id, call string, session *callmedia.Session) error {
	return s.core.BindPreparedSocket(ctx, id, callmedia.ActiveCall{ID: call, State: callmedia.CallStateActive}, session)
}

func (leases *fakeCallLeases) RequireOutgoing(string, string) error          { return leases.err }
func (leases *fakeCallLeases) CancelOutgoing(string, string) (string, error) { return "", leases.err }
func (leases *fakeCallLeases) BeginOutgoing(string, string) error            { return leases.err }
func (leases *fakeCallLeases) RenewOutgoing(string, string, time.Duration) (time.Time, error) {
	return time.Now().Add(30 * time.Second), leases.err
}

func (leases *fakeCallLeases) PrepareOutgoingFor(ctx context.Context, id, line string, owner calllease.Owner) (calllease.OutgoingReservation, error) {
	return leases.ReserveOutgoingFor(ctx, id, line, owner)
}

type outgoingHTTPFixture struct {
	api            *API
	server         *httptest.Server
	communications *outgoingTestCommunication
	core           *callmedia.Core
	leases         *calllease.Manager
	calls          *outgoingTestCalls
}

func outgoingHTTP(t *testing.T) *outgoingHTTPFixture {
	t.Helper()
	endpoint := &httpSocketEndpoint{closed: make(chan struct{}), writes: make(chan []byte, 64)}
	var ownerLeases *calllease.Manager
	core, err := callmedia.New(callmedia.Options{EndpointOpener: endpoint, OnOwnerStateChange: func(id string, connected bool) {
		if ownerLeases == nil {
			return
		}
		if connected {
			ownerLeases.MediaConnected(id)
		} else {
			ownerLeases.MediaDisconnected(id)
		}
	}})
	if err != nil {
		t.Fatal(err)
	}
	calls := &outgoingTestCalls{}
	communications := &outgoingTestCommunication{fakeCommunications: fakeCommunications{call: store.Call{ID: "actual-call", LineID: "line", Phase: "dialing", Direction: "outgoing"}}}
	leases, err := calllease.New(calls, communications, calllease.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := leases.ReconcileAuthoritativeCalls(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	ownerLeases = leases
	api, err := New(&fakeRepository{}, Options{Communications: communications, CallLeases: leases, CallMedia: &httpSocketMedia{core: core}, disableAuthentication: true})
	if err != nil {
		t.Fatal(err)
	}
	communications.onStart = func() {
		api.preparationsMu.Lock()
		var p *outgoingPreparation
		for _, candidate := range api.preparations {
			p = candidate
		}
		api.preparationsMu.Unlock()
		if p == nil || p.session == nil || !p.session.TransportReady() {
			t.Error("dial dispatched without valid media")
		}
		calls.mu.Lock()
		calls.call = communications.call
		calls.mu.Unlock()
		if err := core.ReconcileActiveCalls(context.Background(), []string{communications.call.ID}); err != nil {
			t.Error(err)
		}
	}
	server := httptest.NewServer(api)
	t.Cleanup(func() { server.Close(); core.Close(context.Background()) })
	return &outgoingHTTPFixture{api: api, server: server, communications: communications, core: core, leases: leases, calls: calls}
}

func (f *outgoingHTTPFixture) request(t *testing.T, method, path, body string, status int) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	f.api.ServeHTTP(w, r)
	if w.Code != status {
		t.Fatalf("%s %s: status %d body %s", method, path, w.Code, w.Body.String())
	}
	return w
}

func (f *outgoingHTTPFixture) prepare(t *testing.T, requestID string) string {
	w := f.request(t, "POST", "/api/v1/calls/prepare", `{"request_id":"`+requestID+`","line_id":"line","number":"+81000000001"}`, 201)
	var p struct {
		ID string `json:"preparation_id"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil || p.ID == "" {
		t.Fatal(w.Body.String(), err)
	}
	return p.ID
}

func (f *outgoingHTTPFixture) socket(t *testing.T, id string) *websocket.Conn {
	t.Helper()
	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(f.server.URL, "http")+"/api/v1/calls/preparations/"+id+"/media/ws", http.Header{"Origin": []string{f.server.URL}})
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.WriteJSON(map[string]any{"type": "start", "version": 1, "codec": "opus", "sample_rate": 16000, "channels": 1, "frame_ms": 20, "owner_token": "client-owner"}); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	var ready map[string]any
	if err := conn.ReadJSON(&ready); err != nil {
		t.Fatal(err)
	}
	if ready["type"] != "ready" || ready["hardware_media"] != false {
		t.Fatal("pre-dial falsely promised hardware", ready)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

func validOutgoingOpus(t *testing.T) []byte {
	t.Helper()
	factory, err := callmedia.NewProductionOpusFactory()
	if err != nil {
		t.Fatal(err)
	}
	codec, err := factory.New(callmedia.PCMFormat{Encoding: callmedia.PCMEncodingS16LE, SampleRate: 16000, Channels: 1, FrameDuration: 20 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	defer codec.Close()
	payload, err := codec.Encode(bytes.Repeat([]byte{0, 32}, 320))
	if err != nil {
		t.Fatal(err)
	}
	return payload
}

func TestOutgoingPreparationRequiresRealMediaAndCommitIsIdempotent(t *testing.T) {
	f := outgoingHTTP(t)
	id := f.prepare(t, "request")
	if f.prepare(t, "request") != id {
		t.Fatal("prepare retry changed identity")
	}
	path := "/api/v1/calls/preparations/" + id
	f.request(t, "POST", path+"/commit", `{"audio_running":true}`, 409)
	conn := f.socket(t, id)
	f.request(t, "POST", path+"/commit", `{"audio_running":true}`, 409)
	if err := conn.WriteMessage(websocket.BinaryMessage, callmedia.SocketFrame(0, 0, validOutgoingOpus(t))); err != nil {
		t.Fatal(err)
	}
	var ready map[string]any
	if err := conn.ReadJSON(&ready); err != nil || ready["type"] != "uplink_ready" {
		t.Fatal(ready, err)
	}
	f.request(t, "POST", path+"/commit", `{"audio_running":false}`, 409)
	f.request(t, "PUT", path+"/lease", `{}`, 200)
	first := f.request(t, "POST", path+"/commit", `{"audio_running":true}`, 201)
	replay := f.request(t, "POST", path+"/commit", `{"audio_running":true}`, 201)
	if first.Body.String() != replay.Body.String() || f.communications.startCalls != 1 {
		t.Fatal("commit retry redialed", f.communications.startCalls)
	}
	for {
		kind, data, err := conn.ReadMessage()
		if err != nil {
			t.Fatal(err)
		}
		if kind == websocket.TextMessage {
			var event map[string]any
			_ = json.Unmarshal(data, &event)
			if event["type"] == "bound" {
				if event["call_id"] != "actual-call" {
					t.Fatal(event)
				}
				break
			}
		}
	}
	f.request(t, "DELETE", path, "", 204)
	f.communications.endMu.Lock()
	ended := append([]string(nil), f.communications.ended...)
	f.communications.endMu.Unlock()
	if len(ended) == 0 || ended[len(ended)-1] != "actual-call" {
		t.Fatal("cancel leaked committed call", ended)
	}
}

func TestOutgoingCancelledExpiredAndDisconnectedNeverDial(t *testing.T) {
	for _, scenario := range []string{"cancel", "expire", "disconnect", "revoke", "invalid"} {
		t.Run(scenario, func(t *testing.T) {
			f := outgoingHTTP(t)
			id := f.prepare(t, "request")
			path := "/api/v1/calls/preparations/" + id
			conn := f.socket(t, id)
			if scenario == "invalid" {
				_ = conn.WriteMessage(websocket.BinaryMessage, callmedia.SocketFrame(0, 0, []byte{0xff}))
			} else {
				_ = conn.WriteMessage(websocket.BinaryMessage, callmedia.SocketFrame(0, 0, validOutgoingOpus(t)))
				var ready map[string]any
				if err := conn.ReadJSON(&ready); err != nil {
					t.Fatal(err)
				}
			}
			f.api.preparationsMu.Lock()
			p := f.api.preparations[id]
			f.api.preparationsMu.Unlock()
			switch scenario {
			case "cancel":
				f.request(t, "DELETE", path, "", 204)
			case "expire":
				p.mu.Lock()
				p.expires = time.Now().Add(-time.Second)
				p.mu.Unlock()
				f.api.expirePreparation(p)
			case "disconnect":
				conn.Close()
				select {
				case <-p.session.Done():
				case <-time.After(time.Second):
					t.Fatal("socket disconnect not observed")
				}
			case "revoke":
				if err := f.api.revokeCallHolder(context.Background(), developmentCallLeaseHolderID); err != nil {
					t.Fatal(err)
				}
			case "invalid":
				select {
				case <-p.session.Done():
				case <-time.After(time.Second):
					t.Fatal("invalid Opus not rejected")
				}
			}
			f.request(t, "POST", path+"/commit", `{"audio_running":true}`, 409)
			if f.communications.startCalls != 0 {
				t.Fatal("unready media actually dialed")
			}
		})
	}
}

func (f *outgoingHTTPFixture) readyPreparation(t *testing.T, id string) *websocket.Conn {
	conn := f.socket(t, id)
	if err := conn.WriteMessage(websocket.BinaryMessage, callmedia.SocketFrame(0, 0, validOutgoingOpus(t))); err != nil {
		t.Fatal(err)
	}
	var event map[string]any
	if err := conn.ReadJSON(&event); err != nil || event["type"] != "uplink_ready" {
		t.Fatal(event, err)
	}
	return conn
}

func TestPreparedOwnershipActivationFailureRollsBackOnlyCreatedCall(t *testing.T) {
	f := outgoingHTTP(t)
	id := f.prepare(t, "request")
	f.readyPreparation(t, id)
	original := f.communications.onStart
	f.communications.onStart = func() { original(); f.calls.mu.Lock(); f.calls.call.LineID = "different"; f.calls.mu.Unlock() }
	f.request(t, "POST", "/api/v1/calls/preparations/"+id+"/commit", `{"audio_running":true}`, 400)
	f.communications.endMu.Lock()
	defer f.communications.endMu.Unlock()
	if len(f.communications.ended) != 1 || f.communications.ended[0] != "actual-call" {
		t.Fatal("ownership rollback ended wrong call", f.communications.ended)
	}
}

type blockingOutgoingRecording struct {
	fakeRecordingService
	entered chan struct{}
	release chan struct{}
}

func (r *blockingOutgoingRecording) PrepareOutgoing(ctx context.Context, id string, enabled bool) (string, error) {
	close(r.entered)
	<-r.release
	return r.fakeRecordingService.PrepareOutgoing(ctx, id, enabled)
}

func TestMediaFailureWhileCommitWaitsNeverDispatches(t *testing.T) {
	f := outgoingHTTP(t)
	recording := &blockingOutgoingRecording{fakeRecordingService: fakeRecordingService{settings: store.RecordingSettings{DefaultEnabled: true}}, entered: make(chan struct{}), release: make(chan struct{})}
	f.api.recordings = recording
	id := f.prepare(t, "request")
	conn := f.readyPreparation(t, id)
	response := httptest.NewRecorder()
	request := httptest.NewRequest("POST", "/api/v1/calls/preparations/"+id+"/commit", strings.NewReader(`{"audio_running":true}`))
	request.Header.Set("Content-Type", "application/json")
	completed := make(chan struct{})
	go func() { f.api.ServeHTTP(response, request); close(completed) }()
	select {
	case <-recording.entered:
	case <-time.After(time.Second):
		t.Fatal("commit never entered recording preparation")
	}
	f.api.preparationsMu.Lock()
	p := f.api.preparations[id]
	f.api.preparationsMu.Unlock()
	conn.Close()
	select {
	case <-p.session.Context().Done():
	case <-time.After(time.Second):
		t.Fatal("socket failure did not cancel immediately")
	}
	close(recording.release)
	select {
	case <-completed:
	case <-time.After(time.Second):
		t.Fatal("commit did not stop after media failure")
	}
	if response.Code != 503 || f.communications.startCalls != 0 {
		t.Fatal("media failure while waiting still dialed", response.Code, f.communications.startCalls, response.Body.String())
	}
}
