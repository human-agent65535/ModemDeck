package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/human-agent65535/modemdeck/internal/auth"
	"github.com/human-agent65535/modemdeck/internal/calllease"
	"github.com/human-agent65535/modemdeck/internal/callmedia"
	"github.com/human-agent65535/modemdeck/internal/calltest"
	"github.com/human-agent65535/modemdeck/internal/mobilepairing"
)

type httpSocketMedia struct {
	fakeCallMedia
	core *callmedia.Core
}

func (s *httpSocketMedia) OpenSocket(ctx context.Context, id, token string, transport callmedia.SocketTransport) (*callmedia.Session, error) {
	return s.core.OpenSocket(ctx, callmedia.ActiveCall{ID: id, State: callmedia.CallStateActive}, token, transport)
}

type httpSocketEndpoint struct {
	closed chan struct{}
	once   sync.Once
	writes chan []byte
}

func (e *httpSocketEndpoint) Format() callmedia.PCMFormat {
	return callmedia.PCMFormat{Encoding: callmedia.PCMEncodingS16LE, SampleRate: 16000, Channels: 1, FrameDuration: 20 * time.Millisecond}
}
func (e *httpSocketEndpoint) Open(context.Context, callmedia.ActiveCall) (callmedia.MediaEndpoint, error) {
	return e, nil
}
func (e *httpSocketEndpoint) Start(context.Context) error { return nil }
func (e *httpSocketEndpoint) ReadPCM(ctx context.Context, dst []byte) error {
	select {
	case <-time.After(20 * time.Millisecond):
		clear(dst)
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-e.closed:
		return callmedia.ErrTransportClosed
	}
}
func (e *httpSocketEndpoint) WritePCM(ctx context.Context, src []byte) error {
	select {
	case e.writes <- append([]byte(nil), src...):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-e.closed:
		return callmedia.ErrTransportClosed
	}
}
func (e *httpSocketEndpoint) Close() error { e.once.Do(func() { close(e.closed) }); return nil }

