package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"github.com/human-agent65535/modemdeck/internal/communication"
	"github.com/human-agent65535/modemdeck/internal/store"
)

type sendMessageRequest struct {
	RequestID string `json:"request_id"`
	LineID    string `json:"line_id"`
	To        string `json:"to"`
	Content   string `json:"content"`
}

type markMessageReadRequest struct {
	LineID string `json:"line_id"`
	Peer   string `json:"peer"`
}

type startCallRequest struct {
	RequestID        string `json:"request_id"`
	LineID           string `json:"line_id"`
	Number           string `json:"number"`
	RecordingEnabled *bool  `json:"recording_enabled,omitempty"`
}

type callActionRequest struct {
	RequestID string `json:"request_id"`
	Digits    string `json:"digits"`
}

func (api *API) messagesCollection(response http.ResponseWriter, request *http.Request) {
	switch request.Method {
	case http.MethodGet:
		api.messages(response, request)
	case http.MethodPost:
		api.sendMessage(response, request)
	default:
		response.Header().Set("Allow", http.MethodGet+", "+http.MethodPost)
		writeError(response, http.StatusMethodNotAllowed, "method_not_allowed", "Only GET and POST are supported", "")
	}
}

func (api *API) sendMessage(response http.ResponseWriter, request *http.Request) {
	if api.communications == nil {
		writeError(response, http.StatusServiceUnavailable, "communications_unavailable", "Live communications are unavailable", "")
		return
	}
	var input sendMessageRequest
	if !decodeJSONBody(response, request, &input) {
		return
	}
	requestID, ok := commandRequestID(response, request, input.RequestID)
	if !ok {
		return
	}
	lineID := strings.TrimSpace(input.LineID)
	if lineID == "" {
		writeError(response, http.StatusBadRequest, "invalid_argument", "line_id is required", "line_id")
		return
	}
	message, err := api.communications.SendMessage(request.Context(), communication.SendMessageInput{
		RequestID: requestID,
		LineID:    lineID,
		Number:    input.To,
		Text:      input.Content,
	})
	if err != nil {
		api.writeCommunicationError(response, request, "send message", err)
		return
	}
	api.logger.Info(
		"message submitted",
		"line_id",
		message.LineID,
		"request_id",
		requestID,
		"state",
		message.State,
	)
	writeJSON(response, http.StatusCreated, messageResponse{
		Message: messageResponseItemFromStore(message),
	})
}

func (api *API) messageRead(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPatch {
		response.Header().Set("Allow", http.MethodPatch)
		writeError(response, http.StatusMethodNotAllowed, "method_not_allowed", "Only PATCH is supported", "")
		return
	}
	var input markMessageReadRequest
	if !decodeJSONBody(response, request, &input) {
		return
	}
	identity := store.MessageThreadIdentity{
		LineID: strings.TrimSpace(input.LineID),
		Peer:   strings.TrimSpace(input.Peer),
	}
	if identity.LineID == "" || identity.Peer == "" {
		writeError(
			response,
			http.StatusBadRequest,
			"invalid_argument",
			"line_id and peer are required",
			"line_id",
		)
		return
	}
	if err := api.repository.MarkMessageThreadRead(request.Context(), identity); err != nil {
		switch {
		case errors.Is(err, store.ErrMessageThreadNotFound):
			writeError(response, http.StatusNotFound, "message_thread_not_found", "Message thread no longer exists", "")
		default:
			api.writeInternalError(response, request, "mark message thread read", err)
		}
		return
	}
	response.Header().Set("Cache-Control", "no-store")
	response.WriteHeader(http.StatusNoContent)
}

func (api *API) callsCollection(response http.ResponseWriter, request *http.Request) {
	switch request.Method {
	case http.MethodGet:
		api.calls(response, request)
	case http.MethodPost:
		api.startCall(response, request)
	default:
		response.Header().Set("Allow", http.MethodGet+", "+http.MethodPost)
		writeError(response, http.StatusMethodNotAllowed, "method_not_allowed", "Only GET and POST are supported", "")
	}
}

