package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/human-agent65535/modemdeck/internal/store"
)

type sessionResponse struct {
	Authenticated bool   `json:"authenticated"`
	SetupRequired bool   `json:"setup_required"`
	Username      string `json:"username,omitempty"`
	CSRFToken     string `json:"csrf_token,omitempty"`
	Language      string `json:"language"`
}

type bootstrapResponse struct {
	Capabilities   Capabilities          `json:"capabilities"`
	Lines          []lineSummaryResponse `json:"lines"`
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
	Limit int `json:"limit"`
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
}

type callSessionEnvelope struct {
	Call callSessionResponse `json:"call"`
}

type activeCallsResponse struct {
	Calls []callSessionResponse `json:"calls"`
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
