package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"github.com/human-agent65535/modemdeck/internal/agentclient"
	"github.com/human-agent65535/modemdeck/internal/networkruntime"
)

type proxyCreateRequest struct {
	Revision      *int64                `json:"revision"`
	ID            string                `json:"id"`
	Name          string                `json:"name"`
	LineID        string                `json:"line_id"`
	Enabled       bool                  `json:"enabled"`
	Mode          agentclient.ProxyMode `json:"mode"`
	ListenAddress string                `json:"listen_address"`
	ListenPort    uint16                `json:"listen_port"`
	AuthEnabled   bool                  `json:"auth_enabled"`
	Username      string                `json:"username"`
	Password      string                `json:"password"`
}

type proxyUpdateRequest struct {
	Revision      *int64                 `json:"revision"`
	Name          *string                `json:"name"`
	LineID        *string                `json:"line_id"`
	Enabled       *bool                  `json:"enabled"`
	Mode          *agentclient.ProxyMode `json:"mode"`
	ListenAddress *string                `json:"listen_address"`
	ListenPort    *uint16                `json:"listen_port"`
	AuthEnabled   *bool                  `json:"auth_enabled"`
	Username      *string                `json:"username"`
	Password      *string                `json:"password"`
}

type proxyCollectionResponse struct {
	Proxies []networkruntime.Proxy `json:"proxies"`
}

func (api *API) networkStatus(response http.ResponseWriter, request *http.Request) {
	if !api.requireNetworkService(response) {
		return
	}
	status, err := api.network.Status(request.Context())
	if err != nil {
		api.writeNetworkError(response, request, "load network status", err)
		return
	}
	writeJSON(response, http.StatusOK, status)
}

func (api *API) proxyCollection(response http.ResponseWriter, request *http.Request) {
	if !api.requireNetworkService(response) {
		return
	}
	switch request.Method {
	case http.MethodGet:
		proxies, err := api.network.Proxies(request.Context())
		if err != nil {
			api.writeNetworkError(response, request, "list proxy settings", err)
			return
		}
		writeJSON(response, http.StatusOK, proxyCollectionResponse{Proxies: proxies})
	case http.MethodPost:
		var body proxyCreateRequest
		if !decodeJSONBody(response, request, &body) {
			return
		}
		if body.Revision == nil || *body.Revision != 0 {
			writeError(
				response,
				http.StatusBadRequest,
				"invalid_argument",
				"revision must be 0 when creating a proxy",
				"revision",
			)
			return
		}
		result, err := api.network.Create(request.Context(), networkruntime.CreateInput{
			ID:            body.ID,
			Name:          body.Name,
			LineID:        body.LineID,
			Enabled:       body.Enabled,
			Mode:          body.Mode,
			ListenAddress: body.ListenAddress,
			ListenPort:    body.ListenPort,
			AuthEnabled:   body.AuthEnabled,
			Username:      body.Username,
			Password:      body.Password,
		})
		if err != nil {
			api.writeNetworkError(response, request, "create proxy settings", err)
			return
		}
		writeJSON(response, http.StatusCreated, result)
	default:
		response.Header().Set("Allow", http.MethodGet+", "+http.MethodPost)
		writeError(
			response,
			http.StatusMethodNotAllowed,
			"method_not_allowed",
			"Only GET and POST are supported",
			"",
		)
	}
}

func (api *API) proxyResource(
	response http.ResponseWriter,
	request *http.Request,
	id string,
) {
	if !api.requireNetworkService(response) {
		return
	}
	switch request.Method {
	case http.MethodPatch:
		var body proxyUpdateRequest
		if !decodeJSONBody(response, request, &body) {
			return
		}
		if body.Revision == nil || *body.Revision <= 0 {
			writeError(
				response,
				http.StatusBadRequest,
				"invalid_argument",
				"A positive revision is required",
				"revision",
			)
			return
		}
		result, err := api.network.Update(request.Context(), id, networkruntime.UpdateInput{
			Revision:      *body.Revision,
			Name:          body.Name,
			LineID:        body.LineID,
			Enabled:       body.Enabled,
			Mode:          body.Mode,
			ListenAddress: body.ListenAddress,
			ListenPort:    body.ListenPort,
			AuthEnabled:   body.AuthEnabled,
			Username:      body.Username,
			Password:      body.Password,
		})
		if err != nil {
			api.writeNetworkError(response, request, "update proxy settings", err)
			return
		}
		writeJSON(response, http.StatusOK, result)
	case http.MethodDelete:
		revision, ok := requiredPositiveInt64(response, request, "revision")
		if !ok {
			return
		}
		result, err := api.network.Delete(request.Context(), id, revision)
		if err != nil {
			api.writeNetworkError(response, request, "delete proxy settings", err)
			return
		}
		writeJSON(response, http.StatusOK, result)
	default:
		response.Header().Set("Allow", http.MethodPatch+", "+http.MethodDelete)
		writeError(
			response,
			http.StatusMethodNotAllowed,
			"method_not_allowed",
			"Only PATCH and DELETE are supported",
			"",
		)
	}
}

func proxyResourceID(path string) (string, bool) {
	const prefix = "/api/v1/proxies/"
	if !strings.HasPrefix(path, prefix) {
		return "", false
	}
	id := strings.TrimSpace(strings.TrimPrefix(path, prefix))
	if id == "" || strings.Contains(id, "/") || len(id) > maxIdentifierLength {
		return "", false
	}
	return id, true
}

func (api *API) requireNetworkService(response http.ResponseWriter) bool {
	if api.network != nil {
		return true
	}
	writeError(
		response,
		http.StatusServiceUnavailable,
		"network_runtime_unavailable",
		"Network runtime is unavailable",
		"",
	)
	return false
}

func (api *API) writeNetworkError(
	response http.ResponseWriter,
	request *http.Request,
	operation string,
	err error,
) {
	var runtimeError *networkruntime.Error
	if errors.As(err, &runtimeError) {
		switch runtimeError.Code {
		case networkruntime.CodeInvalidArgument:
			writeError(
				response,
				http.StatusBadRequest,
				"invalid_argument",
				runtimeError.Message,
				runtimeError.Field,
			)
		case networkruntime.CodeNotFound:
			writeError(
				response,
				http.StatusNotFound,
				"not_found",
				runtimeError.Message,
				runtimeError.Field,
			)
		case networkruntime.CodeConflict:
			writeError(
				response,
				http.StatusConflict,
				"revision_conflict",
				runtimeError.Message,
				runtimeError.Field,
			)
		case networkruntime.CodeUnavailable:
			writeError(
				response,
				http.StatusServiceUnavailable,
				"network_runtime_unavailable",
				runtimeError.Message,
				runtimeError.Field,
			)
		default:
			api.writeInternalError(response, request, operation, err)
		}
		return
	}
	api.writeInternalError(response, request, operation, err)
}
