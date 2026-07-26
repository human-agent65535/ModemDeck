package modemmanager

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/human-agent65535/modemdeck/agent/internal/domain"
)

type dbusInvocation struct {
	Context     context.Context
	Destination string
	Path        dbus.ObjectPath
	Method      string
	Flags       dbus.Flags
	Args        []any
}

type fakeCaller struct {
	mu                 sync.Mutex
	owner              bool
	ownerName          string
	objects            ManagedObjects
	createdCallPath    dbus.ObjectPath
	createdMessagePath dbus.ObjectPath
	runtimeVersion     string
	callIntrospection  string
	atResponse         string
	connectionProfiles []map[string]dbus.Variant
	ussdResponse       string
	externalSIMs       map[dbus.ObjectPath]Properties
	externalCalls      map[dbus.ObjectPath]Properties
	externalMessages   map[dbus.ObjectPath]Properties
	callLists          map[dbus.ObjectPath][]dbus.ObjectPath
	messageLists       map[dbus.ObjectPath][]dbus.ObjectPath
	signalAfterSetup   map[dbus.ObjectPath]Properties
	errors             map[string]error
	calls              []dbusInvocation
}

func (f *fakeCaller) Call(
	ctx context.Context,
	destination string,
	path dbus.ObjectPath,
	method string,
	flags dbus.Flags,
	args ...any,
) ([]any, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, dbusInvocation{
		Context:     ctx,
		Destination: destination,
		Path:        path,
		Method:      method,
		Flags:       flags,
		Args:        append([]any(nil), args...),
	})
	if err := f.errors[method]; err != nil {
		return nil, err
	}
	switch method {
	case busInterface + ".NameHasOwner":
		return []any{f.owner}, nil
	case busInterface + ".GetNameOwner":
		if !f.owner {
			return nil, dbus.NewError("org.freedesktop.DBus.Error.NameHasNoOwner", nil)
		}
		return []any{f.ownerName}, nil
	case objectManagerInterface + ".GetManagedObjects":
		return []any{cloneTestManagedObjects(f.objects)}, nil
	case propertiesInterface + ".Get":
		return []any{dbus.MakeVariant(f.runtimeVersion)}, nil
	case propertiesInterface + ".GetAll":
		if len(args) != 1 {
			return nil, errors.New("GetAll interface was missing")
		}
		switch args[0] {
		case simInterface:
			return []any{f.externalSIMs[path]}, nil
		case callInterface:
			properties, found := f.externalCalls[path]
			if !found {
				return nil, dbus.NewError(dbusErrorPrefix+"UnknownObject", nil)
			}
			return []any{properties}, nil
		case smsInterface:
			return []any{f.externalMessages[path]}, nil
		default:
			return nil, errors.New("GetAll interface was unexpected")
		}
	case introspectableInterface + ".Introspect":
		return []any{f.callIntrospection}, nil
	case voiceInterface + ".CreateCall":
		return []any{f.createdCallPath}, nil
	case voiceInterface + ".ListCalls":
		if paths, found := f.callLists[path]; found {
			return []any{append([]dbus.ObjectPath(nil), paths...)}, nil
		}
		paths, _ := objectPathValuesProperty(f.objects[path][voiceInterface], "Calls")
		return []any{paths}, nil
	case messagingInterface + ".List":
		if paths, found := f.messageLists[path]; found {
			return []any{append([]dbus.ObjectPath(nil), paths...)}, nil
		}
		paths, _ := objectPathValuesProperty(f.objects[path][messagingInterface], "Messages")
		return []any{paths}, nil
	case messagingInterface + ".Create":
		return []any{f.createdMessagePath}, nil
	case modemInterface + ".Command":
		return []any{f.atResponse}, nil
	case signalInterface + ".Setup":
		rate, ok := args[0].(uint32)
		if !ok {
			return nil, errors.New("signal setup rate was malformed")
		}
		properties := f.objects[path][signalInterface]
		if after, found := f.signalAfterSetup[path]; found {
			properties = after
			f.objects[path][signalInterface] = properties
		}
		properties["Rate"] = dbus.MakeVariant(rate)
		return []any{}, nil
	case profileManagerInterface + ".List":
		return []any{f.connectionProfiles}, nil
	case profileManagerInterface + ".Set":
		return []any{args[0]}, nil
	case ussdInterface + ".Initiate", ussdInterface + ".Respond":
		return []any{f.ussdResponse}, nil
	default:
		return []any{}, nil
	}
}

func TestSnapshotHydratesReferencedSIMOutsideManagedObjects(t *testing.T) {
	t.Parallel()
	objects := emptyLineObjects(true, true)
	simProperties := objects[testSIMPath][simInterface]
	delete(objects, testSIMPath)
	caller := newFakeCaller(objects)
	caller.externalSIMs[testSIMPath] = simProperties
	provider := newTestProvider(caller)

	snapshot, err := provider.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if len(snapshot.Lines) != 1 {
		t.Fatalf("lines = %d, want 1", len(snapshot.Lines))
	}
	line := snapshot.Lines[0]
	if !line.Capabilities.SIMInterface ||
		!line.SIMPresent ||
		line.SIMIdentifier != "8986012345678901234" {
		t.Fatalf("hydrated line = %+v", line)
	}
	invocations := caller.invocations()
	assertMethods(
		t,
		invocations,
		objectManagerInterface+".GetManagedObjects",
		messagingInterface+".List",
		propertiesInterface+".GetAll",
	)
	if len(invocations[2].Args) != 1 || invocations[2].Args[0] != simInterface {
		t.Fatalf("GetAll args = %#v", invocations[2].Args)
	}
}

func TestSnapshotKeepsCoreLineWhenReferencedSIMIsTemporarilyUnavailable(t *testing.T) {
	t.Parallel()
	objects := emptyLineObjects(false, false)
	delete(objects, testSIMPath)
	caller := newFakeCaller(objects)
	caller.errors[propertiesInterface+".GetAll"] = dbus.NewError(
		mobileEquipmentErrorPrefix+"SimPin",
		nil,
	)
	provider := newTestProvider(caller)

	snapshot, err := provider.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if len(snapshot.Lines) != 1 ||
		!snapshot.Lines[0].SIMPresent ||
		snapshot.Lines[0].Capabilities.SIMInterface {
		t.Fatalf("temporarily unavailable SIM snapshot = %+v", snapshot)
	}
	assertMethods(
		t,
		caller.invocations(),
		objectManagerInterface+".GetManagedObjects",
		propertiesInterface+".GetAll",
	)
}

