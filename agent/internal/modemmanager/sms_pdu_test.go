package modemmanager

import (
	"testing"

	"github.com/godbus/dbus/v5"
)

func TestSMSPDUClassification(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		code          uint32
		wantKind      smsPDUKind
		wantDirection string
		wantBusiness  bool
	}{
		{name: "deliver", code: 1, wantKind: smsKindDeliver, wantDirection: "incoming", wantBusiness: true},
		{name: "submit", code: 2, wantKind: smsKindSubmit, wantDirection: "outgoing", wantBusiness: true},
		{name: "status report", code: 3, wantKind: smsKindStatusReport, wantDirection: "control"},
		{name: "CDMA deliver", code: 32, wantKind: smsKindDeliver, wantDirection: "incoming", wantBusiness: true},
		{name: "CDMA submit", code: 33, wantKind: smsKindSubmit, wantDirection: "outgoing", wantBusiness: true},
		{name: "CDMA cancellation", code: 34, wantKind: smsKindProtocolControl, wantDirection: "control"},
		{name: "CDMA delivery acknowledgement", code: 35, wantKind: smsKindProtocolControl, wantDirection: "control"},
		{name: "unknown enum", code: 0, wantKind: smsKindUnknown, wantDirection: "control"},
		{name: "future raw code", code: 99, wantKind: smsKindUnknown, wantDirection: "control"},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			kind := classifySMSPDU(test.code)
			if kind != test.wantKind {
				t.Fatalf("classifySMSPDU(%d) = %q, want %q", test.code, kind, test.wantKind)
			}
			direction, business := smsBusinessDirection(kind)
			if direction != test.wantDirection || business != test.wantBusiness {
				t.Fatalf(
					"smsBusinessDirection(%q) = %q, %t; want %q, %t",
					kind,
					direction,
					business,
					test.wantDirection,
					test.wantBusiness,
				)
			}
		})
	}
}

func TestParseManagedObjectsSeparatesStatusReportsFromBusinessMessages(t *testing.T) {
	t.Parallel()
	objects := emptyLineObjects(false, true)
	deliverPath := dbus.ObjectPath("/org/freedesktop/ModemManager1/SMS/31")
	statusReportPath := dbus.ObjectPath("/org/freedesktop/ModemManager1/SMS/32")
	acknowledgementPath := dbus.ObjectPath("/org/freedesktop/ModemManager1/SMS/33")
	addMessage(objects, deliverPath, smsPDUDeliver, 3, "hello")
	addMessage(objects, statusReportPath, smsPDUStatusReport, 3, "not a conversation")
	objects[statusReportPath][smsInterface]["MessageReference"] = dbus.MakeVariant(uint32(37))
	objects[statusReportPath][smsInterface]["DeliveryState"] = dbus.MakeVariant(uint32(0))
	objects[statusReportPath][smsInterface]["DischargeTimestamp"] = dbus.MakeVariant(
		"2026-07-29T10:15:00+09",
	)
	addMessage(
		objects,
		acknowledgementPath,
		smsPDUCDMADeliveryAcknowledgment,
		3,
		"not a conversation",
	)

	parsed := ParseManagedObjects(objects, newInstanceIDsForTest("bus-a/:1.24"))
	if len(parsed.Messages) != 1 || parsed.Messages[0].Text != "hello" {
		t.Fatalf("business messages = %+v, want only the deliver PDU", parsed.Messages)
	}
	if len(parsed.Lines) != 1 || len(parsed.Lines[0].MessageIDs) != 1 {
		t.Fatalf("line message IDs = %+v, want one business message", parsed.Lines)
	}
	if len(parsed.DeliveryReports) != 1 ||
		parsed.DeliveryReports[0].MessageReference != 37 ||
		!parsed.DeliveryReports[0].MessageReferenceKnown ||
		parsed.DeliveryReports[0].DeliveryState != 0 ||
		!parsed.DeliveryReports[0].DeliveryStateKnown ||
		parsed.DeliveryReports[0].Timestamp != "2026-07-29T10:15:00+09" {
		t.Fatalf("delivery reports = %+v", parsed.DeliveryReports)
	}
	if len(parsed.MessagePaths) != 2 ||
		parsed.MessagePaths[parsed.Messages[0].ID] != deliverPath ||
		parsed.MessagePaths[parsed.DeliveryReports[0].ID] != statusReportPath {
		t.Fatalf("message paths = %+v, want business and status-report paths", parsed.MessagePaths)
	}
}