func (api *API) missedCallsRead(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPatch {
		response.Header().Set("Allow", http.MethodPatch)
		writeError(response, http.StatusMethodNotAllowed, "method_not_allowed", "Only PATCH is supported", "")
		return
	}
	if err := api.repository.MarkMissedCallsRead(request.Context()); err != nil {
		api.writeInternalError(response, request, "mark missed calls read", err)
		return
	}
	response.Header().Set("Cache-Control", "no-store")
	response.WriteHeader(http.StatusNoContent)
}

func (api *API) startCall(response http.ResponseWriter, request *http.Request) {
	if api.communications == nil {
		writeError(response, http.StatusServiceUnavailable, "communications_unavailable", "Live communications are unavailable", "")
		return
	}
	var input startCallRequest
	if !decodeJSONBody(response, request, &input) {
		return
	}
	requestID, ok := commandRequestID(response, request, input.RequestID)
	if !ok {
		return
	}
	if input.RecordingEnabled != nil {
		if api.recordings == nil {
			writeError(response, http.StatusServiceUnavailable, "recording_unavailable", "Call recording is unavailable", "")
			return
		}
		preparedRequestID, err := api.recordings.PrepareOutgoing(
			request.Context(),
			requestID,
			*input.RecordingEnabled,
		)
		if err != nil {
			api.writeRecordingError(response, request, "prepare outgoing call recording", err, nil)
			return
		}
		requestID = preparedRequestID
	}
	call, err := api.communications.StartCall(request.Context(), communication.StartCallInput{
		RequestID: requestID,
		LineID:    input.LineID,
		Number:    input.Number,
	})
	if err != nil {
		api.writeCommunicationError(response, request, "start call", err)
		return
	}
	api.logger.Info(
		"call started",
		"line_id",
		call.LineID,
		"call_id",
		call.ID,
		"request_id",
		requestID,
		"phase",
		call.Phase,
	)
	writeJSON(response, http.StatusCreated, callSessionEnvelope{Call: callSession(call)})
}

func (api *API) activeCalls(response http.ResponseWriter, request *http.Request) {
	if api.communications == nil {
		writeError(response, http.StatusServiceUnavailable, "communications_unavailable", "Live communications are unavailable", "")
		return
	}
	calls, err := api.communications.ActiveCalls(request.Context())
	if err != nil {
		api.writeCommunicationError(response, request, "list active calls", err)
		return
	}
	sessions := make([]callSessionResponse, 0, len(calls))
	for _, call := range calls {
		sessions = append(sessions, callSession(call))
	}
	writeJSON(response, http.StatusOK, activeCallsResponse{Calls: sessions})
}

func (api *API) callAction(response http.ResponseWriter, request *http.Request, callID, action string) {
	if request.Method != http.MethodPost {
		response.Header().Set("Allow", http.MethodPost)
		writeError(response, http.StatusMethodNotAllowed, "method_not_allowed", "Only POST is supported", "")
		return
	}
	if api.communications == nil {
		writeError(response, http.StatusServiceUnavailable, "communications_unavailable", "Live communications are unavailable", "")
		return
	}
	var input callActionRequest
	if !decodeJSONBody(response, request, &input) {
		return
	}
	requestID, ok := commandRequestID(response, request, input.RequestID)
	if !ok {
		return
	}
	_, err := api.communications.CallAction(request.Context(), communication.CallActionInput{
		RequestID: requestID,
		CallID:    callID,
		Action:    action,
		Digits:    input.Digits,
	})
	if err != nil {
		api.writeCommunicationError(response, request, "control call", err)
		return
	}
	api.logger.Info(
		"call control accepted",
		"call_id",
		callID,
		"action",
		action,
		"request_id",
		requestID,
	)
	writeJSON(response, http.StatusAccepted, map[string]string{
		"request_id": requestID,
		"call_id":    callID,
	})
}

