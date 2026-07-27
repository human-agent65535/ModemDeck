package modemmanager

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/godbus/dbus/v5"
	"github.com/human-agent65535/modemdeck/agent/internal/domain"
)

func TestSIMStatusAndPINCommandUseReferencedSIM(t *testing.T) {
	t.Parallel()
	objects := emptyLineObjects(true, true)
	objects[testModemPath][modemInterface]["UnlockRequired"] = dbus.MakeVariant(uint32(2))
	objects[testModemPath][modemInterface]["UnlockRetries"] = dbus.MakeVariant(map[uint32]uint32{2: 3})
	objects[testModemPath][modem3GPPInterface] = Properties{
		"OperatorCode":      dbus.MakeVariant("44010"),
		"OperatorName":      dbus.MakeVariant("NTT DOCOMO"),
		"RegistrationState": dbus.MakeVariant(uint32(5)),
	}
	objects[testSIMPath][simInterface]["Active"] = dbus.MakeVariant(true)
	objects[testSIMPath][simInterface]["Imsi"] = dbus.MakeVariant("440501234567890")
	objects[testSIMPath][simInterface]["OperatorIdentifier"] = dbus.MakeVariant("44050")
	objects[testSIMPath][simInterface]["OperatorName"] = dbus.MakeVariant("KDDI")
	caller := newFakeCaller(objects)
	provider := newTestProvider(caller)
	lineID := parsedLineID(objects, provider.ids)

	status, err := provider.SIMStatus(context.Background(), lineID)
	if err != nil {
		t.Fatalf("SIMStatus() error = %v", err)
	}
	if !status.Present || !status.Active || status.Identifier != "8986012345678901234" ||
		status.IMSI != "440501234567890" || status.UnlockRequired != "sim-pin" ||
		status.UnlockRetries["sim-pin"] != 3 {
		t.Fatalf("SIM status = %+v", status)
	}
	if status.HomeOperatorCode != "44050" || status.HomeOperatorName != "KDDI" ||
		status.ServingOperatorCode != "44010" || status.ServingOperatorName != "NTT DOCOMO" ||
		status.OperatorIdentifier != status.HomeOperatorCode ||
		status.OperatorName != status.HomeOperatorName ||
		!status.RegistrationStateKnown || status.RegistrationStateCode != 5 ||
		status.RegistrationState != "roaming" || !status.Roaming {
		t.Fatalf("SIM network status = %+v", status)
	}
	if status.SIMSlotsKnown ||
		len(status.SIMSlots) != 1 ||
		!status.SIMSlots[0].Present ||
		!status.SIMSlots[0].Current ||
		status.CurrentSIMSlotKnown {
		t.Fatalf("observed SIM fallback = %+v", status)
	}
	caller.calls = nil
	receipt, err := provider.SIMCommand(context.Background(), domain.SIMCommandRequest{
		RequestID: "sim-pin-1",
		LineID:    lineID,
		Operation: domain.SIMSendPIN,
		PIN:       "1234",
	})
	if err != nil {
		t.Fatalf("SIMCommand() error = %v", err)
	}
	if receipt.RequestID != "sim-pin-1" || receipt.ResourceID != lineID {
		t.Fatalf("receipt = %+v", receipt)
	}
	invocations := caller.invocations()
	assertMethods(t, invocations,
		objectManagerInterface+".GetManagedObjects",
		simInterface+".SendPin",
	)
	if invocations[1].Path != testSIMPath || len(invocations[1].Args) != 1 ||
		invocations[1].Args[0] != "1234" {
		t.Fatalf("SendPin invocation = %+v", invocations[1])
	}
}

