package phone

import (
	"errors"
	"strings"
	"testing"
)

func TestNormalize(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		value     string
		original  string
		canonical string
	}{
		{
			value:     " +81 (80) 1234-5678 ",
			original:  "+81 (80) 1234-5678",
			canonical: "+818012345678",
		},
		{
			value:     " 0081 80 1234 5678 ",
			original:  "0081 80 1234 5678",
			canonical: "+818012345678",
		},
	} {
		original, canonical, err := Normalize(test.value)
		if err != nil {
			t.Fatalf("Normalize(%q) error = %v", test.value, err)
		}
		if original != test.original || canonical != test.canonical {
			t.Fatalf(
				"Normalize(%q) = %q, %q; want %q, %q",
				test.value,
				original,
				canonical,
				test.original,
				test.canonical,
			)
		}
	}
}

func TestNormalizeNetworkNumberKeepsNonInternationalValuesTruthful(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"0081 80 1234 5678":  "+818012345678",
		"+81 (80) 1234-5678": "+818012345678",
		"090-1234-5678":      "090-1234-5678",
		"10655526":           "10655526",
		"00123":              "00123",
		"unknown":            "unknown",
	}
	for input, expected := range tests {
		if actual := NormalizeNetworkNumber(input); actual != expected {
			t.Fatalf("NormalizeNetworkNumber(%q) = %q, want %q", input, actual, expected)
		}
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

func TestNormalizeDialTargetKeepsLocalNumbersNetworkDefined(t *testing.T) {
	t.Parallel()

	tests := []struct {
		value      string
		normalized string
	}{
		{value: "090-1234-5678", normalized: "09012345678"},
		{value: "110", normalized: "110"},
		{value: "+81 (80) 1234-5678", normalized: "+818012345678"},
		{value: "+49 30/1234.5678", normalized: "+493012345678"},
	}
	for _, test := range tests {
		original, normalized, err := NormalizeDialTarget(test.value)
		if err != nil {
			t.Fatalf("NormalizeDialTarget(%q) error = %v", test.value, err)
		}
		if original != test.value || normalized != test.normalized {
			t.Fatalf(
				"NormalizeDialTarget(%q) = %q, %q; want %q, %q",
				test.value,
				original,
				normalized,
				test.value,
				test.normalized,
			)
		}
	}
}

func TestNormalizeDialTargetRejectsUnsafeSyntax(t *testing.T) {
	t.Parallel()

	tests := []struct {
		value string
		code  ErrorCode
	}{
		{value: "", code: CodeRequired},
		{value: "Aiko", code: CodeInvalidCharacter},
		{value: "*123#", code: CodeInvalidCharacter},
		{value: "81+8012345678", code: CodeInvalidCharacter},
		{value: "+()", code: CodeInvalidLength},
		{value: strings.Repeat("1", MaxDialTargetDigits+1), code: CodeInvalidLength},
		{value: strings.Repeat("1", MaxNumberRunes+1), code: CodeTooLong},
	}
	for _, test := range tests {
		_, _, err := NormalizeDialTarget(test.value)
		if !errors.Is(err, ErrInvalid) {
			t.Fatalf("NormalizeDialTarget(%q) error = %v, want ErrInvalid", test.value, err)
		}
		var numberError *Error
		if !errors.As(err, &numberError) || numberError.Code != test.code {
			t.Fatalf(
				"NormalizeDialTarget(%q) error = %v, want code %q",
				test.value,
				err,
				test.code,
			)
		}
	}
}
