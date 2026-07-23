package phone

import (
	"errors"
	"strings"
)

const MaxNumberRunes = 64

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
func Normalize(value string) (original string, canonical string, err error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", "", &Error{Code: CodeRequired}
	}
	if len([]rune(value)) > MaxNumberRunes {
		return "", "", &Error{Code: CodeTooLong}
	}
	if value[0] != '+' {
		return "", "", &Error{Code: CodeInternationalPrefixRequired}
	}

	var digits strings.Builder
	digits.Grow(len(value) - 1)
	for _, character := range value[1:] {
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
