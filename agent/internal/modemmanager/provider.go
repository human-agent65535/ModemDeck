package modemmanager

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/godbus/dbus/v5"
	"github.com/human-agent65535/modemdeck/agent/internal/domain"
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
)

type Provider struct {
	caller        Caller
	ownerResolver ownerResolver
	close         func() error
	now           func() time.Time
	ids           *instanceIDs

	callMu        sync.Mutex
	configMu      sync.Mutex
	snapshotMu    sync.Mutex
	terminalCalls map[string]terminalCallProjection
}

type terminalCallProjection struct {
	call      domain.Call
	expiresAt time.Time
}

type ownerResolver interface {
	ResolveOwner(context.Context) (string, error)
}

type callerOwnerResolver struct {
	caller Caller
}

func (r callerOwnerResolver) ResolveOwner(ctx context.Context) (string, error) {
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
	return strings.TrimSpace(owner), nil
}

type staticOwnerResolver struct {
	owner string
}

func (r staticOwnerResolver) ResolveOwner(context.Context) (string, error) {
	return r.owner, nil
}

func New(caller Caller) (*Provider, error) {
	return newProvider(caller, newInstanceIDs()), nil
}

func newProvider(caller Caller, ids *instanceIDs) *Provider {
	if ids == nil {
		ids = newInstanceIDs()
	}
	var resolver ownerResolver = callerOwnerResolver{caller: caller}
	if owner := ids.providerEpoch(); owner != "" {
		resolver = staticOwnerResolver{owner: owner}
	}
	return &Provider{
		caller:        caller,
		ownerResolver: resolver,
		now:           time.Now,
		ids:           ids,
		terminalCalls: make(map[string]terminalCallProjection),
	}
}

func OpenSystemBus() (*Provider, error) {
	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		return nil, fmt.Errorf("connect to system D-Bus: %w", err)
	}
	provider, err := New(&connectionCaller{conn: conn})
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	provider.close = conn.Close
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
	identity, err := p.resolveProviderIdentity(ctx, operation)
	if err != nil {
		return domain.Snapshot{}, err
	}
	objects, err := p.managedObjects(ctx, operation)
	if err != nil {
		return domain.Snapshot{}, err
	}
	objects, err = p.hydrateReferencedSIMs(ctx, operation, objects)
	if err != nil {
		return domain.Snapshot{}, err
	}
	parsed := ParseManagedObjects(objects, identity)
	observedAt := p.now().UTC()
	p.projectTerminatedCalls(&parsed, observedAt)
	revision, err := snapshotRevision(parsed.Lines, parsed.Calls, parsed.Messages)
	if err != nil {
		return domain.Snapshot{}, domain.Internal(operation, "failed to revision the ModemManager snapshot", err)
	}
	return domain.Snapshot{
		Revision:   revision,
		ObservedAt: observedAt,
		Lines:      parsed.Lines,
		Calls:      parsed.Calls,
		Messages:   parsed.Messages,
	}, nil
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
	line, found := findLine(parsed.Lines, request.LineID)
	if !found {
		return domain.CommandReceipt{}, domain.NotFound(operation, "line was not found")
	}
	if !line.Capabilities.VoiceInterface {
		return domain.CommandReceipt{}, domain.NotSupported(operation, "line does not expose the ModemManager Voice interface")
	}
	if lineHasCall(parsed.Calls, line.ID, "") {
		return domain.CommandReceipt{}, domain.Conflict(operation, "line already has an ongoing call")
	}
	linePath := parsed.LinePaths[line.ID]

	properties := map[string]dbus.Variant{
		"number": dbus.MakeVariant(request.Number),
	}
	body, err := p.call(
		ctx,
		linePath,
		voiceInterface+".CreateCall",
		operation,
		"ModemManager failed to create the outgoing call",
		properties,
	)
	if err != nil {
		return domain.CommandReceipt{}, err
	}
	callPath, err := objectPathResult(operation, "ModemManager returned an invalid call path", body)
	if err != nil {
		return domain.CommandReceipt{}, err
	}
	if !strings.HasPrefix(string(callPath), "/org/freedesktop/ModemManager1/Call/") {
		return domain.CommandReceipt{}, domain.Internal(operation, "ModemManager returned an unexpected call path", nil)
	}
	if _, err := p.call(
		ctx,
		callPath,
		callInterface+".Start",
		operation,
		"ModemManager failed to start the outgoing call",
	); err != nil {
		return domain.CommandReceipt{}, err
	}
	return domain.CommandReceipt{RequestID: request.RequestID, ResourceID: parsed.ids.callID(callPath)}, nil
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
	return p.controlCall(ctx, operation, request, func(call domain.Call, parsed ParsedObjects) error {
		if call.StateCode != 3 && call.StateCode != 6 {
			return domain.Conflict(operation, "call is not an incoming ringing or waiting call")
		}
		_, err := p.call(
			ctx,
			parsed.CallPaths[call.ID],
			callInterface+".Hangup",
			operation,
			"ModemManager failed to reject the call",
		)
		return err
	})
}

