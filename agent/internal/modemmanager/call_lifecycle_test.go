package modemmanager

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/human-agent65535/modemdeck/agent/internal/domain"
)

func callStateSignal(path dbus.ObjectPath, old, next int32) *dbus.Signal {
	return &dbus.Signal{Path: path, Name: callInterface + ".StateChanged", Body: []any{old, next, uint32(0)}}
}

func TestSnapshotKeepsAnswerAcross64msStateRegression(t *testing.T) {
	objects := emptyLineObjects(true, false)
	path := testCallPath(21)
	objects[testModemPath][voiceInterface]["Calls"] = dbus.MakeVariant([]dbus.ObjectPath{path})
	caller := newFakeCaller(objects)
	caller.externalCalls[path] = testCallProperties(2, 2)
	p := newTestProvider(caller)
	now := time.Date(2026, 10, 11, 6, 1, 45, 0, time.UTC)
	p.now = func() time.Time { return now }
	if _, err := p.Snapshot(context.Background()); err != nil {
		t.Fatal(err)
	}
	now = now.Add(5 * time.Second)
	answered := now
	p.recordCallSignal(callStateSignal(path, 2, 4))
	now = now.Add(64 * time.Millisecond)
	p.recordCallSignal(callStateSignal(path, 4, 2))
	now = now.Add(250 * time.Millisecond)
	got, err := p.Snapshot(context.Background())
	if err != nil || len(got.Calls) != 1 {
		t.Fatalf("snapshot: %+v, %v", got.Calls, err)
	}
	call := got.Calls[0]
	if call.State != "active" || call.ReportedState != "ringing-out" || !call.ConnectedAt.Equal(answered) {
		t.Fatalf("answer was overwritten by a pre-answer report: %+v", call)
	}
	// The same identity can still hold and resume. Answering is not a blanket
	// active-state latch that would swallow subsequent valid transitions.
	for _, next := range []int32{5, 4, 7} {
		now = now.Add(time.Second)
		p.recordCallSignal(callStateSignal(path, call.StateCode, next))
		caller.externalCalls[path] = testCallProperties(next, 2)
		got, err = p.Snapshot(context.Background())
		if err != nil || len(got.Calls) != 1 || got.Calls[0].StateCode != next {
			t.Fatalf("valid transition %d lost: %+v %v", next, got.Calls, err)
		}
		call = got.Calls[0]
	}
	if call.TerminatedAt.IsZero() || !call.ConnectedAt.Equal(answered) {
		t.Fatalf("lifecycle times lost: %+v", call)
	}
}

type lifecycleRaceCaller struct {
	Caller
	path      dbus.ObjectPath
	afterRead func()
}

func (c *lifecycleRaceCaller) Call(ctx context.Context, destination string, path dbus.ObjectPath, method string, flags dbus.Flags, args ...any) ([]any, error) {
	body, err := c.Caller.Call(ctx, destination, path, method, flags, args...)
	if path == c.path && method == propertiesInterface+".GetAll" && c.afterRead != nil {
		callback := c.afterRead
		c.afterRead = nil
		callback()
	}
	return body, err
}

func TestSnapshotReplyCannotOverrideNewerHoldSignal(t *testing.T) {
	objects := emptyLineObjects(true, false)
	path := testCallPath(22)
	objects[testModemPath][voiceInterface]["Calls"] = dbus.MakeVariant([]dbus.ObjectPath{path})
	fake := newFakeCaller(objects)
	fake.externalCalls[path] = testCallProperties(4, 2)
	race := &lifecycleRaceCaller{Caller: fake, path: path}
	p := newTestProvider(race)
	race.afterRead = func() { p.recordCallSignal(callStateSignal(path, 4, 5)) }
	got, err := p.Snapshot(context.Background())
	if err != nil || len(got.Calls) != 1 || got.Calls[0].State != "held" {
		t.Fatalf("stale query superseded a newer hold: %+v %v", got.Calls, err)
	}
}