func TestSIMStatusExposesStandardESIMFactsWithoutRawIdentifiers(t *testing.T) {
	t.Parallel()
	const (
		rawCurrentEID  = "89049032000000000000000012345678"
		rawInactiveEID = "89049032000000000000000087654321"
	)
	physicalPath := dbus.ObjectPath("/org/freedesktop/ModemManager1/SIM/1")
	inactiveESIMPath := dbus.ObjectPath("/org/freedesktop/ModemManager1/SIM/2")
	objects := emptyLineObjects(true, true)
	objects[testModemPath][modemInterface]["SimSlots"] = dbus.MakeVariant([]dbus.ObjectPath{
		physicalPath,
		testSIMPath,
		inactiveESIMPath,
		"/",
	})
	objects[testModemPath][modemInterface]["PrimarySimSlot"] = dbus.MakeVariant(uint32(2))
	objects[testSIMPath][simInterface]["Active"] = dbus.MakeVariant(true)
	objects[testSIMPath][simInterface]["SimType"] = dbus.MakeVariant(uint32(modemManagerSIMTypeESIM))
	objects[testSIMPath][simInterface]["EsimStatus"] = dbus.MakeVariant(uint32(modemManagerESIMStatusWithProfiles))
	objects[testSIMPath][simInterface]["Eid"] = dbus.MakeVariant(rawCurrentEID)

	caller := newFakeCaller(objects)
	caller.externalSIMs[physicalPath] = Properties{
		"SimType":    dbus.MakeVariant(uint32(modemManagerSIMTypePhysical)),
		"EsimStatus": dbus.MakeVariant(uint32(modemManagerESIMStatusUnknown)),
	}
	caller.externalSIMs[inactiveESIMPath] = Properties{
		"SimType":    dbus.MakeVariant(uint32(modemManagerSIMTypeESIM)),
		"EsimStatus": dbus.MakeVariant(uint32(modemManagerESIMStatusNoProfiles)),
		"Eid":        dbus.MakeVariant(rawInactiveEID),
	}
	provider := newTestProvider(caller)

	status, err := provider.SIMStatus(context.Background(), parsedLineID(objects, provider.ids))
	if err != nil {
		t.Fatalf("SIMStatus() error = %v", err)
	}
	if status.SIMType != domain.SIMTypeESIM ||
		status.ESIMStatus != domain.ESIMStatusWithProfiles ||
		status.EIDMasked != "****5678" {
		t.Fatalf("current eSIM facts = %+v", status)
	}
	if !status.SIMSlotsKnown || len(status.SIMSlots) != 4 ||
		!status.PrimarySIMSlotKnown || status.PrimarySIMSlot != 2 ||
		!status.CurrentSIMSlotKnown || status.CurrentSIMSlot != 2 {
		t.Fatalf("slot summary = %+v", status)
	}
	if status.SIMSlots[0].SIMType != domain.SIMTypePhysical ||
		status.SIMSlots[0].ESIMStatus != domain.ESIMStatusUnknown ||
		status.SIMSlots[0].Current ||
		!status.SIMSlots[0].Present {
		t.Fatalf("physical slot = %+v", status.SIMSlots[0])
	}
	if !status.SIMSlots[1].Current ||
		status.SIMSlots[1].EIDMasked != "****5678" ||
		status.SIMSlots[2].ESIMStatus != domain.ESIMStatusNoProfiles ||
		status.SIMSlots[2].EIDMasked != "****4321" ||
		status.SIMSlots[3].Present {
		t.Fatalf("eSIM slots = %+v", status.SIMSlots)
	}
	if status.ProfileManagement.Supported ||
		status.ProfileManagement.Reason != simProfileManagementNotSupportedReason {
		t.Fatalf("profile management = %+v", status.ProfileManagement)
	}

	encoded, err := json.Marshal(status)
	if err != nil {
		t.Fatalf("marshal SIM status: %v", err)
	}
	logValue := fmt.Sprintf("%+v", status)
	for _, privateValue := range []string{
		rawCurrentEID,
		rawInactiveEID,
		string(testSIMPath),
		string(physicalPath),
		string(inactiveESIMPath),
	} {
		if strings.Contains(string(encoded), privateValue) ||
			strings.Contains(logValue, privateValue) {
			t.Fatalf("SIM status exposed private value %q: json=%s log=%s", privateValue, encoded, logValue)
		}
	}
	if !strings.Contains(string(encoded), `"eid":"****5678"`) {
		t.Fatalf("SIM status JSON did not contain the masked EID: %s", encoded)
	}
}

