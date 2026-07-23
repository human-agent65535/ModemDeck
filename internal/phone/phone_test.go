package phone

import (
	"errors"
	"testing"
)

func TestNormalize(t *testing.T) {
	t.Parallel()

	original, canonical, err := Normalize(" +81 (80) 1234-5678 ")
	if err != nil {
		t.Fatalf("Normalize() error = %v", err)
	}
	if original != "+81 (80) 1234-5678" || canonical != "+818012345678" {
		t.Fatalf("Normalize() = %q, %q", original, canonical)
	}
}

func TestNormalizeRejectsInvalidNumbers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		value string
		code  ErrorCode
	}{
		{value: "", code: CodeRequired},
		{value: "818012345678", code: CodeInternationalPrefixRequired},
		{value: "+81.8012345678", code: CodeInvalidCharacter},
		{value: "+123", code: CodeInvalidLength},
		{value: "+0123456789", code: CodeInvalidCountryCode},
	}
	for _, test := range tests {
		_, _, err := Normalize(test.value)
		if !errors.Is(err, ErrInvalid) {
			t.Fatalf("Normalize(%q) error = %v, want ErrInvalid", test.value, err)
		}
		var numberError *Error
		if !errors.As(err, &numberError) || numberError.Code != test.code {
			t.Fatalf("Normalize(%q) error = %v, want code %q", test.value, err, test.code)
		}
	}
}
