package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/human-agent65535/modemdeck/internal/auth"
	"github.com/human-agent65535/modemdeck/internal/callmedia"
	"github.com/human-agent65535/modemdeck/internal/calltest"
)

func TestBrowserCallTestAuthenticationIsolationAndAudio(t *testing.T) {
	// No push, TURN, modem or recording service is configured. A regular member
	// with no SIM access can test their own browser audio.
	service := calltest.New(nil, nil, nil)
	defer service.Close()
	authenticator, token, csrf := newAPIUserAuthenticator(t)
	api, err := New(&fakeRepository{}, Options{Authenticator: authenticator, CallTests: service})
	if err != nil {
		t.Fatal(err)
	}
	send := func(method, path, cookie, csrfToken, body string, expected int) *httptest.ResponseRecorder {
		t.Helper()
		response := httptest.NewRecorder()
		request := authorizedAPIRequest(method, "/api/v1/call-tests"+path, bytes.NewReader([]byte(body)), cookie, csrfToken)
		request.Header.Set("Content-Type", "application/json")
		api.ServeHTTP(response, request)
		if response.Code != expected {
			t.Fatalf("%s %s: %d %s", method, path, response.Code, response.Body.String())
		}
		return response
	}
	send(http.MethodPost, "", "", "", `{}`, http.StatusUnauthorized)
	send(http.MethodPost, "", token, "", `{}`, http.StatusForbidden)
	send(http.MethodGet, "", token, csrf, "", http.StatusMethodNotAllowed)
	send(http.MethodPost, "", token, csrf, `{"owner":"someone-else"}`, http.StatusBadRequest)
	response := send(http.MethodPost, "", token, csrf, `{"language":"zh-CN"}`, http.StatusAccepted)
	var started calltest.Status
	if err := json.Unmarshal(response.Body.Bytes(), &started); err != nil || started.Phase != "active" {
		t.Fatalf("start: %+v %v", started, err)
	}
	path := "/" + started.ID
	send(http.MethodPost, "", token, csrf, `{}`, http.StatusTooManyRequests)
	response = send(http.MethodGet, path+"/status", token, "", "", http.StatusOK)
	var status struct {
		calltest.Status
		Audio calltest.AudioStatus `json:"test_audio"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &status); err != nil || status.Audio.CapturedPeakDBFS != -96 ||
		status.Audio.Phase != "connecting" || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("status: %+v %v", status, err)
	}
	// A second authenticated session cannot inspect or control this test.
	otherAuth, otherToken, otherCSRF := newAPIAuthenticator(t)
	otherAPI, err := New(&fakeRepository{}, Options{Authenticator: otherAuth, CallTests: service})
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range []struct{ method, path string }{
		{http.MethodGet, "/status"}, {http.MethodGet, "/active"},
		{http.MethodPost, "/hangup"}, {http.MethodPut, "/lease"},
		{http.MethodDelete, "/media"}, {http.MethodGet, "/media/ws"},
	} {
		recorder := httptest.NewRecorder()
		body := `{}`
		if action.path == "/media" {
			body = `{"owner_token":"foreign"}`
		}
		request := authorizedAPIRequest(action.method, "/api/v1/call-tests"+path+action.path,
			bytes.NewReader([]byte(body)), otherToken, otherCSRF)
		request.Header.Set("Content-Type", "application/json")
		otherAPI.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusNotFound {
			t.Fatalf("foreign %s = %d: %s", action.path, recorder.Code, recorder.Body.String())
		}
	}

	server := httptest.NewServer(api)
	defer server.Close()
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/api/v1/call-tests" + path + "/media/ws"
	header := http.Header{"Origin": {server.URL}, "Cookie": {sessionCookieName + "=" + token}}
	foreignOrigin := header.Clone()
	foreignOrigin.Set("Origin", "https://other.example")
	if connection, response, err := websocket.DefaultDialer.Dial(wsURL, foreignOrigin); err == nil || response.StatusCode != http.StatusForbidden {
		if connection != nil {
			connection.Close()
		}
		t.Fatalf("foreign origin: %v %v", response, err)
	} else {
		response.Body.Close()
	}
	connection, _, err := websocket.DefaultDialer.Dial(wsURL, header)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if err := connection.WriteJSON(socketStart{Type: "start", Version: 1, Codec: "opus", SampleRate: 16000,
		Channels: 1, FrameMS: 20, OwnerToken: "browser-owner"}); err != nil {
		t.Fatal(err)
	}
	connection.SetReadDeadline(time.Now().Add(3 * time.Second))
	var ready map[string]any
	if err := connection.ReadJSON(&ready); err != nil || ready["type"] != "ready" {
		t.Fatalf("ready: %v %v", ready, err)
	}
	factory, err := callmedia.NewProductionOpusFactory()
	if err != nil {
		t.Fatal(err)
	}
	codec, err := factory.New(callmedia.PCMFormat{Encoding: callmedia.PCMEncodingS16LE, SampleRate: 16000,
		Channels: 1, FrameDuration: 20 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	defer codec.Close()
	opus, err := codec.Encode(make([]byte, 640))
	if err != nil {
		t.Fatal(err)
	}
	if err := connection.WriteMessage(websocket.BinaryMessage, callmedia.SocketFrame(0, 0, opus)); err != nil {
		t.Fatal(err)
	}
	kind, data, err := connection.ReadMessage()
	if err != nil || kind != websocket.BinaryMessage {
		t.Fatalf("downlink: %d %v", kind, err)
	}
	_, _, payload, err := callmedia.ParseSocketFrame(data)
	if err != nil {
		t.Fatal(err)
	}
	if decoded, err := codec.Decode(payload); err != nil || len(decoded.PCM) != 640 {
		t.Fatalf("audio decode: %v", err)
	}
	owner := "web\x00" + callLeaseHolderForSessionToken(auth.SessionToken(token))
	audio, err := service.AudioStatus(started.ID, owner)
	if err != nil || audio.ReceivedPackets < 1 || audio.SentPackets < 1 {
		t.Fatalf("duplex counters: %+v %v", audio, err)
	}
	send(http.MethodPost, path+"/hangup", token, csrf, `{}`, http.StatusNoContent)
	response = send(http.MethodGet, path+"/status", token, "", "", http.StatusOK)
	if err := json.Unmarshal(response.Body.Bytes(), &status); err != nil || status.Phase != "ended" {
		t.Fatalf("end: %+v %v", status, err)
	}
	send(http.MethodGet, path+"/media/ws", token, "", "", http.StatusConflict)
}
