package httpapi

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/human-agent65535/modemdeck/agent/internal/domain"
)

const networkScanWriteTimeout = 125 * time.Second

func (h *handler) postNetworkScan(writer http.ResponseWriter, request *http.Request) {
	if h.networkSelection == nil {
		h.writeAPIError(
			writer,
			http.StatusNotImplemented,
			domain.ErrorNotSupported,
			"scan_networks",
			"",
			"network selection is unavailable",
		)
		return
	}
	var input domain.NetworkScanRequest
	if err := decodeJSON(writer, request, &input); err != nil {
		h.writeAPIError(
			writer,
			http.StatusBadRequest,
			domain.ErrorInvalidArgument,
			"scan_networks",
			"",
			"invalid JSON request",
		)
		return
	}
	requestID, ok := normalizeRequestID(input.RequestID)
	if !ok {
		h.writeAPIError(
			writer,
			http.StatusBadRequest,
			domain.ErrorInvalidArgument,
			"scan_networks",
			"",
			"request_id is required and must be valid",
		)
		return
	}
	input.RequestID = requestID
	input.LineID = strings.TrimSpace(request.PathValue("id"))

	// A cellular network scan may legitimately exceed the server's ordinary
	// 60-second write timeout. Extend only this response; all other routes keep
	// the server default.
	if err := http.NewResponseController(writer).SetWriteDeadline(
		time.Now().Add(networkScanWriteTimeout),
	); err != nil && !errors.Is(err, http.ErrNotSupported) {
		h.writeError(
			writer,
			domain.Unavailable("scan_networks", "failed to extend the scan response deadline", err),
			requestID,
		)
		return
	}

	result, err := h.networkSelection.ScanNetworks(request.Context(), input)
	if err != nil {
		h.writeError(writer, err, requestID)
		return
	}
	h.writeJSON(writer, http.StatusOK, result)
}

func (h *handler) putNetworkSelection(writer http.ResponseWriter, request *http.Request) {
	if h.networkSelection == nil {
		h.writeAPIError(
			writer,
			http.StatusNotImplemented,
			domain.ErrorNotSupported,
			"set_network_selection",
			"",
			"network selection is unavailable",
		)
		return
	}
	var input domain.ApplyNetworkSelectionRequest
	if err := decodeJSON(writer, request, &input); err != nil {
		h.writeAPIError(
			writer,
			http.StatusBadRequest,
			domain.ErrorInvalidArgument,
			"set_network_selection",
			"",
			"invalid JSON request",
		)
		return
	}
	requestID, ok := normalizeRequestID(input.RequestID)
	if !ok {
		h.writeAPIError(
			writer,
			http.StatusBadRequest,
			domain.ErrorInvalidArgument,
			"set_network_selection",
			"",
			"request_id is required and must be valid",
		)
		return
	}
	input.RequestID = requestID
	input.LineID = strings.TrimSpace(request.PathValue("id"))
	receipt, err := h.networkSelection.SetNetworkSelection(request.Context(), input)
	if err != nil {
		h.writeError(writer, err, requestID)
		return
	}
	h.writeJSON(writer, http.StatusOK, receipt)
}
