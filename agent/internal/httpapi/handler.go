package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

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

type linesResponse struct {
	Lines []domain.Line `json:"lines"`
}

type errorBody struct {
	Error apiError `json:"error"`
}

type apiError struct {
	Code      domain.ErrorCode `json:"code"`
	Operation string           `json:"operation,omitempty"`
	Message   string           `json:"message"`
}

func New(provider domain.Provider, agentVersion string) http.Handler {
	h := &handler{provider: provider, agentVersion: agentVersion}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/health", h.health)
	mux.HandleFunc("GET /v1/lines", h.lines)
	mux.HandleFunc("POST /v1/calls", h.startCall)
	mux.HandleFunc("POST /v1/calls/{id}/answer", h.answerCall)
	mux.HandleFunc("POST /v1/calls/{id}/hangup", h.hangupCall)
	mux.HandleFunc("POST /v1/messages", h.sendMessage)
	mux.HandleFunc("/", h.notFound)
	return mux
}

func (h *handler) health(w http.ResponseWriter, r *http.Request) {
	health, err := h.provider.Health(r.Context())
	if err != nil {
		h.writeError(w, err)
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

func (h *handler) lines(w http.ResponseWriter, r *http.Request) {
	lines, err := h.provider.Lines(r.Context())
	if err != nil {
		h.writeError(w, err)
		return
	}
	if lines == nil {
		lines = []domain.Line{}
	}
	h.writeJSON(w, http.StatusOK, linesResponse{Lines: lines})
}

func (h *handler) startCall(w http.ResponseWriter, r *http.Request) {
	var request domain.StartCallRequest
	if err := decodeJSON(w, r, &request); err != nil {
		h.writeAPIError(w, http.StatusBadRequest, domain.ErrorInvalidArgument, "start_call", "invalid JSON request")
		return
	}
	request.LineID = strings.TrimSpace(request.LineID)
	request.Number = strings.TrimSpace(request.Number)
	if request.LineID == "" || request.Number == "" {
		h.writeAPIError(w, http.StatusBadRequest, domain.ErrorInvalidArgument, "start_call", "line_id and number are required")
		return
	}
	call, err := h.provider.StartCall(r.Context(), request)
	if err != nil {
		h.writeError(w, err)
		return
	}
	h.writeJSON(w, http.StatusCreated, call)
}

func (h *handler) answerCall(w http.ResponseWriter, r *http.Request) {
	callID := strings.TrimSpace(r.PathValue("id"))
	if callID == "" {
		h.writeAPIError(w, http.StatusBadRequest, domain.ErrorInvalidArgument, "answer_call", "call id is required")
		return
	}
	call, err := h.provider.AnswerCall(r.Context(), callID)
	if err != nil {
		h.writeError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, call)
}

func (h *handler) hangupCall(w http.ResponseWriter, r *http.Request) {
	callID := strings.TrimSpace(r.PathValue("id"))
	if callID == "" {
		h.writeAPIError(w, http.StatusBadRequest, domain.ErrorInvalidArgument, "hangup_call", "call id is required")
		return
	}
	call, err := h.provider.HangupCall(r.Context(), callID)
	if err != nil {
		h.writeError(w, err)
		return
	}
	h.writeJSON(w, http.StatusOK, call)
}

func (h *handler) sendMessage(w http.ResponseWriter, r *http.Request) {
	var request domain.SendMessageRequest
	if err := decodeJSON(w, r, &request); err != nil {
		h.writeAPIError(w, http.StatusBadRequest, domain.ErrorInvalidArgument, "send_message", "invalid JSON request")
		return
	}
	request.LineID = strings.TrimSpace(request.LineID)
	request.Number = strings.TrimSpace(request.Number)
	if request.LineID == "" || request.Number == "" || strings.TrimSpace(request.Text) == "" {
		h.writeAPIError(w, http.StatusBadRequest, domain.ErrorInvalidArgument, "send_message", "line_id, number, and text are required")
		return
	}
	message, err := h.provider.SendMessage(r.Context(), request)
	if err != nil {
		h.writeError(w, err)
		return
	}
	h.writeJSON(w, http.StatusCreated, message)
}

func (h *handler) notFound(w http.ResponseWriter, _ *http.Request) {
	h.writeAPIError(w, http.StatusNotFound, domain.ErrorNotFound, "route", "endpoint not found")
}

func (h *handler) writeError(w http.ResponseWriter, err error) {
	operationError, ok := domain.AsOperationError(err)
	if !ok {
		h.writeAPIError(w, http.StatusInternalServerError, domain.ErrorInternal, "", "internal error")
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
	case domain.ErrorUnavailable:
		status = http.StatusServiceUnavailable
	case domain.ErrorInternal:
		status = http.StatusInternalServerError
	}
	h.writeAPIError(w, status, operationError.Code, operationError.Operation, operationError.Message)
}

func (h *handler) writeAPIError(w http.ResponseWriter, status int, code domain.ErrorCode, operation, message string) {
	h.writeJSON(w, status, errorBody{Error: apiError{
		Code:      code,
		Operation: operation,
		Message:   message,
	}})
}

func (h *handler) writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func decodeJSON(w http.ResponseWriter, r *http.Request, destination any) error {
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
