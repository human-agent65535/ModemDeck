package httpapi

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/human-agent65535/modemdeck/internal/agentclient"
	"github.com/human-agent65535/modemdeck/internal/auth"
	"github.com/human-agent65535/modemdeck/internal/calllease"
	"github.com/human-agent65535/modemdeck/internal/communication"
	"github.com/human-agent65535/modemdeck/internal/diagnostics"
	"github.com/human-agent65535/modemdeck/internal/messageevents"
	"github.com/human-agent65535/modemdeck/internal/mobilepairing"
	"github.com/human-agent65535/modemdeck/internal/networkruntime"
	"github.com/human-agent65535/modemdeck/internal/recording"
	"github.com/human-agent65535/modemdeck/internal/rtcconfig"
	"github.com/human-agent65535/modemdeck/internal/runtimeevents"
	"github.com/human-agent65535/modemdeck/internal/store"
	"github.com/human-agent65535/modemdeck/internal/telegramsettings"
	"github.com/human-agent65535/modemdeck/internal/updatecheck"
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
	DeleteContacts(context.Context, []store.ContactRevision) error
	MessageThreads(context.Context, store.ThreadQuery) ([]store.MessageThread, error)
	Messages(context.Context, store.MessageQuery) ([]store.Message, error)
	MarkMessageThreadRead(context.Context, store.MessageThreadIdentity) error
	UpdateMessageThreads(
		context.Context,
		[]store.MessageThreadIdentity,
		store.MessageThreadAction,
	) error
	DeleteMessageThread(context.Context, store.MessageThreadIdentity) error
	Calls(context.Context, store.CallQuery) ([]store.Call, error)
	MarkMissedCallsRead(context.Context) error
	MarkMissedCallsReadByIDs(context.Context, []string) error
	MarkMissedCallsUnreadByIDs(context.Context, []string) error
	SetCallFavoritesByIDs(context.Context, []string, bool) error
	RecordingEntries(context.Context, store.RecordingQuery) ([]store.RecordingEntry, error)
	SetRecordingFavorites(context.Context, []store.RecordingIdentity, bool) error
	Devices(context.Context) ([]store.Device, error)
	CreateDevice(context.Context, store.DeviceInput) (store.Device, error)
	RenameDevice(context.Context, string, string) (store.Device, error)
	DeleteDevice(context.Context, string) error
	Lines(context.Context) ([]store.LineSummary, error)
	UpdateLineLabel(
		context.Context,
		string,
		string,
		*store.LineColor,
	) (store.LineSummary, error)
	LineSettings(context.Context) (store.LineSettings, error)
	UpdateLineSettings(context.Context, string, int64) (store.LineSettings, error)
	SystemSettings(context.Context) (store.SystemSettings, error)
	UpdateSystemSettings(
		context.Context,
		store.SystemLanguage,
		int64,
	) (store.SystemSettings, error)
	IOSPairingPrincipalByTokenDigest(
		context.Context,
		mobilepairing.TokenDigest,
	) (auth.Principal, bool, error)
	ConfirmIOSPairingCredential(
		context.Context,
		mobilepairing.TokenDigest,
	) (bool, error)
}

