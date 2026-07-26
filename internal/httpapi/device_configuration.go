package httpapi

import (
	"net/http"
	"strings"

	"github.com/human-agent65535/modemdeck/internal/agentclient"
	"github.com/human-agent65535/modemdeck/internal/store"
)

const setIncomingCallPolicyOperation agentclient.DeviceConfigurationOperation = "set_incoming_call_policy"

type callPolicyEnforcement struct {
	Mode                   string `json:"mode"`
	MaxSubmissionsPerCall  int    `json:"max_submissions_per_call"`
	NewIncomingRingingOnly bool   `json:"new_incoming_ringing_only"`
	Available              bool   `json:"available"`
	ConfigOnly             bool   `json:"config_only"`
	Reason                 string `json:"reason,omitempty"`
}

type globalCallSettingsResponse struct {
	ReceiveCalls bool  `json:"receive_calls"`
	Revision     int64 `json:"revision"`
}

type lineIncomingCallConfiguration struct {
	Policy             store.LineCallPolicyValue      `json:"policy"`
	Revision           int64                          `json:"revision"`
	EffectivePolicy    store.EffectiveCallPolicyValue `json:"effective_policy"`
	GlobalReceiveCalls bool                           `json:"global_receive_calls"`
	GlobalRevision     int64                          `json:"global_revision"`
	UpdatedAt          string                         `json:"updated_at"`
	Enforcement        callPolicyEnforcement          `json:"enforcement"`
	LastAction         *incomingCallActionResponse    `json:"last_action,omitempty"`
}

type incomingCallActionResponse struct {
	CallID          string                         `json:"call_id"`
	EffectivePolicy store.EffectiveCallPolicyValue `json:"effective_policy"`
	Status          string                         `json:"status"`
	ErrorCode       string                         `json:"error_code,omitempty"`
	CreatedAt       string                         `json:"created_at"`
	UpdatedAt       string                         `json:"updated_at"`
}

type deviceConfigurationResponse struct {
	Hardware      *agentclient.DeviceConfiguration `json:"hardware,omitempty"`
	IncomingCalls *lineIncomingCallConfiguration   `json:"incoming_calls,omitempty"`
}

type updateGlobalCallSettingsRequest struct {
	ReceiveCalls     *bool `json:"receive_calls"`
	ExpectedRevision int64 `json:"expected_revision"`
}

type updateDeviceConfigurationRequest struct {
	RequestID              string                                   `json:"request_id"`
	Operation              agentclient.DeviceConfigurationOperation `json:"operation"`
	ExpectedDeviceRevision string                                   `json:"expected_device_revision"`
	ExpectedPolicyRevision int64                                    `json:"expected_policy_revision"`
	RadioEnabled           *bool                                    `json:"radio_enabled,omitempty"`
	APN                    string                                   `json:"apn,omitempty"`
	IPFamily               string                                   `json:"ip_family,omitempty"`
	VoLTEPolicy            string                                   `json:"volte_policy,omitempty"`
	IncomingCallPolicy     store.LineCallPolicyValue                `json:"incoming_call_policy,omitempty"`
}

func (api *API) callSettings(response http.ResponseWriter, request *http.Request) {
	if api.callPolicies == nil {
		writeError(
			response,
			http.StatusServiceUnavailable,
			"call_policy_unavailable",
			"Call policy configuration is unavailable",
			"",
		)
		return
	}
	switch request.Method {
	case http.MethodGet:
		settings, err := api.callPolicies.GlobalCallSettings(request.Context())
		if err != nil {
			api.writeCommunicationError(response, request, "read global call settings", err)
			return
		}
		writeJSON(response, http.StatusOK, globalCallSettingsEnvelope(settings))
	case http.MethodPatch:
		var input updateGlobalCallSettingsRequest
		if !decodeJSONBody(response, request, &input) {
			return
		}
		if input.ReceiveCalls == nil || input.ExpectedRevision <= 0 {
			writeError(
				response,
				http.StatusBadRequest,
				"invalid_argument",
				"receive_calls and a positive expected_revision are required",
				"",
			)
			return
		}
		settings, err := api.callPolicies.UpdateGlobalCallSettings(
			request.Context(),
			*input.ReceiveCalls,
			input.ExpectedRevision,
		)
		if err != nil {
			api.writeCommunicationError(response, request, "update global call settings", err)
			return
		}
		writeJSON(response, http.StatusOK, globalCallSettingsEnvelope(settings))
	default:
		response.Header().Set("Allow", http.MethodGet+", "+http.MethodPatch)
		writeError(
			response,
			http.StatusMethodNotAllowed,
			"method_not_allowed",
			"Only GET and PATCH are supported",
			"",
		)
	}
}

