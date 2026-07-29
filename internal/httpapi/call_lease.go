package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"github.com/human-agent65535/modemdeck/internal/calllease"
)

type renewCallLeaseRequest struct {
	HolderID string `json:"holder_id"`
}

func (api *API) renewCallLease(
	response http.ResponseWriter,
	request *http.Request,
	callID string,
) {
	if request.Method != http.MethodPut {
		response.Header().Set("Allow", http.MethodPut)
		writeError(
			response,
			http.StatusMethodNotAllowed,
			"method_not_allowed",
			"Only PUT is supported",
			"",
		)
		return
	}
	if api.callLeases == nil {
		writeError(
			response,
			http.StatusServiceUnavailable,
			"call_lease_unavailable",
			"Browser call leases are unavailable",
			"",
		)
		return
	}
	var input renewCallLeaseRequest
	if !decodeJSONBody(response, request, &input) {
		return
	}
	status, err := api.callLeases.Renew(
		request.Context(),
		callID,
		input.HolderID,
	)
	if err != nil {
		api.writeCallLeaseError(
			response,
			request,
			"renew browser call lease",
			err,
		)
		return
	}
	response.Header().Set("Cache-Control", "no-store")
	writeJSON(response, http.StatusOK, status)
}

func (api *API) writeCallLeaseError(
	response http.ResponseWriter,
	request *http.Request,
	operation string,
	err error,
) {
	switch {
	case errors.Is(err, calllease.ErrInvalidArgument):
		writeError(
			response,
			http.StatusBadRequest,
			"invalid_argument",
			"call id and holder_id are required",
			"holder_id",
		)
	case errors.Is(err, calllease.ErrCallNotFound):
		writeError(
			response,
			http.StatusNotFound,
			"call_not_found",
			"Call no longer exists",
			"",
		)
	case errors.Is(err, calllease.ErrCallNotActive):
		writeError(
			response,
			http.StatusConflict,
			"call_not_active",
			"Call is no longer active",
			"",
		)
	case errors.Is(err, calllease.ErrCallOwned):
		writeError(
			response,
			http.StatusConflict,
			"line_in_use",
			"Line is already in use by another browser",
			"",
		)
	case errors.Is(err, calllease.ErrHolderBusy):
		writeError(
			response,
			http.StatusConflict,
			"browser_call_busy",
			"This browser is already handling another call",
			"",
		)
	case errors.Is(err, calllease.ErrNotOwner):
		writeError(
			response,
			http.StatusConflict,
			"call_not_owned",
			"This browser does not own the call",
			"",
		)
	default:
		api.writeInternalError(response, request, operation, err)
	}
}

func callLeaseResourceID(path string) (string, bool) {
	const prefix = "/api/v1/calls/"
	const suffix = "/lease"
	if !strings.HasPrefix(path, prefix) || !strings.HasSuffix(path, suffix) {
		return "", false
	}
	callID := strings.TrimSuffix(strings.TrimPrefix(path, prefix), suffix)
	if strings.TrimSpace(callID) == "" ||
		strings.Contains(callID, "/") ||
		len(callID) > maxIdentifierLength {
		return "", false
	}
	return callID, true
}