func (p *Provider) HangupCall(ctx context.Context, request domain.CallCommandRequest) (domain.CommandReceipt, error) {
	const operation = "hangup_call"
	return p.controlCall(ctx, operation, request, func(call domain.Call, parsed ParsedObjects) error {
		if call.StateCode == callStateTerminated {
			return domain.Conflict(operation, "call is already terminated")
		}
		_, err := p.call(
			ctx,
			parsed.CallPaths[call.ID],
			callInterface+".Hangup",
			operation,
			"ModemManager failed to hang up the call",
		)
		return err
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
	call, found := findCall(parsed.Calls, request.CallID)
	if !found {
		return domain.CommandReceipt{}, domain.NotFound(operation, "active call was not found")
	}
	if call.StateCode != 4 {
		return domain.CommandReceipt{}, domain.Conflict(operation, "DTMF requires an active call")
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

	properties := map[string]dbus.Variant{
		"number": dbus.MakeVariant(request.Number),
		"text":   dbus.MakeVariant(request.Text),
	}
	body, err := p.call(
		ctx,
		parsed.LinePaths[line.ID],
		messagingInterface+".Create",
		operation,
		"ModemManager failed to create the SMS",
		properties,
	)
	if err != nil {
		return domain.CommandReceipt{}, err
	}
	messagePath, err := objectPathResult(operation, "ModemManager returned an invalid SMS path", body)
	if err != nil {
		return domain.CommandReceipt{}, err
	}
	if !strings.HasPrefix(string(messagePath), "/org/freedesktop/ModemManager1/SMS/") {
		return domain.CommandReceipt{}, domain.Internal(operation, "ModemManager returned an unexpected SMS path", nil)
	}
	if _, err := p.call(
		ctx,
		messagePath,
		smsInterface+".Send",
		operation,
		"ModemManager failed to send the SMS",
	); err != nil {
		return domain.CommandReceipt{}, err
	}
	return domain.CommandReceipt{RequestID: request.RequestID, ResourceID: parsed.ids.messageID(messagePath)}, nil
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
	return ParseManagedObjects(objects, identity), nil
}

func (p *Provider) resolveProviderIdentity(
	ctx context.Context,
	operation string,
) (*instanceIDs, error) {
	if err := p.requireCaller(ctx, operation); err != nil {
		return nil, err
	}
	if p.ownerResolver == nil {
		return nil, domain.Unavailable(operation, "ModemManager owner resolver is unavailable", nil)
	}
	owner, err := p.ownerResolver.ResolveOwner(ctx)
	if err != nil {
		return nil, mapCallError(operation, "failed to resolve the ModemManager D-Bus owner", err)
	}
	if owner == "" {
		return nil, domain.Unavailable(operation, "ModemManager D-Bus owner is unavailable", nil)
	}
	previousOwner := p.ids.providerEpoch()
	if err := p.ids.setProviderOwner(owner); err != nil {
		return nil, domain.Internal(operation, "ModemManager D-Bus owner was invalid", err)
	}
	if previousOwner != "" && previousOwner != owner {
		p.snapshotMu.Lock()
		p.terminalCalls = make(map[string]terminalCallProjection)
		p.snapshotMu.Unlock()
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
	p.ids.clearProviderOwner()
	p.snapshotMu.Lock()
	p.terminalCalls = make(map[string]terminalCallProjection)
	p.snapshotMu.Unlock()
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
	for _, interfaces := range objects {
		modemProperties, found := interfaces[modemInterface]
		if !found {
			continue
		}
		path, present := objectPathProperty(modemProperties, "Sim")
		if !present || path == "/" {
			continue
		}
		if existing, found := objects[path]; found {
			if _, found := existing[simInterface]; found {
				continue
			}
		}
		if _, duplicate := seen[path]; duplicate {
			continue
		}
		seen[path] = struct{}{}
		paths = append(paths, path)
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
				operationError.Code == domain.ErrorNotFound {
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
	if err := p.requireCaller(ctx, operation); err != nil {
		return nil, err
	}
	body, err := p.caller.Call(ctx, serviceName, path, method, dbus.FlagNoAutoStart, args...)
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

func snapshotRevision(lines []domain.Line, calls []domain.Call, messages []domain.Message) (string, error) {
	content := struct {
		Lines    []domain.Line    `json:"lines"`
		Calls    []domain.Call    `json:"calls"`
		Messages []domain.Message `json:"messages"`
	}{
		Lines:    lines,
		Calls:    calls,
		Messages: messages,
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