type Capabilities struct {
	AgentConnected     bool              `json:"agent_connected"`
	Dial               bool              `json:"dial"`
	AnswerCall         bool              `json:"answer_call"`
	HangupCall         bool              `json:"hangup_call"`
	RejectCall         bool              `json:"reject_call"`
	SendDTMF           bool              `json:"send_dtmf"`
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

type HealthProbe interface {
	Health(context.Context) (agentclient.Health, error)
}

type UpdateChecker interface {
	Check(context.Context) updatecheck.Result
}

type UpdateManager interface {
	Apply(context.Context, updatecheck.ApplyRequest) (updatecheck.Operation, error)
	Status(context.Context) (updatecheck.Operation, error)
}

type Authenticator interface {
	Status(context.Context) (auth.AdminStatus, error)
	Setup(context.Context, string, string) error
	Login(context.Context, string, string) (auth.LoginResult, error)
	ChangePassword(context.Context, string, string) error
	Authenticate(context.Context, auth.SessionToken) (auth.Authentication, error)
	Logout(context.Context, auth.SessionToken) error
}

type CommunicationService interface {
	Status(context.Context) (communication.Status, error)
	SendMessage(context.Context, communication.SendMessageInput) (store.Message, error)
	ReplayStartCall(context.Context, communication.StartCallInput) (store.Call, bool, error)
	StartCall(context.Context, communication.StartCallInput) (store.Call, error)
	ReplayCallAction(context.Context, communication.CallActionInput) (store.Call, bool, error)
	CallAction(context.Context, communication.CallActionInput) (store.Call, error)
	ActiveCalls(context.Context) ([]store.Call, error)
	EndCall(context.Context, string) error
}

type DeviceConfigurationService interface {
	DeviceConfiguration(context.Context, string) (agentclient.DeviceConfiguration, error)
	ApplyDeviceConfiguration(
		context.Context,
		string,
		agentclient.ApplyDeviceConfigurationRequest,
	) (agentclient.DeviceConfiguration, error)
}

type LineService interface {
	SIMStatus(context.Context, string) (agentclient.SIMStatus, error)
	SIMCommand(
		context.Context,
		string,
		agentclient.SIMCommandRequest,
	) (agentclient.CommandReceipt, error)
	ConnectionProfiles(context.Context, string) ([]agentclient.ConnectionProfile, error)
	SaveConnectionProfile(
		context.Context,
		string,
		agentclient.SaveConnectionProfileRequest,
	) (agentclient.ConnectionProfile, error)
	DeleteConnectionProfile(
		context.Context,
		string,
		agentclient.DeleteConnectionProfileRequest,
	) (agentclient.CommandReceipt, error)
	USSDStatus(context.Context, string) (agentclient.USSDStatus, error)
	USSDCommand(
		context.Context,
		string,
		agentclient.USSDRequest,
	) (agentclient.USSDResponse, error)
}

type CallPolicyService interface {
	GlobalCallSettings(context.Context) (store.GlobalCallSettings, error)
	UpdateGlobalCallSettings(context.Context, bool, int64) (store.GlobalCallSettings, error)
	LineCallPolicy(context.Context, string) (store.LineCallPolicy, error)
	UpdateLineCallPolicy(
		context.Context,
		string,
		store.LineCallPolicyValue,
		int64,
	) (store.LineCallPolicy, error)
	EffectiveCallPolicy(context.Context, string) (store.EffectiveCallPolicy, error)
	CallPolicyConfiguration(context.Context, string) (store.CallPolicyConfiguration, error)
	LatestIncomingCallAction(context.Context, string) (*store.IncomingCallAction, error)
}

type MessagePolicyService interface {
	MessageDeliveryPolicy(context.Context, string) (store.MessageDeliveryPolicy, error)
	UpdateMessageDeliveryPolicy(
		context.Context,
		string,
		bool,
		int64,
	) (store.MessageDeliveryPolicy, error)
}

type TelegramSettingsService interface {
	List(context.Context) ([]telegramsettings.Unit, error)
	Create(context.Context, telegramsettings.CreateInput) (telegramsettings.Unit, error)
	Update(context.Context, string, telegramsettings.UpdateInput) (telegramsettings.Unit, error)
	Delete(context.Context, string, int64) error
	NotifyAccessChanged()
}

type CallMediaService interface {
	Exchange(context.Context, string, string, string, bool) (string, error)
	ReleaseOwner(context.Context, string, string) error
	CloseCall(context.Context, string) error
}

type CallLeaseService interface {
	ReserveOutgoingFor(context.Context, string, string, calllease.Owner) (calllease.OutgoingReservation, error)
	ActivateOutgoing(context.Context, string, string, string) (calllease.Status, error)
	ReleaseOutgoing(string, string) (bool, error)
	AwaitOutgoingResolution(string, string) error
	OutgoingReservations(string) ([]calllease.OutgoingReservation, error)
	ProjectActive([]store.Call, string) (calllease.ActiveProjection, error)
	ClaimFor(context.Context, string, calllease.Owner) (calllease.Status, error)
	Renew(context.Context, string, string) (calllease.Status, error)
	Require(context.Context, string, string) error
	ControlState(context.Context, string, string) (calllease.ControlState, error)
	RevokeHolder(string) ([]string, error)
	RevokeSubject(string) ([]string, error)
}

type RecordingService interface {
	Settings(context.Context) (store.RecordingSettings, error)
	UpdateSettings(context.Context, bool, int64) (store.RecordingSettings, error)
	PrepareOutgoing(context.Context, string, bool) (string, error)
	CallRecordings(context.Context, string) (recording.CallRecordings, error)
	SetEnabled(context.Context, string, bool) (store.CallRecordingState, error)
	Download(context.Context, string, string) (recording.Download, error)
	DeleteRecording(context.Context, string, string) error
	DeleteCall(context.Context, string) error
	FinalizeCall(context.Context, string) error
}

type NetworkService interface {
	Status(context.Context) (networkruntime.Status, error)
	Proxies(context.Context) ([]networkruntime.Proxy, error)
	Create(
		context.Context,
		networkruntime.CreateInput,
	) (networkruntime.ProxyMutation, error)
	Update(
		context.Context,
		string,
		networkruntime.UpdateInput,
	) (networkruntime.ProxyMutation, error)
	Delete(context.Context, string, int64) (networkruntime.DeleteResult, error)
	NetworkSelection(
		context.Context,
		string,
	) (networkruntime.NetworkSelection, error)
	UpdateNetworkSelection(
		context.Context,
		string,
		networkruntime.UpdateNetworkSelectionInput,
	) (networkruntime.NetworkSelection, error)
	ScanNetworks(context.Context, string) (networkruntime.NetworkScan, error)
}

type Options struct {
	HealthProbe           HealthProbe
	Capabilities          CapabilitySource
	Communications        CommunicationService
	DeviceConfigurations  DeviceConfigurationService
	LineServices          LineService
	CallPolicies          CallPolicyService
	MessagePolicies       MessagePolicyService
	CallMedia             CallMediaService
	CallLeases            CallLeaseService
	Recording             RecordingService
	Network               NetworkService
	TelegramSettings      TelegramSettingsService
	TLSSettings           TLSSettingsService
	CloudflareOriginTLS   CloudflareOriginTLSService
	MobilePairing         mobilepairing.Availability
	RTCConfiguration      rtcconfig.Provider
	Authenticator         Authenticator
	SecureCookies         bool
	Logger                *slog.Logger
	DiagnosticLogs        diagnostics.LogSource
	MessageEvents         messageevents.Source
	RuntimeEvents         runtimeevents.Source
	UpdateChecker         UpdateChecker
	UpdateManager         UpdateManager
	ApplicationVersion    string
	disableAuthentication bool
}

type API struct {
	repository                 Repository
	healthProbe                HealthProbe
	capabilities               CapabilitySource
	communications             CommunicationService
	deviceConfigurations       DeviceConfigurationService
	lineServices               LineService
	callPolicies               CallPolicyService
	messagePolicies            MessagePolicyService
	callMedia                  CallMediaService
	callLeases                 CallLeaseService
	recordings                 RecordingService
	network                    NetworkService
	telegram                   TelegramSettingsService
	tlsSettingsService         TLSSettingsService
	cloudflareOriginTLSService CloudflareOriginTLSService
	mobilePairingAvailability  mobilepairing.Availability
	rtcConfiguration           rtcconfig.Provider
	turnAvailability           turnAvailabilityCache
	authenticator              Authenticator
	secureCookies              bool
	loginSlots                 chan struct{}
	loginFailures              *loginFailureLimiter
	eventStreams               *eventStreamLimiter
	streamAuthInterval         time.Duration
	logger                     *slog.Logger
	diagnosticLogs             diagnostics.LogSource
	messageEvents              messageevents.Source
	runtimeEvents              runtimeevents.Source
	updateChecker              UpdateChecker
	updateManager              UpdateManager
	applicationVersion         string
}

func New(repository Repository, options Options) (*API, error) {
	if repository == nil {
		return nil, ErrRepositoryRequired
	}
	if options.Authenticator == nil && !options.disableAuthentication {
		return nil, ErrAuthenticatorRequired
	}
	logger := options.Logger
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	healthProbe := options.HealthProbe
	if healthProbe == nil && options.Communications != nil {
		healthProbe, _ = options.Communications.(HealthProbe)
	}
	return &API{
		repository:                 repository,
		healthProbe:                healthProbe,
		capabilities:               options.Capabilities,
		communications:             options.Communications,
		deviceConfigurations:       options.DeviceConfigurations,
		lineServices:               options.LineServices,
		callPolicies:               options.CallPolicies,
		messagePolicies:            options.MessagePolicies,
		callMedia:                  options.CallMedia,
		callLeases:                 options.CallLeases,
		recordings:                 options.Recording,
		network:                    options.Network,
		telegram:                   options.TelegramSettings,
		tlsSettingsService:         options.TLSSettings,
		cloudflareOriginTLSService: options.CloudflareOriginTLS,
		mobilePairingAvailability:  options.MobilePairing,
		rtcConfiguration:           options.RTCConfiguration,
		authenticator:              options.Authenticator,
		secureCookies:              options.SecureCookies,
		loginSlots:                 make(chan struct{}, 2),
		loginFailures:              newLoginFailureLimiter(defaultLoginFailurePolicy),
		eventStreams:               newEventStreamLimiter(defaultEventStreamLimitPolicy),
		streamAuthInterval:         runtimeHeartbeatInterval,
		logger:                     logger,
		diagnosticLogs:             options.DiagnosticLogs,
		messageEvents:              options.MessageEvents,
		runtimeEvents:              options.RuntimeEvents,
		updateChecker:              options.UpdateChecker,
		updateManager:              options.UpdateManager,
		applicationVersion:         normalizedApplicationVersion(options.ApplicationVersion),
	}, nil
}

func (api *API) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	api.setSecurityHeaders(response)
	if !strings.HasPrefix(request.URL.Path, "/api/") {
		http.NotFound(response, request)
		return
	}
	switch request.URL.Path {
	case "/api/v1/health/live":
		api.getOnly(response, request, api.liveness)
		return
	case "/api/v1/health", "/api/v1/health/ready":
		api.getOnly(response, request, api.readiness)
		return
	case "/api/v1/version":
		api.getOnly(response, request, api.version)
		return
	case mobilepairing.CloudflareProbePath:
		api.mobileTunnelProbe(response, request)
		return
	}
	if request.URL.Path == "/api/v1/session" {
		api.session(response, request)
		return
	}
	if request.URL.Path == "/api/v1/setup" {
		api.setup(response, request)
		return
	}
	if !api.authorizeAPI(response, request) {
		return
	}
	if adminOnlyAPIPath(request.URL.Path, request.Method) &&
		!api.requireAdmin(response, request) {
		return
	}

	switch request.URL.Path {
	case "/api/v1/bootstrap":
		api.getOnly(response, request, api.bootstrap)
	case "/api/v1/about":
		api.getOnly(response, request, api.about)
	case "/api/v1/updates/check":
		api.getOnly(response, request, api.updateCheck)
	case "/api/v1/updates/apply":
		api.postOnly(response, request, api.updateApply)
	case "/api/v1/updates/status":
		api.getOnly(response, request, api.updateStatus)
	case "/api/v1/updates/events":
		api.getOnly(response, request, api.updateEventStream)
	case "/api/v1/account/password":
		api.accountPassword(response, request)
	case "/api/v1/account/contact":
		api.accountContact(response, request)
	case "/api/v1/account/preferences":
		api.accountPreferences(response, request)
	case "/api/v1/external-access/status":
		api.getOnly(response, request, api.externalAccessStatus)
	case "/api/v1/external-access/refresh":
		api.postOnly(response, request, api.refreshExternalAccess)
	case "/api/v1/external-access/origin-tls":
		api.cloudflareOriginTLS(response, request)
	case "/api/v1/mobile/pairing":
		api.mobilePairing(response, request)
	case "/api/v1/users":
		api.usersCollection(response, request)
	case "/api/v1/contacts":
		api.contactsCollection(response, request)
	case "/api/v1/contacts/batch":
		api.contactsBatch(response, request)
	case "/api/v1/messages/threads":
		api.messageThreadsCollection(response, request)
	case "/api/v1/messages":
		api.messagesCollection(response, request)
	case "/api/v1/messages/read":
		api.messageRead(response, request)
	case "/api/v1/messages/threads/state":
		api.messageThreadState(response, request)
	case "/api/v1/messages/events":
		api.getOnly(response, request, api.messageEventStream)
	case "/api/v1/runtime/events":
		api.getOnly(response, request, api.runtimeEventStream)
	case "/api/v1/calls":
		api.callsCollection(response, request)
	case "/api/v1/calls/missed/read":
		api.missedCallsRead(response, request)
	case "/api/v1/calls/batch":
		api.callsBatch(response, request)
	case "/api/v1/calls/active":
		api.getOnly(response, request, api.activeCalls)
	case "/api/v1/recordings":
		api.getOnly(response, request, api.recordingEntries)
	case "/api/v1/recordings/batch":
		api.recordingsBatch(response, request)
	case "/api/v1/network":
		api.getOnly(response, request, api.networkStatus)
	case "/api/v1/proxies":
		api.proxyCollection(response, request)
	case "/api/v1/devices":
		api.devicesCollection(response, request)
	case "/api/v1/diagnostics":
		api.getOnly(response, request, api.diagnostics)
	case "/api/v1/diagnostics/logs":
		api.getOnly(response, request, api.diagnosticLogHistory)
	case "/api/v1/diagnostics/logs/stream":
		api.getOnly(response, request, api.diagnosticLogStream)
	case "/api/v1/diagnostics/logs/download":
		api.getOnly(response, request, api.downloadDiagnosticLogs)
	case "/api/v1/settings/telegram":
		api.telegramCollection(response, request)
	case "/api/v1/settings/calls":
		api.callSettings(response, request)
	case "/api/v1/settings/lines":
		api.lineSettings(response, request)
	case "/api/v1/settings/system":
		api.systemSettings(response, request)
	case "/api/v1/settings/recording":
		api.recordingSettings(response, request)
	case "/api/v1/settings/tls":
		api.tlsSettings(response, request)
	case "/api/v1/settings/tls/ca":
		api.getOnly(response, request, api.tlsCertificateAuthority)
	default:
		if userID, action, ok := userResourcePath(request.URL.Path); ok {
			api.userResource(response, request, userID, action)
			return
		}
		if id, ok := contactResourceID(request.URL.Path); ok {
			api.contactResource(response, request, id)
			return
		}
		if id, ok := callLeaseResourceID(request.URL.Path); ok {
			api.renewCallLease(response, request, id)
			return
		}
		if id, action, ok := callActionResource(request.URL.Path); ok {
			api.callAction(response, request, id, action)
			return
		}
		if id, ok := callMediaICEConfigurationResourceID(
			request.URL.Path,
		); ok {
			api.callMediaICEConfiguration(response, request, id)
			return
		}
		if id, ok := callMediaResourceID(request.URL.Path); ok {
			api.callMediaExchange(response, request, id)
			return
		}
		if resource, ok := parseRecordingResource(request.URL.Path); ok {
			api.recordingResource(response, request, resource)
			return
		}
		if resource, ok := callRecordResource(request.URL.Path); ok {
			api.callRecordResource(response, request, resource)
			return
		}
		if id, ok := telegramResourceID(request.URL.Path); ok {
			api.telegramResource(response, request, id)
			return
		}
		if id, ok := proxyResourceID(request.URL.Path); ok {
			api.proxyResource(response, request, id)
			return
		}
		if imei, ok := deviceResourceIMEI(request.URL.Path); ok {
			api.deviceResource(response, request, imei)
			return
		}
		if lineID, ok := lineLabelResourceID(request.URL.EscapedPath()); ok {
			api.lineLabelResource(response, request, lineID)
			return
		}
		if id, ok := diagnosticDeviceConfigurationResourceID(request.URL.Path); ok {
			api.diagnosticDeviceConfiguration(response, request, id)
			return
		}
		if id, ok := deviceConfigurationResourceID(request.URL.Path); ok {
			api.deviceConfiguration(response, request, id)
			return
		}
		if id, resource, ok := networkSelectionResource(request.URL.Path); ok {
			api.networkSelectionResource(response, request, id, resource)
			return
		}
		if id, resource, ok := lineServiceResource(request.URL.Path); ok {
			api.lineServiceResource(response, request, id, resource)
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

func (api *API) postOnly(response http.ResponseWriter, request *http.Request, handler http.HandlerFunc) {
	if request.Method != http.MethodPost {
		response.Header().Set("Allow", http.MethodPost)
		writeError(response, http.StatusMethodNotAllowed, "method_not_allowed", "Only POST is supported", "")
		return
	}
	handler(response, request)
}

func (api *API) bootstrap(response http.ResponseWriter, request *http.Request) {
	persistedLines, err := api.repository.Lines(request.Context())
	if err != nil {
		api.writeInternalError(response, request, "load bootstrap lines", err)
		return
	}
	lines := persistedLines
	capabilities := Capabilities{}
	if api.communications != nil {
		status, statusErr := api.communications.Status(request.Context())
		if statusErr != nil {
			api.logger.Warn("live communication state is unavailable", "error", statusErr)
			lines = attachedPersistedLines(persistedLines)
			capabilities = disconnectedCapabilities()
		} else {
			lines = mergePersistedLineMetadata(status.Lines, persistedLines)
			capabilities = capabilitiesForLines(status.Lines)
			capabilities.WebRTCAudio = capabilities.WebRTCAudio &&
				status.Capabilities.Media && api.callMedia != nil
		}
	} else if api.capabilities != nil {
		capabilities, err = api.capabilities.Capabilities(request.Context())
		if err != nil {
			api.logger.Warn("host agent is unavailable", "error", err)
			capabilities = disconnectedCapabilities()
		} else {
			capabilities.WebRTCAudio = capabilities.WebRTCAudio && api.callMedia != nil
		}
	} else {
		capabilities = disconnectedCapabilities()
	}
	if isMobileRequest(request) && api.rtcConfiguration == nil {
		capabilities.WebRTCAudio = false
	}
	lineCatalog := persistedLines
	lines = filterLinesForPrincipal(request.Context(), lines)
	persistedLines = filterLinesForPrincipal(request.Context(), persistedLines)
	if _, scoped := auth.PrincipalFromContext(request.Context()); scoped &&
		api.communications != nil &&
		capabilities.AgentConnected {
		mediaAvailable := capabilities.WebRTCAudio
		capabilities = capabilitiesForLines(lines)
		capabilities.WebRTCAudio = capabilities.WebRTCAudio && mediaAvailable
	}
	if principal, exists := auth.PrincipalFromContext(request.Context()); !exists ||
		isMobileRequest(request) ||
		!principal.IsAdmin() {
		lineCatalog = persistedLines
	}
	capabilities = gateCapabilities(capabilities)
	lineSettings, err := api.repository.LineSettings(request.Context())
	if err != nil {
		api.writeInternalError(response, request, "load line settings", err)
		return
	}
	if principal, exists := auth.PrincipalFromContext(request.Context()); exists {
		if !principal.CanAccessLine(lineSettings.DefaultLineID) {
			lineSettings.DefaultLineID = ""
			if len(lines) > 0 {
				lineSettings.DefaultLineID = lines[0].ID
			}
		}
	}
	systemSettings, err := api.repository.SystemSettings(request.Context())
	if err != nil {
		api.writeInternalError(response, request, "load system settings", err)
		return
	}
	writeJSON(response, http.StatusOK, bootstrapResponse{
		Capabilities:   capabilities,
		Lines:          lineSummaryResponses(lines),
		LineCatalog:    lineSummaryResponses(lineCatalog),
		LineSettings:   lineSettings,
		SystemSettings: systemSettings,
	})
}

func attachedPersistedLines(lines []store.LineSummary) []store.LineSummary {
	attached := make([]store.LineSummary, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line.EndpointID) == "" ||
			strings.TrimSpace(line.DeviceIMEI) == "" {
			continue
		}
		attached = append(attached, line)
	}
	return attached
}

func mergePersistedLineMetadata(
	liveLines []store.LineSummary,
	persistedLines []store.LineSummary,
) []store.LineSummary {
	byLineID := make(map[string]store.LineSummary, len(persistedLines))
	for _, line := range persistedLines {
		if lineID := strings.TrimSpace(line.ID); lineID != "" {
			byLineID[lineID] = line
		}
	}

	merged := make([]store.LineSummary, len(liveLines))
	for index, live := range liveLines {
		line := live
		persisted, found := byLineID[strings.TrimSpace(line.ID)]
		if found {
			line.LineLabel = persisted.LineLabel
			line.LineColor = persisted.LineColor
			if line.PhoneNumber == "" {
				line.PhoneNumber = persisted.PhoneNumber
			}
			if line.Operator == "" {
				line.Operator = persisted.Operator
			}
			if line.HomeOperatorCode == "" {
				line.HomeOperatorCode = persisted.HomeOperatorCode
			}
			if line.HomeOperatorName == "" {
				line.HomeOperatorName = persisted.HomeOperatorName
			}
			if line.HomeOperatorName == "" {
				line.HomeOperatorName = line.Operator
			}
			if line.DeviceIMEI == "" {
				line.DeviceIMEI = persisted.DeviceIMEI
			}
			if line.DeviceName == "" {
				line.DeviceName = persisted.DeviceName
			}
		}
		merged[index] = line
	}
	return merged
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

func capabilitiesForLines(lines []store.LineSummary) Capabilities {
	capabilities := Capabilities{
		AgentConnected:     true,
		UnavailableReasons: map[string]string{},
	}
	for _, line := range lines {
		capabilities.Dial = capabilities.Dial || line.Capabilities.Dial
		capabilities.AnswerCall = capabilities.AnswerCall || line.Capabilities.AnswerCall
		capabilities.HangupCall = capabilities.HangupCall || line.Capabilities.HangupCall
		capabilities.RejectCall = capabilities.RejectCall || line.Capabilities.RejectCall
		capabilities.SendDTMF = capabilities.SendDTMF || line.Capabilities.SendDTMF
		capabilities.Message = capabilities.Message || line.Capabilities.SendMessage
		capabilities.WebRTCAudio = capabilities.WebRTCAudio || line.Capabilities.Media
	}
	if !capabilities.Dial {
		capabilities.UnavailableReasons["dial"] = "No attached line supports dialing"
	}
	if !capabilities.Message {
		capabilities.UnavailableReasons["message"] = "No attached line supports sending messages"
	}
	return capabilities
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

func (api *API) contactsBatch(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPatch {
		response.Header().Set("Allow", http.MethodPatch)
		writeError(response, http.StatusMethodNotAllowed, "method_not_allowed", "Only PATCH is supported", "")
		return
	}
	var input contactsBatchRequest
	if !decodeJSONBody(response, request, &input) {
		return
	}
	if strings.TrimSpace(input.Action) != "delete" {
		writeError(response, http.StatusBadRequest, "invalid_argument", "action is invalid", "action")
		return
	}
	if len(input.Contacts) == 0 || len(input.Contacts) > 100 {
		writeError(response, http.StatusBadRequest, "invalid_argument", "contacts must contain between 1 and 100 items", "contacts")
		return
	}
	if err := api.repository.DeleteContacts(request.Context(), input.Contacts); err != nil {
		api.writeContactError(response, request, "delete contacts", err)
		return
	}
	api.publishRuntimeResources(
		runtimeevents.ResourceSession,
		runtimeevents.ResourceContacts,
		runtimeevents.ResourceMessages,
		runtimeevents.ResourceCalls,
		runtimeevents.ResourceRecordings,
	)
	response.Header().Set("Cache-Control", "no-store")
	response.WriteHeader(http.StatusNoContent)
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
	var after store.ContactCursor
	hasCursor, ok := decodePageCursor(
		response,
		request,
		"contacts",
		search,
		&after,
	)
	if !ok {
		return
	}
	if hasCursor && strings.TrimSpace(after.ID) == "" {
		writeInvalidPageCursor(response)
		return
	}
	query := store.ContactQuery{
		Search:    search,
		Limit:     limit,
		Lookahead: true,
	}
	if hasCursor {
		query.After = &after
	}
	contacts, err := api.repository.Contacts(request.Context(), query)
	if err != nil {
		api.writeInternalError(response, request, "list contacts", err)
		return
	}
	contacts, hasMore := pageItems(contacts, limit)
	meta := responseMeta{Limit: limit, HasMore: hasMore}
	if hasMore {
		last := contacts[len(contacts)-1]
		meta.NextCursor, err = encodePageCursor(
			"contacts",
			search,
			store.ContactCursor{DisplayName: last.DisplayName, ID: last.ID},
		)
		if err != nil {
			api.writeInternalError(response, request, "encode contacts cursor", err)
			return
		}
	}
	writeJSON(response, http.StatusOK, contactsResponse{Contacts: contacts, Meta: meta})
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
	api.publishRuntimeResources(
		runtimeevents.ResourceSession,
		runtimeevents.ResourceContacts,
		runtimeevents.ResourceMessages,
		runtimeevents.ResourceCalls,
		runtimeevents.ResourceRecordings,
	)
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
	api.publishRuntimeResources(
		runtimeevents.ResourceSession,
		runtimeevents.ResourceContacts,
		runtimeevents.ResourceMessages,
		runtimeevents.ResourceCalls,
		runtimeevents.ResourceRecordings,
	)
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
	api.publishRuntimeResources(
		runtimeevents.ResourceSession,
		runtimeevents.ResourceContacts,
		runtimeevents.ResourceMessages,
		runtimeevents.ResourceCalls,
		runtimeevents.ResourceRecordings,
	)
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
	var after store.ThreadCursor
	hasCursor, ok := decodePageCursor(
		response,
		request,
		"message_threads",
		search,
		&after,
	)
	if !ok {
		return
	}
	if hasCursor &&
		(after.LastMessageID < 0 ||
			strings.TrimSpace(after.LineID) == "" ||
			strings.TrimSpace(after.Peer) == "") {
		writeInvalidPageCursor(response)
		return
	}
	query := store.ThreadQuery{
		Search:    search,
		Limit:     limit,
		Lookahead: true,
	}
	if hasCursor {
		query.After = &after
	}
	threads, err := api.repository.MessageThreads(request.Context(), query)
	if err != nil {
		api.writeInternalError(response, request, "list message threads", err)
		return
	}
	threads, hasMore := pageItems(threads, limit)
	meta := responseMeta{Limit: limit, HasMore: hasMore}
	if hasMore {
		last := threads[len(threads)-1]
		meta.NextCursor, err = encodePageCursor(
			"message_threads",
			search,
			store.ThreadCursor{
				LastTimestamp: last.SortTimestamp,
				LastMessageID: last.LastMessageID,
				LineID:        last.LineID,
				Peer:          last.Peer,
			},
		)
		if err != nil {
			api.writeInternalError(response, request, "encode message threads cursor", err)
			return
		}
	}
	writeJSON(response, http.StatusOK, threadsResponse{
		Threads: messageThreadResponses(threads),
		Meta:    meta,
	})
}

func (api *API) messages(response http.ResponseWriter, request *http.Request) {
	limit, ok := requestLimit(response, request)
	if !ok {
		return
	}
	lineID, ok := boundedFilter(
		response,
		request.URL.Query().Get("line_id"),
		"line_id",
		maxIdentifierLength,
	)
	if !ok {
		return
	}
	if lineID == "" {
		writeError(
			response,
			http.StatusBadRequest,
			"invalid_argument",
			"line_id is required",
			"line_id",
		)
		return
	}
	if !api.requireLineAccess(response, request, lineID) {
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
	queryIdentity := lineID + "\x00" + peer
	var after store.MessageCursor
	hasCursor, ok := decodePageCursor(
		response,
		request,
		"messages",
		queryIdentity,
		&after,
	)
	if !ok {
		return
	}
	if hasCursor && after.ID <= 0 {
		writeInvalidPageCursor(response)
		return
	}
	query := store.MessageQuery{
		LineID:        lineID,
		Peer:          peer,
		Limit:         limit,
		Chronological: true,
		Lookahead:     true,
	}
	if hasCursor {
		query.After = &after
	}
	messages, err := api.repository.Messages(request.Context(), query)
	if err != nil {
		api.writeInternalError(response, request, "list messages", err)
		return
	}
	hasMore := len(messages) > limit
	if hasMore {
		messages = messages[len(messages)-limit:]
	}
	meta := responseMeta{Limit: limit, HasMore: hasMore}
	if hasMore {
		oldest := messages[0]
		meta.NextCursor, err = encodePageCursor(
			"messages",
			queryIdentity,
			store.MessageCursor{Timestamp: oldest.SortTimestamp, ID: oldest.ID},
		)
		if err != nil {
			api.writeInternalError(response, request, "encode messages cursor", err)
			return
		}
	}
	writeJSON(response, http.StatusOK, messagesResponse{
		Messages: messageResponseItems(messages),
		Meta:     meta,
	})
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
	queryIdentity := string(kind) + "\x00" + search
	var after store.CallCursor
	hasCursor, ok := decodePageCursor(
		response,
		request,
		"calls",
		queryIdentity,
		&after,
	)
	if !ok {
		return
	}
	if hasCursor && strings.TrimSpace(after.ID) == "" {
		writeInvalidPageCursor(response)
		return
	}
	query := store.CallQuery{
		Kind:      kind,
		Search:    search,
		Limit:     limit,
		Lookahead: true,
	}
	if hasCursor {
		query.After = &after
	}
	calls, err := api.repository.Calls(request.Context(), query)
	if err != nil {
		api.writeInternalError(response, request, "list calls", err)
		return
	}
	calls, hasMore := pageItems(calls, limit)
	meta := responseMeta{Limit: limit, HasMore: hasMore}
	if hasMore {
		last := calls[len(calls)-1]
		meta.NextCursor, err = encodePageCursor(
			"calls",
			queryIdentity,
			store.CallCursor{EndedAt: last.SortEndedAt, ID: last.ID},
		)
		if err != nil {
			api.writeInternalError(response, request, "encode calls cursor", err)
			return
		}
	}
	writeJSON(response, http.StatusOK, callsResponse{
		Calls: callRecordResponses(calls),
		Meta:  meta,
	})
}

func (api *API) devices(response http.ResponseWriter, request *http.Request) {
	devices, err := api.repository.Devices(request.Context())
	if err != nil {
		api.writeInternalError(response, request, "list devices", err)
		return
	}
	if api.communications != nil {
		status, statusErr := api.communications.Status(request.Context())
		if statusErr != nil {
			api.logger.Warn("live device network state is unavailable", "error", statusErr)
		} else if status.Connected {
			devices = mergeLiveDeviceNetwork(devices, status.Lines)
		}
	}
	devices = filterDevicesForPrincipal(request.Context(), devices)
	writeJSON(response, http.StatusOK, devicesResponse{
		Devices: devices,
	})
}

func mergeLiveDeviceNetwork(
	devices []store.Device,
	lines []store.LineSummary,
) []store.Device {
	byIMEI := make(map[string]store.LineSummary, len(lines))
	byICCID := make(map[string]store.LineSummary, len(lines))
	for _, line := range lines {
		if imei := strings.TrimSpace(line.DeviceIMEI); imei != "" {
			byIMEI[imei] = line
		}
		if iccid := strings.TrimSpace(line.ICCID); iccid != "" {
			byICCID[iccid] = line
		}
	}

	merged := append([]store.Device(nil), devices...)
	for index, device := range merged {
		line, found := byIMEI[strings.TrimSpace(device.IMEI)]
		if !found {
			line, found = byICCID[strings.TrimSpace(device.CurrentICCID)]
		}
		if !found || device.SIM == nil {
			continue
		}
		sim := *device.SIM
		sim.HomeOperatorCode = line.HomeOperatorCode
		sim.HomeOperatorName = line.HomeOperatorName
		if sim.HomeOperatorName == "" {
			sim.HomeOperatorName = sim.Operator
		}
		if sim.Operator == "" {
			sim.Operator = sim.HomeOperatorName
		}
		sim.ServingOperatorCode = line.ServingOperatorCode
		sim.ServingOperatorName = line.ServingOperatorName
		sim.RegistrationStateKnown = line.RegistrationStateKnown
		sim.RegistrationStateCode = line.RegistrationStateCode
		sim.RegistrationState = line.RegistrationState
		sim.Roaming = line.Roaming
		if line.RegistrationStateKnown {
			sim.RegStatus = int64(line.RegistrationStateCode)
			sim.RegStatusText = line.RegistrationState
		}
		device.SIM = &sim
		merged[index] = device
	}
	return merged
}

func (api *API) writeInternalError(response http.ResponseWriter, request *http.Request, operation string, err error) {
	if requestWasCanceled(request, err) {
		return
	}
	api.logger.Error(operation, "method", request.Method, "path", request.URL.Path, "error", err)
	writeError(response, http.StatusInternalServerError, "internal_error", "Request could not be completed", "")
}

func requestWasCanceled(request *http.Request, err error) bool {
	return errors.Is(err, context.Canceled) ||
		errors.Is(request.Context().Err(), context.Canceled)
}