func TestSnapshotListsAndHydratesMessagesMissingFromManagedObjects(t *testing.T) {
	t.Parallel()
	objects := emptyLineObjects(true, true)
	messagePath := dbus.ObjectPath("/org/freedesktop/ModemManager1/SMS/7")
	caller := newFakeCaller(objects)
	caller.messageLists[testModemPath] = []dbus.ObjectPath{messagePath}
	caller.externalMessages[messagePath] = Properties{
		"Number":    dbus.MakeVariant("+818012345678"),
		"Text":      dbus.MakeVariant("persisted while the app was offline"),
		"PduType":   dbus.MakeVariant(uint32(1)),
		"State":     dbus.MakeVariant(uint32(3)),
		"Timestamp": dbus.MakeVariant("2026-07-24T14:30:17+08"),
	}
	provider := newTestProvider(caller)

	snapshot, err := provider.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if len(snapshot.Messages) != 1 ||
		snapshot.Messages[0].Text != "persisted while the app was offline" {
		t.Fatalf("messages = %+v, want hydrated persisted SMS", snapshot.Messages)
	}
	if len(snapshot.Lines) != 1 ||
		len(snapshot.Lines[0].MessageIDs) != 1 ||
		snapshot.Lines[0].MessageIDs[0] != snapshot.Messages[0].ID {
		t.Fatalf("line message references = %+v", snapshot.Lines)
	}
	invocations := caller.invocations()
	assertMethods(
		t,
		invocations,
		objectManagerInterface+".GetManagedObjects",
		messagingInterface+".List",
		propertiesInterface+".GetAll",
	)
	if invocations[1].Path != testModemPath {
		t.Fatalf("List path = %q, want %q", invocations[1].Path, testModemPath)
	}
	if invocations[2].Path != messagePath ||
		len(invocations[2].Args) != 1 ||
		invocations[2].Args[0] != smsInterface {
		t.Fatalf("SMS GetAll invocation = %+v", invocations[2])
	}
}

func TestSnapshotReturnsDisabledLineWithoutServiceHydration(t *testing.T) {
	t.Parallel()
	objects := emptyLineObjects(true, true)
	objects[testModemPath][modemInterface]["State"] =
		dbus.MakeVariant(int32(modemStateDisabled))
	delete(objects[testModemPath][voiceInterface], "Calls")
	delete(objects[testModemPath][messagingInterface], "Messages")
	caller := newFakeCaller(objects)
	caller.errors[voiceInterface+".ListCalls"] = dbus.NewError(
		modemManagerCoreErrorPrefix+"WrongState",
		nil,
	)
	caller.errors[messagingInterface+".List"] = dbus.NewError(
		modemManagerCoreErrorPrefix+"WrongState",
		nil,
	)
	provider := newTestProvider(caller)

	snapshot, err := provider.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if len(snapshot.Lines) != 1 ||
		snapshot.Lines[0].State != "disabled" ||
		len(snapshot.Calls) != 0 ||
		len(snapshot.Messages) != 0 {
		t.Fatalf("disabled snapshot = %+v", snapshot)
	}
	assertMethods(
		t,
		caller.invocations(),
		objectManagerInterface+".GetManagedObjects",
	)
}

func TestSnapshotKeepsCoreLineDuringServiceStateRace(t *testing.T) {
	t.Parallel()
	objects := emptyLineObjects(true, true)
	delete(objects[testModemPath][voiceInterface], "Calls")
	delete(objects[testModemPath][messagingInterface], "Messages")
	caller := newFakeCaller(objects)
	caller.errors[voiceInterface+".ListCalls"] = dbus.NewError(
		modemManagerCoreErrorPrefix+"WrongState",
		nil,
	)
	caller.errors[messagingInterface+".List"] = dbus.NewError(
		modemManagerCoreErrorPrefix+"WrongState",
		nil,
	)
	provider := newTestProvider(caller)

	snapshot, err := provider.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if len(snapshot.Lines) != 1 ||
		len(snapshot.Calls) != 0 ||
		len(snapshot.Messages) != 0 {
		t.Fatalf("service-race snapshot = %+v", snapshot)
	}
	assertMethods(
		t,
		caller.invocations(),
		objectManagerInterface+".GetManagedObjects",
		voiceInterface+".ListCalls",
		messagingInterface+".List",
	)
}

func TestSnapshotSkipsMessageRemovedBetweenListAndPropertyRead(t *testing.T) {
	t.Parallel()
	objects := emptyLineObjects(true, true)
	messagePath := dbus.ObjectPath("/org/freedesktop/ModemManager1/SMS/8")
	caller := newFakeCaller(objects)
	caller.messageLists[testModemPath] = []dbus.ObjectPath{messagePath}
	caller.errors[propertiesInterface+".GetAll"] = dbus.NewError(
		"org.freedesktop.DBus.Error.UnknownObject",
		nil,
	)
	provider := newTestProvider(caller)

	snapshot, err := provider.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if len(snapshot.Messages) != 0 || len(snapshot.Lines[0].MessageIDs) != 0 {
		t.Fatalf("snapshot retained a vanished message: %+v", snapshot)
	}
}

func TestATTransportResolvesLineAndUsesModemManagerCommand(t *testing.T) {
	t.Parallel()
	caller := newFakeCaller(emptyLineObjects(true, true))
	caller.atResponse = "+QCFG: \"ims\",1,1\r\nOK\r\n"
	provider := newTestProvider(caller)
	snapshot, err := provider.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if len(snapshot.Lines) != 1 {
		t.Fatalf("lines = %d, want 1", len(snapshot.Lines))
	}
	caller.calls = nil

	response, err := provider.ATTransport(snapshot.Lines[0].ID).Command(
		context.Background(),
		`AT+QCFG="ims"`,
	)
	if err != nil {
		t.Fatalf("Command() error = %v", err)
	}
	if response != strings.TrimSpace(caller.atResponse) {
		t.Fatalf("response = %q, want %q", response, strings.TrimSpace(caller.atResponse))
	}
	invocations := caller.invocations()
	assertMethods(
		t,
		invocations,
		objectManagerInterface+".GetManagedObjects",
		modemInterface+".Command",
	)
	if got := invocations[1].Args; len(got) != 2 ||
		got[0] != `AT+QCFG="ims"` ||
		got[1] != modemCommandTimeoutSeconds {
		t.Fatalf("Command args = %#v", got)
	}
}

func TestATTransportRejectsUnroutableInputBeforeCommand(t *testing.T) {
	t.Parallel()
	caller := newFakeCaller(emptyLineObjects(true, true))
	provider := newTestProvider(caller)
	if _, err := provider.ATTransport("").Command(context.Background(), `AT+QCFG="ims"`); err == nil {
		t.Fatal("empty line Command() error = nil")
	}
	if _, err := provider.ATTransport("line").Command(context.Background(), "AT\rD"); err == nil {
		t.Fatal("newline Command() error = nil")
	}
	if len(caller.invocations()) != 0 {
		t.Fatalf("invalid commands reached D-Bus: %+v", caller.invocations())
	}
}