func (api *API) deviceConfiguration(
	response http.ResponseWriter,
	request *http.Request,
	lineID string,
) {
	if api.deviceConfigurations == nil || api.callPolicies == nil {
		writeError(
			response,
			http.StatusServiceUnavailable,
			"device_configuration_unavailable",
			"Device configuration is unavailable",
			"",
		)
		return
	}
	switch request.Method {
	case http.MethodGet:
		api.readDeviceConfiguration(response, request, lineID)
	case http.MethodPatch:
		api.updateDeviceConfiguration(response, request, lineID)
	default:
		response.Header().Set("Allow", http.MethodGet+", "+http.MethodPatch)
		writeError(
			response,
			http.StatusMethodNotAllowed,
			"method_not_allowed",
			"Only GET and PATCH are supported",
			"",
		)
	}
}

func (api *API) readDeviceConfiguration(
	response http.ResponseWriter,
	request *http.Request,
	lineID string,
) {
	hardware, err := api.deviceConfigurations.DeviceConfiguration(request.Context(), lineID)
	if err != nil {
		api.writeCommunicationError(response, request, "read device configuration", err)
		return
	}
	incomingCalls, err := api.lineIncomingCallConfiguration(request, lineID)
	if err != nil {
		api.writeCommunicationError(response, request, "read line call policy", err)
		return
	}
	writeJSON(response, http.StatusOK, deviceConfigurationResponse{
		Hardware:      &hardware,
		IncomingCalls: &incomingCalls,
	})
}

func (api *API) updateDeviceConfiguration(
	response http.ResponseWriter,
	request *http.Request,
	lineID string,
) {
	var input updateDeviceConfigurationRequest
	if !decodeJSONBody(response, request, &input) {
		return
	}
	if input.Operation == setIncomingCallPolicyOperation {
		if input.ExpectedPolicyRevision <= 0 ||
			!validIncomingCallPolicy(input.IncomingCallPolicy) {
			writeError(
				response,
				http.StatusBadRequest,
				"invalid_argument",
				"incoming_call_policy and a positive expected_policy_revision are required",
				"",
			)
			return
		}
		if input.ExpectedDeviceRevision != "" || input.RadioEnabled != nil ||
			input.APN != "" || input.IPFamily != "" || input.VoLTEPolicy != "" ||
			strings.TrimSpace(input.RequestID) != "" {
			writeError(
				response,
				http.StatusBadRequest,
				"invalid_argument",
				"set_incoming_call_policy accepts only incoming-call policy fields",
				"",
			)
			return
		}
		if _, err := api.callPolicies.UpdateLineCallPolicy(
			request.Context(),
			lineID,
			input.IncomingCallPolicy,
			input.ExpectedPolicyRevision,
		); err != nil {
			api.writeCommunicationError(response, request, "update line call policy", err)
			return
		}
		incomingCalls, err := api.lineIncomingCallConfiguration(request, lineID)
		if err != nil {
			api.writeCommunicationError(response, request, "read line call policy", err)
			return
		}
		writeJSON(response, http.StatusOK, deviceConfigurationResponse{
			IncomingCalls: &incomingCalls,
		})
		return
	}

	requestID, ok := commandRequestID(response, request, input.RequestID)
	if !ok {
		return
	}
	if strings.TrimSpace(input.ExpectedDeviceRevision) == "" ||
		!validHardwareConfigurationOperation(input.Operation) ||
		input.ExpectedPolicyRevision != 0 ||
		input.IncomingCallPolicy != "" {
		writeError(
			response,
			http.StatusBadRequest,
			"invalid_argument",
			"a supported operation and expected_device_revision are required",
			"",
		)
		return
	}
	hardware, err := api.deviceConfigurations.ApplyDeviceConfiguration(
		request.Context(),
		lineID,
		agentclient.ApplyDeviceConfigurationRequest{
			RequestID:        requestID,
			ExpectedRevision: input.ExpectedDeviceRevision,
			Operation:        input.Operation,
			RadioEnabled:     input.RadioEnabled,
			APN:              input.APN,
			IPFamily:         input.IPFamily,
			VoLTEPolicy:      input.VoLTEPolicy,
		},
	)
	if err != nil {
		api.writeCommunicationError(response, request, "apply device configuration", err)
		return
	}
	api.logger.Info(
		"device configuration applied",
		"line_id",
		lineID,
		"operation",
		input.Operation,
		"request_id",
		requestID,
	)
	writeJSON(response, http.StatusOK, deviceConfigurationResponse{Hardware: &hardware})
}

