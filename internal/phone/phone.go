package phone

import (
	"errors"
	"strings"
	"unicode"
)

const (
	MaxNumberRunes      = 64
	MaxDialTargetDigits = 32
)

type ErrorCode string

const (
	CodeRequired                    ErrorCode = "required"
	CodeTooLong                     ErrorCode = "too_long"
	CodeInternationalPrefixRequired ErrorCode = "international_prefix_required"
	CodeInvalidCharacter            ErrorCode = "invalid_character"
	CodeInvalidLength               ErrorCode = "invalid_length"
	CodeInvalidCountryCode          ErrorCode = "invalid_country_code"
)

type Error struct {
	Code ErrorCode
}

func (e *Error) Error() string {
	if e == nil {
		return "<nil>"
	}
	return "invalid phone number: " + string(e.Code)
}

func (e *Error) Is(target error) bool {
	other, ok := target.(*Error)
	return ok && e != nil && e.Code == other.Code
}

var ErrInvalid = errors.New("invalid phone number")

func (e *Error) Unwrap() error {
	return ErrInvalid
}

// Normalize preserves the operator-entered representation and returns a
// canonical E.164 value suitable for ownership and routing comparisons.
// Both "+" and the international "00" prefix resolve to the same identity.
func Normalize(value string) (original string, canonical string, err error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", "", &Error{Code: CodeRequired}
	}
	if len([]rune(value)) > MaxNumberRunes {
		return "", "", &Error{Code: CodeTooLong}
	}
	prefixLength := 0
	switch {
	case strings.HasPrefix(value, "+"):
		prefixLength = 1
	case strings.HasPrefix(value, "00"):
		prefixLength = 2
	default:
		return "", "", &Error{Code: CodeInternationalPrefixRequired}
	}

	var digits strings.Builder
	digits.Grow(len(value) - prefixLength)
	for _, character := range value[prefixLength:] {
		switch {
		case character >= '0' && character <= '9':
			digits.WriteRune(character)
		case character == ' ', character == '(', character == ')', character == '-':
		default:
			return "", "", &Error{Code: CodeInvalidCharacter}
		}
	}
	canonicalDigits := digits.String()
	if len(canonicalDigits) < 8 || len(canonicalDigits) > 15 {
		return "", "", &Error{Code: CodeInvalidLength}
	}
	if canonicalDigits[0] == '0' {
		return "", "", &Error{Code: CodeInvalidCountryCode}
	}
	return value, "+" + canonicalDigits, nil
}

// NormalizeNetworkNumber canonicalizes only an explicitly international
// network-reported number. Local, short, withheld, and otherwise unknown
// values remain untouched because their interpretation belongs to the network.
func NormalizeNetworkNumber(value string) string {
	value = strings.TrimSpace(value)
	_, canonical, err := Normalize(value)
	if err != nil {
		return value
	}
	return canonical
}

// NormalizeDialTarget accepts a dial string without attempting to determine
// whether a national number exists. The selected mobile network remains the
// authority for local and short numbers.
func NormalizeDialTarget(value string) (original string, normalized string, err error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", "", &Error{Code: CodeRequired}
	}
	if len([]rune(value)) > MaxNumberRunes {
		return "", "", &Error{Code: CodeTooLong}
	}

	var target strings.Builder
	target.Grow(len(value))
	digitCount := 0
	for index, character := range value {
		switch {
		case character >= '0' && character <= '9':
			target.WriteRune(character)
			digitCount++
		case character == '+' && index == 0:
			target.WriteRune(character)
		case unicode.IsControl(character):
			return "", "", &Error{Code: CodeInvalidCharacter}
		case unicode.IsSpace(character) || strings.ContainsRune("().-/", character):
		default:
			return "", "", &Error{Code: CodeInvalidCharacter}
		}
	}
	if digitCount == 0 || digitCount > MaxDialTargetDigits {
		return "", "", &Error{Code: CodeInvalidLength}
	}
	return value, target.String(), nil
}
