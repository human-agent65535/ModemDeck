package httpapi

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/human-agent65535/modemdeck/internal/agentclient"
	"github.com/human-agent65535/modemdeck/internal/auth"
	"github.com/human-agent65535/modemdeck/internal/networkruntime"
	"github.com/human-agent65535/modemdeck/internal/store"
)

const (
	networkScanWriteTimeout      = 130 * time.Second
	networkSelectionWriteTimeout = 55 * time.Second
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

type networkSelectionUpdateRequest struct {
	ExpectedRevision *int64                           `json:"expected_revision"`
	Mode             agentclient.NetworkSelectionMode `json:"mode"`
	OperatorCode     string                           `json:"operator_code"`
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
	if _, scoped := auth.PrincipalFromContext(request.Context()); scoped {
		proxies, proxyErr := api.network.Proxies(request.Context())
		if proxyErr != nil {
			api.writeNetworkError(response, request, "load network proxy scope", proxyErr)
			return
		}
		status = filterNetworkStatusForPrincipal(request, status, proxies)
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
		proxies = filterProxiesForPrincipal(request, proxies)
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
		if !api.requireLineAccess(response, request, body.LineID) {
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
		current, ok := api.authorizedProxy(response, request, id)
		if !ok {
			return
		}
		if body.LineID != nil &&
			strings.TrimSpace(*body.LineID) != strings.TrimSpace(current.LineID) &&
			!api.requireLineAccess(response, request, *body.LineID) {
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
		if _, authorized := api.authorizedProxy(response, request, id); !authorized {
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

func (api *API) networkSelectionResource(
	response http.ResponseWriter,
	request *http.Request,
	lineID string,
	resource string,
) {
	if !api.requireNetworkService(response) {
		return
	}
	if !api.requireLineAccess(response, request, lineID) {
		return
	}
	switch resource {
	case "network-selection":
		api.networkSelection(response, request, lineID)
	case "network-scan":
		api.networkScan(response, request, lineID)
	default:
		writeError(response, http.StatusNotFound, "not_found", "API endpoint was not found", "")
	}
}

func (api *API) authorizedProxy(
	response http.ResponseWriter,
	request *http.Request,
	id string,
) (networkruntime.Proxy, bool) {
	if _, scoped := auth.PrincipalFromContext(request.Context()); !scoped {
		return networkruntime.Proxy{}, true
	}
	proxies, err := api.network.Proxies(request.Context())
	if err != nil {
		api.writeNetworkError(response, request, "load proxy settings", err)
		return networkruntime.Proxy{}, false
	}
	for _, proxy := range proxies {
		if proxy.ID != id {
			continue
		}
		if !canAccessLine(request.Context(), proxy.LineID) {
			break
		}
		return proxy, true
	}
	writeError(response, http.StatusNotFound, "not_found", "Proxy was not found", "")
	return networkruntime.Proxy{}, false
}

func filterProxiesForPrincipal(
	request *http.Request,
	proxies []networkruntime.Proxy,
) []networkruntime.Proxy {
	if _, scoped := auth.PrincipalFromContext(request.Context()); !scoped {
		return proxies
	}
	result := make([]networkruntime.Proxy, 0, len(proxies))
	for _, proxy := range proxies {
		if canAccessLine(request.Context(), proxy.LineID) {
			result = append(result, proxy)
		}
	}
	return result
}

func filterNetworkStatusForPrincipal(
	request *http.Request,
	status networkruntime.Status,
	configuredProxies []networkruntime.Proxy,
) networkruntime.Status {
	allowedProxyIDs := make(map[string]struct{}, len(configuredProxies))
	for _, proxy := range configuredProxies {
		if canAccessLine(request.Context(), proxy.LineID) {
			allowedProxyIDs[proxy.ID] = struct{}{}
		}
	}

	lines := make([]agentclient.NetworkLine, 0, len(status.Lines))
	for _, line := range status.Lines {
		if canAccessLine(request.Context(), line.LineID) {
			lines = append(lines, line)
		}
	}
	status.Lines = lines

	proxies := make([]agentclient.NetworkProxy, 0, len(status.Proxies))
	for _, proxy := range status.Proxies {
		if _, allowed := allowedProxyIDs[proxy.ID]; allowed {
			proxies = append(proxies, proxy)
		}
	}
	status.Proxies = proxies
	status.TodayUsage, status.TodayTotal = filterNetworkUsage(
		request,
		status.TodayUsage,
		allowedProxyIDs,
	)
	status.MonthUsage, status.MonthTotal = filterNetworkUsage(
		request,
		status.MonthUsage,
		allowedProxyIDs,
	)
	return status
}

func filterNetworkUsage(
	request *http.Request,
	usage []networkruntime.Usage,
	allowedProxyIDs map[string]struct{},
) ([]networkruntime.Usage, networkruntime.UsageTotal) {
	result := make([]networkruntime.Usage, 0, len(usage))
	var total networkruntime.UsageTotal
	for _, item := range usage {
		allowed := false
		switch item.ScopeKind {
		case store.NetworkScopeLine:
			allowed = canAccessLine(request.Context(), item.ScopeID)
		case store.NetworkScopeProxy:
			_, allowed = allowedProxyIDs[item.ScopeID]
		}
		if !allowed {
			continue
		}
		result = append(result, item)
		if item.ScopeKind == store.NetworkScopeLine {
			total.RXBytes += item.RXBytes
			total.TXBytes += item.TXBytes
		}
	}
	return result, total
}

func (api *API) networkSelection(
	response http.ResponseWriter,
	request *http.Request,
	lineID string,
) {
	switch request.Method {
	case http.MethodGet:
		selection, err := api.network.NetworkSelection(request.Context(), lineID)
		if err != nil {
			api.writeNetworkError(response, request, "read network selection", err)
			return
		}
		writeJSON(response, http.StatusOK, selection)
	case http.MethodPut:
		if err := http.NewResponseController(response).SetWriteDeadline(
			time.Now().Add(networkSelectionWriteTimeout),
		); err != nil && !errors.Is(err, http.ErrNotSupported) {
			writeError(
				response,
				http.StatusServiceUnavailable,
				"network_runtime_unavailable",
				"Network selection response deadline could not be extended",
				"",
			)
			return
		}
		var body networkSelectionUpdateRequest
		if !decodeJSONBody(response, request, &body) {
			return
		}
		if body.ExpectedRevision == nil || *body.ExpectedRevision <= 0 {
			writeError(
				response,
				http.StatusBadRequest,
				"invalid_argument",
				"A positive expected_revision is required",
				"expected_revision",
			)
			return
		}
		selection, err := api.network.UpdateNetworkSelection(
			request.Context(),
			lineID,
			networkruntime.UpdateNetworkSelectionInput{
				ExpectedRevision: *body.ExpectedRevision,
				Mode:             body.Mode,
				OperatorCode:     body.OperatorCode,
			},
		)
		if err != nil {
			api.writeNetworkError(response, request, "update network selection", err)
			return
		}
		writeJSON(response, http.StatusOK, selection)
	default:
		response.Header().Set("Allow", http.MethodGet+", "+http.MethodPut)
		writeError(
			response,
			http.StatusMethodNotAllowed,
			"method_not_allowed",
			"Only GET and PUT are supported",
			"",
		)
	}
}

func (api *API) networkScan(
	response http.ResponseWriter,
	request *http.Request,
	lineID string,
) {
	if request.Method != http.MethodPost {
		response.Header().Set("Allow", http.MethodPost)
		writeError(
			response,
			http.StatusMethodNotAllowed,
			"method_not_allowed",
			"Only POST is supported",
			"",
		)
		return
	}
	if err := http.NewResponseController(response).SetWriteDeadline(
		time.Now().Add(networkScanWriteTimeout),
	); err != nil && !errors.Is(err, http.ErrNotSupported) {
		writeError(
			response,
			http.StatusServiceUnavailable,
			"network_runtime_unavailable",
			"Network scan response deadline could not be extended",
			"",
		)
		return
	}
	result, err := api.network.ScanNetworks(request.Context(), lineID)
	if err != nil {
		api.writeNetworkError(response, request, "scan mobile networks", err)
		return
	}
	writeJSON(response, http.StatusOK, result)
}

func networkSelectionResource(path string) (lineID string, resource string, ok bool) {
	const prefix = "/api/v1/devices/"
	if !strings.HasPrefix(path, prefix) {
		return "", "", false
	}
	parts := strings.Split(strings.TrimPrefix(path, prefix), "/")
	if len(parts) != 2 ||
		strings.TrimSpace(parts[0]) == "" ||
		len(parts[0]) > maxIdentifierLength {
		return "", "", false
	}
	switch parts[1] {
	case "network-selection", "network-scan":
		return parts[0], parts[1], true
	default:
		return "", "", false
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
		case networkruntime.CodeOperationConflict:
			writeError(
				response,
				http.StatusConflict,
				"operation_conflict",
				runtimeError.Message,
				runtimeError.Field,
			)
		case networkruntime.CodeNotSupported:
			writeError(
				response,
				http.StatusNotImplemented,
				"not_supported",
				runtimeError.Message,
				runtimeError.Field,
			)
		case networkruntime.CodeFailedPrecondition:
			writeError(
				response,
				http.StatusPreconditionFailed,
				"failed_precondition",
				runtimeError.Message,
				runtimeError.Field,
			)
		case networkruntime.CodeNetworkRejected:
			writeError(
				response,
				http.StatusUnprocessableEntity,
				"network_rejected",
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
