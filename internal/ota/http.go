package ota

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/human-agent65535/modemdeck/internal/updatecheck"
)

const maximumRequestBytes = 16 << 10

type Handler struct {
	controller *Controller
	token      string
}

func NewHandler(controller *Controller, token string) (*Handler, error) {
	if controller == nil || strings.TrimSpace(token) == "" {
		return nil, errors.New("updater controller and authentication token are required")
	}
	return &Handler{controller: controller, token: strings.TrimSpace(token)}, nil
}

func (handler *Handler) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	response.Header().Set("Cache-Control", "no-store")
	response.Header().Set("X-Content-Type-Options", "nosniff")
	if request.URL.Path == "/health" {
		if request.Method != http.MethodGet {
			methodNotAllowed(response, http.MethodGet)
			return
		}
		writeJSON(response, http.StatusOK, map[string]string{"status": "ok"})
		return
	}
	if !handler.authorized(request) {
		writeError(response, http.StatusUnauthorized, "unauthorized")
		return
	}
	switch request.URL.Path {
	case "/v1/updates/check":
		if request.Method != http.MethodGet {
			methodNotAllowed(response, http.MethodGet)
			return
		}
		writeJSON(response, http.StatusOK, handler.controller.Check(request.Context()))
	case "/v1/updates/status":
		if request.Method != http.MethodGet {
			methodNotAllowed(response, http.MethodGet)
			return
		}
		operation, err := handler.controller.Status()
		if errors.Is(err, ErrNoOperation) {
			writeError(response, http.StatusNotFound, "operation_not_found")
			return
		}
		if err != nil {
			writeError(response, http.StatusServiceUnavailable, "operation_unavailable")
			return
		}
		writeJSON(response, http.StatusOK, operation)
	case "/v1/updates/apply":
		if request.Method != http.MethodPost {
			methodNotAllowed(response, http.MethodPost)
			return
		}
		var input updatecheck.ApplyRequest
		decoder := json.NewDecoder(io.LimitReader(request.Body, maximumRequestBytes))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&input); err != nil {
			writeError(response, http.StatusBadRequest, "invalid_request")
			return
		}
		operation, err := handler.controller.Apply(request.Context(), input)
		switch {
		case errors.Is(err, ErrHardwareConfirmationRequired):
			writeError(response, http.StatusConflict, "hardware_confirmation_required")
		case errors.Is(err, ErrOperationRunning):
			writeError(response, http.StatusConflict, "update_in_progress")
		case errors.Is(err, ErrNoUpdate), errors.Is(err, ErrVersionChanged):
			writeError(response, http.StatusConflict, "update_not_available")
		case err != nil:
			writeError(response, http.StatusServiceUnavailable, "updater_unavailable")
		default:
			writeJSON(response, http.StatusAccepted, operation)
		}
	default:
		writeError(response, http.StatusNotFound, "not_found")
	}
}

func (handler *Handler) authorized(request *http.Request) bool {
	provided := strings.TrimSpace(strings.TrimPrefix(request.Header.Get("Authorization"), "Bearer "))
	if len(provided) != len(handler.token) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(provided), []byte(handler.token)) == 1
}

func methodNotAllowed(response http.ResponseWriter, allowed string) {
	response.Header().Set("Allow", allowed)
	writeError(response, http.StatusMethodNotAllowed, "method_not_allowed")
}

func writeError(response http.ResponseWriter, status int, code string) {
	writeJSON(response, status, map[string]string{"code": code})
}

func writeJSON(response http.ResponseWriter, status int, value any) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(value)
}