func (f *fakeCaller) invocations() []dbusInvocation {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]dbusInvocation(nil), f.calls...)
}

func (f *fakeCaller) resetInvocations() {
	f.mu.Lock()
	f.calls = nil
	f.mu.Unlock()
}

func (f *fakeCaller) setOwnerName(ownerName string) {
	f.mu.Lock()
	f.ownerName = ownerName
	f.mu.Unlock()
}

func TestHealthAdvertisesImplementedCapabilitiesOnlyWithOwner(t *testing.T) {
	caller := newFakeCaller(emptyLineObjects(true, true))
	caller.owner = true
	provider := newTestProvider(caller)
	ctx := context.WithValue(context.Background(), contextKey{}, "health")

	health, err := provider.Health(ctx)
	if err != nil {
		t.Fatalf("Health: %v", err)
	}
	if !health.Available || health.BootEpoch != "boot-test" {
		t.Fatalf("unexpected health: %+v", health)
	}
	if health.RuntimeVersion != "1.24.2" {
		t.Fatalf("runtime version = %q", health.RuntimeVersion)
	}
	if !health.Capabilities.Discovery || !health.Capabilities.Snapshot ||
		!health.Capabilities.Dial || !health.Capabilities.AnswerCall ||
		!health.Capabilities.RejectCall || !health.Capabilities.HangupCall ||
		!health.Capabilities.SendDTMF || !health.Capabilities.SendMessage {
		t.Fatalf("implemented capabilities missing: %+v", health.Capabilities)
	}
	assertMethods(t, caller.invocations(),
		busInterface+".NameHasOwner",
		propertiesInterface+".Get",
	)
	assertContexts(t, caller.invocations(), ctx)

	caller = newFakeCaller(ManagedObjects{})
	provider = newTestProvider(caller)
	health, err = provider.Health(context.Background())
	if err != nil {
		t.Fatalf("Health without owner: %v", err)
	}
	if health.Available || health.Capabilities != (domain.AgentCapabilities{}) {
		t.Fatalf("unavailable provider advertised capabilities: %+v", health)
	}
}

func TestHealthKeepsProviderAvailableWhenVersionPropertyIsUnsupported(t *testing.T) {
	caller := newFakeCaller(emptyLineObjects(true, true))
	caller.owner = true
	caller.errors[propertiesInterface+".Get"] = dbus.NewError(
		"org.freedesktop.DBus.Error.UnknownProperty",
		nil,
	)
	provider := newTestProvider(caller)

	health, err := provider.Health(context.Background())
	if err != nil {
		t.Fatalf("Health: %v", err)
	}
	if !health.Available || health.RuntimeVersion != "" || !health.Capabilities.SendDTMF {
		t.Fatalf("unexpected compatibility health: %+v", health)
	}
}

func TestSnapshotUsesOneManagedObjectsCallAndStableContentRevision(t *testing.T) {
	objects := emptyLineObjects(true, true)
	addCall(objects, "/org/freedesktop/ModemManager1/Call/1", 4)
	addMessage(objects, "/org/freedesktop/ModemManager1/SMS/1", 1, 3, "hello")
	caller := newFakeCaller(objects)
	provider := newTestProvider(caller)
	times := []time.Time{
		time.Date(2026, 7, 23, 1, 2, 3, 0, time.UTC),
		time.Date(2026, 7, 23, 1, 2, 4, 0, time.UTC),
		time.Date(2026, 7, 23, 1, 2, 5, 0, time.UTC),
	}
	provider.now = func() time.Time {
		value := times[0]
		times = times[1:]
		return value
	}

	first, err := provider.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("first Snapshot: %v", err)
	}
	second, err := provider.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("second Snapshot: %v", err)
	}
	if first.Revision == "" || !strings.HasPrefix(first.Revision, "sha256:") {
		t.Fatalf("revision = %q", first.Revision)
	}
	if first.Revision != second.Revision {
		t.Fatalf("unchanged content revision changed: %q != %q", first.Revision, second.Revision)
	}
	if first.ObservedAt.Equal(second.ObservedAt) {
		t.Fatalf("observed_at did not advance: %s", first.ObservedAt)
	}
	if len(first.Lines) != 1 || len(first.Calls) != 1 || len(first.Messages) != 1 {
		t.Fatalf("unexpected snapshot: %+v", first)
	}

	caller.objects["/org/freedesktop/ModemManager1/SMS/1"][smsInterface]["Text"] = dbus.MakeVariant("changed")
	third, err := provider.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("changed Snapshot: %v", err)
	}
	if third.Revision == second.Revision {
		t.Fatalf("changed content kept revision %q", third.Revision)
	}

	assertMethods(
		t,
		caller.invocations(),
		objectManagerInterface+".GetManagedObjects",
		messagingInterface+".List",
		objectManagerInterface+".GetManagedObjects",
		messagingInterface+".List",
		objectManagerInterface+".GetManagedObjects",
		messagingInterface+".List",
	)
}

func TestSnapshotStartsExtendedSignalPollingOnceAndReadsMetrics(t *testing.T) {
	t.Parallel()
	objects := emptyLineObjects(true, true)
	objects[testModemPath][modemInterface]["AccessTechnologies"] =
		dbus.MakeVariant(accessTechnologyLTE)
	objects[testModemPath][signalInterface] = Properties{
		"Rate": dbus.MakeVariant(uint32(0)),
		"Lte":  dbus.MakeVariant(map[string]dbus.Variant{}),
	}
	caller := newFakeCaller(objects)
	caller.signalAfterSetup[testModemPath] = Properties{
		"Lte": dbus.MakeVariant(map[string]dbus.Variant{
			"rssi": dbus.MakeVariant(float64(-68)),
			"rsrp": dbus.MakeVariant(float64(-94)),
			"rsrq": dbus.MakeVariant(float64(-11)),
			"snr":  dbus.MakeVariant(float64(6.5)),
		}),
	}
	provider := newTestProvider(caller)

	first, err := provider.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if len(first.Lines) != 1 ||
		first.Lines[0].SignalDBM == nil || *first.Lines[0].SignalDBM != -68 ||
		first.Lines[0].SignalRSRP == nil || *first.Lines[0].SignalRSRP != -94 ||
		first.Lines[0].SignalRSRQ == nil || *first.Lines[0].SignalRSRQ != -11 ||
		first.Lines[0].SignalSNR == nil || *first.Lines[0].SignalSNR != 6.5 {
		t.Fatalf("extended signal = %+v", first.Lines)
	}
	assertMethods(
		t,
		caller.invocations(),
		objectManagerInterface+".GetManagedObjects",
		signalInterface+".Setup",
		objectManagerInterface+".GetManagedObjects",
		messagingInterface+".List",
	)

	caller.resetInvocations()
	if _, err := provider.Snapshot(context.Background()); err != nil {
		t.Fatalf("second Snapshot() error = %v", err)
	}
	assertMethods(
		t,
		caller.invocations(),
		objectManagerInterface+".GetManagedObjects",
		messagingInterface+".List",
	)
}

