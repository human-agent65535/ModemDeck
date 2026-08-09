package mobilepairing

import (
	"strings"
	"testing"
)

func TestPushRegistrationNormalizesAndValidatesTokens(t *testing.T) {
	t.Parallel()

	registration, err := (PushRegistration{
		APNSToken: " " + strings.ToUpper(strings.Repeat("ab", 32)) + " ",
		VoIPToken: strings.Repeat("cd", 32),
		BundleID:  " com.example.modemdeck ",
	}).Normalize()
	if err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
	if registration.Environment != "development" ||
		registration.APNSToken != strings.Repeat("ab", 32) ||
		registration.BundleID != "com.example.modemdeck" {
		t.Fatalf("registration = %+v", registration)
	}
}

func TestPushRegistrationRejectsMalformedTokens(t *testing.T) {
	t.Parallel()

	for _, registration := range []PushRegistration{
		{APNSToken: "not-hex", Environment: "development"},
		{APNSToken: "aa", Environment: "development"},
		{APNSToken: "aa" + "bb", Environment: "staging"},
		{Environment: "development"},
		{APNSToken: strings.Repeat("ab", 32)},
		{APNSToken: strings.Repeat("ab", 32), BundleID: "invalid bundle"},
	} {
		if _, err := registration.Normalize(); err != ErrInvalidPushToken {
			t.Fatalf("Normalize(%+v) error = %v, want ErrInvalidPushToken", registration, err)
		}
	}
}