func TestSIMStatusPreservesUnknownForMalformedStandardProperties(t *testing.T) {
	t.Parallel()
	objects := emptyLineObjects(true, true)
	objects[testModemPath][modemInterface]["SimSlots"] = dbus.MakeVariant([]string{string(testSIMPath)})
	objects[testModemPath][modemInterface]["PrimarySimSlot"] = dbus.MakeVariant(int32(1))
	objects[testSIMPath][simInterface]["SimType"] = dbus.MakeVariant(uint32(99))
	objects[testSIMPath][simInterface]["EsimStatus"] = dbus.MakeVariant("with_profiles")
	objects[testSIMPath][simInterface]["Eid"] = dbus.MakeVariant("not-a-valid-eid")
	provider := newTestProvider(newFakeCaller(objects))

	status, err := provider.SIMStatus(context.Background(), parsedLineID(objects, provider.ids))
	if err != nil {
		t.Fatalf("SIMStatus() error = %v", err)
	}
	if status.SIMType != domain.SIMTypeUnknown ||
		status.ESIMStatus != domain.ESIMStatusUnknown ||
		status.EIDMasked != "" ||
		status.SIMSlotsKnown ||
		len(status.SIMSlots) != 1 ||
		!status.SIMSlots[0].Present ||
		!status.SIMSlots[0].Current ||
		status.SIMSlots[0].SIMType != domain.SIMTypeUnknown ||
		status.SIMSlots[0].ESIMStatus != domain.ESIMStatusUnknown ||
		status.PrimarySIMSlotKnown ||
		status.CurrentSIMSlotKnown {
		t.Fatalf("malformed standard properties were guessed: %+v", status)
	}
}

func TestSIMStatusTreatsEmptyReportedSlotInventoryAsUnknown(t *testing.T) {
	t.Parallel()
	objects := emptyLineObjects(true, true)
	objects[testModemPath][modemInterface]["SimSlots"] =
		dbus.MakeVariant([]dbus.ObjectPath{})
	provider := newTestProvider(newFakeCaller(objects))

	status, err := provider.SIMStatus(context.Background(), parsedLineID(objects, provider.ids))
	if err != nil {
		t.Fatalf("SIMStatus() error = %v", err)
	}
	if status.SIMSlotsKnown ||
		len(status.SIMSlots) != 1 ||
		!status.SIMSlots[0].Present ||
		!status.SIMSlots[0].Current ||
		status.CurrentSIMSlotKnown {
		t.Fatalf("empty reported slot inventory = %+v", status)
	}
}

