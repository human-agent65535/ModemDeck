package modemmanager

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/godbus/dbus/v5"
)

var smsCacheTestModemPath = dbus.ObjectPath(
	"/org/freedesktop/ModemManager1/Modem/0",
)

type smsCacheCaller struct {
	mu           sync.Mutex
	owner        string
	objects      ManagedObjects
	listedPaths  []dbus.ObjectPath
	properties   map[dbus.ObjectPath]Properties
	getAllCalls  map[dbus.ObjectPath]int
	messageLists int
}

func newSMSCacheCaller() *smsCacheCaller {
	return &smsCacheCaller{
		owner: ":1.41",
		objects: ManagedObjects{
			smsCacheTestModemPath: {
				modemInterface: {
					"Manufacturer":        dbus.MakeVariant("Quectel"),
					"Model":               dbus.MakeVariant("EG25-G"),
					"EquipmentIdentifier": dbus.MakeVariant("867530900000001"),
					"DeviceIdentifier":    dbus.MakeVariant("device-0"),
					"Physdev":             dbus.MakeVariant("/sys/devices/usb1/1-2"),
					"PrimaryPort":         dbus.MakeVariant("cdc-wdm0"),
					"State":               dbus.MakeVariant(int32(8)),
				},
				messagingInterface: {},
			},
		},
		properties:  make(map[dbus.ObjectPath]Properties),
		getAllCalls: make(map[dbus.ObjectPath]int),
	}
}

func (c *smsCacheCaller) Call(
	_ context.Context,
	_ string,
	path dbus.ObjectPath,
	method string,
	_ dbus.Flags,
	args ...any,
) ([]any, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	switch method {
	case busInterface + ".GetNameOwner":
		return []any{c.owner}, nil
	case objectManagerInterface + ".GetManagedObjects":
		return []any{cloneSMSCacheManagedObjects(c.objects)}, nil
	case messagingInterface + ".List":
		c.messageLists++
		return []any{append([]dbus.ObjectPath(nil), c.listedPaths...)}, nil
	case propertiesInterface + ".GetAll":
		if len(args) != 1 || args[0] != smsInterface {
			return nil, errors.New("unexpected GetAll interface")
		}
		c.getAllCalls[path]++
		properties, found := c.properties[path]
		if !found {
			return nil, dbus.NewError(
				"org.freedesktop.DBus.Error.UnknownObject",
				nil,
			)
		}
		return []any{cloneMessageProperties(properties)}, nil
	default:
		return nil, fmt.Errorf("unexpected D-Bus method %s", method)
	}
}

func (c *smsCacheCaller) setMessages(
	paths []dbus.ObjectPath,
	properties map[dbus.ObjectPath]Properties,
) {
	c.mu.Lock()
	c.listedPaths = append([]dbus.ObjectPath(nil), paths...)
	c.properties = make(map[dbus.ObjectPath]Properties, len(properties))
	for path, values := range properties {
		c.properties[path] = cloneMessageProperties(values)
	}
	c.mu.Unlock()
}

func (c *smsCacheCaller) setOwner(owner string) {
	c.mu.Lock()
	c.owner = owner
	c.mu.Unlock()
}

func (c *smsCacheCaller) getAllCount(path dbus.ObjectPath) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.getAllCalls[path]
}

func (c *smsCacheCaller) listCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.messageLists
}

func TestMessageSnapshotCachesCompleteTerminalProperties(t *testing.T) {
	t.Parallel()
	path := smsCacheTestPath(1)
	caller := newSMSCacheCaller()
	caller.setMessages(
		[]dbus.ObjectPath{path},
		map[dbus.ObjectPath]Properties{
			path: smsCacheTestProperties(3, "first"),
		},
	)
	provider, err := New(caller)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	first, err := provider.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("first Snapshot() error = %v", err)
	}
	second, err := provider.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("second Snapshot() error = %v", err)
	}

	if caller.getAllCount(path) != 1 {
		t.Fatalf("GetAll count = %d, want 1", caller.getAllCount(path))
	}
	if caller.listCount() != 2 {
		t.Fatalf("List count = %d, want 2", caller.listCount())
	}
	if len(first.Messages) != 1 || len(second.Messages) != 1 ||
		first.Messages[0].Text != "first" ||
		second.Messages[0].Text != "first" {
		t.Fatalf("cached messages = first %+v, second %+v", first.Messages, second.Messages)
	}
}