func TestSnapshotKeepsCoreDataWhenExtendedSignalSetupFails(t *testing.T) {
	t.Parallel()
	objects := emptyLineObjects(true, true)
	objects[testModemPath][signalInterface] = Properties{
		"Rate": dbus.MakeVariant(uint32(0)),
	}
	caller := newFakeCaller(objects)
	caller.errors[signalInterface+".Setup"] = dbus.NewError(
		modemManagerCoreErrorPrefix+"Unsupported",
		[]any{"extended signal is unavailable"},
	)
	provider := newTestProvider(caller)

	first, err := provider.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if len(first.Lines) != 1 {
		t.Fatalf("lines = %d, want 1", len(first.Lines))
	}
	assertMethods(
		t,
		caller.invocations(),
		objectManagerInterface+".GetManagedObjects",
		signalInterface+".Setup",
		messagingInterface+".List",
	)

	caller.resetInvocations()
	if _, err := provider.Snapshot(context.Background()); err != nil {
		t.Fatalf("second Snapshot() error = %v", err)
	}
	assertMethods(
		t,
		caller.invocations(),
		objectManagerInterface+".GetManagedObjects",
		messagingInterface+".List",
	)
}

func TestSnapshotRetriesTransientExtendedSignalSetupFailure(t *testing.T) {
	objects := emptyLineObjects(true, true)
	objects[testModemPath][modemInterface]["AccessTechnologies"] =
		dbus.MakeVariant(accessTechnologyLTE)
	objects[testModemPath][signalInterface] = Properties{
		"Rate": dbus.MakeVariant(uint32(0)),
		"Lte":  dbus.MakeVariant(map[string]dbus.Variant{}),
	}
	caller := newFakeCaller(objects)
	caller.errors[signalInterface+".Setup"] = dbus.NewError(
		dbusErrorPrefix+"NoReply",
		[]any{"transient signal setup failure"},
	)
	provider := newTestProvider(caller)
	now := time.Date(2026, 7, 24, 1, 2, 3, 0, time.UTC)
	provider.now = func() time.Time { return now }

	if _, err := provider.Snapshot(context.Background()); err != nil {
		t.Fatalf("first Snapshot() error = %v", err)
	}
	assertMethods(
		t,
		caller.invocations(),
		objectManagerInterface+".GetManagedObjects",
		signalInterface+".Setup",
		messagingInterface+".List",
	)

	caller.resetInvocations()
	if _, err := provider.Snapshot(context.Background()); err != nil {
		t.Fatalf("Snapshot() during backoff error = %v", err)
	}
	assertMethods(
		t,
		caller.invocations(),
		objectManagerInterface+".GetManagedObjects",
		messagingInterface+".List",
	)

	now = now.Add(signalSetupRetryDelay)
	delete(caller.errors, signalInterface+".Setup")
	caller.signalAfterSetup[testModemPath] = Properties{
		"Lte": dbus.MakeVariant(map[string]dbus.Variant{
			"rssi": dbus.MakeVariant(float64(-71)),
			"rsrp": dbus.MakeVariant(float64(-97)),
			"rsrq": dbus.MakeVariant(float64(-12)),
		}),
	}
	caller.resetInvocations()
	recovered, err := provider.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("recovered Snapshot() error = %v", err)
	}
	if len(recovered.Lines) != 1 ||
		recovered.Lines[0].SignalDBM == nil ||
		*recovered.Lines[0].SignalDBM != -71 {
		t.Fatalf("recovered signal = %+v", recovered.Lines)
	}
	assertMethods(
		t,
		caller.invocations(),
		objectManagerInterface+".GetManagedObjects",
		signalInterface+".Setup",
		objectManagerInterface+".GetManagedObjects",
		messagingInterface+".List",
	)
}

func TestProviderIdentityFollowsModemManagerOwnerAcrossAgentRestarts(t *testing.T) {
	t.Parallel()

	objects := emptyLineObjects(true, false)
	addCall(objects, "/org/freedesktop/ModemManager1/Call/17", 4)
	caller := newFakeCaller(objects)
	caller.owner = true
	caller.setOwnerName(":1.41")

	firstProvider, err := New(caller)
	if err != nil {
		t.Fatalf("first New() error = %v", err)
	}
	first, err := firstProvider.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("first Snapshot() error = %v", err)
	}
	restartedAgentProvider, err := New(caller)
	if err != nil {
		t.Fatalf("restarted agent New() error = %v", err)
	}
	restartedAgent, err := restartedAgentProvider.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("restarted agent Snapshot() error = %v", err)
	}
	if len(first.Calls) != 1 || len(restartedAgent.Calls) != 1 ||
		first.Calls[0].ID != restartedAgent.Calls[0].ID {
		t.Fatalf("agent restart changed call identity: first=%+v restarted=%+v", first.Calls, restartedAgent.Calls)
	}

	caller.setOwnerName(":1.42")
	restartedModemManager, err := firstProvider.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("new owner Snapshot() error = %v", err)
	}
	if len(restartedModemManager.Calls) != 1 ||
		restartedModemManager.Calls[0].ID == first.Calls[0].ID {
		t.Fatalf("ModemManager owner change did not change call identity: before=%+v after=%+v", first.Calls, restartedModemManager.Calls)
	}
	health, err := firstProvider.Health(context.Background())
	if err != nil {
		t.Fatalf("Health() error = %v", err)
	}
	if health.BootEpoch != ":1.42" {
		t.Fatalf("provider boot_epoch = %q, want ModemManager owner :1.42", health.BootEpoch)
	}
}

func TestProviderDoesNotFallbackWhenOwnerCannotBeResolved(t *testing.T) {
	t.Parallel()

	caller := newFakeCaller(emptyLineObjects(true, false))
	caller.owner = true
	caller.errors[busInterface+".GetNameOwner"] = dbus.NewError(
		"org.freedesktop.DBus.Error.NameHasNoOwner",
		nil,
	)
	provider, err := New(caller)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	_, err = provider.Snapshot(context.Background())
	assertOperationError(t, err, domain.ErrorUnavailable, "snapshot")
	assertMethods(t, caller.invocations(), busInterface+".GetNameOwner")
}