func TestMediaSocketHTTPUpgradeAuthenticationOriginAndOpusRoundTrip(t *testing.T) {
	endpoint := &httpSocketEndpoint{closed: make(chan struct{}), writes: make(chan []byte, 64)}
	core, err := callmedia.New(callmedia.Options{EndpointOpener: endpoint})
	if err != nil {
		t.Fatal(err)
	}
	defer core.Close(context.Background())
	if err := core.ReconcileActiveCalls(context.Background(), []string{"call-1"}); err != nil {
		t.Fatal(err)
	}
	authentication, sessionToken, _ := newAPIAuthenticator(t)
	api, err := New(&fakeRepository{}, Options{Authenticator: authentication, CallMedia: &httpSocketMedia{core: core}, CallLeases: &fakeCallLeases{}})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(api)
	defer server.Close()
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/api/v1/calls/call-1/media/ws"
	header := http.Header{"Origin": []string{server.URL}, "Cookie": []string{sessionCookieName + "=" + sessionToken}}
	for _, scenario := range []struct {
		name    string
		headers http.Header
		status  int
	}{
		{"unauthenticated", http.Header{"Origin": []string{server.URL}}, http.StatusUnauthorized},
		{"cross-origin", http.Header{"Origin": []string{"https://evil.example"}, "Cookie": header.Values("Cookie")}, http.StatusForbidden},
		{"missing-origin", http.Header{"Cookie": header.Values("Cookie")}, http.StatusForbidden},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			connection, response, err := websocket.DefaultDialer.Dial(wsURL, scenario.headers)
			if connection != nil {
				connection.Close()
				t.Fatal("unauthorized upgrade succeeded")
			}
			if err == nil || response == nil || response.StatusCode != scenario.status {
				t.Fatalf("status=%v error=%v", response, err)
			}
			response.Body.Close()
		})
	}
	connection, response, err := websocket.DefaultDialer.Dial(wsURL, header)
	if err != nil {
		t.Fatalf("upgrade: %v (%v)", err, response)
	}
	defer connection.Close()
	if err := connection.WriteJSON(socketStart{Type: "start", Version: 1, Codec: "opus", SampleRate: 16000, Channels: 1, FrameMS: 20, OwnerToken: "owner"}); err != nil {
		t.Fatal(err)
	}
	var ready map[string]any
	if err := connection.ReadJSON(&ready); err != nil || ready["type"] != "ready" {
		t.Fatalf("ready=%v err=%v", ready, err)
	}
	factory, err := callmedia.NewProductionOpusFactory()
	if err != nil {
		t.Fatal(err)
	}
	codec, err := factory.New(endpoint.Format())
	if err != nil {
		t.Fatal(err)
	}
	defer codec.Close()
	pcm := make([]byte, 640)
	for i := range pcm {
		pcm[i] = 0x11
	}
	opus, err := codec.Encode(pcm)
	if err != nil {
		t.Fatal(err)
	}
	if err := connection.WriteMessage(websocket.BinaryMessage, callmedia.SocketFrame(0, 0, opus)); err != nil {
		t.Fatal(err)
	}
	connection.SetReadDeadline(time.Now().Add(2 * time.Second))
	kind, data, err := connection.ReadMessage()
	if err != nil || kind != websocket.BinaryMessage {
		t.Fatalf("downlink: %d %v", kind, err)
	}
	sequence, timestamp, payload, err := callmedia.ParseSocketFrame(data)
	if err != nil || sequence != 0 || timestamp != 0 {
		t.Fatalf("binary header: %d %d %v", sequence, timestamp, err)
	}
	decoded, err := codec.Decode(payload)
	if err != nil || len(decoded.PCM) != 640 {
		t.Fatalf("real downlink Opus: %v", err)
	}
	// Duplicate audio closes only the socket owner and carries a final error.
	if err := connection.WriteMessage(websocket.BinaryMessage, callmedia.SocketFrame(0, 0, opus)); err != nil {
		t.Fatal(err)
	}
	foundError := false
	for i := 0; i < 10; i++ {
		kind, data, err = connection.ReadMessage()
		if err != nil {
			break
		}
		if kind == websocket.TextMessage {
			var value map[string]any
			if json.Unmarshal(data, &value) == nil && value["type"] == "error" {
				foundError = true
			}
		}
	}
	if !foundError {
		t.Fatal("malformed sequence did not report a transport error")
	}
	stats := core.Statistics("call-1")
	if stats.ReceivedPackets != 1 || stats.FailureCode != "invalid_audio" {
		t.Fatalf("final socket stats: %+v", stats)
	}
}

func TestMediaSocketLeaseAndMobileHeaderAuthentication(t *testing.T) {
	token, _, err := mobilepairing.NewToken()
	if err != nil {
		t.Fatal(err)
	}
	endpoint := &httpSocketEndpoint{closed: make(chan struct{}), writes: make(chan []byte, 64)}
	core, err := callmedia.New(callmedia.Options{EndpointOpener: endpoint})
	if err != nil {
		t.Fatal(err)
	}
	defer core.Close(context.Background())
	repository := &fakeRepository{mobileFound: true, mobilePrincipal: auth.Principal{UserID: "member", Role: auth.RoleMember, IOSPairingEnabled: true, AllowedLineIDs: []string{"line"}}, callLineID: "line"}
	api, err := New(repository, Options{disableAuthentication: true, CallMedia: &httpSocketMedia{core: core}, CallLeases: &fakeCallLeases{err: calllease.ErrNotOwner}})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(api)
	defer server.Close()
	header := http.Header{"Authorization": []string{"Bearer " + string(token)}}
	connection, response, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/api/v1/calls/call-1/media/ws", header)
	if connection != nil {
		connection.Close()
		t.Fatal("non-owner mobile upgraded")
	}
	if err == nil || response == nil || response.StatusCode != http.StatusConflict {
		t.Fatalf("lease response: %v %v", response, err)
	}
	response.Body.Close()
}

