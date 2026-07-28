package httpapi

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/human-agent65535/modemdeck/agent/internal/domain"
)

func (h *handler) renewControlLease(w http.ResponseWriter, r *http.Request) {
	if h.controlLease == nil {
		h.writeAPIError(
			w,
			http.StatusNotImplemented,
			domain.ErrorNotSupported,
			"renew_control_lease",
			"",
			"control leases are unavailable",
		)
		return
	}
	status, err := h.controlLease.Renew(controllerID(r))
	if err != nil {
		h.writeError(w, err, "")
		return
	}
	h.writeJSON(w, http.StatusOK, status)
}

func (h *handler) releaseControlLease(w http.ResponseWriter, r *http.Request) {
	if h.controlLease == nil {
		h.writeAPIError(
			w,
			http.StatusNotImplemented,
			domain.ErrorNotSupported,
			"release_control_lease",
			"",
			"control leases are unavailable",
		)
		return
	}
	releaseContext, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := h.controlLease.Release(releaseContext, controllerID(r)); err != nil {
		h.writeError(w, err, "")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *handler) requireControlLease(
	w http.ResponseWriter,
	r *http.Request,
	requestID string,
) bool {
	if h.controlLease == nil {
		return true
	}
	if err := h.controlLease.Require(controllerID(r)); err != nil {
		h.writeError(w, err, requestID)
		return false
	}
	return true
}

func controllerID(r *http.Request) string {
	return strings.TrimSpace(r.Header.Get(domain.ControlLeaseHeader))
}