func TestSnapshotRetainsTerminatedCallForBoundedProjection(t *testing.T) {
	objects := emptyLineObjects(true, false)
	callPath := dbus.ObjectPath("/org/freedesktop/ModemManager1/Call/8")
	addCall(objects, callPath, callStateTerminated)
	caller := newFakeCaller(objects)
	provider := newTestProvider(caller)
	start := time.Date(2026, 7, 23, 1, 2, 3, 0, time.UTC)
	now := start
	provider.now = func() time.Time { return now }

	observed, err := provider.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot with terminal call: %v", err)
	}
	if len(observed.Calls) != 1 || observed.Calls[0].State != "terminated" {
		t.Fatalf("terminal call was not projected: %+v", observed.Calls)
	}
	terminalID := observed.Calls[0].ID
	delete(objects, callPath)
	objects[testModemPath][voiceInterface]["Calls"] = dbus.MakeVariant([]dbus.ObjectPath{})

	now = start.Add(10 * time.Second)
	retained, err := provider.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot during terminal retention: %v", err)
	}
	if len(retained.Calls) != 1 || retained.Calls[0].ID != terminalID {
		t.Fatalf("terminal call was not retained: %+v", retained.Calls)
	}
	if len(retained.Lines[0].CallIDs) != 1 || retained.Lines[0].CallIDs[0] != terminalID {
		t.Fatalf("retained terminal call missing from line: %+v", retained.Lines[0].CallIDs)
	}
	if retained.Revision != observed.Revision {
		t.Fatalf("unchanged retained projection changed revision: %q != %q", retained.Revision, observed.Revision)
	}

	now = start.Add(terminalCallRetention + time.Second)
	expired, err := provider.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot after terminal retention: %v", err)
	}
	if len(expired.Calls) != 0 || len(expired.Lines[0].CallIDs) != 0 {
		t.Fatalf("expired terminal call remained projected: %+v", expired)
	}
	if expired.Revision == retained.Revision {
		t.Fatalf("terminal expiry did not change revision %q", expired.Revision)
	}
	assertMethods(t, caller.invocations(),
		objectManagerInterface+".GetManagedObjects",
		objectManagerInterface+".GetManagedObjects",
		objectManagerInterface+".GetManagedObjects",
	)
}

func TestStartCallUsesVoiceCreateThenCallStartAndOpaqueReceipt(t *testing.T) {
	caller := newFakeCaller(emptyLineObjects(true, false))
	provider := newTestProvider(caller)
	lineID := parsedLineID(caller.objects, provider.ids)
	ctx := context.WithValue(context.Background(), contextKey{}, "dial")

	receipt, err := provider.StartCall(ctx, domain.StartCallRequest{
		RequestID: "request-dial-1",
		LineID:    lineID,
		Number:    " +818012345678 ",
	})
	if err != nil {
		t.Fatalf("StartCall: %v", err)
	}
	if receipt.RequestID != "request-dial-1" ||
		!strings.HasPrefix(receipt.ResourceID, "call_") ||
		strings.Contains(receipt.ResourceID, "/") {
		t.Fatalf("unexpected receipt: %+v", receipt)
	}

	calls := caller.invocations()
	assertMethods(t, calls,
		objectManagerInterface+".GetManagedObjects",
		voiceInterface+".CreateCall",
		callInterface+".Start",
	)
	assertContexts(t, calls, ctx)
	if calls[1].Path != testModemPath {
		t.Fatalf("CreateCall path = %q", calls[1].Path)
	}
	properties, ok := calls[1].Args[0].(map[string]dbus.Variant)
	if !ok || properties["number"].Value() != "+818012345678" {
		t.Fatalf("CreateCall properties = %#v", calls[1].Args)
	}
	if calls[2].Path != caller.createdCallPath {
		t.Fatalf("Start path = %q", calls[2].Path)
	}
}

func TestStartCallRejectsSecondOngoingCallOnSameLine(t *testing.T) {
	objects := emptyLineObjects(true, false)
	addCall(objects, "/org/freedesktop/ModemManager1/Call/1", 4)
	caller := newFakeCaller(objects)
	provider := newTestProvider(caller)

	_, err := provider.StartCall(context.Background(), domain.StartCallRequest{
		RequestID: "request-dial-2",
		LineID:    parsedLineID(objects, provider.ids),
		Number:    "+818012345678",
	})
	assertOperationError(t, err, domain.ErrorConflict, "start_call")
	assertMethods(t, caller.invocations(), objectManagerInterface+".GetManagedObjects")
}

func TestStartCallDoesNotTreatTerminatedCallAsLineConflict(t *testing.T) {
	objects := emptyLineObjects(true, false)
	addCall(objects, "/org/freedesktop/ModemManager1/Call/1", callStateTerminated)
	caller := newFakeCaller(objects)
	provider := newTestProvider(caller)

	_, err := provider.StartCall(context.Background(), domain.StartCallRequest{
		RequestID: "request-after-terminal",
		LineID:    parsedLineID(objects, provider.ids),
		Number:    "+818012345678",
	})
	if err != nil {
		t.Fatalf("StartCall after terminal call: %v", err)
	}
	assertMethods(t, caller.invocations(),
		objectManagerInterface+".GetManagedObjects",
		voiceInterface+".CreateCall",
		callInterface+".Start",
	)
}

func TestStartCallConflictIsScopedToOneLine(t *testing.T) {
	objects := emptyLineObjects(true, false)
	addCall(objects, "/org/freedesktop/ModemManager1/Call/1", 4)
	secondModemPath := dbus.ObjectPath("/org/freedesktop/ModemManager1/Modem/1")
	objects[secondModemPath] = Interfaces{
		modemInterface: {
			"EquipmentIdentifier": dbus.MakeVariant("867530900000002"),
			"DeviceIdentifier":    dbus.MakeVariant("device-1"),
			"Physdev":             dbus.MakeVariant("/sys/devices/usb1/1-3"),
			"State":               dbus.MakeVariant(int32(8)),
		},
		voiceInterface: {
			"Calls": dbus.MakeVariant([]dbus.ObjectPath{}),
		},
	}
	caller := newFakeCaller(objects)
	provider := newTestProvider(caller)
	parsed := ParseManagedObjects(objects, provider.ids)
	var secondLineID string
	for id, path := range parsed.LinePaths {
		if path == secondModemPath {
			secondLineID = id
		}
	}
	if secondLineID == "" {
		t.Fatal("second line was not parsed")
	}

	_, err := provider.StartCall(context.Background(), domain.StartCallRequest{
		RequestID: "request-second-line",
		LineID:    secondLineID,
		Number:    "+818012345678",
	})
	if err != nil {
		t.Fatalf("StartCall on idle second line: %v", err)
	}
	calls := caller.invocations()
	assertMethods(t, calls,
		objectManagerInterface+".GetManagedObjects",
		voiceInterface+".CreateCall",
		callInterface+".Start",
	)
	if calls[1].Path != secondModemPath {
		t.Fatalf("CreateCall path = %q, want second line %q", calls[1].Path, secondModemPath)
	}
}

