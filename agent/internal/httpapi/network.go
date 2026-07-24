package httpapi

import (
	"net/http"

	"github.com/human-agent65535/modemdeck/agent/internal/domain"
)

func (h *handler) getNetwork(writer http.ResponseWriter, request *http.Request) {
	if h.network == nil {
		h.writeError(
			writer,
			domain.NotSupported("network_snapshot", "network service is unavailable"),
			"",
		)
		return
	}
	snapshot, err := h.network.NetworkSnapshot(request.Context())
	if err != nil {
		h.writeError(writer, err, "")
		return
	}
	h.writeJSON(writer, http.StatusOK, snapshot)
}

func (h *handler) putProxies(writer http.ResponseWriter, request *http.Request) {
	if h.network == nil {
		h.writeError(
			writer,
			domain.NotSupported("apply_proxies", "proxy service is unavailable"),
			"",
		)
		return
	}
	var desired domain.ProxyDesiredSet
	if err := decodeJSON(writer, request, &desired); err != nil {
		h.writeAPIError(
			writer,
			http.StatusBadRequest,
			domain.ErrorInvalidArgument,
			"apply_proxies",
			"",
			"invalid JSON request",
		)
		return
	}
	if desired.Proxies == nil {
		h.writeAPIError(
			writer,
			http.StatusBadRequest,
			domain.ErrorInvalidArgument,
			"apply_proxies",
			"",
			"proxies is required",
		)
		return
	}
	snapshot, err := h.network.ApplyProxySet(request.Context(), desired)
	if err != nil {
		h.writeError(writer, err, "")
		return
	}
	h.writeJSON(writer, http.StatusOK, snapshot)
}
