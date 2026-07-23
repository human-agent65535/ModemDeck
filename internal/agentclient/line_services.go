package agentclient

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type SIMStatus struct {
	LineID             string            `json:"line_id"`
	Present            bool              `json:"present"`
	Active             bool              `json:"active"`
	Identifier         string            `json:"identifier"`
	IMSI               string            `json:"imsi"`
	EID                string            `json:"eid,omitempty"`
	OperatorIdentifier string            `json:"operator_identifier"`
	OperatorName       string            `json:"operator_name"`
	UnlockRequired     string            `json:"unlock_required"`
	UnlockRequiredCode uint32            `json:"unlock_required_code"`
	UnlockRetries      map[string]uint32 `json:"unlock_retries"`
	ObservedAt         time.Time         `json:"observed_at"`
}

type SIMOperation string

const (
	SIMSendPIN   SIMOperation = "send_pin"
	SIMSendPUK   SIMOperation = "send_puk"
	SIMEnablePIN SIMOperation = "enable_pin"
	SIMChangePIN SIMOperation = "change_pin"
)

type SIMCommandRequest struct {
	RequestID string       `json:"request_id"`
	Operation SIMOperation `json:"operation"`
	PIN       string       `json:"pin,omitempty"`
	PUK       string       `json:"puk,omitempty"`
	NewPIN    string       `json:"new_pin,omitempty"`
	Enabled   *bool        `json:"enabled,omitempty"`
}

type ConnectionProfile struct {
	ProfileID            int32  `json:"profile_id"`
	ProfileName          string `json:"profile_name"`
	APN                  string `json:"apn"`
	IPFamily             string `json:"ip_family"`
	IPType               uint32 `json:"ip_type"`
	APNType              uint32 `json:"apn_type"`
	AllowedAuth          uint32 `json:"allowed_auth"`
	User                 string `json:"user,omitempty"`
	AccessTypePreference uint32 `json:"access_type_preference"`
	RoamingAllowance     uint32 `json:"roaming_allowance"`
	ProfileSource        uint32 `json:"profile_source"`
}

type SaveConnectionProfileRequest struct {
	RequestID            string `json:"request_id"`
	ProfileID            *int32 `json:"profile_id,omitempty"`
	ProfileName          string `json:"profile_name,omitempty"`
	APN                  string `json:"apn,omitempty"`
	IPFamily             string `json:"ip_family,omitempty"`
	APNType              uint32 `json:"apn_type,omitempty"`
	AllowedAuth          uint32 `json:"allowed_auth,omitempty"`
	User                 string `json:"user,omitempty"`
	Password             string `json:"password,omitempty"`
	AccessTypePreference uint32 `json:"access_type_preference,omitempty"`
	RoamingAllowance     uint32 `json:"roaming_allowance,omitempty"`
}

type DeleteConnectionProfileRequest struct {
	RequestID   string `json:"request_id"`
	ProfileID   *int32 `json:"profile_id,omitempty"`
	ProfileName string `json:"profile_name,omitempty"`
}

type USSDStatus struct {
	LineID              string    `json:"line_id"`
	State               string    `json:"state"`
	StateCode           uint32    `json:"state_code"`
	NetworkNotification string    `json:"network_notification,omitempty"`
	NetworkRequest      string    `json:"network_request,omitempty"`
	ObservedAt          time.Time `json:"observed_at"`
}

type USSDAction string

const (
	USSDInitiate USSDAction = "initiate"
	USSDRespond  USSDAction = "respond"
	USSDCancel   USSDAction = "cancel"
)

type USSDRequest struct {
	RequestID string     `json:"request_id"`
	Action    USSDAction `json:"action"`
	Command   string     `json:"command,omitempty"`
}

type USSDResponse struct {
	Response string `json:"response,omitempty"`
}

func (client *Client) SIMStatus(ctx context.Context, lineID string) (SIMStatus, error) {
	path, ok := lineServicePath(lineID, "sim")
	if !ok {
		return SIMStatus{}, ErrInvalidRequest
	}
	var response struct {
		SIM SIMStatus `json:"sim"`
	}
	if err := client.doJSON(ctx, http.MethodGet, path, nil, http.StatusOK, &response); err != nil {
		return SIMStatus{}, err
	}
	if response.SIM.UnlockRetries == nil {
		response.SIM.UnlockRetries = map[string]uint32{}
	}
	return response.SIM, nil
}