func TestAnswerAndEndSurviveObjectRemovalBeforeNextSnapshot(t *testing.T) {
	objects := emptyLineObjects(true, false)
	path := testCallPath(23)
	objects[testModemPath][voiceInterface]["Calls"] = dbus.MakeVariant([]dbus.ObjectPath{path})
	fake := newFakeCaller(objects)
	fake.externalCalls[path] = testCallProperties(2, 2)
	p := newTestProvider(fake)
	if _, err := p.Snapshot(context.Background()); err != nil {
		t.Fatal(err)
	}
	p.recordCallSignal(callStateSignal(path, 2, 4))
	p.recordCallSignal(callStateSignal(path, 4, 7))
	objects[testModemPath][voiceInterface]["Calls"] = dbus.MakeVariant([]dbus.ObjectPath{})
	delete(fake.externalCalls, path)
	got, err := p.Snapshot(context.Background())
	if err != nil || len(got.Calls) != 1 || got.Calls[0].State != "terminated" || got.Calls[0].ConnectedAt.IsZero() || got.Calls[0].TerminatedAt.IsZero() {
		t.Fatalf("coalescing lost short call lifecycle: %+v %v", got.Calls, err)
	}
}

func TestLifecycleDoesNotLeakBetweenCallIDsOrProviderEpochs(t *testing.T) {
	objects := emptyLineObjects(true, false)
	first, second := testCallPath(24), testCallPath(25)
	objects[testModemPath][voiceInterface]["Calls"] = dbus.MakeVariant([]dbus.ObjectPath{second})
	fake := newFakeCaller(objects)
	fake.externalCalls[second] = testCallProperties(2, 2)
	p := newTestProvider(fake)
	p.recordCallSignal(callStateSignal(first, 2, 4))
	got, err := p.Snapshot(context.Background())
	if err != nil || len(got.Calls) != 1 || got.Calls[0].State != "ringing-out" || !got.Calls[0].ConnectedAt.IsZero() {
		t.Fatalf("new identity inherited old answer: %+v %v", got.Calls, err)
	}
	p.recordCallSignal(callStateSignal(second, 2, 4))
	p.epochResolver = staticProviderEpochResolver{epoch: "next-boot"}
	got, err = p.Snapshot(context.Background())
	if err != nil || len(got.Calls) != 1 || got.Calls[0].State != "ringing-out" || !got.Calls[0].ConnectedAt.IsZero() {
		t.Fatalf("new provider inherited old answer: %+v %v", got.Calls, err)
	}
}

func TestLateActiveReportCannotAnswerTerminatedCall(t *testing.T) {
	l := newCallLifecycle()
	now := time.Now().UTC()
	l.observe("call", 7, 0, "state_changed", now, 0)
	c := l.observe("call", 4, 0, "snapshot", now.Add(time.Second), l.watermark())
	if c.state != 7 || !c.connected.IsZero() {
		t.Fatalf("late reply resurrected an unanswered terminal call: %+v", c)
	}
}

func TestCriticalStateIsCapturedBeforeNotificationConsumerRuns(t *testing.T) {
	p := newTestProvider(newFakeCaller(emptyLineObjects(true, false)))
	handler := newCallSignalHandler()
	handler.provider.Store(p)
	notifications := make(chan *dbus.Signal)
	handler.AddSignal(notifications)
	defer handler.Terminate()
	path := testCallPath(26)
	// No consumer reads notifications. The two payloads must still be folded
	// synchronously before any subsequent query reply can return to the caller.
	handler.DeliverSignal(callInterface, "StateChanged", callStateSignal(path, 2, 4))
	handler.DeliverSignal(callInterface, "StateChanged", callStateSignal(path, 4, 2))
	p.callLifecycle.mu.Lock()
	c := p.callLifecycle.calls[p.ids.callID(path)]
	if c == nil || c.state != 4 || c.connected.IsZero() || c.reported != 2 {
		t.Fatalf("critical state waited for the notification reader: %+v", c)
	}
	p.callLifecycle.mu.Unlock()
}

func callMetadataSignal(path dbus.ObjectPath, properties Properties) *dbus.Signal {
	return &dbus.Signal{Path: managerPath, Name: objectManagerInterface + ".InterfacesAdded",
		Body: []any{path, map[string]map[string]dbus.Variant{callInterface: properties}}}
}

