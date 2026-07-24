package modemmanager

import (
	"context"
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
