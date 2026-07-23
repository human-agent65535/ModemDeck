package httpapi

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/human-agent65535/modemdeck/internal/store"
)

var ErrRepositoryRequired = errors.New("http api repository is required")

type Repository interface {
	Ping(context.Context) error
	Contacts(context.Context, store.ContactQuery) ([]store.Contact, error)
	MessageThreads(context.Context, store.ThreadQuery) ([]store.MessageThread, error)
	Messages(context.Context, store.MessageQuery) ([]store.Message, error)
	Calls(context.Context, store.CallQuery) ([]store.Call, error)
	Devices(context.Context) ([]store.Device, error)
	Lines(context.Context) ([]store.LineSummary, error)
}

type Capabilities struct {
	AgentConnected     bool              `json:"agent_connected"`
	Dial               bool              `json:"dial"`
	Message            bool              `json:"message"`
	WebRTCAudio        bool              `json:"webrtc_audio"`
	DeviceControl      bool              `json:"device_control"`
	VoLTEControl       bool              `json:"volte_control"`
	VoWiFiControl      bool              `json:"vowifi_control"`
	UnavailableReasons map[string]string `json:"unavailable_reasons"`
}

type CapabilitySource interface {
	Capabilities(context.Context) (Capabilities, error)
}

type Options struct {
	Capabilities CapabilitySource
	Logger       *slog.Logger
	Web          http.Handler
}

type API struct {
	repository   Repository
	capabilities CapabilitySource
	logger       *slog.Logger
	web          http.Handler
}

func New(repository Repository, options Options) (*API, error) {
	if repository == nil {
		return nil, ErrRepositoryRequired
	}
	logger := options.Logger
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &API{
		repository:   repository,
		capabilities: options.Capabilities,
		logger:       logger,
		web:          options.Web,
	}, nil
}

func (api *API) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	if !strings.HasPrefix(request.URL.Path, "/api/") {
		if api.web != nil {
			api.web.ServeHTTP(response, request)
			return
		}
		http.NotFound(response, request)
		return
	}

	switch request.URL.Path {
	case "/api/v1/health":
		api.getOnly(response, request, api.health)
	case "/api/v1/bootstrap":
		api.getOnly(response, request, api.bootstrap)
	case "/api/v1/contacts":
		api.getOnly(response, request, api.contacts)
	case "/api/v1/messages/threads":
		api.getOnly(response, request, api.messageThreads)
	case "/api/v1/messages":
		api.getOnly(response, request, api.messages)
	case "/api/v1/calls":
		api.getOnly(response, request, api.calls)
	case "/api/v1/devices":
		api.getOnly(response, request, api.devices)
	default:
		writeError(response, http.StatusNotFound, "not_found", "API endpoint was not found", "")
	}
}

func (api *API) getOnly(response http.ResponseWriter, request *http.Request, handler http.HandlerFunc) {
	if request.Method != http.MethodGet {
		response.Header().Set("Allow", http.MethodGet)
		writeError(response, http.StatusMethodNotAllowed, "method_not_allowed", "Only GET is supported", "")
		return
	}
	handler(response, request)
}

func (api *API) health(response http.ResponseWriter, request *http.Request) {
	if err := api.repository.Ping(request.Context()); err != nil {
		api.logger.Error("health check failed", "error", err)
		writeError(response, http.StatusServiceUnavailable, "database_unavailable", "Database is unavailable", "")
		return
	}
	writeJSON(response, http.StatusOK, healthResponse{Status: "ok"})
}

func (api *API) bootstrap(response http.ResponseWriter, request *http.Request) {
	lines, err := api.repository.Lines(request.Context())
	if err != nil {
		api.writeInternalError(response, request, "load bootstrap lines", err)
		return
	}
	capabilities := Capabilities{}
	if api.capabilities != nil {
		capabilities, err = api.capabilities.Capabilities(request.Context())
		if err != nil {
			api.logger.Warn("host agent is unavailable", "error", err)
			capabilities = disconnectedCapabilities()
		}
	} else {
		capabilities = disconnectedCapabilities()
	}
	capabilities = gateCapabilities(capabilities)
	writeJSON(response, http.StatusOK, bootstrapResponse{
		Capabilities: capabilities,
		Lines:        lines,
	})
}