func (api *API) lineIncomingCallConfiguration(
	request *http.Request,
	lineID string,
) (lineIncomingCallConfiguration, error) {
	configuration, err := api.callPolicies.CallPolicyConfiguration(request.Context(), lineID)
	if err != nil {
		return lineIncomingCallConfiguration{}, err
	}
	lastAction, err := api.callPolicies.LatestIncomingCallAction(request.Context(), lineID)
	if err != nil {
		return lineIncomingCallConfiguration{}, err
	}
	available := api.lineRejectAvailable(request, lineID)
	enforcement := oneShotRejectEnforcement()
	enforcement.Available = available
	enforcement.ConfigOnly = !available
	if !available {
		enforcement.Reason = "the attached line does not currently advertise reject-call capability"
	}
	result := lineIncomingCallConfiguration{
		Policy:             configuration.Line.Policy,
		Revision:           configuration.Line.Revision,
		EffectivePolicy:    configuration.Effective.Policy,
		GlobalReceiveCalls: configuration.Global.ReceiveCalls,
		GlobalRevision:     configuration.Global.Revision,
		UpdatedAt:          configuration.Line.UpdatedAt,
		Enforcement:        enforcement,
	}
	if lastAction != nil {
		result.LastAction = &incomingCallActionResponse{
			CallID:          lastAction.CallID,
			EffectivePolicy: lastAction.EffectivePolicy,
			Status:          lastAction.Status,
			ErrorCode:       lastAction.ErrorCode,
			CreatedAt:       lastAction.CreatedAt,
			UpdatedAt:       lastAction.UpdatedAt,
		}
	}
	return result, nil
}

func (api *API) lineRejectAvailable(request *http.Request, lineID string) bool {
	if api.communications == nil {
		return false
	}
	status, err := api.communications.Status(request.Context())
	if err != nil {
		return false
	}
	for _, line := range status.Lines {
		if line.ID == lineID {
			return line.Capabilities.RejectCall
		}
	}
	return false
}

func globalCallSettingsEnvelope(settings store.GlobalCallSettings) globalCallSettingsResponse {
	return globalCallSettingsResponse{
		ReceiveCalls: settings.ReceiveCalls,
		Revision:     settings.Revision,
	}
}

func oneShotRejectEnforcement() callPolicyEnforcement {
	return callPolicyEnforcement{
		Mode:                   "one_shot_reject",
		MaxSubmissionsPerCall:  1,
		NewIncomingRingingOnly: true,
	}
}

func validIncomingCallPolicy(policy store.LineCallPolicyValue) bool {
	switch policy {
	case store.LineCallPolicyFollowGlobal,
		store.LineCallPolicyReceive,
		store.LineCallPolicyDND:
		return true
	default:
		return false
	}
}

func validHardwareConfigurationOperation(
	operation agentclient.DeviceConfigurationOperation,
) bool {
	switch operation {
	case agentclient.DeviceConfigurationSetRadioEnabled,
		agentclient.DeviceConfigurationConnectData,
		agentclient.DeviceConfigurationDisconnectData,
		agentclient.DeviceConfigurationSetVoLTEPolicy,
		agentclient.DeviceConfigurationRestartModem,
		agentclient.DeviceConfigurationResetUSB:
		return true
	default:
		return false
	}
}

func deviceConfigurationResourceID(path string) (string, bool) {
	const (
		prefix = "/api/v1/devices/"
		suffix = "/configuration"
	)
	if !strings.HasPrefix(path, prefix) || !strings.HasSuffix(path, suffix) {
		return "", false
	}
	lineID := strings.TrimSuffix(strings.TrimPrefix(path, prefix), suffix)
	if strings.TrimSpace(lineID) == "" ||
		strings.Contains(lineID, "/") ||
		len(lineID) > maxIdentifierLength {
		return "", false
	}
	return lineID, true
}