func TestCallControlsEnforceStateAndUseCurrentOpaqueMapping(t *testing.T) {
	tests := []struct {
		name       string
		state      int32
		run        func(*Provider, string) (domain.CommandReceipt, error)
		wantMethod string
	}{
		{
			name:  "answer",
			state: 3,
			run: func(provider *Provider, callID string) (domain.CommandReceipt, error) {
				return provider.AnswerCall(context.Background(), domain.CallCommandRequest{
					RequestID: "request-answer",
					CallID:    callID,
				})
			},
			wantMethod: callInterface + ".Accept",
		},
		{
			name:  "reject",
			state: 3,
			run: func(provider *Provider, callID string) (domain.CommandReceipt, error) {
				return provider.RejectCall(context.Background(), domain.CallCommandRequest{
					RequestID: "request-reject",
					CallID:    callID,
				})
			},
			wantMethod: callInterface + ".Hangup",
		},
		{
			name:  "hangup",
			state: 4,
			run: func(provider *Provider, callID string) (domain.CommandReceipt, error) {
				return provider.HangupCall(context.Background(), domain.CallCommandRequest{
					RequestID: "request-hangup",
					CallID:    callID,
				})
			},
			wantMethod: callInterface + ".Hangup",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			objects := emptyLineObjects(true, false)
			path := dbus.ObjectPath("/org/freedesktop/ModemManager1/Call/7")
			addCall(objects, path, test.state)
			caller := newFakeCaller(objects)
			provider := newTestProvider(caller)
			callID := ParseManagedObjects(objects, provider.ids).Calls[0].ID

			receipt, err := test.run(provider, callID)
			if err != nil {
				t.Fatalf("%s: %v", test.name, err)
			}
			if receipt.ResourceID != callID {
				t.Fatalf("receipt = %+v", receipt)
			}
			calls := caller.invocations()
			assertMethods(t, calls, objectManagerInterface+".GetManagedObjects", test.wantMethod)
			if calls[1].Path != path {
				t.Fatalf("D-Bus path = %q, want %q", calls[1].Path, path)
			}
		})
	}
}

func TestCallControlsRejectInvalidStateAndOldBootID(t *testing.T) {
	objects := emptyLineObjects(true, false)
	addCall(objects, "/org/freedesktop/ModemManager1/Call/7", 4)

	t.Run("answer active call", func(t *testing.T) {
		caller := newFakeCaller(objects)
		provider := newTestProvider(caller)
		callID := ParseManagedObjects(objects, provider.ids).Calls[0].ID
		_, err := provider.AnswerCall(context.Background(), domain.CallCommandRequest{
			RequestID: "request-answer-conflict",
			CallID:    callID,
		})
		assertOperationError(t, err, domain.ErrorConflict, "answer_call")
		assertMethods(t, caller.invocations(), objectManagerInterface+".GetManagedObjects")
	})

	t.Run("answer while another call consumes line", func(t *testing.T) {
		twoCalls := emptyLineObjects(true, false)
		addCall(twoCalls, "/org/freedesktop/ModemManager1/Call/7", 3)
		addCall(twoCalls, "/org/freedesktop/ModemManager1/Call/8", 4)
		caller := newFakeCaller(twoCalls)
		provider := newTestProvider(caller)
		parsed := ParseManagedObjects(twoCalls, provider.ids)
		var ringingID string
		for _, call := range parsed.Calls {
			if call.StateCode == 3 {
				ringingID = call.ID
			}
		}
		_, err := provider.AnswerCall(context.Background(), domain.CallCommandRequest{
			RequestID: "request-answer-busy",
			CallID:    ringingID,
		})
		assertOperationError(t, err, domain.ErrorConflict, "answer_call")
		assertMethods(t, caller.invocations(), objectManagerInterface+".GetManagedObjects")
	})

	t.Run("old boot id", func(t *testing.T) {
		caller := newFakeCaller(objects)
		provider := newTestProvider(caller)
		oldIDs := newInstanceIDsForTest(":1.40")
		oldCallID := ParseManagedObjects(objects, oldIDs).Calls[0].ID
		_, err := provider.HangupCall(context.Background(), domain.CallCommandRequest{
			RequestID: "request-old-id",
			CallID:    oldCallID,
		})
		assertOperationError(t, err, domain.ErrorNotFound, "hangup_call")
		assertMethods(t, caller.invocations(), objectManagerInterface+".GetManagedObjects")
	})

	t.Run("hangup terminal call", func(t *testing.T) {
		terminalObjects := emptyLineObjects(true, false)
		addCall(terminalObjects, "/org/freedesktop/ModemManager1/Call/9", callStateTerminated)
		caller := newFakeCaller(terminalObjects)
		provider := newTestProvider(caller)
		callID := ParseManagedObjects(terminalObjects, provider.ids).Calls[0].ID
		_, err := provider.HangupCall(context.Background(), domain.CallCommandRequest{
			RequestID: "request-terminal-hangup",
			CallID:    callID,
		})
		assertOperationError(t, err, domain.ErrorConflict, "hangup_call")
		assertMethods(t, caller.invocations(), objectManagerInterface+".GetManagedObjects")
	})
}

func TestSendDTMFUsesSequentialDigitsBeforeModemManager126(t *testing.T) {
	objects := emptyLineObjects(true, false)
	path := dbus.ObjectPath("/org/freedesktop/ModemManager1/Call/4")
	addCall(objects, path, 4)
	caller := newFakeCaller(objects)
	provider := newTestProvider(caller)
	callID := ParseManagedObjects(objects, provider.ids).Calls[0].ID
	ctx := context.WithValue(context.Background(), contextKey{}, "dtmf-old")

	receipt, err := provider.SendDTMF(ctx, domain.DTMFRequest{
		RequestID: "request-dtmf",
		CallID:    callID,
		Digits:    "12a#",
	})
	if err != nil {
		t.Fatalf("SendDTMF: %v", err)
	}
	if receipt.ResourceID != callID {
		t.Fatalf("receipt = %+v", receipt)
	}
	calls := caller.invocations()
	assertMethods(t, calls,
		objectManagerInterface+".GetManagedObjects",
		introspectableInterface+".Introspect",
		propertiesInterface+".Get",
		callInterface+".SendDtmf",
		callInterface+".SendDtmf",
		callInterface+".SendDtmf",
		callInterface+".SendDtmf",
	)
	wantDigits := []string{"1", "2", "A", "#"}
	for index, digit := range wantDigits {
		invocation := calls[index+3]
		if len(invocation.Args) != 1 || invocation.Args[0] != digit {
			t.Fatalf("DTMF call %d args = %#v, want %q", index, invocation.Args, digit)
		}
	}
	assertContexts(t, calls, ctx)

	caller = newFakeCaller(objects)
	provider = newTestProvider(caller)
	_, err = provider.SendDTMF(context.Background(), domain.DTMFRequest{
		RequestID: "request-dtmf-invalid",
		CallID:    callID,
		Digits:    "12X",
	})
	assertOperationError(t, err, domain.ErrorInvalidArgument, "send_dtmf")
	if len(caller.invocations()) != 0 {
		t.Fatalf("invalid DTMF reached D-Bus: %#v", caller.invocations())
	}
}