func (client *Client) SIMCommand(
	ctx context.Context,
	lineID string,
	request SIMCommandRequest,
) (CommandReceipt, error) {
	path, ok := lineServicePath(lineID, "sim/commands")
	if !ok || strings.TrimSpace(request.RequestID) == "" ||
		strings.TrimSpace(string(request.Operation)) == "" {
		return CommandReceipt{}, ErrInvalidRequest
	}
	var response struct {
		Receipt CommandReceipt `json:"receipt"`
	}
	if err := client.doJSON(ctx, http.MethodPost, path, request, http.StatusOK, &response); err != nil {
		return CommandReceipt{}, err
	}
	return response.Receipt, nil
}

func (client *Client) ConnectionProfiles(
	ctx context.Context,
	lineID string,
) ([]ConnectionProfile, error) {
	path, ok := lineServicePath(lineID, "profiles")
	if !ok {
		return nil, ErrInvalidRequest
	}
	var response struct {
		Profiles []ConnectionProfile `json:"profiles"`
	}
	if err := client.doJSON(ctx, http.MethodGet, path, nil, http.StatusOK, &response); err != nil {
		return nil, err
	}
	if response.Profiles == nil {
		response.Profiles = []ConnectionProfile{}
	}
	return response.Profiles, nil
}

func (client *Client) SaveConnectionProfile(
	ctx context.Context,
	lineID string,
	request SaveConnectionProfileRequest,
) (ConnectionProfile, error) {
	path, ok := lineServicePath(lineID, "profiles")
	if !ok || strings.TrimSpace(request.RequestID) == "" {
		return ConnectionProfile{}, ErrInvalidRequest
	}
	var response struct {
		Profile ConnectionProfile `json:"profile"`
	}
	if err := client.doJSON(ctx, http.MethodPut, path, request, http.StatusOK, &response); err != nil {
		return ConnectionProfile{}, err
	}
	return response.Profile, nil
}

func (client *Client) DeleteConnectionProfile(
	ctx context.Context,
	lineID string,
	request DeleteConnectionProfileRequest,
) (CommandReceipt, error) {
	path, ok := lineServicePath(lineID, "profiles")
	if !ok || strings.TrimSpace(request.RequestID) == "" {
		return CommandReceipt{}, ErrInvalidRequest
	}
	var response struct {
		Receipt CommandReceipt `json:"receipt"`
	}
	if err := client.doJSON(ctx, http.MethodDelete, path, request, http.StatusOK, &response); err != nil {
		return CommandReceipt{}, err
	}
	return response.Receipt, nil
}

func (client *Client) USSDStatus(ctx context.Context, lineID string) (USSDStatus, error) {
	path, ok := lineServicePath(lineID, "ussd")
	if !ok {
		return USSDStatus{}, ErrInvalidRequest
	}
	var response struct {
		USSD USSDStatus `json:"ussd"`
	}
	if err := client.doJSON(ctx, http.MethodGet, path, nil, http.StatusOK, &response); err != nil {
		return USSDStatus{}, err
	}
	return response.USSD, nil
}

func (client *Client) USSDCommand(
	ctx context.Context,
	lineID string,
	request USSDRequest,
) (USSDResponse, error) {
	path, ok := lineServicePath(lineID, "ussd")
	if !ok || strings.TrimSpace(request.RequestID) == "" ||
		strings.TrimSpace(string(request.Action)) == "" {
		return USSDResponse{}, ErrInvalidRequest
	}
	var response struct {
		Result USSDResponse `json:"result"`
	}
	if err := client.doJSON(ctx, http.MethodPost, path, request, http.StatusOK, &response); err != nil {
		return USSDResponse{}, err
	}
	return response.Result, nil
}

func lineServicePath(lineID, resource string) (string, bool) {
	lineID = strings.TrimSpace(lineID)
	resource = strings.Trim(resource, "/")
	if lineID == "" || resource == "" {
		return "", false
	}
	return "/v1/lines/" + url.PathEscape(lineID) + "/" + resource, true
}
