package modemmanager

import (
	"context"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
)

func TestParseQuectelNetworkInfo(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		response    string
		technology  string
		duplex      string
		band        string
		channel     uint32
		channelType string
	}{
		{
			name:        "FDD LTE serving band",
			response:    "\r\n+QNWINFO: \"FDD LTE\",\"44051\",\"LTE BAND 1\",100\r\n\r\nOK\r\n",
			technology:  "lte",
			duplex:      "fdd",
			band:        "B1",
			channel:     100,
			channelType: "earfcn",
		},
		{
			name:        "TDD LTE serving band",
			response:    "+QNWINFO: \"TDD LTE\",\"46000\",\"LTE BAND 41\",40936",
			technology:  "lte",
			duplex:      "tdd",
			band:        "B41",
			channel:     40936,
			channelType: "earfcn",
		},
		{
			name:        "5G NR serving band",
			response:    "+QNWINFO: \"TDD NR5G\",\"46000\",\"NR5G BAND 78\",627264",
			technology:  "nr5g",
			duplex:      "tdd",
			band:        "n78",
			channel:     627264,
			channelType: "nrarfcn",
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			radio, err := parseQuectelNetworkInfo(test.response)
			if err != nil {
				t.Fatalf("parseQuectelNetworkInfo() error = %v", err)
			}
			if radio == nil ||
				radio.AccessTechnology != test.technology ||
				radio.DuplexMode != test.duplex ||
				radio.Band != test.band ||
				radio.Channel == nil ||
				*radio.Channel != test.channel ||
				radio.ChannelType != test.channelType ||
				radio.Source != servingRadioSource {
				t.Fatalf("serving radio = %+v", radio)
			}
		})
	}
}

func TestParseQuectelNetworkInfoTreatsNoServiceAsUnavailable(t *testing.T) {
	t.Parallel()

	radio, err := parseQuectelNetworkInfo(`+QNWINFO: "No Service"`)
	if err != nil || radio != nil {
		t.Fatalf("no-service response = %+v, %v", radio, err)
	}
}

func TestSnapshotProjectsCurrentServingRadio(t *testing.T) {
	t.Parallel()

	objects := emptyLineObjects(false, false)
	objects[testModemPath][modemInterface]["AccessTechnologies"] =
		dbus.MakeVariant(accessTechnologyLTE)
	objects[testModemPath][modem3GPPInterface] = Properties{
		"RegistrationState": dbus.MakeVariant(uint32(5)),
	}
	caller := newFakeCaller(objects)
	caller.atResponses[quectelNetworkInfoQuery] =
		`+QNWINFO: "FDD LTE","44051","LTE BAND 1",100`
	provider := newTestProvider(caller)
	currentTime := time.Date(2026, time.August, 1, 7, 0, 0, 0, time.UTC)
	provider.now = func() time.Time { return currentTime }

	snapshot, err := provider.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if len(snapshot.Lines) != 1 {
		t.Fatalf("lines = %d, want 1", len(snapshot.Lines))
	}
	radio := snapshot.Lines[0].ServingRadio
	if radio == nil || radio.AccessTechnology != "lte" ||
		radio.DuplexMode != "fdd" || radio.Band != "B1" ||
		radio.Channel == nil || *radio.Channel != 100 {
		t.Fatalf("serving radio = %+v", radio)
	}

	foundQuery := false
	for _, invocation := range caller.invocations() {
		if invocation.Method == modemInterface+".Command" &&
			len(invocation.Args) > 0 && invocation.Args[0] == quectelNetworkInfoQuery {
			foundQuery = true
			break
		}
	}
	if !foundQuery {
		t.Fatal("snapshot did not issue the read-only QNWINFO query")
	}

	currentTime = currentTime.Add(30 * time.Second)
	if _, err := provider.Snapshot(context.Background()); err != nil {
		t.Fatalf("cached Snapshot() error = %v", err)
	}
	if queries := countATQueries(caller.invocations(), quectelNetworkInfoQuery); queries != 1 {
		t.Fatalf("QNWINFO queries inside refresh interval = %d, want 1", queries)
	}

	currentTime = currentTime.Add(31 * time.Second)
	if _, err := provider.Snapshot(context.Background()); err != nil {
		t.Fatalf("refreshed Snapshot() error = %v", err)
	}
	if queries := countATQueries(caller.invocations(), quectelNetworkInfoQuery); queries != 2 {
		t.Fatalf("QNWINFO queries after refresh interval = %d, want 2", queries)
	}
}

func TestSnapshotDoesNotInferServingBandFromAccessMask(t *testing.T) {
	t.Parallel()

	objects := emptyLineObjects(false, false)
	objects[testModemPath][modemInterface]["AccessTechnologies"] =
		dbus.MakeVariant(accessTechnologyLTE)
	caller := newFakeCaller(objects)
	provider := newTestProvider(caller)

	snapshot, err := provider.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if snapshot.Lines[0].ServingRadio != nil {
		t.Fatalf("unregistered line inferred serving radio = %+v", snapshot.Lines[0].ServingRadio)
	}
	for _, invocation := range caller.invocations() {
		if invocation.Method == modemInterface+".Command" &&
			len(invocation.Args) > 0 && invocation.Args[0] == quectelNetworkInfoQuery {
			t.Fatal("unregistered line unexpectedly queried serving radio")
		}
	}
}

func countATQueries(invocations []dbusInvocation, command string) int {
	count := 0
	for _, invocation := range invocations {
		if invocation.Method == modemInterface+".Command" &&
			len(invocation.Args) > 0 && invocation.Args[0] == command {
			count++
		}
	}
	return count
}
