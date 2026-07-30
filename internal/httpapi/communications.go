package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/human-agent65535/modemdeck/internal/calllease"
	"github.com/human-agent65535/modemdeck/internal/communication"
	"github.com/human-agent65535/modemdeck/internal/runtimeevents"
	"github.com/human-agent65535/modemdeck/internal/store"
)

const callControlRollbackTimeout = 5 * time.Second

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

type messageThreadStateRequest struct {
	Action  string                   `json:"action"`
	Threads []markMessageReadRequest `json:"threads"`
}

type startCallRequest struct {
	RequestID        string `json:"request_id"`
	LineID           string `json:"line_id"`
	Number           string `json:"number"`
	HolderID         string `json:"holder_id"`
	RecordingEnabled *bool  `json:"recording_enabled,omitempty"`
}

type callActionRequest struct {
	RequestID string `json:"request_id"`
	Digits    string `json:"digits"`
	HolderID  string `json:"holder_id"`
}

type callsBatchRequest struct {
	Action string   `json:"action"`
	IDs    []string `json:"ids"`
}

type callRecordPath struct {
	ID     string
	Action string
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
	if !api.requireLineAccess(response, request, lineID) {
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
	api.publishRuntimeResources(runtimeevents.ResourceMessages)
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
	if !api.requireLineAccess(response, request, identity.LineID) {
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
	api.publishRuntimeResources(runtimeevents.ResourceMessages)
	response.Header().Set("Cache-Control", "no-store")
	response.WriteHeader(http.StatusNoContent)
}

func (api *API) messageThreadState(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPatch {
		response.Header().Set("Allow", http.MethodPatch)
		writeError(response, http.StatusMethodNotAllowed, "method_not_allowed", "Only PATCH is supported", "")
		return
	}
	var input messageThreadStateRequest
	if !decodeJSONBody(response, request, &input) {
		return
	}
	action := store.MessageThreadAction(strings.TrimSpace(input.Action))
	switch action {
	case store.MessageThreadMarkRead,
		store.MessageThreadMarkUnread,
		store.MessageThreadFavorite,
		store.MessageThreadUnfavorite,
		store.MessageThreadDelete:
	default:
		writeError(response, http.StatusBadRequest, "invalid_argument", "action is invalid", "action")
		return
	}
	if len(input.Threads) == 0 || len(input.Threads) > 100 {
		writeError(response, http.StatusBadRequest, "invalid_argument", "threads must contain between 1 and 100 items", "threads")
		return
	}
	identities := make([]store.MessageThreadIdentity, 0, len(input.Threads))
	for _, thread := range input.Threads {
		identity := store.MessageThreadIdentity{
			LineID: strings.TrimSpace(thread.LineID),
			Peer:   strings.TrimSpace(thread.Peer),
		}
		if identity.LineID == "" || identity.Peer == "" {
			writeError(response, http.StatusBadRequest, "invalid_argument", "line_id and peer are required", "threads")
			return
		}
		if !api.requireLineAccess(response, request, identity.LineID) {
			return
		}
		identities = append(identities, identity)
	}
	if action == store.MessageThreadDelete && !api.requireAdmin(response, request) {
		return
	}
	if err := api.repository.UpdateMessageThreads(
		request.Context(),
		identities,
		action,
	); err != nil {
		switch {
		case errors.Is(err, store.ErrMessageThreadNotFound):
			writeError(response, http.StatusNotFound, "message_thread_not_found", "A message thread no longer exists", "")
		default:
			api.writeInternalError(response, request, "update message threads", err)
		}
		return
	}
	api.publishRuntimeResources(runtimeevents.ResourceMessages)
	response.Header().Set("Cache-Control", "no-store")
	response.WriteHeader(http.StatusNoContent)
}

func (api *API) messageThreadsCollection(
	response http.ResponseWriter,
	request *http.Request,
) {
	switch request.Method {
	case http.MethodGet:
		api.messageThreads(response, request)
	case http.MethodDelete:
		api.deleteMessageThread(response, request)
	default:
		response.Header().Set("Allow", http.MethodGet+", "+http.MethodDelete)
		writeError(response, http.StatusMethodNotAllowed, "method_not_allowed", "Only GET and DELETE are supported", "")
	}
}

func (api *API) deleteMessageThread(response http.ResponseWriter, request *http.Request) {
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
	if !api.requireAdmin(response, request) ||
		!api.requireLineAccess(response, request, identity.LineID) {
		return
	}
	if err := api.repository.DeleteMessageThread(request.Context(), identity); err != nil {
		switch {
		case errors.Is(err, store.ErrMessageThreadNotFound):
			writeError(response, http.StatusNotFound, "message_thread_not_found", "Message thread no longer exists", "")
		default:
			api.writeInternalError(response, request, "delete message thread", err)
		}
		return
	}
	api.publishRuntimeResources(runtimeevents.ResourceMessages)
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
	api.publishRuntimeResources(runtimeevents.ResourceCalls)
	response.Header().Set("Cache-Control", "no-store")
	response.WriteHeader(http.StatusNoContent)
}

func (api *API) callsBatch(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPatch {
		response.Header().Set("Allow", http.MethodPatch)
		writeError(response, http.StatusMethodNotAllowed, "method_not_allowed", "Only PATCH is supported", "")
		return
	}
	var input callsBatchRequest
	if !decodeJSONBody(response, request, &input) {
		return
	}
	action := strings.TrimSpace(input.Action)
	if action != "read" &&
		action != "unread" &&
		action != "favorite" &&
		action != "unfavorite" &&
		action != "delete" {
		writeError(response, http.StatusBadRequest, "invalid_argument", "action is invalid", "action")
		return
	}
	if len(input.IDs) == 0 || len(input.IDs) > 100 {
		writeError(response, http.StatusBadRequest, "invalid_argument", "ids must contain between 1 and 100 items", "ids")
		return
	}
	ids := make([]string, 0, len(input.IDs))
	seen := make(map[string]struct{}, len(input.IDs))
	for _, value := range input.IDs {
		id := strings.TrimSpace(value)
		if !validRecordingPathID(id) {
			writeError(response, http.StatusBadRequest, "invalid_argument", "ids contains an invalid call ID", "ids")
			return
		}
		if _, duplicate := seen[id]; duplicate {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	for _, id := range ids {
		if !api.requireCallAccess(response, request, id) {
			return
		}
	}
	switch action {
	case "read":
		if err := api.repository.MarkMissedCallsReadByIDs(request.Context(), ids); err != nil {
			api.writeInternalError(response, request, "mark missed calls read", err)
			return
		}
		api.publishRuntimeResources(runtimeevents.ResourceCalls)
	case "unread":
		if err := api.repository.MarkMissedCallsUnreadByIDs(request.Context(), ids); err != nil {
			api.writeInternalError(response, request, "mark missed calls unread", err)
			return
		}
		api.publishRuntimeResources(runtimeevents.ResourceCalls)
	case "favorite", "unfavorite":
		if err := api.repository.SetCallFavoritesByIDs(
			request.Context(),
			ids,
			action == "favorite",
		); err != nil {
			if errors.Is(err, store.ErrCallNotFound) {
				writeError(response, http.StatusNotFound, "call_not_found", "A call no longer exists", "")
				return
			}
			api.writeInternalError(response, request, "update call favorite state", err)
			return
		}
		api.publishRuntimeResources(runtimeevents.ResourceCalls)
	case "delete":
		if !api.requireAdmin(response, request) {
			return
		}
		if api.recordings == nil {
			writeError(response, http.StatusServiceUnavailable, "recording_unavailable", "Call history deletion is unavailable", "")
			return
		}
		for _, id := range ids {
			if err := api.recordings.DeleteCall(request.Context(), id); err != nil {
				api.writeRecordingError(response, request, "delete call history", err, nil)
				return
			}
		}
		api.publishRuntimeResources(
			runtimeevents.ResourceCalls,
			runtimeevents.ResourceRecordings,
		)
	}
	response.Header().Set("Cache-Control", "no-store")
	response.WriteHeader(http.StatusNoContent)
}

func (api *API) callRecordResource(
	response http.ResponseWriter,
	request *http.Request,
	resource callRecordPath,
) {
	if !api.requireCallAccess(response, request, resource.ID) {
		return
	}
	switch resource.Action {
	case "":
		if !api.requireAdmin(response, request) {
			return
		}
		if request.Method != http.MethodDelete {
			response.Header().Set("Allow", http.MethodDelete)
			writeError(response, http.StatusMethodNotAllowed, "method_not_allowed", "Only DELETE is supported", "")
			return
		}
		if api.recordings == nil {
			writeError(response, http.StatusServiceUnavailable, "recording_unavailable", "Call history deletion is unavailable", "")
			return
		}
		if err := api.recordings.DeleteCall(request.Context(), resource.ID); err != nil {
			api.writeRecordingError(response, request, "delete call history", err, nil)
			return
		}
		api.publishRuntimeResources(
			runtimeevents.ResourceCalls,
			runtimeevents.ResourceRecordings,
		)
	case "read":
		if request.Method != http.MethodPatch {
			response.Header().Set("Allow", http.MethodPatch)
			writeError(response, http.StatusMethodNotAllowed, "method_not_allowed", "Only PATCH is supported", "")
			return
		}
		if err := api.repository.MarkMissedCallsReadByIDs(
			request.Context(),
			[]string{resource.ID},
		); err != nil {
			api.writeInternalError(response, request, "mark missed call read", err)
			return
		}
		api.publishRuntimeResources(runtimeevents.ResourceCalls)
	case "unread":
		if request.Method != http.MethodPatch {
			response.Header().Set("Allow", http.MethodPatch)
			writeError(response, http.StatusMethodNotAllowed, "method_not_allowed", "Only PATCH is supported", "")
			return
		}
		if err := api.repository.MarkMissedCallsUnreadByIDs(
			request.Context(),
			[]string{resource.ID},
		); err != nil {
			api.writeInternalError(response, request, "mark missed call unread", err)
			return
		}
		api.publishRuntimeResources(runtimeevents.ResourceCalls)
	default:
		writeError(response, http.StatusNotFound, "not_found", "API endpoint was not found", "")
		return
	}
	response.Header().Set("Cache-Control", "no-store")
	response.WriteHeader(http.StatusNoContent)
}

func callRecordResource(path string) (callRecordPath, bool) {
	const prefix = "/api/v1/calls/"
	if !strings.HasPrefix(path, prefix) {
		return callRecordPath{}, false
	}
	parts := strings.Split(strings.TrimPrefix(path, prefix), "/")
	if !validRecordingPathID(parts[0]) {
		return callRecordPath{}, false
	}
	switch {
	case len(parts) == 1:
		return callRecordPath{ID: parts[0]}, true
	case len(parts) == 2 && (parts[1] == "read" || parts[1] == "unread"):
		return callRecordPath{ID: parts[0], Action: parts[1]}, true
	default:
		return callRecordPath{}, false
	}
}

func (api *API) startCall(response http.ResponseWriter, request *http.Request) {
	if api.communications == nil {
		writeError(response, http.StatusServiceUnavailable, "communications_unavailable", "Live communications are unavailable", "")
		return
	}
	if api.callLeases == nil {
		writeError(response, http.StatusServiceUnavailable, "call_lease_unavailable", "Browser call ownership is unavailable", "")
		return
	}
	var input startCallRequest
	if !decodeJSONBody(response, request, &input) {
		return
	}
	if !api.requireLineAccess(response, request, input.LineID) {
		return
	}
	holderID, err := calllease.NormalizeHolderID(input.HolderID)
	if err != nil {
		api.writeCallLeaseError(response, request, "validate browser call owner", err)
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
	} else if api.recordings != nil {
		settings, err := api.recordings.Settings(request.Context())
		if err != nil {
			api.writeRecordingError(response, request, "load outgoing call recording default", err, nil)
			return
		}
		if settings.DefaultEnabled {
			preparedRequestID, err := api.recordings.PrepareOutgoing(
				request.Context(),
				requestID,
				true,
			)
			if err != nil {
				api.writeRecordingError(response, request, "prepare outgoing call recording", err, nil)
				return
			}
			requestID = preparedRequestID
		}
	}
	reservation, err := api.callLeases.ReserveOutgoing(
		request.Context(),
		requestID,
		input.LineID,
		holderID,
	)
	if err != nil {
		api.writeCallLeaseError(response, request, "reserve outgoing call line", err)
		return
	}
	reservationActive := reservation.Created
	releaseReservation := func() {
		if !reservationActive {
			return
		}
		reservationActive = false
		released, releaseErr := api.callLeases.ReleaseOutgoing(requestID, holderID)
		if releaseErr != nil {
			api.logger.Warn(
				"outgoing call reservation could not be released",
				"component", "calls",
				"request_id", requestID,
				"line_id", strings.TrimSpace(input.LineID),
				"error", releaseErr,
			)
		}
		if released {
			api.publishRuntimeResources(runtimeevents.ResourceCalls)
		}
	}
	defer releaseReservation()
	api.publishRuntimeResources(runtimeevents.ResourceCalls)

	call, err := api.communications.StartCall(request.Context(), communication.StartCallInput{
		RequestID: requestID,
		LineID:    input.LineID,
		Number:    input.Number,
	})
	if err != nil {
		releaseReservation()
		api.writeCommunicationError(response, request, "start call", err)
		return
	}
	if _, err := api.callLeases.ActivateOutgoing(
		request.Context(),
		requestID,
		call.ID,
		holderID,
	); err != nil {
		rollbackContext, cancel := context.WithTimeout(
			context.WithoutCancel(request.Context()),
			callControlRollbackTimeout,
		)
		releaseErr := api.communications.EndCall(
			rollbackContext,
			call.ID,
		)
		cancel()
		if releaseErr != nil {
			api.logger.Error(
				"outgoing call ownership failed and the created call could not be released",
				"component", "calls",
				"call_id", call.ID,
				"error", errors.Join(err, releaseErr),
			)
		}
		releaseReservation()
		api.writeCallLeaseError(response, request, "claim outgoing call owner", err)
		return
	}
	reservationActive = false
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
	api.publishRuntimeResources(runtimeevents.ResourceCalls)
	writeJSON(response, http.StatusCreated, callSessionEnvelope{
		Call: callSession(call, calllease.ControlOwned),
	})
}

func (api *API) activeCalls(response http.ResponseWriter, request *http.Request) {
	if api.communications == nil {
		writeError(response, http.StatusServiceUnavailable, "communications_unavailable", "Live communications are unavailable", "")
		return
	}
	if api.callLeases == nil {
		writeError(response, http.StatusServiceUnavailable, "call_lease_unavailable", "Browser call ownership is unavailable", "")
		return
	}
	holderID, err := calllease.NormalizeHolderID(
		request.URL.Query().Get("holder_id"),
	)
	if err != nil {
		api.writeCallLeaseError(response, request, "validate browser call owner", err)
		return
	}
	calls, err := api.communications.ActiveCalls(request.Context())
	if err != nil {
		api.writeCommunicationError(response, request, "list active calls", err)
		return
	}
	projection, err := api.callLeases.ProjectActive(calls, holderID)
	if err != nil {
		api.writeCallLeaseError(
			response,
			request,
			"project active browser calls",
			err,
		)
		return
	}
	sessions := make([]callSessionResponse, 0, len(projection.Calls))
	for _, projected := range projection.Calls {
		if !canAccessLine(request.Context(), projected.Call.LineID) {
			continue
		}
		sessions = append(
			sessions,
			callSession(projected.Call, projected.ControlState),
		)
	}
	reservationResponses := make(
		[]outgoingCallReservationResponse,
		0,
		len(projection.Reservations),
	)
	for _, reservation := range projection.Reservations {
		if !canAccessLine(request.Context(), reservation.LineID) {
			continue
		}
		reservationResponses = append(
			reservationResponses,
			outgoingCallReservationResponse{
				RequestID:    reservation.ID,
				LineID:       reservation.LineID,
				ControlState: string(reservation.ControlState),
				CreatedAt:    reservation.CreatedAt.UTC().Format(time.RFC3339Nano),
			},
		)
	}
	writeJSON(response, http.StatusOK, activeCallsResponse{
		Calls:        sessions,
		Reservations: reservationResponses,
	})
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
	if api.callLeases == nil {
		writeError(response, http.StatusServiceUnavailable, "call_lease_unavailable", "Browser call ownership is unavailable", "")
		return
	}
	if !api.requireCallAccess(response, request, callID) {
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
	claimed := action == "answer" || action == "reject"
	var leaseErr error
	if claimed {
		_, leaseErr = api.callLeases.Claim(
			request.Context(),
			callID,
			input.HolderID,
		)
	} else {
		leaseErr = api.callLeases.Require(
			request.Context(),
			callID,
			input.HolderID,
		)
	}
	if leaseErr != nil {
		api.writeCallLeaseError(response, request, "authorize call control", leaseErr)
		return
	}
	if claimed {
		api.publishRuntimeResources(runtimeevents.ResourceCalls)
	}
	_, err := api.communications.CallAction(request.Context(), communication.CallActionInput{
		RequestID: requestID,
		CallID:    callID,
		Action:    action,
		Digits:    input.Digits,
	})
	if err != nil {
		if claimed {
			rollbackContext, cancel := context.WithTimeout(
				context.WithoutCancel(request.Context()),
				callControlRollbackTimeout,
			)
			if releaseErr := api.callLeases.Release(
				rollbackContext,
				callID,
				input.HolderID,
			); releaseErr != nil {
				api.logger.Warn(
					"failed call action owner could not be released",
					"component", "calls",
					"call_id", callID,
					"action", action,
					"error", releaseErr,
				)
			}
			cancel()
			api.publishRuntimeResources(runtimeevents.ResourceCalls)
		}
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
	api.publishRuntimeResources(runtimeevents.ResourceCalls)
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

func callSession(
	call store.Call,
	controlState calllease.ControlState,
) callSessionResponse {
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
		ControlState:   string(controlState),
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
