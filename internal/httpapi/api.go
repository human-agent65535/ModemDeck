package httpapi

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/human-agent65535/modemdeck/internal/auth"
	"github.com/human-agent65535/modemdeck/internal/store"
)

var (
	ErrRepositoryRequired    = errors.New("http api repository is required")
	ErrAuthenticatorRequired = errors.New("http api authenticator is required")
)

type Repository interface {
	Ping(context.Context) error
	Contacts(context.Context, store.ContactQuery) ([]store.Contact, error)
	Contact(context.Context, string) (store.Contact, error)
	CreateContact(context.Context, store.ContactInput) (store.Contact, error)
	UpdateContact(context.Context, string, store.ContactInput) (store.Contact, error)
	DeleteContact(context.Context, string, int64) error
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

type Authenticator interface {
	Login(context.Context, string) (auth.LoginResult, error)
	Authenticate(context.Context, auth.SessionToken) (auth.Authentication, error)
	Logout(context.Context, auth.SessionToken) error
}

type Options struct {
	Capabilities          CapabilitySource
	Authenticator         Authenticator
	AdminUsername         string
	SecureCookies         bool
	Logger                *slog.Logger
	Web                   http.Handler
	disableAuthentication bool
}

type API struct {
	repository    Repository
	capabilities  CapabilitySource
	authenticator Authenticator
	adminUsername string
	secureCookies bool
	loginSlots    chan struct{}
	logger        *slog.Logger
	web           http.Handler
}

func New(repository Repository, options Options) (*API, error) {
	if repository == nil {
		return nil, ErrRepositoryRequired
	}
	if options.Authenticator == nil && !options.disableAuthentication {
		return nil, ErrAuthenticatorRequired
	}
	adminUsername := strings.TrimSpace(options.AdminUsername)
	if adminUsername == "" {
		adminUsername = "admin"
	}
	logger := options.Logger
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &API{
		repository:    repository,
		capabilities:  options.Capabilities,
		authenticator: options.Authenticator,
		adminUsername: adminUsername,
		secureCookies: options.SecureCookies,
		loginSlots:    make(chan struct{}, 2),
		logger:        logger,
		web:           options.Web,
	}, nil
}

func (api *API) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	api.setSecurityHeaders(response)
	if !strings.HasPrefix(request.URL.Path, "/api/") {
		if api.web != nil {
			api.web.ServeHTTP(response, request)
			return
		}
		http.NotFound(response, request)
		return
	}
	if request.URL.Path == "/api/v1/health" {
		api.getOnly(response, request, api.health)
		return
	}
	if request.URL.Path == "/api/v1/session" {
		api.session(response, request)
		return
	}
	if !api.authorizeAPI(response, request) {
		return
	}

	switch request.URL.Path {
	case "/api/v1/bootstrap":
		api.getOnly(response, request, api.bootstrap)
	case "/api/v1/contacts":
		api.contactsCollection(response, request)
	case "/api/v1/messages/threads":
		api.getOnly(response, request, api.messageThreads)
	case "/api/v1/messages":
		api.getOnly(response, request, api.messages)
	case "/api/v1/calls":
		api.getOnly(response, request, api.calls)
	case "/api/v1/devices":
		api.getOnly(response, request, api.devices)
	default:
		if id, ok := contactResourceID(request.URL.Path); ok {
			api.contactResource(response, request, id)
			return
		}
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

func (api *API) contactsCollection(response http.ResponseWriter, request *http.Request) {
	switch request.Method {
	case http.MethodGet:
		api.contacts(response, request)
	case http.MethodPost:
		api.createContact(response, request)
	default:
		response.Header().Set("Allow", http.MethodGet+", "+http.MethodPost)
		writeError(response, http.StatusMethodNotAllowed, "method_not_allowed", "Only GET and POST are supported", "")
	}
}

func (api *API) contactResource(response http.ResponseWriter, request *http.Request, id string) {
	switch request.Method {
	case http.MethodGet:
		api.contact(response, request, id)
	case http.MethodPut:
		api.updateContact(response, request, id)
	case http.MethodDelete:
		api.deleteContact(response, request, id)
	default:
		response.Header().Set("Allow", http.MethodGet+", "+http.MethodPut+", "+http.MethodDelete)
		writeError(response, http.StatusMethodNotAllowed, "method_not_allowed", "Only GET, PUT, and DELETE are supported", "")
	}
}

func contactResourceID(path string) (string, bool) {
	const prefix = "/api/v1/contacts/"
	if !strings.HasPrefix(path, prefix) {
		return "", false
	}
	id := strings.TrimSpace(strings.TrimPrefix(path, prefix))
	if id == "" || strings.Contains(id, "/") || len(id) > maxIdentifierLength {
		return "", false
	}
	return id, true
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

func (api *API) contact(response http.ResponseWriter, request *http.Request, id string) {
	contact, err := api.repository.Contact(request.Context(), id)
	if err != nil {
		api.writeContactError(response, request, "load contact", err)
		return
	}
	writeJSON(response, http.StatusOK, contactResponse{Contact: contact})
}

func (api *API) createContact(response http.ResponseWriter, request *http.Request) {
	input, ok := decodeContactInput(response, request)
	if !ok {
		return
	}
	if input.Revision != 0 {
		writeError(response, http.StatusBadRequest, "invalid_argument", "revision must be omitted when creating a contact", "revision")
		return
	}
	contact, err := api.repository.CreateContact(request.Context(), input)
	if err != nil {
		api.writeContactError(response, request, "create contact", err)
		return
	}
	writeJSON(response, http.StatusCreated, contactResponse{Contact: contact})
}

func (api *API) updateContact(response http.ResponseWriter, request *http.Request, id string) {
	input, ok := decodeContactInput(response, request)
	if !ok {
		return
	}
	if input.Revision <= 0 {
		writeError(response, http.StatusBadRequest, "invalid_argument", "revision is required when updating a contact", "revision")
		return
	}
	contact, err := api.repository.UpdateContact(request.Context(), id, input)
	if err != nil {
		api.writeContactError(response, request, "update contact", err)
		return
	}
	writeJSON(response, http.StatusOK, contactResponse{Contact: contact})
}

func (api *API) deleteContact(response http.ResponseWriter, request *http.Request, id string) {
	revision, ok := requiredPositiveInt64(response, request, "revision")
	if !ok {
		return
	}
	if err := api.repository.DeleteContact(request.Context(), id, revision); err != nil {
		api.writeContactError(response, request, "delete contact", err)
		return
	}
	response.Header().Set("Cache-Control", "no-store")
	response.WriteHeader(http.StatusNoContent)
}

func (api *API) writeContactError(response http.ResponseWriter, request *http.Request, operation string, err error) {
	switch {
	case errors.Is(err, store.ErrContactNotFound):
		writeError(response, http.StatusNotFound, "contact_not_found", "Contact was not found", "")
	case errors.Is(err, store.ErrContactRevisionConflict):
		writeError(response, http.StatusConflict, "revision_conflict", "Contact changed since it was loaded", "revision")
	case errors.Is(err, store.ErrContactPhoneConflict):
		writeError(response, http.StatusConflict, "phone_conflict", "Phone number is already assigned to another contact", "phones")
	case errors.Is(err, store.ErrContactValidation):
		var validation *store.ContactValidationError
		field := ""
		if errors.As(err, &validation) {
			field = validation.Field
		}
		writeError(response, http.StatusBadRequest, "invalid_contact", "Contact data is invalid", field)
	default:
		api.writeInternalError(response, request, operation, err)
	}
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
