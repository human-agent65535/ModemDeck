package timeutil

import (
	"testing"
	"time"
)

func TestParseModemTimestamp(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		value string
		want  time.Time
	}{
		{
			name:  "RFC3339 offset",
			value: "2026-07-24T14:30:17+08:00",
			want:  time.Date(2026, 7, 24, 6, 30, 17, 0, time.UTC),
		},
		{
			name:  "hour-only offset observed on Quectel",
			value: "2026-07-24T14:30:17+08",
			want:  time.Date(2026, 7, 24, 6, 30, 17, 0, time.UTC),
		},
		{
			name:  "compact offset",
			value: "2026-07-24T14:30:17+0800",
			want:  time.Date(2026, 7, 24, 6, 30, 17, 0, time.UTC),
		},
		{
			name:  "legacy 3GPP timestamp",
			value: "260724143017+08",
			want:  time.Date(2026, 7, 24, 6, 30, 17, 0, time.UTC),
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, ok := ParseModemTimestamp(test.value)
			if !ok || !got.Equal(test.want) {
				t.Fatalf("ParseModemTimestamp(%q) = %s, %t, want %s", test.value, got, ok, test.want)
			}
		})
	}
}

func TestParseModemTimestampRejectsInvalidValue(t *testing.T) {
	t.Parallel()
	if parsed, ok := ParseModemTimestamp("not-a-timestamp"); ok || !parsed.IsZero() {
		t.Fatalf("invalid timestamp parsed as %s, %t", parsed, ok)
	}
}

func TestParseModemTimestampRejectsEmptyValue(t *testing.T) {
	t.Parallel()
	if parsed, ok := ParseModemTimestamp("  "); ok || !parsed.IsZero() {
		t.Fatalf("empty timestamp parsed as %s, %t", parsed, ok)
	}
}

func TestParseDatabaseTimeReadsEveryStoredForm(t *testing.T) {
	t.Parallel()
	want := time.Date(2026, 7, 24, 6, 30, 17, 0, time.UTC)
	for _, value := range []string{
		"2026-07-24T06:30:17Z",
		"2026-07-24T14:30:17+08:00",
		"2026-07-24 14:30:17+08:00",
		"2026-07-24 06:30:17.000",
		"2026-07-24 06:30:17",
		" 2026-07-24 06:30:17 ",
		want.Format(SQLiteTimestampLayout),
	} {
		got, ok := ParseDatabaseTime(value)
		if !ok || !got.Equal(want) {
			t.Fatalf("ParseDatabaseTime(%q) = %s, %t, want %s", value, got, ok, want)
		}
	}
	for _, value := range []string{"", "2026-07-24", "not a time"} {
		if got, ok := ParseDatabaseTime(value); ok || !got.IsZero() {
			t.Fatalf("ParseDatabaseTime(%q) = %s, %t, want a failure", value, got, ok)
		}
	}
}
