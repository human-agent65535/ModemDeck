package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
	"unicode"

	"github.com/human-agent65535/modemdeck/agent/internal/domain"
)

const maxRequestBodyBytes = 256 << 10

type handler struct {
	provider     domain.Provider
	agentVersion string
}

type healthResponse struct {
	Status       string                `json:"status"`
	APIVersion   string                `json:"api_version"`
	AgentVersion string                `json:"agent_version"`
	Provider     domain.ProviderHealth `json:"provider"`
}

type errorBody struct {
	Error apiError `json:"error"`
}

type apiError struct {
	Code      domain.ErrorCode `json:"code"`
	Operation string           `json:"operation,omitempty"`
	RequestID string           `json:"request_id,omitempty"`
	Message   string           `json:"message"`
}

func New(provider domain.Provider, agentVersion string) http.Handler {
	h := &handler{provider: provider, agentVersion: agentVersion}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/health", h.health)
	mux.HandleFunc("GET /v1/snapshot", h.snapshot)
	mux.HandleFunc("POST /v1/calls", h.startCall)
	mux.HandleFunc("POST /v1/calls/{id}/answer", h.answerCall)
	mux.HandleFunc("POST /v1/calls/{id}/reject", h.rejectCall)
	mux.HandleFunc("POST /v1/calls/{id}/hangup", h.hangupCall)
	mux.HandleFunc("POST /v1/calls/{id}/dtmf", h.sendDTMF)
	mux.HandleFunc("POST /v1/messages", h.sendMessage)
	mux.HandleFunc("/", h.notFound)
	return mux
}

func (h *handler) health(w http.ResponseWriter, r *http.Request) {
	health, err := h.provider.Health(r.Context())
	if err != nil {
		h.writeError(w, err, "")
		return
	}
	status := "ok"
	if !health.Available {
		status = "degraded"
	}
	h.writeJSON(w, http.StatusOK, healthResponse{
		Status:       status,
		APIVersion:   domain.APIVersion,
		AgentVersion: h.agentVersion,
		Provider:     health,
	})
}

func (h *handler) snapshot(w http.ResponseWriter, r *http.Request) {
	snapshot, err := h.provider.Snapshot(r.Context())
	if err != nil {
		h.writeError(w, err, "")
		return
	}
	h.writeJSON(w, http.StatusOK, snapshot)
}

func (h *handler) startCall(w http.ResponseWriter, r *http.Request) {
	var request domain.StartCallRequest
	if err := decodeJSON(w, r, &request); err != nil {
		h.writeAPIError(w, http.StatusBadRequest, domain.ErrorInvalidArgument, "start_call", "", "invalid JSON request")
		return
	}
	if requestID, ok := normalizeRequestID(request.RequestID); ok {
		request.RequestID = requestID
	} else {
		h.writeAPIError(w, http.StatusBadRequest, domain.ErrorInvalidArgument, "start_call", "", "request_id is required and must be valid")
		return
	}
	receipt, err := h.provider.StartCall(r.Context(), request)
	if err != nil {
		h.writeError(w, err, strings.TrimSpace(request.RequestID))
		return
	}
	h.writeJSON(w, http.StatusCreated, receipt)
}

func (h *handler) answerCall(w http.ResponseWriter, r *http.Request) {
	h.callCommand(w, r, "answer_call", h.provider.AnswerCall)
}

func (h *handler) rejectCall(w http.ResponseWriter, r *http.Request) {
	h.callCommand(w, r, "reject_call", h.provider.RejectCall)
}

func (h *handler) hangupCall(w http.ResponseWriter, r *http.Request) {
	h.callCommand(w, r, "hangup_call", h.provider.HangupCall)
}

func (h *handler) callCommand(
	w http.ResponseWriter,
	r *http.Request,
	operation string,
	run func(context.Context, domain.CallCommandRequest) (domain.CommandReceipt, error),
) {
	var request domain.CallCommandRequest
	if err := decodeJSON(w, r, &request); err != nil {
		h.writeAPIError(w, http.StatusBadRequest, domain.ErrorInvalidArgument, operation, "", "invalid JSON request")
		return
	}
	if requestID, ok := normalizeRequestID(request.RequestID); ok {
		request.RequestID = requestID
	} else {
		h.writeAPIError(w, http.StatusBadRequest, domain.ErrorInvalidArgument, operation, "", "request_id is required and must be valid")
		return
	}
	request.CallID = strings.TrimSpace(r.PathValue("id"))
	if request.CallID == "" {
		h.writeAPIError(w, http.StatusBadRequest, domain.ErrorInvalidArgument, operation, strings.TrimSpace(request.RequestID), "call id is required")
		return
	}
	receipt, err := run(r.Context(), request)
	if err != nil {
		h.writeError(w, err, strings.TrimSpace(request.RequestID))
		return
	}
	h.writeJSON(w, http.StatusOK, receipt)
}

