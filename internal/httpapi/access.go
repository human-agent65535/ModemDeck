package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/human-agent65535/modemdeck/internal/auth"
	"github.com/human-agent65535/modemdeck/internal/store"
)

type callLineRepository interface {
	CallLineID(context.Context, string) (string, error)
}

func (api *API) requireLineAccess(
	response http.ResponseWriter,
	request *http.Request,
	lineID string,
) bool {
	if canAccessLine(request.Context(), lineID) {
		return true
	}
	writeError(response, http.StatusNotFound, "line_not_found", "Line was not found", "")
	return false
}

func canAccessLine(ctx context.Context, lineID string) bool {
	principal, exists := auth.PrincipalFromContext(ctx)
	return !exists || principal.CanAccessLine(strings.TrimSpace(lineID))
}

func (api *API) requireCallAccess(
	response http.ResponseWriter,
	request *http.Request,
	callID string,
) bool {
	principal, exists := auth.PrincipalFromContext(request.Context())
	if !exists {
		return true
	}
	repository, ok := api.repository.(callLineRepository)
	if !ok {
		writeError(response, http.StatusNotFound, "call_not_found", "Call was not found", "")
		return false
	}
	lineID, err := repository.CallLineID(request.Context(), callID)
	if errors.Is(err, store.ErrCallNotFound) || err == nil && !principal.CanAccessLine(lineID) {
		writeError(response, http.StatusNotFound, "call_not_found", "Call was not found", "")
		return false
	}
	if err != nil {
		api.writeInternalError(response, request, "authorize call", err)
		return false
	}
	return true
}

func filterLinesForPrincipal(
	ctx context.Context,
	lines []store.LineSummary,
) []store.LineSummary {
	principal, exists := auth.PrincipalFromContext(ctx)
	if !exists {
		return lines
	}
	result := make([]store.LineSummary, 0, len(lines))
	for _, line := range lines {
		if !principal.CanAccessLine(line.ID) {
			continue
		}
		result = append(result, line)
	}
	return result
}

func filterDevicesForPrincipal(
	ctx context.Context,
	devices []store.Device,
) []store.Device {
	principal, exists := auth.PrincipalFromContext(ctx)
	if !exists {
		return devices
	}
	result := make([]store.Device, 0, len(devices))
	for _, device := range devices {
		if device.SIM == nil || !principal.CanAccessLine(device.SIM.LineID) {
			continue
		}
		result = append(result, device)
	}
	return result
}
