package modemmanager

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/godbus/dbus/v5"
	"github.com/human-agent65535/modemdeck/agent/internal/domain"
	"github.com/human-agent65535/modemdeck/agent/internal/usbrecovery"
)

const (
	serviceName             = "org.freedesktop.ModemManager1"
	managerPath             = dbus.ObjectPath("/org/freedesktop/ModemManager1")
	objectManagerInterface  = "org.freedesktop.DBus.ObjectManager"
	busServiceName          = "org.freedesktop.DBus"
	busPath                 = dbus.ObjectPath("/org/freedesktop/DBus")
	busInterface            = "org.freedesktop.DBus"
	propertiesInterface     = "org.freedesktop.DBus.Properties"
	introspectableInterface = "org.freedesktop.DBus.Introspectable"
	terminalCallRetention   = 30 * time.Second
	signalRefreshInterval   = uint32(10)
	signalSetupRetryDelay   = 30 * time.Second
)

type Provider struct {
	caller        Caller
	epochResolver providerEpochResolver
	close         func() error
	now           func() time.Time
	ids           *instanceIDs
	identityMu    sync.Mutex
	dataPlane     DataPlane
	ownedBearers  *bearerOwnershipStore
	radioStates   *radioStateStore
	usbRecovery   USBRecovery

	callMu        sync.Mutex
	configMu      sync.Mutex
	snapshotMu    sync.Mutex
	terminalCalls map[string]terminalCallProjection

	networkOperationMu sync.Mutex
	networkOperations  map[string]struct{}

	telemetryMu        sync.Mutex
	signalSetupStates  map[string]signalSetupState
	servingRadioMu     sync.Mutex
	servingRadioStates map[string]servingRadioState

	voiceProbeMu sync.Mutex
	voiceProbes  map[string]voiceProbeResult

	atCallStateMu  sync.Mutex
	atCalls        map[string]map[int]atCallLifecycle
	atPendingCalls map[string]atCallLifecycle
	atCallSequence uint64

	messageProperties *messagePropertyCache
	changes           *changeHub
}

type terminalCallProjection struct {
	call      domain.Call
	expiresAt time.Time
}

type signalSetupState struct {
	complete    bool
	nextAttempt time.Time
}

type providerEpochResolver interface {
	ResolveEpoch(context.Context) (string, error)
}

type callerProviderEpochResolver struct {
	caller Caller
}

func (r callerProviderEpochResolver) ResolveEpoch(ctx context.Context) (string, error) {
	body, err := r.caller.Call(
		ctx,
		busServiceName,
		busPath,
		busInterface+".GetNameOwner",
		dbus.FlagNoAutoStart,
		serviceName,
	)
	if err != nil {
		return "", err
	}
	var owner string
	if err := dbus.Store(body, &owner); err != nil {
		return "", fmt.Errorf("decode ModemManager D-Bus owner: %w", err)
	}
	owner = strings.TrimSpace(owner)
	if owner == "" {
		return "", fmt.Errorf("ModemManager D-Bus owner is empty")
	}

	body, err = r.caller.Call(
		ctx,
		busServiceName,
		busPath,
		busInterface+".GetId",
		dbus.FlagNoAutoStart,
	)
	if err != nil {
		return "", err
	}
	var busID string
	if err := dbus.Store(body, &busID); err != nil {
		return "", fmt.Errorf("decode D-Bus instance ID: %w", err)
	}
	busID = strings.TrimSpace(busID)
	if busID == "" {
		return "", fmt.Errorf("D-Bus instance ID is empty")
	}
	return busID + "/" + owner, nil
}

type staticProviderEpochResolver struct {
	epoch string
}

func (r staticProviderEpochResolver) ResolveEpoch(context.Context) (string, error) {
	return r.epoch, nil
}

func New(caller Caller) (*Provider, error) {
	return NewWithOptions(caller, Options{})
}

func newProvider(caller Caller, ids *instanceIDs) *Provider {
	provider, err := newProviderWithOptions(caller, ids, Options{})
	if err != nil {
		panic(err)
	}
	return provider
}

func NewWithOptions(caller Caller, options Options) (*Provider, error) {
	return newProviderWithOptions(caller, newInstanceIDs(), options)
}

func newProviderWithOptions(
	caller Caller,
	ids *instanceIDs,
	options Options,
) (*Provider, error) {
	if ids == nil {
		ids = newInstanceIDs()
	}
	if options.DataPlane == nil {
		options.DataPlane = noopDataPlane{}
	}
	if options.USBRecovery == nil {
		options.USBRecovery = usbrecovery.New()
	}
	ownedBearers, err := newBearerOwnershipStore(options.BearerStateFile)
	if err != nil {
		return nil, fmt.Errorf("load owned bearer state: %w", err)
	}
	radioStates, err := newRadioStateStore(options.RadioStateFile)
	if err != nil {
		return nil, fmt.Errorf("load radio state: %w", err)
	}
	var resolver providerEpochResolver = callerProviderEpochResolver{caller: caller}
	if epoch := ids.providerEpoch(); epoch != "" {
		resolver = staticProviderEpochResolver{epoch: epoch}
	}
	return &Provider{
		caller:             caller,
		epochResolver:      resolver,
		now:                time.Now,
		ids:                ids,
		dataPlane:          options.DataPlane,
		ownedBearers:       ownedBearers,
		radioStates:        radioStates,
		usbRecovery:        options.USBRecovery,
		terminalCalls:      make(map[string]terminalCallProjection),
		networkOperations:  make(map[string]struct{}),
		signalSetupStates:  make(map[string]signalSetupState),
		servingRadioStates: make(map[string]servingRadioState),
		voiceProbes:        make(map[string]voiceProbeResult),
		atCalls:            make(map[string]map[int]atCallLifecycle),
		atPendingCalls:     make(map[string]atCallLifecycle),
		messageProperties:  newMessagePropertyCache(defaultMessagePropertyCacheLimit),
		changes:            newChangeHub(),
	}, nil
}