func TestShortIncomingCallBeforeFirstSnapshotKeepsVerifiedLineAndMetadata(t *testing.T) {
	for _, association := range []string{"call_added_before", "call_added_after", "voice_calls", "modem_added", "late_metadata"} {
		t.Run(association, func(t *testing.T) {
			objects := emptyLineObjects(true, false)
			fake := newFakeCaller(objects)
			p := newTestProvider(fake)
			lineID := parsedLineID(objects, p.ids)
			path := testCallPath(31)
			now := time.Date(2026, 10, 12, 1, 2, 3, 0, time.UTC)
			p.now = func() time.Time { return now }
			metadata := callMetadataSignal(path, testCallProperties(3, 1))
			added := &dbus.Signal{Path: testModemPath, Name: voiceInterface + ".CallAdded", Body: []any{path}}
			if association == "call_added_before" {
				p.recordCallSignal(added)
			}
			if association != "late_metadata" {
				p.recordCallSignal(metadata)
			}
			switch association {
			case "call_added_after", "late_metadata":
				p.recordCallSignal(added)
			case "voice_calls":
				p.recordCallSignal(&dbus.Signal{Path: testModemPath, Name: propertiesInterface + ".PropertiesChanged",
					Body: []any{voiceInterface, Properties{"Calls": dbus.MakeVariant([]dbus.ObjectPath{path})}, []string{}}})
			case "modem_added":
				p.recordCallSignal(&dbus.Signal{Path: managerPath, Name: objectManagerInterface + ".InterfacesAdded",
					Body: []any{testModemPath, map[string]map[string]dbus.Variant{
						voiceInterface: {"Calls": dbus.MakeVariant([]dbus.ObjectPath{path})}}}})
			}
			now = now.Add(time.Second)
			connected := now
			p.recordCallSignal(callStateSignal(path, 3, 4))
			now = now.Add(64 * time.Millisecond)
			ended := now
			p.recordCallSignal(callStateSignal(path, 4, 7))
			if association == "late_metadata" {
				p.recordCallSignal(metadata)
			}
			if len(fake.invocations()) != 0 {
				t.Fatal("critical metadata capture issued a D-Bus query")
			}
			// The real call object is already absent; no call snapshot preceded it.
			got, err := p.Snapshot(context.Background())
			if err != nil || len(got.Calls) != 1 {
				t.Fatalf("short call lost: %+v %v", got.Calls, err)
			}
			call := got.Calls[0]
			if call.LineID != lineID || call.ID != p.ids.callID(path) || call.Number != "+818012345678" ||
				call.Direction != "incoming" || call.State != "terminated" ||
				!call.ConnectedAt.Equal(connected) || !call.TerminatedAt.Equal(ended) {
				t.Fatalf("short call metadata/times changed: %+v", call)
			}
		})
	}
}

func TestShortCallWithoutLineEvidenceDoesNotGuessSingleAvailableLine(t *testing.T) {
	for _, conflicting := range []bool{false, true} {
		p := newTestProvider(newFakeCaller(emptyLineObjects(true, false)))
		path := testCallPath(32)
		p.recordCallSignal(callMetadataSignal(path, testCallProperties(3, 1)))
		if conflicting {
			for _, modem := range []dbus.ObjectPath{testModemPath, "/org/freedesktop/ModemManager1/Modem/2"} {
				p.recordCallSignal(&dbus.Signal{Path: modem, Name: voiceInterface + ".CallAdded", Body: []any{path}})
			}
		}
		p.recordCallSignal(callStateSignal(path, 3, 4))
		p.recordCallSignal(callStateSignal(path, 4, 7))
		got, err := p.Snapshot(context.Background())
		if err != nil || len(got.Calls) != 0 {
			t.Fatalf("guessed/ambiguous line projection: %+v %v", got.Calls, err)
		}
	}
}

