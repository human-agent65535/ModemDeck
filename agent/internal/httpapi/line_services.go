package httpapi

import (
	"net/http"
	"strings"

	"github.com/human-agent65535/modemdeck/agent/internal/domain"
)

type simStatusResponse struct {
	SIM domain.SIMStatus `json:"sim"`
}

type commandReceiptResponse struct {
	Receipt domain.CommandReceipt `json:"receipt"`
}

type connectionProfilesResponse struct {
	Profiles []domain.ConnectionProfile `json:"profiles"`
}

type connectionProfileResponse struct {
	Profile domain.ConnectionProfile `json:"profile"`
}

type ussdStatusResponse struct {
	USSD domain.USSDStatus `json:"ussd"`
}

type ussdCommandResponse struct {
	Result domain.USSDResponse `json:"result"`
}

func (h *handler) getSIMStatus(response http.ResponseWriter, request *http.Request) {
	if h.lineServices == nil {
		h.writeAPIError(response, http.StatusNotImplemented, domain.ErrorNotSupported, "read_sim_status", "", "SIM management is unavailable")
		return
	}
	status, err := h.lineServices.SIMStatus(request.Context(), strings.TrimSpace(request.PathValue("id")))
	if err != nil {
		h.writeError(response, err, "")
		return
	}
	h.writeJSON(response, http.StatusOK, simStatusResponse{SIM: status})
}

func (h *handler) postSIMCommand(response http.ResponseWriter, request *http.Request) {
	if h.lineServices == nil {
		h.writeAPIError(response, http.StatusNotImplemented, domain.ErrorNotSupported, "sim_command", "", "SIM management is unavailable")
		return
	}
	var input domain.SIMCommandRequest
	if err := decodeJSON(response, request, &input); err != nil {
		h.writeAPIError(response, http.StatusBadRequest, domain.ErrorInvalidArgument, "sim_command", "", "invalid JSON request")
		return
	}
	input.LineID = strings.TrimSpace(request.PathValue("id"))
	receipt, err := h.lineServices.SIMCommand(request.Context(), input)
	if err != nil {
		h.writeError(response, err, strings.TrimSpace(input.RequestID))
		return
	}
	h.writeJSON(response, http.StatusOK, commandReceiptResponse{Receipt: receipt})
}

func (h *handler) getConnectionProfiles(response http.ResponseWriter, request *http.Request) {
	if h.lineServices == nil {
		h.writeAPIError(response, http.StatusNotImplemented, domain.ErrorNotSupported, "list_connection_profiles", "", "Connection profiles are unavailable")
		return
	}
	profiles, err := h.lineServices.ConnectionProfiles(
		request.Context(),
		strings.TrimSpace(request.PathValue("id")),
	)
	if err != nil {
		h.writeError(response, err, "")
		return
	}
	if profiles == nil {
		profiles = []domain.ConnectionProfile{}
	}
	h.writeJSON(response, http.StatusOK, connectionProfilesResponse{Profiles: profiles})
}

func (h *handler) putConnectionProfile(response http.ResponseWriter, request *http.Request) {
	if h.lineServices == nil {
		h.writeAPIError(response, http.StatusNotImplemented, domain.ErrorNotSupported, "save_connection_profile", "", "Connection profiles are unavailable")
		return
	}
	var input domain.SaveConnectionProfileRequest
	if err := decodeJSON(response, request, &input); err != nil {
		h.writeAPIError(response, http.StatusBadRequest, domain.ErrorInvalidArgument, "save_connection_profile", "", "invalid JSON request")
		return
	}
	input.LineID = strings.TrimSpace(request.PathValue("id"))
	profile, err := h.lineServices.SaveConnectionProfile(request.Context(), input)
	if err != nil {
		h.writeError(response, err, strings.TrimSpace(input.RequestID))
		return
	}
	h.writeJSON(response, http.StatusOK, connectionProfileResponse{Profile: profile})
}

func (h *handler) deleteConnectionProfile(response http.ResponseWriter, request *http.Request) {
	if h.lineServices == nil {
		h.writeAPIError(response, http.StatusNotImplemented, domain.ErrorNotSupported, "delete_connection_profile", "", "Connection profiles are unavailable")
		return
	}
	var input domain.DeleteConnectionProfileRequest
	if err := decodeJSON(response, request, &input); err != nil {
		h.writeAPIError(response, http.StatusBadRequest, domain.ErrorInvalidArgument, "delete_connection_profile", "", "invalid JSON request")
		return
	}
	input.LineID = strings.TrimSpace(request.PathValue("id"))
	receipt, err := h.lineServices.DeleteConnectionProfile(request.Context(), input)
	if err != nil {
		h.writeError(response, err, strings.TrimSpace(input.RequestID))
		return
	}
	h.writeJSON(response, http.StatusOK, commandReceiptResponse{Receipt: receipt})
}

func (h *handler) getUSSDStatus(response http.ResponseWriter, request *http.Request) {
	if h.lineServices == nil {
		h.writeAPIError(response, http.StatusNotImplemented, domain.ErrorNotSupported, "read_ussd_status", "", "USSD is unavailable")
		return
	}
	status, err := h.lineServices.USSDStatus(
		request.Context(),
		strings.TrimSpace(request.PathValue("id")),
	)
	if err != nil {
		h.writeError(response, err, "")
		return
	}
	h.writeJSON(response, http.StatusOK, ussdStatusResponse{USSD: status})
}

func (h *handler) postUSSDCommand(response http.ResponseWriter, request *http.Request) {
	if h.lineServices == nil {
		h.writeAPIError(response, http.StatusNotImplemented, domain.ErrorNotSupported, "ussd_command", "", "USSD is unavailable")
		return
	}
	var input domain.USSDRequest
	if err := decodeJSON(response, request, &input); err != nil {
		h.writeAPIError(response, http.StatusBadRequest, domain.ErrorInvalidArgument, "ussd_command", "", "invalid JSON request")
		return
	}
	input.LineID = strings.TrimSpace(request.PathValue("id"))
	result, err := h.lineServices.USSDCommand(request.Context(), input)
	if err != nil {
		h.writeError(response, err, strings.TrimSpace(input.RequestID))
		return
	}
	h.writeJSON(response, http.StatusOK, ussdCommandResponse{Result: result})
}
