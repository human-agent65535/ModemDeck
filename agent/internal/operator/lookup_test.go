package operator

import (
	"crypto/sha256"
	"fmt"
	"testing"
)

func TestNameUsesEmbeddedCompleteTable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		plmn string
		want string
		ok   bool
	}{
		{name: "China Unicom", plmn: "46001", want: "China Unicom", ok: true},
		{name: "KDDI", plmn: "44051", want: "KDDI", ok: true},
		{name: "two digit MNC with leading zero", plmn: "41201", want: "AWCC", ok: true},
		{name: "three digit MNC", plmn: "310260", want: "T-Mobile", ok: true},
		{name: "three digit MNC with leading zero", plmn: "344030", want: "imobile / APUA", ok: true},
		{name: "surrounding whitespace", plmn: " 46001 ", want: "China Unicom", ok: true},
		{name: "unknown PLMN", plmn: "12345", want: "", ok: false},
		{name: "invalid short PLMN", plmn: "4601", want: "", ok: false},
		{name: "invalid non-decimal PLMN", plmn: "460A1", want: "", ok: false},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, ok := Name(test.plmn)
			if got != test.want || ok != test.ok {
				t.Fatalf("Name(%q) = (%q, %v), want (%q, %v)", test.plmn, got, ok, test.want, test.ok)
			}
		})
	}
}

func TestEmbeddedDatabaseSnapshotIsComplete(t *testing.T) {
	t.Parallel()

	if databaseSource != "https://github.com/musalbas/mcc-mnc-table" {
		t.Fatalf("database source = %q", databaseSource)
	}
	if databaseRevision != "45b06a15ee614ba3d71ddf2b45717225894d0dd8" {
		t.Fatalf("database revision = %q", databaseRevision)
	}
	if got := fmt.Sprintf("%x", sha256.Sum256(mccMNCData)); got != databaseSHA256 {
		t.Fatalf("database SHA-256 = %q, want %q", got, databaseSHA256)
	}
	if got := len(operators.rows); got != 2599 {
		t.Fatalf("embedded MCC/MNC rows = %d, want 2599", got)
	}
	if got := len(operators.byPLMN); got < 2000 {
		t.Fatalf("embedded MCC/MNC lookup entries = %d, want at least 2000", got)
	}
}
