package modemmanager

import (
	"testing"

	"github.com/godbus/dbus/v5"
)

func TestStableIncomingMessageIDSurvivesProviderRestart(t *testing.T) {
	t.Parallel()
	properties := Properties{
		"Number":           dbus.MakeVariant("+818012345678"),
		"Text":             dbus.MakeVariant("same stored message"),
		"PduType":          dbus.MakeVariant(uint32(1)),
		"State":            dbus.MakeVariant(uint32(3)),
		"Timestamp":        dbus.MakeVariant("2026-07-24T14:30:17+08"),
		"SMSC":             dbus.MakeVariant("+8613010704500"),
		"MessageReference": dbus.MakeVariant(uint32(0)),
	}

	first, ok := stableIncomingMessageID("line-a", properties)
	if !ok || first == "" {
		t.Fatalf("stableIncomingMessageID() = %q, %t", first, ok)
	}
	properties["State"] = dbus.MakeVariant(uint32(2))
	replayed, ok := stableIncomingMessageID("line-a", properties)
	if !ok || replayed != first {
		t.Fatalf("state transition changed message ID: %q != %q", replayed, first)
	}

	properties["Timestamp"] = dbus.MakeVariant("2026-07-24T14:30:18+08")
	distinct, ok := stableIncomingMessageID("line-a", properties)
	if !ok || distinct == first {
		t.Fatalf("distinct transport timestamp reused message ID %q", first)
	}
}

func TestStableIncomingMessageIDRequiresCompletedIdentity(t *testing.T) {
	t.Parallel()
	properties := Properties{
		"Number":  dbus.MakeVariant("+818012345678"),
		"Text":    dbus.MakeVariant("message"),
		"PduType": dbus.MakeVariant(uint32(1)),
	}
	if id, ok := stableIncomingMessageID("line-a", properties); ok || id != "" {
		t.Fatalf("timestamp-free incoming ID = %q, %t", id, ok)
	}
	properties["Timestamp"] = dbus.MakeVariant("2026-07-24T14:30:17+08")
	properties["PduType"] = dbus.MakeVariant(uint32(2))
	if id, ok := stableIncomingMessageID("line-a", properties); ok || id != "" {
		t.Fatalf("outgoing stable ID = %q, %t", id, ok)
	}
}

func TestOutgoingMessageIDStillTracksProviderObjectEpoch(t *testing.T) {
	t.Parallel()
	objects := emptyLineObjects(false, true)
	addMessage(
		objects,
		"/org/freedesktop/ModemManager1/SMS/12",
		uint32(2),
		uint32(5),
		"outgoing",
	)

	first := ParseManagedObjects(objects, newInstanceIDsForTest(":1.41"))
	restarted := ParseManagedObjects(objects, newInstanceIDsForTest(":1.42"))
	if len(first.Messages) != 1 || len(restarted.Messages) != 1 {
		t.Fatalf("messages first=%+v restarted=%+v", first.Messages, restarted.Messages)
	}
	if first.Messages[0].ID == restarted.Messages[0].ID {
		t.Fatalf("outgoing object ID was reused across provider epochs: %q", first.Messages[0].ID)
	}
}