func TestMediaSocketOriginAllowsNativeOnlyWithoutOrigin(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "https://deck.example/api/v1/calls/call/media/ws", nil)
	if mediaSocketOriginAllowed(request) {
		t.Fatal("browser missing Origin accepted")
	}
	request = request.WithContext(context.WithValue(request.Context(), mobileAuthenticationContextKey{}, mobileAuthentication{}))
	if !mediaSocketOriginAllowed(request) {
		t.Fatal("native authenticated upgrade rejected")
	}
	request.Header.Set("Origin", "https://deck.example/")
	if mediaSocketOriginAllowed(request) {
		t.Fatal("non-origin URL accepted")
	}
}

func TestMobileTestSocketUsesSharedRuntimeWithoutTURN(t *testing.T) {
	push := make(localAudioTestPush, 1)
	service := calltest.New(push, nil, nil)
	defer service.Close()
	started, err := service.Start("user", "phone")
	if err != nil {
		t.Fatalf("WSS test incorrectly requires TURN: %v", err)
	}
	select {
	case <-push:
	case <-time.After(8 * time.Second):
		t.Fatal("local test did not ring")
	}
	owner := "user\x00phone"
	if _, err := service.Answer(started.ID, owner); err != nil {
		t.Fatal(err)
	}
	repository := &fakeRepository{mobileFound: true, mobilePrincipal: auth.Principal{UserID: "user", Role: auth.RoleMember, IOSPairingEnabled: true}, iosCurrentCredentialID: "phone"}
	api, err := New(repository, Options{CallTests: service, disableAuthentication: true})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(api)
	defer server.Close()
	token, _, err := mobilepairing.NewToken()
	if err != nil {
		t.Fatal(err)
	}
	socket, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/api/v1/mobile/call-tests/"+started.ID+"/media/ws", http.Header{"Authorization": []string{"Bearer " + string(token)}})
	if err != nil {
		t.Fatal(err)
	}
	defer socket.Close()
	if err := socket.WriteJSON(socketStart{Type: "start", Version: 1, Codec: "opus", SampleRate: 16000, Channels: 1, FrameMS: 20, OwnerToken: "native-owner"}); err != nil {
		t.Fatal(err)
	}
	var ready map[string]any
	if err := socket.ReadJSON(&ready); err != nil || ready["type"] != "ready" {
		t.Fatalf("native ready: %v %v", ready, err)
	}
	quality, err := service.AudioStatus(started.ID, owner)
	if err != nil {
		t.Fatal(err)
	}
	if quality.Phase != "connecting" {
		t.Fatal("test guide advanced before first activated microphone packet")
	}
	factory, err := callmedia.NewProductionOpusFactory()
	if err != nil {
		t.Fatal(err)
	}
	codec, err := factory.New(callmedia.PCMFormat{Encoding: callmedia.PCMEncodingS16LE, SampleRate: 16000, Channels: 1, FrameDuration: 20 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	defer codec.Close()
	opus, err := codec.Encode(make([]byte, 640))
	if err != nil {
		t.Fatal(err)
	}
	if err := socket.WriteMessage(websocket.BinaryMessage, callmedia.SocketFrame(0, 0, opus)); err != nil {
		t.Fatal(err)
	}
	socket.SetReadDeadline(time.Now().Add(2 * time.Second))
	kind, data, err := socket.ReadMessage()
	if err != nil || kind != websocket.BinaryMessage {
		t.Fatalf("test downlink: %d %v", kind, err)
	}
	seq, ts, payload, err := callmedia.ParseSocketFrame(data)
	if err != nil || seq != 0 || ts != 0 {
		t.Fatalf("test binary: %d %d %v", seq, ts, err)
	}
	if _, err := codec.Decode(payload); err != nil {
		t.Fatal(err)
	}
	quality, err = service.AudioStatus(started.ID, owner)
	if err != nil {
		t.Fatal(err)
	}
	if quality.Transport != "websocket" || quality.ReceivedPackets != 1 || quality.SentPackets == 0 {
		t.Fatalf("shared WSS stats: %+v", quality)
	}
}

type revocableSocketLease struct {
	fakeCallLeases
	revoked atomic.Bool
}

func (l *revocableSocketLease) Require(context.Context, string, string) error {
	if l.revoked.Load() {
		return calllease.ErrNotOwner
	}
	return nil
}

type heldSocketMedia struct {
	httpSocketMedia
	attached chan struct{}
	release  chan struct{}
}

func (s *heldSocketMedia) OpenSocket(ctx context.Context, id, token string, transport callmedia.SocketTransport) (*callmedia.Session, error) {
	session, err := s.httpSocketMedia.OpenSocket(ctx, id, token, transport)
	close(s.attached)
	select {
	case <-s.release:
	case <-ctx.Done():
	}
	return session, err
}
func TestMediaSocketRevalidatesRevocationDuringHandshakeAndAttach(t *testing.T) {
	for _, duringAttach := range []bool{false, true} {
		t.Run(fmt.Sprint("during-attach=", duringAttach), func(t *testing.T) {
			endpoint := &httpSocketEndpoint{closed: make(chan struct{}), writes: make(chan []byte, 64)}
			core, err := callmedia.New(callmedia.Options{EndpointOpener: endpoint})
			if err != nil {
				t.Fatal(err)
			}
			defer core.Close(context.Background())
			if err := core.ReconcileActiveCalls(context.Background(), []string{"call-1"}); err != nil {
				t.Fatal(err)
			}
			leases := &revocableSocketLease{}
			media := &heldSocketMedia{httpSocketMedia: httpSocketMedia{core: core}, attached: make(chan struct{}), release: make(chan struct{})}
			if !duringAttach {
				close(media.release)
			}
			api, err := New(&fakeRepository{}, Options{CallMedia: media, CallLeases: leases, disableAuthentication: true})
			if err != nil {
				t.Fatal(err)
			}
			server := httptest.NewServer(api)
			defer server.Close()
			socket, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/api/v1/calls/call-1/media/ws", http.Header{"Origin": []string{server.URL}})
			if err != nil {
				t.Fatal(err)
			}
			defer socket.Close()
			if !duringAttach {
				leases.revoked.Store(true)
				if err := core.CloseCall(context.Background(), "call-1"); err != nil {
					t.Fatal(err)
				}
			}
			if err := socket.WriteJSON(socketStart{Type: "start", Version: 1, Codec: "opus", SampleRate: 16000, Channels: 1, FrameMS: 20, OwnerToken: "revoked-owner"}); err != nil {
				t.Fatal(err)
			}
			if duringAttach {
				select {
				case <-media.attached:
				case <-time.After(2 * time.Second):
					t.Fatal("media attach never completed")
				}
				leases.revoked.Store(true)
				close(media.release)
			}
			socket.SetReadDeadline(time.Now().Add(2 * time.Second))
			var message map[string]any
			if err := socket.ReadJSON(&message); err != nil {
				t.Fatal(err)
			}
			if message["type"] != "error" {
				t.Fatalf("revoked media reached ready: %+v", message)
			}
			if !duringAttach {
				select {
				case <-media.attached:
					t.Fatal("revoked handshake opened media")
				default:
				}
			}
			if duringAttach {
				deadline := time.Now().Add(2 * time.Second)
				for time.Now().Before(deadline) && core.Statistics("call-1").State != "disconnected" {
					time.Sleep(10 * time.Millisecond)
				}
				if core.Statistics("call-1").State != "disconnected" {
					t.Fatal("revoked attached socket was not closed")
				}
			}
		})
	}
}