func TestOutgoingCreateSeedsOnlyRealMetadataBeforeAnyCallSnapshot(t *testing.T) {
	fake := newFakeCaller(emptyLineObjects(true, false))
	p := newTestProvider(fake)
	lineID := parsedLineID(fake.objects, p.ids)
	receipt, err := p.StartCall(context.Background(), domain.StartCallRequest{
		RequestID: "short-outgoing", LineID: lineID, Number: "+818012345678"})
	if err != nil {
		t.Fatal(err)
	}
	p.callLifecycle.mu.Lock()
	c := p.callLifecycle.calls[receipt.ResourceID]
	if c == nil || c.projection.LineID != lineID || c.projection.Number != "+818012345678" ||
		c.projection.Direction != "outgoing" || !c.connected.IsZero() || !c.ended.IsZero() || c.source != "" {
		t.Fatalf("create seeded invented state or lost real metadata: %+v", c)
	}
	p.callLifecycle.mu.Unlock()
	p.recordCallSignal(callStateSignal(fake.createdCallPath, 1, 4))
	p.recordCallSignal(callStateSignal(fake.createdCallPath, 4, 7))
	got, err := p.Snapshot(context.Background())
	if err != nil || len(got.Calls) != 1 || got.Calls[0].ID != receipt.ResourceID ||
		got.Calls[0].LineID != lineID || got.Calls[0].State != "terminated" || got.Calls[0].ConnectedAt.IsZero() {
		t.Fatalf("short outgoing call lost after object disappearance: %+v %v", got.Calls, err)
	}
}

func TestOwnerChangeDuringGetAllRejectsOldEpochSnapshot(t *testing.T) {
	for _, commandSnapshot := range []bool{false, true} {
		objects := emptyLineObjects(true, false)
		path := testCallPath(33)
		objects[testModemPath][voiceInterface]["Calls"] = dbus.MakeVariant([]dbus.ObjectPath{path})
		fake := newFakeCaller(objects)
		fake.externalCalls[path] = testCallProperties(4, 2)
		race := &lifecycleRaceCaller{Caller: fake, path: path}
		p := newTestProvider(race)
		race.afterRead = func() {
			p.recordCallSignal(&dbus.Signal{Name: busInterface + ".NameOwnerChanged",
				Body: []any{serviceName, ":1.41", ":1.42"}})
		}
		var err error
		if commandSnapshot {
			_, err = p.snapshotContent(context.Background(), "owner-race")
		} else {
			_, err = p.Snapshot(context.Background())
		}
		if err == nil || !strings.Contains(err.Error(), "changed during the snapshot") {
			t.Fatalf("old epoch reply accepted: %v", err)
		}
		p.callLifecycle.mu.Lock()
		if len(p.callLifecycle.calls) != 0 {
			t.Fatal("stale query recreated cleared lifecycle facts")
		}
		p.callLifecycle.mu.Unlock()
	}
}

func TestCriticalCaptureNeverWritesLogsAndWatcherLogsRawEventAtConsumption(t *testing.T) {
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	defer slog.SetDefault(previous)
	p := newTestProvider(newFakeCaller(emptyLineObjects(true, false)))
	now := time.Date(2026, 10, 12, 1, 2, 3, 0, time.UTC)
	p.now = func() time.Time { return now }
	signal := callStateSignal(testCallPath(34), 2, 4)
	p.recordCallSignal(signal)
	if output.Len() != 0 {
		t.Fatal("critical D-Bus receive wrote a log")
	}
	now = now.Add(5 * time.Second)
	p.logCallSignal(signal)
	log := output.String()
	if !strings.Contains(log, "logged_at") || !strings.Contains(log, "2026-10-12T01:02:08Z") ||
		strings.Contains(log, "observed_at") || strings.Contains(log, "effective_state") || strings.Contains(log, "+8180") {
		t.Fatalf("watcher misrepresented event timing/state/privacy: %s", log)
	}
}

func TestStaleSnapshotCannotEraseMetadataCapturedByInterfacesAdded(t *testing.T) {
	objects := emptyLineObjects(true, false)
	path := testCallPath(35)
	objects[testModemPath][voiceInterface]["Calls"] = dbus.MakeVariant([]dbus.ObjectPath{path})
	fake := newFakeCaller(objects)
	fake.externalCalls[path] = Properties{"State": dbus.MakeVariant(int32(4))}
	race := &lifecycleRaceCaller{Caller: fake, path: path}
	p := newTestProvider(race)
	race.afterRead = func() { p.recordCallSignal(callMetadataSignal(path, testCallProperties(4, 1))) }
	got, err := p.Snapshot(context.Background())
	if err != nil || len(got.Calls) != 1 || got.Calls[0].Number != "+818012345678" || got.Calls[0].Direction != "incoming" {
		t.Fatalf("stale query erased actual event metadata: %+v %v", got.Calls, err)
	}
}
