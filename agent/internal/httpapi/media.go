package httpapi

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/human-agent65535/modemdeck/agent/internal/domain"
	"github.com/human-agent65535/modemdeck/agent/internal/media"
)

type MediaHandlerOptions struct {
	HandshakeTimeout time.Duration
}

func NewMediaHandler(manager *media.Manager, options MediaHandlerOptions) http.Handler {
	if options.HandshakeTimeout <= 0 {
		options.HandshakeTimeout = 2 * time.Second
	}
	handler := &mediaHandler{
		manager: manager,
		options: options,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/media/calls/{id}", handler.stream)
	return mux
}

type mediaHandler struct {
	manager *media.Manager
	options MediaHandlerOptions
}

func decorateMediaSnapshot(snapshot *domain.Snapshot, manager *media.Manager) {
	if snapshot == nil {
		return
	}
	for index := range snapshot.Calls {
		call := &snapshot.Calls[index]
		call.MediaConfigured = manager != nil && manager.IsConfigured(call.AudioPort)
		call.MediaActive = manager != nil && manager.IsActive(call.ID)
	}
}

func (h *mediaHandler) stream(w http.ResponseWriter, r *http.Request) {
	if h.manager == nil {
		writeMediaError(w, http.StatusServiceUnavailable, domain.ErrorUnavailable, "media handler is unavailable")
		return
	}
	if !headerHasToken(r.Header.Get("Connection"), "upgrade") ||
		!strings.EqualFold(strings.TrimSpace(r.Header.Get("Upgrade")), media.UpgradeProtocol) {
		writeMediaError(
			w,
			http.StatusUpgradeRequired,
			domain.ErrorInvalidArgument,
			"media protocol upgrade is required",
		)
		return
	}

	callID := r.PathValue("id")
	if callID == "" {
		writeMediaError(w, http.StatusBadRequest, domain.ErrorInvalidArgument, "call id is required")
		return
	}

	session, err := h.manager.Open(r.Context(), callID)
	if err != nil {
		slog.Warn(
			"host call media open failed",
			"component", "media",
			"call_id", callID,
			"error", err,
		)
		status, code, message := mapMediaError(err)
		writeMediaError(w, status, code, message)
		return
	}

	hijacker, ok := w.(http.Hijacker)
	if !ok {
		_ = session.Close()
		writeMediaError(
			w,
			http.StatusInternalServerError,
			domain.ErrorInternal,
			"HTTP transport does not support media upgrade",
		)
		return
	}
	connection, buffered, err := hijacker.Hijack()
	if err != nil {
		_ = session.Close()
		return
	}

	if err := writeMediaHandshake(connection, buffered, session.Format(), h.options.HandshakeTimeout); err != nil {
		slog.Warn(
			"host call media handshake failed",
			"component", "media",
			"call_id", callID,
			"error", err,
		)
		_ = connection.Close()
		_ = session.Close()
		return
	}
	slog.Info(
		"host call media stream opened",
		"component", "media",
		"call_id", callID,
	)
	if err := session.Serve(r.Context(), &bufferedMediaConnection{
		Conn:   connection,
		reader: buffered.Reader,
	}); err != nil {
		slog.Warn(
			"host call media stream ended with error",
			"component", "media",
			"call_id", callID,
			"error", err,
		)
		return
	}
	slog.Info(
		"host call media stream closed",
		"component", "media",
		"call_id", callID,
	)
}

type bufferedMediaConnection struct {
	net.Conn
	reader *bufio.Reader
}

func (c *bufferedMediaConnection) Read(payload []byte) (int, error) {
	return c.reader.Read(payload)
}

func writeMediaHandshake(
	connection net.Conn,
	buffered *bufio.ReadWriter,
	format media.Format,
	timeout time.Duration,
) error {
	if err := connection.SetWriteDeadline(time.Now().Add(timeout)); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(
		buffered,
		"HTTP/1.1 101 Switching Protocols\r\n"+
			"Connection: Upgrade\r\n"+
			"Upgrade: %s\r\n"+
			"ModemDeck-Media-Version: %d\r\n"+
			"ModemDeck-Media-Encoding: %s\r\n"+
			"ModemDeck-Media-Resolution: %s\r\n"+
			"ModemDeck-Media-Rate: %d\r\n"+
			"ModemDeck-Media-Channels: %d\r\n"+
			"ModemDeck-Media-Frame-Duration-Ms: %d\r\n"+
			"ModemDeck-Media-Frame-Bytes: %d\r\n\r\n",
		media.UpgradeProtocol,
		media.ProtocolVersion,
		format.Encoding,
		format.Resolution,
		format.Rate,
		format.Channels,
		format.FrameDuration/time.Millisecond,
		format.FrameBytes,
	); err != nil {
		return err
	}
	if err := buffered.Flush(); err != nil {
		return err
	}
	return connection.SetWriteDeadline(time.Time{})
}

func headerHasToken(value, token string) bool {
	for _, candidate := range strings.Split(value, ",") {
		if strings.EqualFold(strings.TrimSpace(candidate), token) {
			return true
		}
	}
	return false
}

func mapMediaError(err error) (int, domain.ErrorCode, string) {
	mediaError, ok := media.AsError(err)
	if !ok {
		return http.StatusInternalServerError, domain.ErrorInternal, "internal media error"
	}

	switch mediaError.Code {
	case media.ErrorInvalidArgument:
		return http.StatusBadRequest, domain.ErrorInvalidArgument, mediaError.Message
	case media.ErrorNotFound:
		return http.StatusNotFound, domain.ErrorNotFound, mediaError.Message
	case media.ErrorConflict, media.ErrorNotActive:
		return http.StatusConflict, domain.ErrorConflict, mediaError.Message
	case media.ErrorUnsupportedFormat:
		return http.StatusNotImplemented, domain.ErrorNotSupported, mediaError.Message
	case media.ErrorMediaUnavailable, media.ErrorUnboundAudioPort, media.ErrorBackendUnavailable:
		return http.StatusServiceUnavailable, domain.ErrorUnavailable, mediaError.Message
	default:
		return http.StatusInternalServerError, domain.ErrorInternal, "internal media error"
	}
}

func writeMediaError(w http.ResponseWriter, status int, code domain.ErrorCode, message string) {
	w.Header().Set("Content-Type", "application/json")
	if status == http.StatusUpgradeRequired {
		w.Header().Set("Upgrade", media.UpgradeProtocol)
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(errorBody{Error: apiError{
		Code:      code,
		Operation: "open_media",
		Message:   message,
	}})
}