func TestMessageSnapshotReadsNewlyListedMessage(t *testing.T) {
	t.Parallel()
	firstPath := smsCacheTestPath(1)
	secondPath := smsCacheTestPath(2)
	caller := newSMSCacheCaller()
	caller.setMessages(
		[]dbus.ObjectPath{firstPath},
		map[dbus.ObjectPath]Properties{
			firstPath: smsCacheTestProperties(3, "first"),
		},
	)
	provider, err := New(caller)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if _, err := provider.Snapshot(context.Background()); err != nil {
		t.Fatalf("first Snapshot() error = %v", err)
	}

	caller.setMessages(
		[]dbus.ObjectPath{firstPath, secondPath},
		map[dbus.ObjectPath]Properties{
			firstPath:  smsCacheTestProperties(3, "first"),
			secondPath: smsCacheTestProperties(3, "second"),
		},
	)
	snapshot, err := provider.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("second Snapshot() error = %v", err)
	}

	if caller.getAllCount(firstPath) != 1 ||
		caller.getAllCount(secondPath) != 1 {
		t.Fatalf(
			"GetAll counts = first %d, second %d; want 1 each",
			caller.getAllCount(firstPath),
			caller.getAllCount(secondPath),
		)
	}
	if len(snapshot.Messages) != 2 {
		t.Fatalf("messages = %+v, want 2", snapshot.Messages)
	}
}

func TestMessageSnapshotRefreshesNonTerminalUntilTerminal(t *testing.T) {
	t.Parallel()
	path := smsCacheTestPath(1)
	caller := newSMSCacheCaller()
	caller.setMessages(
		[]dbus.ObjectPath{path},
		map[dbus.ObjectPath]Properties{
			path: smsCacheTestProperties(2, "partial"),
		},
	)
	provider, err := New(caller)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	first, err := provider.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("first Snapshot() error = %v", err)
	}
	if len(first.Messages) != 1 || first.Messages[0].State != "receiving" {
		t.Fatalf("first messages = %+v", first.Messages)
	}

	caller.setMessages(
		[]dbus.ObjectPath{path},
		map[dbus.ObjectPath]Properties{
			path: smsCacheTestProperties(3, "complete"),
		},
	)
	second, err := provider.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("second Snapshot() error = %v", err)
	}
	if len(second.Messages) != 1 ||
		second.Messages[0].State != "received" ||
		second.Messages[0].Text != "complete" {
		t.Fatalf("second messages = %+v", second.Messages)
	}
	if _, err := provider.Snapshot(context.Background()); err != nil {
		t.Fatalf("third Snapshot() error = %v", err)
	}
	if caller.getAllCount(path) != 2 {
		t.Fatalf("GetAll count = %d, want 2", caller.getAllCount(path))
	}
}

func TestMessageSnapshotRefreshesIncompleteTerminalProperties(t *testing.T) {
	t.Parallel()
	path := smsCacheTestPath(1)
	incomplete := smsCacheTestProperties(3, "incomplete")
	delete(incomplete, "Timestamp")
	caller := newSMSCacheCaller()
	caller.setMessages(
		[]dbus.ObjectPath{path},
		map[dbus.ObjectPath]Properties{path: incomplete},
	)
	provider, err := New(caller)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if _, err := provider.Snapshot(context.Background()); err != nil {
		t.Fatalf("first Snapshot() error = %v", err)
	}

	caller.setMessages(
		[]dbus.ObjectPath{path},
		map[dbus.ObjectPath]Properties{
			path: smsCacheTestProperties(3, "complete"),
		},
	)
	if _, err := provider.Snapshot(context.Background()); err != nil {
		t.Fatalf("second Snapshot() error = %v", err)
	}
	if _, err := provider.Snapshot(context.Background()); err != nil {
		t.Fatalf("third Snapshot() error = %v", err)
	}
	if caller.getAllCount(path) != 2 {
		t.Fatalf("GetAll count = %d, want 2", caller.getAllCount(path))
	}
}

func TestMessageSnapshotPrunesDeletedPathBeforeReuse(t *testing.T) {
	t.Parallel()
	path := smsCacheTestPath(1)
	caller := newSMSCacheCaller()
	caller.setMessages(
		[]dbus.ObjectPath{path},
		map[dbus.ObjectPath]Properties{
			path: smsCacheTestProperties(3, "old"),
		},
	)
	provider, err := New(caller)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if _, err := provider.Snapshot(context.Background()); err != nil {
		t.Fatalf("first Snapshot() error = %v", err)
	}

	caller.setMessages(nil, nil)
	empty, err := provider.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("empty Snapshot() error = %v", err)
	}
	if len(empty.Messages) != 0 || provider.messageProperties.size() != 0 {
		t.Fatalf(
			"deleted message remained: messages=%+v cache=%d",
			empty.Messages,
			provider.messageProperties.size(),
		)
	}

	caller.setMessages(
		[]dbus.ObjectPath{path},
		map[dbus.ObjectPath]Properties{
			path: smsCacheTestProperties(3, "reused"),
		},
	)
	reused, err := provider.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("reused Snapshot() error = %v", err)
	}
	if caller.getAllCount(path) != 2 {
		t.Fatalf("GetAll count = %d, want 2", caller.getAllCount(path))
	}
	if len(reused.Messages) != 1 || reused.Messages[0].Text != "reused" {
		t.Fatalf("reused messages = %+v", reused.Messages)
	}
}

