package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/human-agent65535/modemdeck/internal/auth"
	"github.com/human-agent65535/modemdeck/internal/callmedia"
	"github.com/human-agent65535/modemdeck/internal/mediaapp"
)

type socketMediaService interface {
	OpenSocket(context.Context, string, string, callmedia.SocketTransport) (*callmedia.Session, error)
}

type socketStart struct {
	Type       string `json:"type"`
	Version    int    `json:"version"`
	Codec      string `json:"codec"`
	SampleRate int    `json:"sample_rate"`
	Channels   int    `json:"channels"`
	FrameMS    int    `json:"frame_ms"`
	OwnerToken string `json:"owner_token"`
}

func mediaSocketOriginAllowed(request *http.Request) bool {
	values := request.Header.Values("Origin")
	if len(values) == 0 {
		return isMobileRequest(request)
	}
	if len(values) != 1 {
		return false
	}
	origin, err := url.Parse(values[0])
	return err == nil && (origin.Scheme == "https" || origin.Scheme == "http") && origin.User == nil && origin.Path == "" && origin.RawQuery == "" && origin.Fragment == "" && strings.EqualFold(origin.Host, request.Host)
}

func (api *API) callMediaSocket(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Only GET is supported", "")
		return
	}
	if !api.requireCallAccess(w, r, id) {
		return
	}
	service, ok := api.callMedia.(socketMediaService)
	if !ok || api.callLeases == nil {
		api.writeCallMediaError(w, r, mediaapp.ErrUnavailable)
		return
	}
	holder, err := api.callLeaseHolder(r.Context())
	if err != nil {
		api.writeCallLeaseError(w, r, "validate call owner", err)
		return
	}
	if err = api.callLeases.Require(r.Context(), id, holder); err != nil {
		api.writeCallLeaseError(w, r, "authorize socket media", err)
		return
	}
	api.serveMediaSocket(w, r, func(token string, transport callmedia.SocketTransport) (*callmedia.Session, error) {
		return service.OpenSocket(r.Context(), id, token, transport)
	}, func() error { return api.callLeases.Require(r.Context(), id, holder) })
}

func (api *API) serveMediaSocket(w http.ResponseWriter, r *http.Request, open func(string, callmedia.SocketTransport) (*callmedia.Session, error), requireLease func() error) {
	if !mediaSocketOriginAllowed(r) {
		writeError(w, http.StatusForbidden, "invalid_origin", "Media socket origin is not allowed", "")
		return
	}
	if r.URL.RawQuery != "" {
		writeError(w, http.StatusBadRequest, "invalid_media_request", "Media socket query parameters are not supported", "")
		return
	}
	upgrade := websocket.Upgrader{HandshakeTimeout: 5 * time.Second, ReadBufferSize: 2048, WriteBufferSize: 2048, CheckOrigin: mediaSocketOriginAllowed, EnableCompression: false}
	conn, err := upgrade.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	transport := &mediaSocketTransport{conn: conn, ready: make(chan struct{}), done: make(chan struct{})}
	defer transport.Close()
	conn.SetReadLimit(callmedia.SocketHeaderBytes + 1275)
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	kind, data, err := conn.ReadMessage()
	var start socketStart
	if err != nil || kind != websocket.TextMessage || json.Unmarshal(data, &start) != nil || start.Type != "start" || start.Version != 1 || start.Codec != "opus" || start.SampleRate != 16000 || start.Channels != 1 || start.FrameMS != 20 || strings.TrimSpace(start.OwnerToken) == "" {
		transport.Finish(errors.New("unsupported media start"), callmedia.AudioStatistics{InputDBFS: -96, InputPeakDBFS: -96, OutputDBFS: -96})
		return
	}
	validate := func() error {
		if err := api.revalidateSocketAuthentication(r); err != nil {
			return err
		}
		return requireLease()
	}
	if err := validate(); err != nil {
		transport.Finish(err, callmedia.AudioStatistics{InputDBFS: -96, InputPeakDBFS: -96, OutputDBFS: -96})
		return
	}
	session, err := open(start.OwnerToken, transport)
	if err != nil {
		transport.Finish(err, callmedia.AudioStatistics{InputDBFS: -96, InputPeakDBFS: -96, OutputDBFS: -96})
		return
	}
	defer session.Close(context.Background())
	// Endpoint acquisition can wait on the Agent. Revalidate after it completes,
	// before ready makes this session usable, so revocation cannot race attach.
	if err := validate(); err != nil {
		transport.Finish(err, session.Statistics())
		return
	}
	if err := transport.json(map[string]any{"type": "ready", "version": 1, "codec": "opus", "sample_rate": 16000, "channels": 1, "frame_ms": 20}); err != nil {
		return
	}
	_ = conn.SetReadDeadline(time.Now().Add(30 * time.Second))
	conn.SetPongHandler(func(string) error { return conn.SetReadDeadline(time.Now().Add(30 * time.Second)) })
	close(transport.ready)
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-session.Done():
			return
		case <-r.Context().Done():
			return
		case <-ticker.C:
			if err := conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(time.Second)); err != nil {
				return
			}
			if err := transport.json(map[string]any{"type": "stats", "final": false, "audio": session.Statistics()}); err != nil {
				return
			}
		}
	}
}

