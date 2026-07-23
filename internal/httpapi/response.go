package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/human-agent65535/modemdeck/internal/store"
)

type healthResponse struct {
	Status string `json:"status"`
}

type bootstrapResponse struct {
	Capabilities Capabilities        `json:"capabilities"`
	Lines        []store.LineSummary `json:"lines"`
}

type responseMeta struct {
	Limit int `json:"limit"`
}

type contactsResponse struct {
	Contacts []store.Contact `json:"contacts"`
	Meta     responseMeta    `json:"meta"`
}

type threadsResponse struct {
	Threads []store.MessageThread `json:"threads"`
	Meta    responseMeta          `json:"meta"`
}

type messagesResponse struct {
	Messages []store.Message `json:"messages"`
	Meta     responseMeta    `json:"meta"`
}

type callsResponse struct {
	Calls []store.Call `json:"calls"`
	Meta  responseMeta `json:"meta"`
}

type devicesResponse struct {
	Devices []store.Device `json:"devices"`
	Meta    responseMeta   `json:"meta"`
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