func OpenSystemBus() (*Provider, error) {
	return OpenSystemBusWithOptions(Options{})
}

func OpenSystemBusWithOptions(options Options) (*Provider, error) {
	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		return nil, fmt.Errorf("connect to system D-Bus: %w", err)
	}
	provider, err := NewWithOptions(&connectionCaller{conn: conn}, options)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	stopChanges, err := provider.startSystemBusChangeWatcher(conn)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	provider.close = func() error {
		stopChanges()
		return conn.Close()
	}
	return provider, nil
}

func (p *Provider) Close() error {
	if p == nil || p.close == nil {
		return nil
	}
	return p.close()
}

func (p *Provider) Health(ctx context.Context) (domain.ProviderHealth, error) {
	const operation = "health"
	health := domain.ProviderHealth{Name: serviceName}
	if err := p.requireCaller(ctx, operation); err != nil {
		return health, err
	}

	body, err := p.caller.Call(
		ctx,
		busServiceName,
		busPath,
		busInterface+".NameHasOwner",
		dbus.FlagNoAutoStart,
		serviceName,
	)
	if err != nil {
		return health, mapCallError(operation, "failed to query the ModemManager D-Bus owner", err)
	}

	var available bool
	if err := dbus.Store(body, &available); err != nil {
		return health, domain.Internal(operation, "ModemManager owner response was malformed", err)
	}
	health.Available = available
	if available {
		identity, err := p.resolveProviderIdentity(ctx, operation)
		if err != nil {
			return health, err
		}
		health.BootEpoch = identity.providerEpoch()
		health.Capabilities = implementedCapabilities()
		health.RuntimeVersion, err = p.runtimeVersion(ctx, operation)
		if err != nil {
			return health, err
		}
	} else {
		p.clearProviderIdentity()
	}
	return health, nil
}

func (p *Provider) Snapshot(ctx context.Context) (domain.Snapshot, error) {
	const operation = "snapshot"
	// A complete call snapshot and every call/media command share one provider
	// order. This remains necessary after an HTTP client times out: the handler
	// may still be finishing its provider command when the App asks for the
	// reconciliation snapshot.
	p.callMu.Lock()
	defer p.callMu.Unlock()
	identity, err := p.resolveProviderIdentity(ctx, operation)
	if err != nil {
		return domain.Snapshot{}, err
	}
	objects, err := p.managedObjects(ctx, operation)
	if err != nil {
		return domain.Snapshot{}, err
	}
	if p.prepareExtendedSignal(ctx, operation, objects) {
		objects, err = p.managedObjects(ctx, operation)
		if err != nil {
			return domain.Snapshot{}, err
		}
	}
	objects, err = p.hydrateCalls(ctx, operation, objects)
	if err != nil {
		return domain.Snapshot{}, err
	}
	objects, err = p.hydrateMessages(ctx, operation, objects)
	if err != nil {
		return domain.Snapshot{}, err
	}
	objects, err = p.hydrateReferencedSIMs(ctx, operation, objects)
	if err != nil {
		return domain.Snapshot{}, err
	}
	parsed := ParseManagedObjects(objects, identity)
	p.initializeVoiceModel(ctx, operation, &parsed)
	if err := p.refreshAuthoritativeATCalls(ctx, operation, &parsed); err != nil {
		return domain.Snapshot{}, err
	}
	p.projectServingRadios(ctx, operation, &parsed)
	p.projectDesiredRadioState(parsed.Lines)
	observedAt := p.now().UTC()
	p.projectTerminatedCalls(&parsed, observedAt)
	revision, err := snapshotRevision(
		parsed.Lines,
		parsed.Calls,
		parsed.Messages,
		parsed.DeliveryReports,
	)
	if err != nil {
		return domain.Snapshot{}, domain.Internal(operation, "failed to revision the ModemManager snapshot", err)
	}
	return domain.Snapshot{
		Revision:        revision,
		ObservedAt:      observedAt,
		Lines:           parsed.Lines,
		Calls:           parsed.Calls,
		Messages:        parsed.Messages,
		DeliveryReports: parsed.DeliveryReports,
	}, nil
}

func (p *Provider) projectDesiredRadioState(lines []domain.Line) {
	for index := range lines {
		line := &lines[index]
		if !line.SavedPolicySupported || p.radioStates == nil {
			continue
		}
		line.RadioDesiredEnabled = p.radioStates.enabled(line.ID)
		line.RadioDesiredEnabledKnown = true
	}
}