func TestMessageSnapshotClearsCacheWhenProviderOwnerChanges(t *testing.T) {
	t.Parallel()
	path := smsCacheTestPath(1)
	caller := newSMSCacheCaller()
	caller.setMessages(
		[]dbus.ObjectPath{path},
		map[dbus.ObjectPath]Properties{
			path: smsCacheTestProperties(3, "before restart"),
		},
	)
	provider, err := New(caller)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if _, err := provider.Snapshot(context.Background()); err != nil {
		t.Fatalf("first Snapshot() error = %v", err)
	}

	caller.setOwner(":1.42")
	caller.setMessages(
		[]dbus.ObjectPath{path},
		map[dbus.ObjectPath]Properties{
			path: smsCacheTestProperties(3, "after restart"),
		},
	)
	restarted, err := provider.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("restarted Snapshot() error = %v", err)
	}

	if caller.getAllCount(path) != 2 {
		t.Fatalf("GetAll count = %d, want 2", caller.getAllCount(path))
	}
	if len(restarted.Messages) != 1 ||
		restarted.Messages[0].Text != "after restart" {
		t.Fatalf("restarted messages = %+v", restarted.Messages)
	}
}

func TestMessagePropertyCacheEnforcesConfiguredLimit(t *testing.T) {
	t.Parallel()
	paths := []dbus.ObjectPath{
		smsCacheTestPath(1),
		smsCacheTestPath(2),
		smsCacheTestPath(3),
	}
	properties := make(map[dbus.ObjectPath]Properties, len(paths))
	for index, path := range paths {
		properties[path] = smsCacheTestProperties(
			3,
			fmt.Sprintf("message %d", index+1),
		)
	}
	caller := newSMSCacheCaller()
	caller.setMessages(paths, properties)
	provider, err := New(caller)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	provider.messageProperties = newMessagePropertyCache(2)

	if _, err := provider.Snapshot(context.Background()); err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if provider.messageProperties.size() != 2 {
		t.Fatalf(
			"cache size = %d, want 2",
			provider.messageProperties.size(),
		)
	}
}

func TestConcurrentMessageSnapshotsShareTerminalHydration(t *testing.T) {
	t.Parallel()
	path := smsCacheTestPath(1)
	caller := newSMSCacheCaller()
	caller.setMessages(
		[]dbus.ObjectPath{path},
		map[dbus.ObjectPath]Properties{
			path: smsCacheTestProperties(3, "concurrent"),
		},
	)
	provider, err := New(caller)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	const snapshots = 12
	start := make(chan struct{})
	errs := make(chan error, snapshots)
	var wait sync.WaitGroup
	for range snapshots {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			_, err := provider.Snapshot(context.Background())
			errs <- err
		}()
	}
	close(start)
	wait.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent Snapshot() error = %v", err)
		}
	}
	if caller.getAllCount(path) != 1 {
		t.Fatalf("GetAll count = %d, want 1", caller.getAllCount(path))
	}
	if caller.listCount() != snapshots {
		t.Fatalf("List count = %d, want %d", caller.listCount(), snapshots)
	}
}

func TestMessagePropertyCacheOnlyAcceptsCompleteTerminalStates(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		properties Properties
		want       bool
	}{
		{
			name:       "received",
			properties: smsCacheTestProperties(3, "received"),
			want:       true,
		},
		{
			name:       "sent",
			properties: smsCacheTestProperties(5, "sent"),
			want:       true,
		},
		{
			name:       "stored",
			properties: smsCacheTestProperties(1, "stored"),
		},
		{
			name:       "receiving",
			properties: smsCacheTestProperties(2, "receiving"),
		},
		{
			name:       "sending",
			properties: smsCacheTestProperties(4, "sending"),
		},
		{
			name: "terminal but incomplete",
			properties: Properties{
				"State":   dbus.MakeVariant(uint32(3)),
				"PduType": dbus.MakeVariant(uint32(1)),
			},
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := completeTerminalMessageProperties(test.properties); got != test.want {
				t.Fatalf(
					"completeTerminalMessageProperties() = %t, want %t",
					got,
					test.want,
				)
			}
		})
	}
}

func smsCacheTestPath(index int) dbus.ObjectPath {
	return dbus.ObjectPath(fmt.Sprintf(
		"/org/freedesktop/ModemManager1/SMS/%d",
		index,
	))
}

func smsCacheTestProperties(state uint32, text string) Properties {
	return Properties{
		"Number":    dbus.MakeVariant("+818012345678"),
		"Text":      dbus.MakeVariant(text),
		"PduType":   dbus.MakeVariant(uint32(1)),
		"State":     dbus.MakeVariant(state),
		"Timestamp": dbus.MakeVariant("2026-07-24T16:00:00+09:00"),
	}
}

func cloneSMSCacheManagedObjects(objects ManagedObjects) ManagedObjects {
	cloned := make(ManagedObjects, len(objects))
	for path, interfaces := range objects {
		interfaceClone := make(Interfaces, len(interfaces))
		for name, properties := range interfaces {
			interfaceClone[name] = cloneMessageProperties(properties)
		}
		cloned[path] = interfaceClone
	}
	return cloned
}