func TestSendDTMFUsesFullSequenceWithModemManager126(t *testing.T) {
	objects := emptyLineObjects(true, false)
	path := dbus.ObjectPath("/org/freedesktop/ModemManager1/Call/4")
	addCall(objects, path, 4)
	caller := newFakeCaller(objects)
	caller.runtimeVersion = "1.26.0"
	provider := newTestProvider(caller)
	callID := ParseManagedObjects(objects, provider.ids).Calls[0].ID

	_, err := provider.SendDTMF(context.Background(), domain.DTMFRequest{
		RequestID: "request-dtmf-new",
		CallID:    callID,
		Digits:    "12a#",
	})
	if err != nil {
		t.Fatalf("SendDTMF: %v", err)
	}
	calls := caller.invocations()
	assertMethods(t, calls,
		objectManagerInterface+".GetManagedObjects",
		introspectableInterface+".Introspect",
		propertiesInterface+".Get",
		callInterface+".SendDtmf",
	)
	if len(calls[3].Args) != 1 || calls[3].Args[0] != "12A#" {
		t.Fatalf("DTMF args = %#v", calls[3].Args)
	}
}

func TestSendDTMFGatesMissingIntrospectedMethod(t *testing.T) {
	objects := emptyLineObjects(true, false)
	addCall(objects, "/org/freedesktop/ModemManager1/Call/4", 4)
	caller := newFakeCaller(objects)
	caller.callIntrospection = `<node>
		<interface name="org.freedesktop.ModemManager1.Call">
			<method name="Start"/>
		</interface>
	</node>`
	provider := newTestProvider(caller)
	callID := ParseManagedObjects(objects, provider.ids).Calls[0].ID

	_, err := provider.SendDTMF(context.Background(), domain.DTMFRequest{
		RequestID: "request-dtmf-unsupported",
		CallID:    callID,
		Digits:    "1",
	})
	assertOperationError(t, err, domain.ErrorNotSupported, "send_dtmf")
	assertMethods(t, caller.invocations(),
		objectManagerInterface+".GetManagedObjects",
		introspectableInterface+".Introspect",
	)
}

func TestVersionAtLeastUsesMajorMinorAndRejectsUnknownVersions(t *testing.T) {
	tests := []struct {
		version string
		want    bool
	}{
		{version: "1.25.99", want: false},
		{version: "1.26.0", want: true},
		{version: "1.26.0-custom", want: true},
		{version: "2.0.0", want: true},
		{version: "", want: false},
		{version: "development", want: false},
	}
	for _, test := range tests {
		if got := versionAtLeast(test.version, 1, 26); got != test.want {
			t.Fatalf("versionAtLeast(%q) = %t, want %t", test.version, got, test.want)
		}
	}
}

func TestSendMessageUsesMessagingCreateThenSmsSend(t *testing.T) {
	caller := newFakeCaller(emptyLineObjects(false, true))
	provider := newTestProvider(caller)
	lineID := parsedLineID(caller.objects, provider.ids)

	receipt, err := provider.SendMessage(context.Background(), domain.SendMessageRequest{
		RequestID: "request-sms",
		LineID:    lineID,
		Number:    "+818012345678",
		Text:      "hello",
	})
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if receipt.RequestID != "request-sms" ||
		!strings.HasPrefix(receipt.ResourceID, "message_") ||
		strings.Contains(receipt.ResourceID, "/") {
		t.Fatalf("unexpected receipt: %+v", receipt)
	}
	calls := caller.invocations()
	assertMethods(t, calls,
		objectManagerInterface+".GetManagedObjects",
		messagingInterface+".Create",
		smsInterface+".Send",
	)
	properties := calls[1].Args[0].(map[string]dbus.Variant)
	if properties["number"].Value() != "+818012345678" || properties["text"].Value() != "hello" {
		t.Fatalf("Create SMS properties = %#v", properties)
	}
	if calls[2].Path != caller.createdMessagePath {
		t.Fatalf("Send SMS path = %q", calls[2].Path)
	}
}

func TestSendMessageFailureDoesNotInvokeFallbackOrDelete(t *testing.T) {
	caller := newFakeCaller(emptyLineObjects(false, true))
	caller.errors[smsInterface+".Send"] = dbus.NewError(
		"org.freedesktop.ModemManager1.Error.Core.WrongState",
		[]any{"wrong state"},
	)
	provider := newTestProvider(caller)

	_, err := provider.SendMessage(context.Background(), domain.SendMessageRequest{
		RequestID: "request-sms-fail",
		LineID:    parsedLineID(caller.objects, provider.ids),
		Number:    "+818012345678",
		Text:      "hello",
	})
	assertOperationError(t, err, domain.ErrorFailedPrecondition, "send_message")
	assertMethods(t, caller.invocations(),
		objectManagerInterface+".GetManagedObjects",
		messagingInterface+".Create",
		smsInterface+".Send",
	)
}

func TestProviderMapsContextAndDBusErrorsToTypedErrors(t *testing.T) {
	tests := []struct {
		name string
		err  error
		code domain.ErrorCode
	}{
		{
			name: "context deadline",
			err:  context.DeadlineExceeded,
			code: domain.ErrorUnavailable,
		},
		{
			name: "not found",
			err:  dbus.NewError("org.freedesktop.ModemManager1.Error.Core.NotFound", nil),
			code: domain.ErrorNotFound,
		},
		{
			name: "unsupported",
			err:  dbus.NewError("org.freedesktop.DBus.Error.UnknownMethod", nil),
			code: domain.ErrorNotSupported,
		},
		{
			name: "permission",
			err:  dbus.NewError("org.freedesktop.ModemManager1.Error.Core.Unauthorized", nil),
			code: domain.ErrorPermissionDenied,
		},
		{
			name: "SIM precondition",
			err:  dbus.NewError("org.freedesktop.ModemManager1.Error.MobileEquipment.SimPin", nil),
			code: domain.ErrorFailedPrecondition,
		},
		{
			name: "network rejected",
			err:  dbus.NewError("org.freedesktop.ModemManager1.Error.MobileEquipment.MissingOrUnknownApn", nil),
			code: domain.ErrorNetworkRejected,
		},
		{
			name: "internal",
			err:  errors.New("broken transport"),
			code: domain.ErrorInternal,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			caller := newFakeCaller(ManagedObjects{})
			caller.errors[objectManagerInterface+".GetManagedObjects"] = test.err
			provider := newTestProvider(caller)
			_, err := provider.Snapshot(context.Background())
			assertOperationError(t, err, test.code, "snapshot")
		})
	}
}