func (h *handler) sendDTMF(w http.ResponseWriter, r *http.Request) {
	var request domain.DTMFRequest
	if err := decodeJSON(w, r, &request); err != nil {
		h.writeAPIError(w, http.StatusBadRequest, domain.ErrorInvalidArgument, "send_dtmf", "", "invalid JSON request")
		return
	}
	if requestID, ok := normalizeRequestID(request.RequestID); ok {
		request.RequestID = requestID
	} else {
		h.writeAPIError(w, http.StatusBadRequest, domain.ErrorInvalidArgument, "send_dtmf", "", "request_id is required and must be valid")
		return
	}
	request.CallID = strings.TrimSpace(r.PathValue("id"))
	if request.CallID == "" {
		h.writeAPIError(w, http.StatusBadRequest, domain.ErrorInvalidArgument, "send_dtmf", strings.TrimSpace(request.RequestID), "call id is required")
		return
	}
	receipt, err := h.provider.SendDTMF(r.Context(), request)
	if err != nil {
		h.writeError(w, err, strings.TrimSpace(request.RequestID))
		return
	}
	h.writeJSON(w, http.StatusOK, receipt)
}

func (h *handler) sendMessage(w http.ResponseWriter, r *http.Request) {
	var request domain.SendMessageRequest
	if err := decodeJSON(w, r, &request); err != nil {
		h.writeAPIError(w, http.StatusBadRequest, domain.ErrorInvalidArgument, "send_message", "", "invalid JSON request")
		return
	}
	if requestID, ok := normalizeRequestID(request.RequestID); ok {
		request.RequestID = requestID
	} else {
		h.writeAPIError(w, http.StatusBadRequest, domain.ErrorInvalidArgument, "send_message", "", "request_id is required and must be valid")
		return
	}
	receipt, err := h.provider.SendMessage(r.Context(), request)
	if err != nil {
		h.writeError(w, err, strings.TrimSpace(request.RequestID))
		return
	}
	h.writeJSON(w, http.StatusCreated, receipt)
}

func (h *handler) notFound(w http.ResponseWriter, _ *http.Request) {
	h.writeAPIError(w, http.StatusNotFound, domain.ErrorNotFound, "route", "", "endpoint not found")
}

func (h *handler) writeError(w http.ResponseWriter, err error, requestID string) {
	operationError, ok := domain.AsOperationError(err)
	if !ok {
		h.writeAPIError(w, http.StatusInternalServerError, domain.ErrorInternal, "", requestID, "internal error")
		return
	}

	status := http.StatusInternalServerError
	switch operationError.Code {
	case domain.ErrorInvalidArgument:
		status = http.StatusBadRequest
	case domain.ErrorNotFound:
		status = http.StatusNotFound
	case domain.ErrorConflict:
		status = http.StatusConflict
	case domain.ErrorNotSupported:
		status = http.StatusNotImplemented
	case domain.ErrorPermissionDenied:
		status = http.StatusForbidden
	case domain.ErrorUnavailable:
		status = http.StatusServiceUnavailable
	case domain.ErrorInternal:
		status = http.StatusInternalServerError
	}
	h.writeAPIError(w, status, operationError.Code, operationError.Operation, requestID, operationError.Message)
}

func (h *handler) writeAPIError(
	w http.ResponseWriter,
	status int,
	code domain.ErrorCode,
	operation string,
	requestID string,
	message string,
) {
	h.writeJSON(w, status, errorBody{Error: apiError{
		Code:      code,
		Operation: operation,
		RequestID: requestID,
		Message:   message,
	}})
}

func (h *handler) writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func decodeJSON(w http.ResponseWriter, r *http.Request, destination any) error {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return errors.New("content type must be application/json")
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("request body must contain one JSON object")
	}
	return nil
}

func normalizeRequestID(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 128 {
		return "", false
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return "", false
		}
	}
	return value, true
}