func (p *Provider) StartCall(ctx context.Context, request domain.StartCallRequest) (domain.CommandReceipt, error) {
	const operation = "start_call"
	request.RequestID = strings.TrimSpace(request.RequestID)
	request.LineID = strings.TrimSpace(request.LineID)
	request.Number = strings.TrimSpace(request.Number)
	if err := validateRequestID(operation, request.RequestID); err != nil {
		return domain.CommandReceipt{}, err
	}
	if request.LineID == "" || request.Number == "" {
		return domain.CommandReceipt{}, domain.InvalidArgument(operation, "line_id and number are required")
	}
	if invalidText(request.Number, 64) {
		return domain.CommandReceipt{}, domain.InvalidArgument(operation, "number is invalid")
	}

	p.callMu.Lock()
	defer p.callMu.Unlock()

	parsed, err := p.snapshotContent(ctx, operation)
	if err != nil {
		return domain.CommandReceipt{}, err
	}
	p.projectVoiceCapabilities(ctx, operation, &parsed)
	line, found := findLine(parsed.Lines, request.LineID)
	if !found {
		return domain.CommandReceipt{}, domain.NotFound(operation, "line was not found")
	}
	if !line.Capabilities.Dial {
		return domain.CommandReceipt{}, domain.NotSupported(
			operation,
			"line does not expose verified outgoing call control",
		)
	}
	if lineHasCall(parsed.Calls, line.ID, "") {
		return domain.CommandReceipt{}, domain.Conflict(operation, "line already has an ongoing call")
	}
	linePath := parsed.LinePaths[line.ID]
	if result, found := p.ensureQuectelMediaRouting(
		ctx,
		operation,
		parsed.ids,
		line,
		linePath,
	); found {
		projectVoiceProbeResult(&line, result)
	}
	if parsed.CallBackends[line.ID] == callControlQuectelAT {
		return p.startATCall(
			ctx,
			operation,
			request,
			line,
			linePath,
		)
	}

	properties := map[string]dbus.Variant{
		"number": dbus.MakeVariant(request.Number),
	}
	slog.Debug(
		"outgoing call create requested",
		"component", "modemmanager",
		"request_id", request.RequestID,
		"line_id", line.ID,
	)
	body, err := p.call(
		ctx,
		linePath,
		voiceInterface+".CreateCall",
		operation,
		"ModemManager failed to create the outgoing call",
		properties,
	)
	if err != nil {
		slog.Warn(
			"outgoing call create failed",
			"component", "modemmanager",
			"request_id", request.RequestID,
			"line_id", line.ID,
			"error", err,
		)
		return domain.CommandReceipt{}, err
	}
	callPath, err := objectPathResult(operation, "ModemManager returned an invalid call path", body)
	if err != nil {
		return domain.CommandReceipt{}, err
	}
	if !strings.HasPrefix(string(callPath), "/org/freedesktop/ModemManager1/Call/") {
		return domain.CommandReceipt{}, domain.Internal(operation, "ModemManager returned an unexpected call path", nil)
	}
	slog.Debug(
		"outgoing call object created",
		"component", "modemmanager",
		"request_id", request.RequestID,
		"line_id", line.ID,
		"call_path", callPath,
	)
	callID := parsed.ids.callID(callPath)
	if _, err := p.call(
		ctx,
		callPath,
		callInterface+".Start",
		operation,
		"ModemManager failed to start the outgoing call",
	); err != nil {
		slog.Warn(
			"outgoing call start failed",
			"component", "modemmanager",
			"request_id", request.RequestID,
			"line_id", line.ID,
			"call_path", callPath,
			"error", err,
		)
		return domain.CommandReceipt{}, err
	}
	slog.Info(
		"outgoing call start accepted",
		"component", "modemmanager",
		"request_id", request.RequestID,
		"line_id", line.ID,
		"call_path", callPath,
	)
	return domain.CommandReceipt{RequestID: request.RequestID, ResourceID: callID}, nil
}

func (p *Provider) AnswerCall(ctx context.Context, request domain.CallCommandRequest) (domain.CommandReceipt, error) {
	const operation = "answer_call"
	return p.controlCall(ctx, operation, request, func(call domain.Call, parsed ParsedObjects) error {
		if call.StateCode != 3 {
			return domain.Conflict(operation, "call is not ringing")
		}
		if lineHasCall(parsed.Calls, call.LineID, call.ID) {
			return domain.Conflict(operation, "line already has another ongoing call")
		}
		line, found := findLine(parsed.Lines, call.LineID)
		if !found {
			return domain.NotFound(operation, "call line was not found")
		}
		if !line.Capabilities.AnswerCall {
			return domain.NotSupported(operation, "answering calls is not verified for the line")
		}
		linePath := parsed.LinePaths[line.ID]
		if result, found := p.ensureQuectelMediaRouting(
			ctx,
			operation,
			parsed.ids,
			line,
			linePath,
		); found {
			projectVoiceProbeResult(&line, result)
		}
		if lineID, isATCall := parsed.ATCallLines[call.ID]; isATCall {
			_, err := p.commandATPath(
				ctx,
				parsed.LinePaths[lineID],
				operation,
				quectelAnswerCall,
			)
			if err == nil {
				p.refreshATLine(ctx, operation, &line, parsed.LinePaths[lineID])
				p.publishChange()
			}
			return err
		}
		_, err := p.call(
			ctx,
			parsed.CallPaths[call.ID],
			callInterface+".Accept",
			operation,
			"ModemManager failed to answer the call",
		)
		return err
	})
}

func (p *Provider) RejectCall(ctx context.Context, request domain.CallCommandRequest) (domain.CommandReceipt, error) {
	const operation = "reject_call"
	terminationContext, cancelTermination := detachedTimeoutContext(
		ctx,
		callTerminationCommandTimeout,
	)
	defer cancelTermination()
	return p.controlCall(terminationContext, operation, request, func(call domain.Call, parsed ParsedObjects) error {
		if call.StateCode != 3 && call.StateCode != 6 {
			return domain.Conflict(operation, "call is not an incoming ringing or waiting call")
		}
		line, found := findLine(parsed.Lines, call.LineID)
		if !found {
			return domain.NotFound(operation, "call line was not found")
		}
		if !line.Capabilities.RejectCall {
			return domain.NotSupported(operation, "rejecting calls is not verified for the line")
		}
		return p.terminateCall(terminationContext, operation, call, parsed)
	})
}

