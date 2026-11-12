package store

import "testing"

func TestMergeLineMetadataKeepsLiveValuesAndFillsOnlyWhatIsMissing(t *testing.T) {
	live := []LineSummary{
		{ID: "line-1", PhoneNumber: "+819000000001", Operator: "Live Operator", DeviceIMEI: "imei-1", DeviceName: "Live name", LineLabel: "ignored"},
		{ID: "line-2"},
		{ID: "line-new", DeviceIMEI: "imei-2"},
	}
	persisted := []LineSummary{
		{ID: "line-1", LineLabel: "Main", LineColor: LineColorAmber, PhoneNumber: "+819099999999", Operator: "Stored Operator", HomeOperatorCode: "44010", DeviceIMEI: "imei-stored", DeviceName: "Stored name"},
		{ID: "line-2", LineLabel: "Travel", PhoneNumber: "+819000000002", Operator: "Stored Operator", DeviceIMEI: "imei-2", DeviceName: "Travel modem"},
	}

	merged := MergeLineMetadata(live, persisted)
	if len(merged) != 3 {
		t.Fatalf("merged = %+v", merged)
	}

	// What the modem reported stays; the label and colour are always the user's.
	first := merged[0]
	if first.PhoneNumber != "+819000000001" || first.Operator != "Live Operator" ||
		first.DeviceIMEI != "imei-1" || first.DeviceName != "Live name" ||
		first.LineLabel != "Main" || first.LineColor != LineColorAmber ||
		first.HomeOperatorCode != "44010" || first.HomeOperatorName != "Live Operator" {
		t.Fatalf("line with live values = %+v", first)
	}

	// Everything the modem left empty comes from the stored line.
	second := merged[1]
	if second.PhoneNumber != "+819000000002" || second.Operator != "Stored Operator" ||
		second.HomeOperatorName != "Stored Operator" || second.DeviceIMEI != "imei-2" ||
		second.DeviceName != "Travel modem" || second.LineLabel != "Travel" {
		t.Fatalf("line without live values = %+v", second)
	}

	// A line the store has never seen still gets its modem's name by IMEI,
	// and nothing else is invented for it.
	third := merged[2]
	if third.DeviceName != "Travel modem" || third.LineLabel != "" || third.PhoneNumber != "" {
		t.Fatalf("unknown line = %+v", third)
	}
	if live[1].PhoneNumber != "" {
		t.Fatal("the live lines were modified in place")
	}
}