func gateCapabilities(capabilities Capabilities) Capabilities {
	if !capabilities.AgentConnected {
		return disconnectedCapabilities()
	}
	if capabilities.UnavailableReasons == nil {
		capabilities.UnavailableReasons = map[string]string{}
	}
	return capabilities
}

func disconnectedCapabilities() Capabilities {
	return Capabilities{UnavailableReasons: map[string]string{
		"dial":    "Host agent is not connected",
		"message": "Host agent is not connected",
	}}
}

func (api *API) contacts(response http.ResponseWriter, request *http.Request) {
	limit, ok := requestLimit(response, request)
	if !ok {
		return
	}
	search, ok := requestSearch(response, request)
	if !ok {
		return
	}
	contacts, err := api.repository.Contacts(request.Context(), store.ContactQuery{Search: search, Limit: limit})
	if err != nil {
		api.writeInternalError(response, request, "list contacts", err)
		return
	}
	writeJSON(response, http.StatusOK, contactsResponse{Contacts: contacts, Meta: responseMeta{Limit: limit}})
}

func (api *API) messageThreads(response http.ResponseWriter, request *http.Request) {
	limit, ok := requestLimit(response, request)
	if !ok {
		return
	}
	search, ok := requestSearch(response, request)
	if !ok {
		return
	}
	threads, err := api.repository.MessageThreads(request.Context(), store.ThreadQuery{Search: search, Limit: limit})
	if err != nil {
		api.writeInternalError(response, request, "list message threads", err)
		return
	}
	writeJSON(response, http.StatusOK, threadsResponse{Threads: threads, Meta: responseMeta{Limit: limit}})
}

func (api *API) messages(response http.ResponseWriter, request *http.Request) {
	limit, ok := requestLimit(response, request)
	if !ok {
		return
	}
	iccid, ok := boundedFilter(response, request.URL.Query().Get("iccid"), "iccid", maxIdentifierLength)
	if !ok {
		return
	}
	if iccid == "" {
		writeError(response, http.StatusBadRequest, "invalid_argument", "iccid is required", "iccid")
		return
	}
	peer, ok := boundedFilter(response, request.URL.Query().Get("peer"), "peer", maxPhoneLength)
	if !ok {
		return
	}
	if peer == "" {
		writeError(response, http.StatusBadRequest, "invalid_argument", "peer is required", "peer")
		return
	}
	messages, err := api.repository.Messages(request.Context(), store.MessageQuery{ICCID: iccid, Peer: peer, Limit: limit})
	if err != nil {
		api.writeInternalError(response, request, "list messages", err)
		return
	}
	writeJSON(response, http.StatusOK, messagesResponse{Messages: messages, Meta: responseMeta{Limit: limit}})
}

func (api *API) calls(response http.ResponseWriter, request *http.Request) {
	limit, ok := requestLimit(response, request)
	if !ok {
		return
	}
	search, ok := requestSearch(response, request)
	if !ok {
		return
	}
	kindValue := strings.TrimSpace(request.URL.Query().Get("kind"))
	kind, err := store.ParseCallKind(kindValue)
	if err != nil {
		writeError(response, http.StatusBadRequest, "invalid_argument", "kind must be all, incoming, outgoing, or missed", "kind")
		return
	}
	calls, err := api.repository.Calls(request.Context(), store.CallQuery{Kind: kind, Search: search, Limit: limit})
	if err != nil {
		api.writeInternalError(response, request, "list calls", err)
		return
	}
	writeJSON(response, http.StatusOK, callsResponse{Calls: calls, Meta: responseMeta{Limit: limit}})
}

func (api *API) devices(response http.ResponseWriter, request *http.Request) {
	devices, err := api.repository.Devices(request.Context())
	if err != nil {
		api.writeInternalError(response, request, "list devices", err)
		return
	}
	writeJSON(response, http.StatusOK, devicesResponse{
		Devices: devices,
		Meta:    responseMeta{Limit: store.MaxQueryLimit},
	})
}

func (api *API) writeInternalError(response http.ResponseWriter, request *http.Request, operation string, err error) {
	api.logger.Error(operation, "method", request.Method, "path", request.URL.Path, "error", err)
	writeError(response, http.StatusInternalServerError, "internal_error", "Request could not be completed", "")
}
