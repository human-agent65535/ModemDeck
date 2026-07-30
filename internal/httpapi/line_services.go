package httpapi

import (
	"net/http"
	"strings"

	"github.com/human-agent65535/modemdeck/internal/agentclient"
)

type simStatusEnvelope struct {
	SIM agentclient.SIMStatus `json:"sim"`
}

type lineCommandReceiptEnvelope struct {
	Receipt agentclient.CommandReceipt `json:"receipt"`
}

type connectionProfilesEnvelope struct {
	Profiles []agentclient.ConnectionProfile `json:"profiles"`
}

type connectionProfileEnvelope struct {
	Profile agentclient.ConnectionProfile `json:"profile"`
}

type ussdStatusEnvelope struct {
	USSD agentclient.USSDStatus `json:"ussd"`
}

type ussdResultEnvelope struct {
	Result agentclient.USSDResponse `json:"result"`
}

func (api *API) lineServiceResource(
	response http.ResponseWriter,
	request *http.Request,
	lineID string,
	resource string,
) {
	if api.lineServices == nil {
		writeError(
			response,
			http.StatusServiceUnavailable,
			"line_service_unavailable",
			"Line services are unavailable",
			"",
		)
		return
	}
	if !api.requireLineAccess(response, request, lineID) {
		return
	}
	switch resource {
	case "sim":
		api.simStatus(response, request, lineID)
	case "sim/commands":
		api.simCommand(response, request, lineID)
	case "profiles":
		api.connectionProfiles(response, request, lineID)
	case "ussd":
		api.ussd(response, request, lineID)
	default:
		writeError(response, http.StatusNotFound, "not_found", "API endpoint was not found", "")
	}
}

func (api *API) simStatus(
	response http.ResponseWriter,
	request *http.Request,
	lineID string,
) {
	if request.Method != http.MethodGet {
		response.Header().Set("Allow", http.MethodGet)
		writeError(response, http.StatusMethodNotAllowed, "method_not_allowed", "Only GET is supported", "")
		return
	}
	status, err := api.lineServices.SIMStatus(request.Context(), lineID)
	if err != nil {
		api.writeCommunicationError(response, request, "read SIM status", err)
		return
	}
	writeJSON(response, http.StatusOK, simStatusEnvelope{SIM: status})
}

func (api *API) simCommand(
	response http.ResponseWriter,
	request *http.Request,
	lineID string,
) {
	if request.Method != http.MethodPost {
		response.Header().Set("Allow", http.MethodPost)
		writeError(response, http.StatusMethodNotAllowed, "method_not_allowed", "Only POST is supported", "")
		return
	}
	var input agentclient.SIMCommandRequest
	if !decodeJSONBody(response, request, &input) {
		return
	}
	requestID, ok := commandRequestID(response, request, input.RequestID)
	if !ok {
		return
	}
	input.RequestID = requestID
	receipt, err := api.lineServices.SIMCommand(request.Context(), lineID, input)
	if err != nil {
		api.writeCommunicationError(response, request, "control SIM", err)
		return
	}
	api.logger.Info(
		"SIM command completed",
		"line_id",
		lineID,
		"operation",
		input.Operation,
		"request_id",
		receipt.RequestID,
	)
	writeJSON(response, http.StatusOK, lineCommandReceiptEnvelope{Receipt: receipt})
}

func (api *API) connectionProfiles(
	response http.ResponseWriter,
	request *http.Request,
	lineID string,
) {
	switch request.Method {
	case http.MethodGet:
		profiles, err := api.lineServices.ConnectionProfiles(request.Context(), lineID)
		if err != nil {
			api.writeCommunicationError(response, request, "list connection profiles", err)
			return
		}
		if profiles == nil {
			profiles = []agentclient.ConnectionProfile{}
		}
		writeJSON(response, http.StatusOK, connectionProfilesEnvelope{Profiles: profiles})
	case http.MethodPut:
		var input agentclient.SaveConnectionProfileRequest
		if !decodeJSONBody(response, request, &input) {
			return
		}
		requestID, ok := commandRequestID(response, request, input.RequestID)
		if !ok {
			return
		}
		input.RequestID = requestID
		profile, err := api.lineServices.SaveConnectionProfile(request.Context(), lineID, input)
		if err != nil {
			api.writeCommunicationError(response, request, "save connection profile", err)
			return
		}
		api.logger.Info(
			"connection profile saved",
			"line_id",
			lineID,
			"profile_id",
			profile.ProfileID,
			"request_id",
			requestID,
		)
		writeJSON(response, http.StatusOK, connectionProfileEnvelope{Profile: profile})
	case http.MethodDelete:
		var input agentclient.DeleteConnectionProfileRequest
		if !decodeJSONBody(response, request, &input) {
			return
		}
		requestID, ok := commandRequestID(response, request, input.RequestID)
		if !ok {
			return
		}
		input.RequestID = requestID
		receipt, err := api.lineServices.DeleteConnectionProfile(request.Context(), lineID, input)
		if err != nil {
			api.writeCommunicationError(response, request, "delete connection profile", err)
			return
		}
		api.logger.Info(
			"connection profile deleted",
			"line_id",
			lineID,
			"request_id",
			receipt.RequestID,
		)
		writeJSON(response, http.StatusOK, lineCommandReceiptEnvelope{Receipt: receipt})
	default:
		response.Header().Set("Allow", http.MethodGet+", "+http.MethodPut+", "+http.MethodDelete)
		writeError(
			response,
			http.StatusMethodNotAllowed,
			"method_not_allowed",
			"Only GET, PUT, and DELETE are supported",
			"",
		)
	}
}

func (api *API) ussd(
	response http.ResponseWriter,
	request *http.Request,
	lineID string,
) {
	switch request.Method {
	case http.MethodGet:
		status, err := api.lineServices.USSDStatus(request.Context(), lineID)
		if err != nil {
			api.writeCommunicationError(response, request, "read USSD status", err)
			return
		}
		writeJSON(response, http.StatusOK, ussdStatusEnvelope{USSD: status})
	case http.MethodPost:
		var input agentclient.USSDRequest
		if !decodeJSONBody(response, request, &input) {
			return
		}
		requestID, ok := commandRequestID(response, request, input.RequestID)
		if !ok {
			return
		}
		input.RequestID = requestID
		result, err := api.lineServices.USSDCommand(request.Context(), lineID, input)
		if err != nil {
			api.writeCommunicationError(response, request, "send USSD command", err)
			return
		}
		api.logger.Info(
			"USSD command completed",
			"line_id",
			lineID,
			"action",
			input.Action,
			"request_id",
			requestID,
		)
		writeJSON(response, http.StatusOK, ussdResultEnvelope{Result: result})
	default:
		response.Header().Set("Allow", http.MethodGet+", "+http.MethodPost)
		writeError(
			response,
			http.StatusMethodNotAllowed,
			"method_not_allowed",
			"Only GET and POST are supported",
			"",
		)
	}
}

func lineServiceResource(path string) (lineID string, resource string, ok bool) {
	const prefix = "/api/v1/devices/"
	if !strings.HasPrefix(path, prefix) {
		return "", "", false
	}
	parts := strings.Split(strings.TrimPrefix(path, prefix), "/")
	if len(parts) < 2 || len(parts) > 3 ||
		strings.TrimSpace(parts[0]) == "" ||
		len(parts[0]) > maxIdentifierLength {
		return "", "", false
	}
	lineID = parts[0]
	resource = strings.Join(parts[1:], "/")
	switch resource {
	case "sim", "sim/commands", "profiles", "ussd":
		return lineID, resource, true
	default:
		return "", "", false
	}
}