func (p *Provider) HangupCall(ctx context.Context, request domain.CallCommandRequest) (domain.CommandReceipt, error) {
	const operation = "hangup_call"
	terminationContext, cancelTermination := detachedTimeoutContext(
		ctx,
		callTerminationCommandTimeout,
	)
	defer cancelTermination()
	return p.controlCall(terminationContext, operation, request, func(call domain.Call, parsed ParsedObjects) error {
		if call.StateCode == callStateTerminated {
			return domain.Conflict(operation, "call is already terminated")
		}
		return p.terminateCall(terminationContext, operation, call, parsed)
	})
}

func (p *Provider) SendDTMF(ctx context.Context, request domain.DTMFRequest) (domain.CommandReceipt, error) {
	const operation = "send_dtmf"
	request.RequestID = strings.TrimSpace(request.RequestID)
	request.CallID = strings.TrimSpace(request.CallID)
	request.Digits = strings.ToUpper(strings.TrimSpace(request.Digits))
	if err := validateRequestID(operation, request.RequestID); err != nil {
		return domain.CommandReceipt{}, err
	}
	if request.CallID == "" {
		return domain.CommandReceipt{}, domain.InvalidArgument(operation, "call id is required")
	}
	if !validDTMF(request.Digits) {
		return domain.CommandReceipt{}, domain.InvalidArgument(operation, "digits must contain 1 to 32 DTMF characters (0-9, A-D, *, #)")
	}

	p.callMu.Lock()
	defer p.callMu.Unlock()

	parsed, err := p.snapshotContent(ctx, operation)
	if err != nil {
		return domain.CommandReceipt{}, err
	}
	p.projectVoiceCapabilities(ctx, operation, &parsed)
	if err := p.refreshAuthoritativeATCalls(ctx, operation, &parsed); err != nil {
		return domain.CommandReceipt{}, err
	}
	call, found := findCall(parsed.Calls, request.CallID)
	if !found {
		return domain.CommandReceipt{}, domain.NotFound(operation, "active call was not found")
	}
	if call.StateCode != 4 {
		return domain.CommandReceipt{}, domain.Conflict(operation, "DTMF requires an active call")
	}
	line, found := findLine(parsed.Lines, call.LineID)
	if !found {
		return domain.CommandReceipt{}, domain.NotFound(operation, "call line was not found")
	}
	if !line.Capabilities.SendDTMF {
		return domain.CommandReceipt{}, domain.NotSupported(operation, "DTMF is not verified for the line")
	}
	if lineID, isATCall := parsed.ATCallLines[call.ID]; isATCall {
		for _, digit := range request.Digits {
			command := fmt.Sprintf(`AT+VTS="%s"`, string(digit))
			if _, err := p.commandATPath(
				ctx,
				parsed.LinePaths[lineID],
				operation,
				command,
			); err != nil {
				return domain.CommandReceipt{}, err
			}
		}
		return domain.CommandReceipt{
			RequestID:  request.RequestID,
			ResourceID: call.ID,
		}, nil
	}
	callPath := parsed.CallPaths[call.ID]
	if err := p.requireCallMethod(ctx, callPath, "SendDtmf", operation); err != nil {
		return domain.CommandReceipt{}, err
	}
	version, err := p.runtimeVersion(ctx, operation)
	if err != nil {
		return domain.CommandReceipt{}, err
	}
	digits := []string{request.Digits}
	if !versionAtLeast(version, 1, 26) {
		digits = make([]string, 0, len(request.Digits))
		for _, digit := range request.Digits {
			digits = append(digits, string(digit))
		}
	}
	for _, digit := range digits {
		if _, err := p.call(
			ctx,
			callPath,
			callInterface+".SendDtmf",
			operation,
			"ModemManager failed to send DTMF",
			digit,
		); err != nil {
			return domain.CommandReceipt{}, err
		}
	}
	return domain.CommandReceipt{RequestID: request.RequestID, ResourceID: call.ID}, nil
}

func (p *Provider) SendMessage(ctx context.Context, request domain.SendMessageRequest) (domain.CommandReceipt, error) {
	const operation = "send_message"
	request.RequestID = strings.TrimSpace(request.RequestID)
	request.LineID = strings.TrimSpace(request.LineID)
	request.Number = strings.TrimSpace(request.Number)
	if err := validateRequestID(operation, request.RequestID); err != nil {
		return domain.CommandReceipt{}, err
	}
	if request.LineID == "" || request.Number == "" || strings.TrimSpace(request.Text) == "" {
		return domain.CommandReceipt{}, domain.InvalidArgument(operation, "line_id, number, and text are required")
	}
	if invalidText(request.Number, 64) {
		return domain.CommandReceipt{}, domain.InvalidArgument(operation, "number is invalid")
	}
	if len(request.Text) > 64<<10 {
		return domain.CommandReceipt{}, domain.InvalidArgument(operation, "text is too large")
	}

	parsed, err := p.snapshotContent(ctx, operation)
	if err != nil {
		return domain.CommandReceipt{}, err
	}
	line, found := findLine(parsed.Lines, request.LineID)
	if !found {
		return domain.CommandReceipt{}, domain.NotFound(operation, "line was not found")
	}
	if !line.Capabilities.MessagingInterface {
		return domain.CommandReceipt{}, domain.NotSupported(operation, "line does not expose the ModemManager Messaging interface")
	}

	linePath := parsed.LinePaths[line.ID]
	messagePath, err := p.createSMS(
		ctx,
		linePath,
		request.Number,
		request.Text,
		request.DeliveryReportRequested,
	)
	deliveryReportUnsupported := false
	if err != nil {
		if !request.DeliveryReportRequested || !deliveryReportCreateRejected(err) {
			return domain.CommandReceipt{}, err
		}
		messagePath, err = p.createSMS(ctx, linePath, request.Number, request.Text, false)
		if err != nil {
			return domain.CommandReceipt{}, err
		}
		deliveryReportUnsupported = true
	}

	if err := p.sendSMS(ctx, messagePath); err != nil {
		if !request.DeliveryReportRequested ||
			deliveryReportUnsupported ||
			!deliveryReportSendRejected(err) {
			return domain.CommandReceipt{}, err
		}
		if deleteErr := p.deleteSMSPath(ctx, linePath, messagePath); deleteErr != nil {
			return domain.CommandReceipt{}, deleteErr
		}
		messagePath, err = p.createSMS(ctx, linePath, request.Number, request.Text, false)
		if err != nil {
			return domain.CommandReceipt{}, err
		}
		if err := p.sendSMS(ctx, messagePath); err != nil {
			return domain.CommandReceipt{}, err
		}
		deliveryReportUnsupported = true
	}
	return domain.CommandReceipt{
		RequestID:                 request.RequestID,
		ResourceID:                parsed.ids.messageID(messagePath),
		DeliveryReportUnsupported: deliveryReportUnsupported,
	}, nil
}

