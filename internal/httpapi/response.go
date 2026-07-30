package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/human-agent65535/modemdeck/internal/store"
)

type sessionResponse struct {
	Authenticated      bool     `json:"authenticated"`
	SetupRequired      bool     `json:"setup_required"`
	UserID             string   `json:"user_id,omitempty"`
	Username           string   `json:"username,omitempty"`
	Role               string   `json:"role,omitempty"`
	ProfileContactID   string   `json:"profile_contact_id,omitempty"`
	MustChangePassword bool     `json:"must_change_password,omitempty"`
	IOSPairingEnabled  bool     `json:"ios_pairing_enabled"`
	AllowedLineIDs     []string `json:"allowed_line_ids,omitempty"`
	CSRFToken          string   `json:"csrf_token,omitempty"`
	Language           string   `json:"language"`
}

type bootstrapResponse struct {
	Capabilities   Capabilities          `json:"capabilities"`
	Lines          []lineSummaryResponse `json:"lines"`
	LineCatalog    []lineSummaryResponse `json:"line_catalog"`
	LineSettings   store.LineSettings    `json:"line_settings"`
	SystemSettings store.SystemSettings  `json:"system_settings"`
}

type lineSettingsResponse struct {
	Settings store.LineSettings `json:"settings"`
}

type systemSettingsResponse struct {
	Settings store.SystemSettings `json:"settings"`
}

type responseMeta struct {
	Limit      int    `json:"limit"`
	NextCursor string `json:"next_cursor"`
	HasMore    bool   `json:"has_more"`
}

type contactsResponse struct {
	Contacts []store.Contact `json:"contacts"`
	Meta     responseMeta    `json:"meta"`
}

type contactResponse struct {
	Contact store.Contact `json:"contact"`
}

type threadsResponse struct {
	Threads []messageThreadResponse `json:"threads"`
	Meta    responseMeta            `json:"meta"`
}

type messageResponse struct {
	Message messageResponseItem `json:"message"`
}

type messagesResponse struct {
	Messages []messageResponseItem `json:"messages"`
	Meta     responseMeta          `json:"meta"`
}

type callSessionResponse struct {
	ID             string  `json:"id"`
	LineID         string  `json:"line_id"`
	Direction      string  `json:"direction"`
	RemoteNumber   string  `json:"remote_number"`
	DisplayName    string  `json:"display_name,omitempty"`
	Phase          string  `json:"phase"`
	CreatedAt      string  `json:"created_at"`
	ActiveAt       *string `json:"active_at,omitempty"`
	EndedAt        string  `json:"ended_at,omitempty"`
	FailureReason  string  `json:"failure_reason,omitempty"`
	Bearer         string  `json:"bearer,omitempty"`
	MediaAvailable bool    `json:"media_available"`
	ControlState   string  `json:"control_state"`
}

type callSessionEnvelope struct {
	Call callSessionResponse `json:"call"`
}

type activeCallsResponse struct {
	Calls        []callSessionResponse             `json:"calls"`
	Reservations []outgoingCallReservationResponse `json:"reservations"`
}

type outgoingCallReservationResponse struct {
	RequestID    string `json:"request_id"`
	LineID       string `json:"line_id"`
	ControlState string `json:"control_state"`
	CreatedAt    string `json:"created_at"`
}

type callsResponse struct {
	Calls []callRecordResponse `json:"calls"`
	Meta  responseMeta         `json:"meta"`
}

type devicesResponse struct {
	Devices []store.Device `json:"devices"`
}

type errorResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Field   string `json:"field,omitempty"`
}

func writeJSON(response http.ResponseWriter, status int, body any) {
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.Header().Set("Cache-Control", "no-store")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(body)
}

func writeError(response http.ResponseWriter, status int, code, message, field string) {
	writeJSON(response, status, errorResponse{
		Code:    code,
		Message: message,
		Field:   field,
	})
}
