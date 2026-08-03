package mobilepairing

import "testing"

func TestPushRegistrationNormalizesAndValidatesTokens(t *testing.T) {
	t.Parallel()

	registration, err := (PushRegistration{
		APNSToken: " aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa ",
		VoIPToken: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		BundleID:  " com.example.modemdeck ",
	}).Normalize()
	if err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
	if registration.Environment != "development" ||
		registration.APNSToken != "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" ||
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
	} {
		if _, err := registration.Normalize(); err != ErrInvalidPushToken {
			t.Fatalf("Normalize(%+v) error = %v, want ErrInvalidPushToken", registration, err)
		}
	}
}