func (p *Provider) createSMS(
	ctx context.Context,
	linePath dbus.ObjectPath,
	number string,
	text string,
	deliveryReportRequested bool,
) (dbus.ObjectPath, error) {
	const operation = "send_message"
	properties := map[string]dbus.Variant{
		"number": dbus.MakeVariant(number),
		"text":   dbus.MakeVariant(text),
	}
	if deliveryReportRequested {
		properties["delivery-report-request"] = dbus.MakeVariant(true)
	}
	body, err := p.call(
		ctx,
		linePath,
		messagingInterface+".Create",
		operation,
		"ModemManager failed to create the SMS",
		properties,
	)
	if err != nil {
		return "", err
	}
	messagePath, err := objectPathResult(
		operation,
		"ModemManager returned an invalid SMS path",
		body,
	)
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(string(messagePath), "/org/freedesktop/ModemManager1/SMS/") {
		return "", domain.Internal(
			operation,
			"ModemManager returned an unexpected SMS path",
			nil,
		)
	}
	return messagePath, nil
}

func (p *Provider) sendSMS(ctx context.Context, messagePath dbus.ObjectPath) error {
	_, err := p.call(
		ctx,
		messagePath,
		smsInterface+".Send",
		"send_message",
		"ModemManager failed to send the SMS",
	)
	return err
}

func (p *Provider) deleteSMSPath(
	ctx context.Context,
	linePath dbus.ObjectPath,
	messagePath dbus.ObjectPath,
) error {
	_, err := p.call(
		ctx,
		linePath,
		messagingInterface+".Delete",
		"send_message",
		"ModemManager failed to remove the rejected delivery-report SMS",
		messagePath,
	)
	if operationError, ok := domain.AsOperationError(err); ok &&
		operationError.Code == domain.ErrorNotFound {
		err = nil
	}
	if err == nil {
		p.messageProperties.clear()
	}
	return err
}

func (p *Provider) DeleteMessage(ctx context.Context, request domain.DeleteMessageRequest) error {
	const operation = "delete_message"
	request.MessageID = strings.TrimSpace(request.MessageID)
	if !validMessageID(request.MessageID) {
		return domain.InvalidArgument(operation, "message id is invalid")
	}

	identity, err := p.resolveProviderIdentity(ctx, operation)
	if err != nil {
		return err
	}
	objects, err := p.managedObjects(ctx, operation)
	if err != nil {
		return err
	}
	objects, err = p.hydrateMessages(ctx, operation, objects)
	if err != nil {
		return err
	}
	parsed := ParseManagedObjects(objects, identity)
	lineID, found := messageLineID(parsed, request.MessageID)
	if !found {
		return nil
	}
	messagePath := parsed.MessagePaths[request.MessageID]
	linePath := parsed.LinePaths[lineID]
	if messagePath == "" || linePath == "" {
		return domain.Internal(operation, "message path mapping is unavailable", nil)
	}
	if _, err := p.call(
		ctx,
		linePath,
		messagingInterface+".Delete",
		operation,
		"ModemManager failed to delete the SMS",
		messagePath,
	); err != nil {
		if operationError, ok := domain.AsOperationError(err); ok &&
			operationError.Code == domain.ErrorNotFound {
			return nil
		}
		return err
	}
	p.messageProperties.clear()
	return nil
}