type mediaSocketTransport struct {
	conn      *websocket.Conn
	ready     chan struct{}
	done      chan struct{}
	closeOnce sync.Once
	writeMu   sync.Mutex
}

func (t *mediaSocketTransport) Read(ctx context.Context) ([]byte, error) {
	select {
	case <-t.ready:
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-t.done:
		return nil, callmedia.ErrTransportClosed
	}
	kind, data, err := t.conn.ReadMessage()
	if err != nil {
		if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
			return nil, callmedia.ErrCanceled
		}
		return nil, err
	}
	if kind != websocket.BinaryMessage {
		return nil, callmedia.ErrInvalidRTP
	}
	_ = t.conn.SetReadDeadline(time.Now().Add(30 * time.Second))
	return data, nil
}
func (t *mediaSocketTransport) Write(ctx context.Context, data []byte) error {
	select {
	case <-t.ready:
	case <-ctx.Done():
		return ctx.Err()
	case <-t.done:
		return callmedia.ErrTransportClosed
	}
	t.writeMu.Lock()
	defer t.writeMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	_ = t.conn.SetWriteDeadline(time.Now().Add(200 * time.Millisecond))
	return t.conn.WriteMessage(websocket.BinaryMessage, data)
}
func (t *mediaSocketTransport) json(value any) error {
	t.writeMu.Lock()
	defer t.writeMu.Unlock()
	_ = t.conn.SetWriteDeadline(time.Now().Add(time.Second))
	return t.conn.WriteJSON(value)
}
func (t *mediaSocketTransport) Finish(reason error, stats callmedia.AudioStatistics) {
	if reason != nil {
		_ = t.json(map[string]any{"type": "error", "code": callmedia.AudioFailureCode(reason), "message": "Call media disconnected"})
	}
	_ = t.json(map[string]any{"type": "stats", "final": true, "audio": stats})
	code := websocket.CloseNormalClosure
	if reason != nil {
		code = websocket.CloseInternalServerErr
	}
	_ = t.conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(code, ""), time.Now().Add(time.Second))
}
func (t *mediaSocketTransport) InterruptRead() { _ = t.conn.SetReadDeadline(time.Now()) }

func (t *mediaSocketTransport) Close() error {
	var err error
	t.closeOnce.Do(func() { close(t.done); err = t.conn.Close() })
	return err
}

func callMediaSocketResourceID(path string) (string, bool) {
	const suffix = "/media/ws"
	if !strings.HasSuffix(path, suffix) {
		return "", false
	}
	return callMediaResourceID(strings.TrimSuffix(path, "/ws"))
}

func (api *API) revalidateSocketAuthentication(request *http.Request) error {
	if isMobileRequest(request) {
		_, err := api.currentMobilePrincipal(request.Context())
		return err
	}
	if api.authenticator == nil {
		return nil
	}
	cookie, err := request.Cookie(sessionCookieName)
	if err != nil || cookie.Value == "" {
		return auth.ErrUnauthenticated
	}
	_, err = api.authenticator.Authenticate(auth.ContextWithSessionClient(request.Context(), requestSessionClient(request)), auth.SessionToken(cookie.Value))
	return err
}