func TestConnectionProfilesListSaveAndDelete(t *testing.T) {
	t.Parallel()
	objects := emptyLineObjects(true, true)
	objects[testModemPath][profileManagerInterface] = Properties{}
	caller := newFakeCaller(objects)
	caller.connectionProfiles = []map[string]dbus.Variant{{
		"profile-id":   dbus.MakeVariant(int32(2)),
		"profile-name": dbus.MakeVariant("ims"),
		"apn":          dbus.MakeVariant("ims"),
		"ip-type":      dbus.MakeVariant(uint32(bearerIPFamilyIPv4V6)),
		"allowed-auth": dbus.MakeVariant(uint32(1)),
	}}
	provider := newTestProvider(caller)
	lineID := parsedLineID(objects, provider.ids)

	profiles, err := provider.ConnectionProfiles(context.Background(), lineID)
	if err != nil {
		t.Fatalf("ConnectionProfiles() error = %v", err)
	}
	if len(profiles) != 1 || profiles[0].ProfileID != 2 ||
		profiles[0].ProfileName != "ims" || profiles[0].IPFamily != "ipv4v6" {
		t.Fatalf("profiles = %+v", profiles)
	}

	caller.calls = nil
	profileID := int32(2)
	saved, err := provider.SaveConnectionProfile(context.Background(), domain.SaveConnectionProfileRequest{
		RequestID:   "profile-save-1",
		LineID:      lineID,
		ProfileID:   &profileID,
		ProfileName: "ims",
		APN:         "ims",
		IPFamily:    "ipv4v6",
		User:        "subscriber",
		Password:    "not-returned",
	})
	if err != nil {
		t.Fatalf("SaveConnectionProfile() error = %v", err)
	}
	if saved.ProfileID != 2 || saved.APN != "ims" || saved.User != "subscriber" {
		t.Fatalf("saved profile = %+v", saved)
	}
	invocations := caller.invocations()
	assertMethods(t, invocations,
		objectManagerInterface+".GetManagedObjects",
		profileManagerInterface+".Set",
	)
	properties := invocations[1].Args[0].(map[string]dbus.Variant)
	if properties["password"].Value() != "not-returned" {
		t.Fatalf("saved profile properties = %+v", properties)
	}

	caller.calls = nil
	receipt, err := provider.DeleteConnectionProfile(context.Background(), domain.DeleteConnectionProfileRequest{
		RequestID: "profile-delete-1",
		LineID:    lineID,
		ProfileID: &profileID,
	})
	if err != nil {
		t.Fatalf("DeleteConnectionProfile() error = %v", err)
	}
	if receipt.RequestID != "profile-delete-1" {
		t.Fatalf("delete receipt = %+v", receipt)
	}
	assertMethods(t, caller.invocations(),
		objectManagerInterface+".GetManagedObjects",
		profileManagerInterface+".Delete",
	)
}

func TestUSSDStatusAndCommandsUseStandardInterface(t *testing.T) {
	t.Parallel()
	objects := emptyLineObjects(true, true)
	objects[testModemPath][ussdInterface] = Properties{
		"State":               dbus.MakeVariant(uint32(1)),
		"NetworkNotification": dbus.MakeVariant("balance"),
	}
	caller := newFakeCaller(objects)
	caller.ussdResponse = "request accepted"
	provider := newTestProvider(caller)
	lineID := parsedLineID(objects, provider.ids)

	status, err := provider.USSDStatus(context.Background(), lineID)
	if err != nil {
		t.Fatalf("USSDStatus() error = %v", err)
	}
	if status.State != "idle" || status.NetworkNotification != "balance" {
		t.Fatalf("USSD status = %+v", status)
	}

	caller.calls = nil
	result, err := provider.USSDCommand(context.Background(), domain.USSDRequest{
		RequestID: "ussd-1",
		LineID:    lineID,
		Action:    domain.USSDInitiate,
		Command:   "*123#",
	})
	if err != nil {
		t.Fatalf("USSDCommand() error = %v", err)
	}
	if result.Response != "request accepted" {
		t.Fatalf("USSD result = %+v", result)
	}
	invocations := caller.invocations()
	assertMethods(t, invocations,
		objectManagerInterface+".GetManagedObjects",
		ussdInterface+".Initiate",
	)
	if len(invocations[1].Args) != 1 || invocations[1].Args[0] != "*123#" {
		t.Fatalf("Initiate args = %#v", invocations[1].Args)
	}
}

func TestSIMCommandRejectsSecretsBeforeDBus(t *testing.T) {
	t.Parallel()
	caller := newFakeCaller(emptyLineObjects(true, true))
	provider := newTestProvider(caller)
	_, err := provider.SIMCommand(context.Background(), domain.SIMCommandRequest{
		RequestID: "sim-invalid",
		LineID:    parsedLineID(caller.objects, provider.ids),
		Operation: domain.SIMSendPIN,
		PIN:       "12ab",
	})
	assertOperationError(t, err, domain.ErrorInvalidArgument, "sim_command")
	if len(caller.invocations()) != 0 {
		t.Fatalf("invalid PIN reached D-Bus: %+v", caller.invocations())
	}
}