func validMessageID(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for _, character := range value {
		if (character >= 'a' && character <= 'z') ||
			(character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') ||
			character == '-' || character == '_' {
			continue
		}
		return false
	}
	return true
}

func (p *Provider) controlCall(
	ctx context.Context,
	operation string,
	request domain.CallCommandRequest,
	run func(domain.Call, ParsedObjects) error,
) (domain.CommandReceipt, error) {
	request.RequestID = strings.TrimSpace(request.RequestID)
	request.CallID = strings.TrimSpace(request.CallID)
	if err := validateRequestID(operation, request.RequestID); err != nil {
		return domain.CommandReceipt{}, err
	}
	if request.CallID == "" {
		return domain.CommandReceipt{}, domain.InvalidArgument(operation, "call id is required")
	}

	p.callMu.Lock()
	defer p.callMu.Unlock()

	parsed, err := p.snapshotContent(ctx, operation)
	if err != nil {
		return domain.CommandReceipt{}, err
	}
	p.projectVoiceCapabilities(ctx, operation, &parsed)
	if err := p.refreshAuthoritativeATCalls(ctx, operation, &parsed); err != nil {
		return domain.CommandReceipt{}, err
	}
	call, found := findCall(parsed.Calls, request.CallID)
	if !found {
		return domain.CommandReceipt{}, domain.NotFound(operation, "active call was not found")
	}
	if err := run(call, parsed); err != nil {
		return domain.CommandReceipt{}, err
	}
	return domain.CommandReceipt{RequestID: request.RequestID, ResourceID: call.ID}, nil
}

func (p *Provider) projectTerminatedCalls(parsed *ParsedObjects, observedAt time.Time) {
	p.snapshotMu.Lock()
	defer p.snapshotMu.Unlock()

	lineIndexes := make(map[string]int, len(parsed.Lines))
	for index := range parsed.Lines {
		lineIndexes[parsed.Lines[index].ID] = index
	}

	currentCalls := make(map[string]struct{}, len(parsed.Calls))
	for _, call := range parsed.Calls {
		currentCalls[call.ID] = struct{}{}
		if call.StateCode == callStateTerminated {
			p.terminalCalls[call.ID] = terminalCallProjection{
				call:      call,
				expiresAt: observedAt.Add(terminalCallRetention),
			}
			continue
		}
		delete(p.terminalCalls, call.ID)
	}

	for id, projection := range p.terminalCalls {
		if _, current := currentCalls[id]; current {
			continue
		}
		lineIndex, linePresent := lineIndexes[projection.call.LineID]
		if !linePresent || !observedAt.Before(projection.expiresAt) {
			delete(p.terminalCalls, id)
			continue
		}
		parsed.Calls = append(parsed.Calls, projection.call)
		if !containsString(parsed.Lines[lineIndex].CallIDs, id) {
			parsed.Lines[lineIndex].CallIDs = append(parsed.Lines[lineIndex].CallIDs, id)
			sort.Strings(parsed.Lines[lineIndex].CallIDs)
		}
	}
	sort.Slice(parsed.Calls, func(i, j int) bool {
		return parsed.Calls[i].ID < parsed.Calls[j].ID
	})
}

func (p *Provider) runtimeVersion(ctx context.Context, operation string) (string, error) {
	body, err := p.call(
		ctx,
		managerPath,
		propertiesInterface+".Get",
		operation,
		"failed to query the ModemManager runtime version",
		serviceName,
		"Version",
	)
	if err != nil {
		if operationError, ok := domain.AsOperationError(err); ok &&
			(operationError.Code == domain.ErrorNotSupported ||
				operationError.Code == domain.ErrorNotFound) {
			return "", nil
		}
		return "", err
	}

	var value dbus.Variant
	if err := dbus.Store(body, &value); err != nil {
		return "", domain.Internal(operation, "ModemManager version response was malformed", err)
	}
	version, ok := value.Value().(string)
	if !ok {
		return "", domain.Internal(operation, "ModemManager version was not a string", nil)
	}
	return strings.TrimSpace(version), nil
}

type introspectionNode struct {
	Interfaces []introspectionInterface `xml:"interface"`
}

type introspectionInterface struct {
	Name    string                `xml:"name,attr"`
	Methods []introspectionMethod `xml:"method"`
}

type introspectionMethod struct {
	Name string `xml:"name,attr"`
}

func (p *Provider) requireCallMethod(
	ctx context.Context,
	path dbus.ObjectPath,
	methodName string,
	operation string,
) error {
	body, err := p.call(
		ctx,
		path,
		introspectableInterface+".Introspect",
		operation,
		"failed to introspect the ModemManager call object",
	)
	if err != nil {
		return err
	}
	var document string
	if err := dbus.Store(body, &document); err != nil {
		return domain.Internal(operation, "ModemManager call introspection response was malformed", err)
	}
	var node introspectionNode
	if err := xml.Unmarshal([]byte(document), &node); err != nil {
		return domain.Internal(operation, "ModemManager call introspection XML was malformed", err)
	}
	for _, iface := range node.Interfaces {
		if iface.Name != callInterface {
			continue
		}
		for _, method := range iface.Methods {
			if method.Name == methodName {
				return nil
			}
		}
	}
	return domain.NotSupported(
		operation,
		fmt.Sprintf("ModemManager call method %s is not available", methodName),
	)
}

func versionAtLeast(version string, wantMajor, wantMinor int) bool {
	parts := strings.Split(version, ".")
	if len(parts) < 2 {
		return false
	}
	major, majorOK := numericPrefix(parts[0])
	minor, minorOK := numericPrefix(parts[1])
	if !majorOK || !minorOK {
		return false
	}
	return major > wantMajor || major == wantMajor && minor >= wantMinor
}

func numericPrefix(value string) (int, bool) {
	end := 0
	for end < len(value) && value[end] >= '0' && value[end] <= '9' {
		end++
	}
	if end == 0 {
		return 0, false
	}
	number, err := strconv.Atoi(value[:end])
	return number, err == nil
}

func (p *Provider) snapshotContent(
	ctx context.Context,
	operation string,
) (ParsedObjects, error) {
	identity, err := p.resolveProviderIdentity(ctx, operation)
	if err != nil {
		return ParsedObjects{}, err
	}
	objects, err := p.managedObjects(ctx, operation)
	if err != nil {
		return ParsedObjects{}, err
	}
	objects, err = p.hydrateCalls(ctx, operation, objects)
	if err != nil {
		return ParsedObjects{}, err
	}
	return ParseManagedObjects(objects, identity), nil
}

func (p *Provider) resolveProviderIdentity(
	ctx context.Context,
	operation string,
) (*instanceIDs, error) {
	p.identityMu.Lock()
	defer p.identityMu.Unlock()

	if err := p.requireCaller(ctx, operation); err != nil {
		return nil, err
	}
	if p.epochResolver == nil {
		return nil, domain.Unavailable(operation, "ModemManager epoch resolver is unavailable", nil)
	}
	epoch, err := p.epochResolver.ResolveEpoch(ctx)
	if err != nil {
		return nil, mapCallError(operation, "failed to resolve the ModemManager provider epoch", err)
	}
	if epoch == "" {
		return nil, domain.Unavailable(operation, "ModemManager provider epoch is unavailable", nil)
	}
	previousEpoch := p.ids.providerEpoch()
	if err := p.ids.setProviderEpoch(epoch); err != nil {
		return nil, domain.Internal(operation, "ModemManager provider epoch was invalid", err)
	}
	if previousEpoch != "" && previousEpoch != epoch {
		p.messageProperties.clear()
		p.snapshotMu.Lock()
		p.terminalCalls = make(map[string]terminalCallProjection)
		p.snapshotMu.Unlock()
		p.telemetryMu.Lock()
		p.signalSetupStates = make(map[string]signalSetupState)
		p.telemetryMu.Unlock()
		p.servingRadioMu.Lock()
		p.servingRadioStates = make(map[string]servingRadioState)
		p.servingRadioMu.Unlock()
		p.clearATCallState()
		p.clearVoiceProbes()
	}
	identity, err := p.ids.freeze()
	if err != nil {
		return nil, domain.Internal(operation, "ModemManager identity was unavailable", err)
	}
	return identity, nil
}

func (p *Provider) clearProviderIdentity() {
	if p == nil || p.ids == nil {
		return
	}
	p.identityMu.Lock()
	defer p.identityMu.Unlock()

	p.ids.clearProviderEpoch()
	p.messageProperties.clear()
	p.snapshotMu.Lock()
	p.terminalCalls = make(map[string]terminalCallProjection)
	p.snapshotMu.Unlock()
	p.telemetryMu.Lock()
	p.signalSetupStates = make(map[string]signalSetupState)
	p.telemetryMu.Unlock()
	p.servingRadioMu.Lock()
	p.servingRadioStates = make(map[string]servingRadioState)
	p.servingRadioMu.Unlock()
	p.clearATCallState()
	p.clearVoiceProbes()
}

func (p *Provider) prepareExtendedSignal(
	ctx context.Context,
	operation string,
	objects ManagedObjects,
) bool {
	paths := make([]dbus.ObjectPath, 0, len(objects))
	for path, interfaces := range objects {
		if _, found := interfaces[modemInterface]; !found {
			continue
		}
		if _, found := interfaces[signalInterface]; found {
			paths = append(paths, path)
		}
	}
	sort.Slice(paths, func(i, j int) bool {
		return paths[i] < paths[j]
	})

	started := false
	for _, path := range paths {
		interfaces := objects[path]
		state, stateKnown := int32Property(interfaces[modemInterface], "State")
		if !stateKnown || state < modemStateEnabled {
			continue
		}
		signalProperties := interfaces[signalInterface]
		rate, known := uint32Property(signalProperties, "Rate")
		if !known || rate != 0 {
			continue
		}
		key := signalSetupKey(path, interfaces[modemInterface])
		if !p.claimSignalSetup(key) {
			continue
		}
		_, err := p.call(
			ctx,
			path,
			signalInterface+".Setup",
			operation,
			"ModemManager failed to start extended signal polling",
			signalRefreshInterval,
		)
		if err == nil {
			p.completeSignalSetup(key)
			started = true
			continue
		}
		if operationError, ok := domain.AsOperationError(err); ok &&
			(operationError.Code == domain.ErrorNotSupported ||
				operationError.Code == domain.ErrorPermissionDenied) {
			p.completeSignalSetup(key)
		}
	}
	return started
}

func (p *Provider) claimSignalSetup(key string) bool {
	p.telemetryMu.Lock()
	defer p.telemetryMu.Unlock()
	now := time.Now()
	if p.now != nil {
		now = p.now()
	}
	state, found := p.signalSetupStates[key]
	if state.complete || found && now.Before(state.nextAttempt) {
		return false
	}
	state.nextAttempt = now.Add(signalSetupRetryDelay)
	p.signalSetupStates[key] = state
	return true
}

func (p *Provider) completeSignalSetup(key string) {
	p.telemetryMu.Lock()
	defer p.telemetryMu.Unlock()
	state := p.signalSetupStates[key]
	state.complete = true
	p.signalSetupStates[key] = state
}

func signalSetupKey(path dbus.ObjectPath, modemProperties Properties) string {
	equipmentIdentifier, _ := stringProperty(modemProperties, "EquipmentIdentifier")
	deviceIdentifier, _ := stringProperty(modemProperties, "DeviceIdentifier")
	return string(path) + "\x00" + equipmentIdentifier + "\x00" + deviceIdentifier
}

func (p *Provider) managedObjects(ctx context.Context, operation string) (ManagedObjects, error) {
	body, err := p.call(
		ctx,
		managerPath,
		objectManagerInterface+".GetManagedObjects",
		operation,
		"ModemManager GetManagedObjects failed",
	)
	if err != nil {
		return nil, err
	}
	objects := ManagedObjects{}
	if err := dbus.Store(body, &objects); err != nil {
		return nil, domain.Internal(operation, "ModemManager GetManagedObjects response was malformed", err)
	}
	return objects, nil
}

func (p *Provider) hydrateReferencedSIMs(
	ctx context.Context,
	operation string,
	objects ManagedObjects,
) (ManagedObjects, error) {
	paths := make([]dbus.ObjectPath, 0)
	seen := make(map[dbus.ObjectPath]struct{})
	addPath := func(path dbus.ObjectPath) {
		if !validSIMObjectPath(path) {
			return
		}
		if existing, found := objects[path]; found {
			if _, found := existing[simInterface]; found {
				return
			}
		}
		if _, duplicate := seen[path]; duplicate {
			return
		}
		seen[path] = struct{}{}
		paths = append(paths, path)
	}
	for _, interfaces := range objects {
		modemProperties, found := interfaces[modemInterface]
		if !found {
			continue
		}
		path, present := objectPathProperty(modemProperties, "Sim")
		if present {
			addPath(path)
		}
		if slotPaths, known := objectPathValuesProperty(modemProperties, "SimSlots"); known {
			for _, slotPath := range slotPaths {
				addPath(slotPath)
			}
		}
	}
	sort.Slice(paths, func(i, j int) bool {
		return paths[i] < paths[j]
	})
	for _, path := range paths {
		body, err := p.call(
			ctx,
			path,
			propertiesInterface+".GetAll",
			operation,
			"ModemManager failed to read a referenced SIM",
			simInterface,
		)
		if err != nil {
			if operationError, ok := domain.AsOperationError(err); ok &&
				(operationError.Code == domain.ErrorNotFound ||
					operationError.Code == domain.ErrorFailedPrecondition) {
				continue
			}
			return nil, err
		}
		properties := Properties{}
		if err := dbus.Store(body, &properties); err != nil {
			return nil, domain.Internal(
				operation,
				"ModemManager SIM properties response was malformed",
				err,
			)
		}
		interfaces := objects[path]
		if interfaces == nil {
			interfaces = Interfaces{}
		}
		interfaces[simInterface] = properties
		objects[path] = interfaces
	}
	return objects, nil
}

func (p *Provider) call(
	ctx context.Context,
	path dbus.ObjectPath,
	method string,
	operation string,
	message string,
	args ...any,
) ([]any, error) {
	return p.callDestination(
		ctx,
		serviceName,
		path,
		method,
		operation,
		message,
		args...,
	)
}

func (p *Provider) callDestination(
	ctx context.Context,
	destination string,
	path dbus.ObjectPath,
	method string,
	operation string,
	message string,
	args ...any,
) ([]any, error) {
	if err := p.requireCaller(ctx, operation); err != nil {
		return nil, err
	}
	body, err := p.caller.Call(ctx, destination, path, method, dbus.FlagNoAutoStart, args...)
	if err != nil {
		return nil, mapCallError(operation, message, err)
	}
	return body, nil
}

func (p *Provider) requireCaller(ctx context.Context, operation string) error {
	if ctx == nil {
		return domain.InvalidArgument(operation, "request context is required")
	}
	if p == nil || p.caller == nil || p.ids == nil {
		return domain.Unavailable(operation, "system D-Bus connection is unavailable", nil)
	}
	return nil
}

func implementedCapabilities() domain.AgentCapabilities {
	return domain.AgentCapabilities{
		Discovery:           true,
		Snapshot:            true,
		Events:              true,
		DeviceConfiguration: true,
		Dial:                true,
		AnswerCall:          true,
		RejectCall:          true,
		HangupCall:          true,
		SendDTMF:            true,
		SendMessage:         true,
		SIMManagement:       true,
		ConnectionProfiles:  true,
		USSD:                true,
	}
}

func snapshotRevision(
	lines []domain.Line,
	calls []domain.Call,
	messages []domain.Message,
	deliveryReports []domain.MessageDeliveryReport,
) (string, error) {
	content := struct {
		Lines           []domain.Line                  `json:"lines"`
		Calls           []domain.Call                  `json:"calls"`
		Messages        []domain.Message               `json:"messages"`
		DeliveryReports []domain.MessageDeliveryReport `json:"delivery_reports"`
	}{
		Lines:           lines,
		Calls:           calls,
		Messages:        messages,
		DeliveryReports: deliveryReports,
	}
	encoded, err := json.Marshal(content)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func objectPathResult(operation, message string, body []any) (dbus.ObjectPath, error) {
	var path dbus.ObjectPath
	if err := dbus.Store(body, &path); err != nil || !path.IsValid() {
		return "", domain.Internal(operation, message, err)
	}
	return path, nil
}

func validateRequestID(operation, requestID string) error {
	if requestID == "" {
		return domain.InvalidArgument(operation, "request_id is required")
	}
	if invalidText(requestID, 128) {
		return domain.InvalidArgument(operation, "request_id is invalid")
	}
	return nil
}

func invalidText(value string, maximum int) bool {
	if value == "" || len(value) > maximum {
		return true
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return true
		}
	}
	return false
}

func validDTMF(value string) bool {
	if value == "" || len(value) > 32 {
		return false
	}
	for _, r := range value {
		if (r >= '0' && r <= '9') || (r >= 'A' && r <= 'D') || r == '*' || r == '#' {
			continue
		}
		return false
	}
	return true
}

func findLine(lines []domain.Line, id string) (domain.Line, bool) {
	for _, line := range lines {
		if line.ID == id {
			return line, true
		}
	}
	return domain.Line{}, false
}

func findCall(calls []domain.Call, id string) (domain.Call, bool) {
	for _, call := range calls {
		if call.ID == id {
			return call, true
		}
	}
	return domain.Call{}, false
}

func findMessage(messages []domain.Message, id string) (domain.Message, bool) {
	for _, message := range messages {
		if message.ID == id {
			return message, true
		}
	}
	return domain.Message{}, false
}

func messageLineID(parsed ParsedObjects, id string) (string, bool) {
	if message, found := findMessage(parsed.Messages, id); found {
		return message.LineID, true
	}
	for _, report := range parsed.DeliveryReports {
		if report.ID == id {
			return report.LineID, true
		}
	}
	return "", false
}

func lineHasCall(calls []domain.Call, lineID, exceptCallID string) bool {
	for _, call := range calls {
		if call.LineID == lineID &&
			call.ID != exceptCallID &&
			call.StateCode != callStateTerminated {
			return true
		}
	}
	return false
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