func callActionResource(path string) (id string, action string, ok bool) {
	const prefix = "/api/v1/calls/"
	if !strings.HasPrefix(path, prefix) {
		return "", "", false
	}
	parts := strings.Split(strings.TrimPrefix(path, prefix), "/")
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || len(parts[0]) > maxIdentifierLength {
		return "", "", false
	}
	switch parts[1] {
	case "answer", "reject", "hangup", "dtmf":
		return parts[0], parts[1], true
	default:
		return "", "", false
	}
}

func commandRequestID(response http.ResponseWriter, request *http.Request, bodyValue string) (string, bool) {
	headerValue := strings.TrimSpace(request.Header.Get("Idempotency-Key"))
	bodyValue = strings.TrimSpace(bodyValue)
	if headerValue != "" && bodyValue != "" && headerValue != bodyValue {
		writeError(response, http.StatusBadRequest, "invalid_argument", "request_id and Idempotency-Key must match", "request_id")
		return "", false
	}
	if headerValue != "" {
		return headerValue, true
	}
	return bodyValue, true
}

func callSession(call store.Call) callSessionResponse {
	failure := call.FailureCode
	if failure == "" && call.Phase == "failed" {
		failure = call.EndReason
	}
	return callSessionResponse{
		ID:             call.ID,
		LineID:         call.LineID,
		Direction:      call.Direction,
		RemoteNumber:   call.RemoteNumber,
		DisplayName:    call.ContactName,
		Phase:          call.Phase,
		CreatedAt:      call.CreatedAt,
		ActiveAt:       call.ActiveAt,
		EndedAt:        call.EndedAt,
		FailureReason:  failure,
		Bearer:         call.Bearer,
		MediaAvailable: call.MediaAvailable,
	}
}

func (api *API) writeCommunicationError(
	response http.ResponseWriter,
	request *http.Request,
	operation string,
	err error,
) {
	api.logger.Warn(
		operation,
		"method",
		request.Method,
		"path",
		request.URL.Path,
		"error_class",
		communicationErrorClass(err),
		"error",
		err,
	)
	switch {
	case errors.Is(err, communication.ErrInvalidArgument):
		writeError(response, http.StatusBadRequest, "invalid_argument", err.Error(), "")
	case errors.Is(err, communication.ErrNotFound):
		writeError(response, http.StatusNotFound, "not_found", err.Error(), "")
	case errors.Is(err, communication.ErrConflict):
		writeError(response, http.StatusConflict, "conflict", err.Error(), "")
	case errors.Is(err, communication.ErrNotSupported):
		writeError(response, http.StatusNotImplemented, "not_supported", err.Error(), "")
	case errors.Is(err, communication.ErrFailedPrecondition):
		writeError(response, http.StatusPreconditionFailed, "failed_precondition", err.Error(), "")
	case errors.Is(err, communication.ErrNetworkRejected):
		writeError(response, http.StatusUnprocessableEntity, "network_rejected", err.Error(), "")
	case errors.Is(err, communication.ErrUnavailable):
		writeError(response, http.StatusServiceUnavailable, "communications_unavailable", err.Error(), "")
	case errors.Is(err, communication.ErrVerification):
		writeError(response, http.StatusBadGateway, "verification_failed", err.Error(), "")
	default:
		api.writeInternalError(response, request, operation, err)
	}
}

func communicationErrorClass(err error) string {
	switch {
	case errors.Is(err, communication.ErrInvalidArgument):
		return "invalid_argument"
	case errors.Is(err, communication.ErrNotFound):
		return "not_found"
	case errors.Is(err, communication.ErrConflict):
		return "conflict"
	case errors.Is(err, communication.ErrNotSupported):
		return "not_supported"
	case errors.Is(err, communication.ErrFailedPrecondition):
		return "failed_precondition"
	case errors.Is(err, communication.ErrNetworkRejected):
		return "network_rejected"
	case errors.Is(err, communication.ErrUnavailable):
		return "unavailable"
	case errors.Is(err, communication.ErrVerification):
		return "verification_failed"
	default:
		return "internal"
	}
}