func TestProviderRequiresRequestContextAndCaller(t *testing.T) {
	provider := newTestProvider(nil)
	_, err := provider.Snapshot(context.Background())
	assertOperationError(t, err, domain.ErrorUnavailable, "snapshot")

	provider = newTestProvider(newFakeCaller(ManagedObjects{}))
	_, err = provider.Snapshot(nil)
	assertOperationError(t, err, domain.ErrorInvalidArgument, "snapshot")
}

type contextKey struct{}

var (
	testModemPath = dbus.ObjectPath("/org/freedesktop/ModemManager1/Modem/0")
	testSIMPath   = dbus.ObjectPath("/org/freedesktop/ModemManager1/SIM/0")
)

func newTestProvider(caller Caller) *Provider {
	return newProvider(
		caller,
		newInstanceIDsForTest("boot-test"),
	)
}

func newFakeCaller(objects ManagedObjects) *fakeCaller {
	return &fakeCaller{
		objects:            objects,
		ownerName:          ":1.41",
		createdCallPath:    "/org/freedesktop/ModemManager1/Call/99",
		createdMessagePath: "/org/freedesktop/ModemManager1/SMS/99",
		runtimeVersion:     "1.24.2",
		callIntrospection: `<node>
			<interface name="org.freedesktop.ModemManager1.Call">
				<method name="SendDtmf"/>
			</interface>
		</node>`,
		externalSIMs:     make(map[dbus.ObjectPath]Properties),
		externalCalls:    make(map[dbus.ObjectPath]Properties),
		externalMessages: make(map[dbus.ObjectPath]Properties),
		callLists:        make(map[dbus.ObjectPath][]dbus.ObjectPath),
		messageLists:     make(map[dbus.ObjectPath][]dbus.ObjectPath),
		signalAfterSetup: make(map[dbus.ObjectPath]Properties),
		errors:           make(map[string]error),
	}
}

func emptyLineObjects(voice, messaging bool) ManagedObjects {
	interfaces := Interfaces{
		modemInterface: {
			"Manufacturer":        dbus.MakeVariant("Quectel"),
			"Model":               dbus.MakeVariant("EG25-G"),
			"EquipmentIdentifier": dbus.MakeVariant("867530900000001"),
			"DeviceIdentifier":    dbus.MakeVariant("device-0"),
			"Physdev":             dbus.MakeVariant("/sys/devices/usb1/1-2"),
			"PrimaryPort":         dbus.MakeVariant("cdc-wdm0"),
			"State":               dbus.MakeVariant(int32(8)),
			"Sim":                 dbus.MakeVariant(testSIMPath),
		},
	}
	if voice {
		interfaces[voiceInterface] = Properties{
			"Calls": dbus.MakeVariant([]dbus.ObjectPath{}),
		}
	}
	if messaging {
		interfaces[messagingInterface] = Properties{
			"Messages": dbus.MakeVariant([]dbus.ObjectPath{}),
		}
	}
	return ManagedObjects{
		testModemPath: interfaces,
		testSIMPath: {
			simInterface: {
				"SimIdentifier": dbus.MakeVariant("8986012345678901234"),
			},
		},
	}
}

func addCall(objects ManagedObjects, path dbus.ObjectPath, state int32) {
	modem := objects[testModemPath]
	if _, ok := modem[voiceInterface]; !ok {
		modem[voiceInterface] = Properties{}
	}
	paths, _ := objectPathValuesProperty(modem[voiceInterface], "Calls")
	paths = append(paths, path)
	modem[voiceInterface]["Calls"] = dbus.MakeVariant(paths)
	objects[path] = Interfaces{
		callInterface: {
			"Number":    dbus.MakeVariant("+818012345678"),
			"Direction": dbus.MakeVariant(int32(1)),
			"State":     dbus.MakeVariant(state),
		},
	}
}

func addMessage(objects ManagedObjects, path dbus.ObjectPath, pduType, state uint32, text string) {
	modem := objects[testModemPath]
	if _, ok := modem[messagingInterface]; !ok {
		modem[messagingInterface] = Properties{}
	}
	paths, _ := objectPathValuesProperty(modem[messagingInterface], "Messages")
	paths = append(paths, path)
	modem[messagingInterface]["Messages"] = dbus.MakeVariant(paths)
	objects[path] = Interfaces{
		smsInterface: {
			"Number":    dbus.MakeVariant("+818012345678"),
			"Text":      dbus.MakeVariant(text),
			"PduType":   dbus.MakeVariant(pduType),
			"State":     dbus.MakeVariant(state),
			"Timestamp": dbus.MakeVariant("2026-07-23T10:00:00+09:00"),
		},
	}
}

func parsedLineID(objects ManagedObjects, ids *instanceIDs) string {
	return ParseManagedObjects(objects, ids).Lines[0].ID
}

func assertMethods(t *testing.T, calls []dbusInvocation, methods ...string) {
	t.Helper()
	if len(calls) != len(methods) {
		t.Fatalf("D-Bus call count = %d, want %d: %#v", len(calls), len(methods), calls)
	}
	for index, method := range methods {
		if calls[index].Method != method {
			t.Fatalf("D-Bus call %d method = %q, want %q", index, calls[index].Method, method)
		}
		if calls[index].Flags != dbus.FlagNoAutoStart {
			t.Fatalf("D-Bus call %d flags = %v", index, calls[index].Flags)
		}
	}
}

func assertContexts(t *testing.T, calls []dbusInvocation, want context.Context) {
	t.Helper()
	for index, call := range calls {
		if call.Context != want {
			t.Fatalf("D-Bus call %d did not receive request context", index)
		}
	}
}

func assertOperationError(t *testing.T, err error, code domain.ErrorCode, operation string) {
	t.Helper()
	operationError, ok := domain.AsOperationError(err)
	if !ok {
		t.Fatalf("expected OperationError, got %T: %v", err, err)
	}
	if operationError.Code != code || operationError.Operation != operation {
		t.Fatalf("unexpected OperationError: %+v", operationError)
	}
}
