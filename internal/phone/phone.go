package phone

import (
	"errors"
	"strings"
	"unicode"

	"github.com/nyaruka/phonenumbers/v2"
)

const (
	MaxNumberRunes      = 64
	MaxDialTargetDigits = 32
	MaxShortCodeDigits  = 6
)

type ErrorCode string

const (
	CodeRequired                    ErrorCode = "required"
	CodeTooLong                     ErrorCode = "too_long"
	CodeInternationalPrefixRequired ErrorCode = "international_prefix_required"
	CodeRegionRequired              ErrorCode = "region_required"
	CodeInvalidRegion               ErrorCode = "invalid_region"
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

type Kind string

const (
	KindSubscriber   Kind = "subscriber"
	KindShortCode    Kind = "short_code"
	KindAlphanumeric Kind = "alphanumeric"
	KindUnknown      Kind = "unknown"
)

// Address is the single parsed representation used by communication flows.
// E164 is the global subscriber identity. Dial is the unambiguous value sent
// to the modem. Short codes remain line-local and therefore have no E164.
type Address struct {
	Original      string
	Kind          Kind
	E164          string
	Dial          string
	National      string
	International string
	Region        string
}

func ParseSubscriber(value, defaultRegion string) (Address, error) {
	return parseSubscriber(value, defaultRegion, false)
}

func parseSubscriber(value, defaultRegion string, requireValid bool) (Address, error) {
	original, compact, digitCount, err := normalizeSyntax(value)
	if err != nil {
		return Address{}, err
	}
	if digitCount < 2 || digitCount > 15 {
		return Address{}, &Error{Code: CodeInvalidLength}
	}
	region, err := normalizeRegion(defaultRegion)
	if err != nil {
		return Address{}, err
	}
	if !strings.HasPrefix(compact, "+") && region == "" {
		return Address{}, &Error{Code: CodeRegionRequired}
	}
	if !strings.HasPrefix(compact, "+") &&
		!strings.HasPrefix(compact, "00") &&
		digitCount <= MaxShortCodeDigits {
		return Address{}, &Error{Code: CodeInvalidLength}
	}
	number, parseErr := phonenumbers.ParseAndKeepRawInput(compact, region)
	if parseErr != nil {
		if errors.Is(parseErr, phonenumbers.ErrInvalidCountryCode) {
			return Address{}, &Error{Code: CodeInvalidCountryCode}
		}
		return Address{}, &Error{Code: CodeInvalidLength}
	}
	if strings.HasPrefix(compact, "00") &&
		number.GetCountryCodeSource() != phonenumbers.PhoneNumber_FROM_NUMBER_WITH_IDD {
		return Address{}, &Error{Code: CodeInvalidLength}
	}
	if number.GetCountryCodeSource() ==
		phonenumbers.PhoneNumber_FROM_NUMBER_WITHOUT_PLUS_SIGN {
		return Address{}, &Error{Code: CodeInternationalPrefixRequired}
	}
	if !phonenumbers.IsPossibleNumber(number) ||
		(requireValid && !phonenumbers.IsValidNumber(number)) {
		return Address{}, &Error{Code: CodeInvalidLength}
	}
	e164 := phonenumbers.Format(number, phonenumbers.E164)
	if e164 == "" {
		return Address{}, &Error{Code: CodeInvalidCountryCode}
	}
	return Address{
		Original:      original,
		Kind:          KindSubscriber,
		E164:          e164,
		Dial:          e164,
		National:      phonenumbers.Format(number, phonenumbers.NATIONAL),
		International: phonenumbers.Format(number, phonenumbers.INTERNATIONAL),
		Region:        phonenumbers.GetRegionCodeForNumber(number),
	}, nil
}

func ParseDestination(value, defaultRegion string) (Address, error) {
	original, compact, digitCount, syntaxErr := normalizeSyntax(value)
	if syntaxErr != nil {
		return Address{}, syntaxErr
	}
	region, regionErr := normalizeRegion(defaultRegion)
	if regionErr != nil {
		return Address{}, regionErr
	}
	if !strings.HasPrefix(compact, "+") &&
		!strings.HasPrefix(compact, "00") &&
		digitCount >= 2 &&
		digitCount <= MaxShortCodeDigits {
		return Address{
			Original: original,
			Kind:     KindShortCode,
			Dial:     compact,
			National: compact,
			Region:   region,
		}, nil
	}
	address, subscriberErr := ParseSubscriber(original, region)
	if subscriberErr == nil {
		return address, nil
	}
	return Address{}, subscriberErr
}

// ParseNetworkAddress classifies a modem-reported address without inventing an
// identity when the network omitted the context required to parse it.
func ParseNetworkAddress(value, defaultRegion string) Address {
	value = strings.TrimSpace(value)
	if value == "" {
		return Address{}
	}
	original, compact, _, syntaxErr := normalizeSyntax(value)
	region, regionErr := normalizeRegion(defaultRegion)
	if syntaxErr == nil && regionErr == nil {
		if strings.HasPrefix(compact, "00") {
			if address, parseErr := parseSubscriber(
				"+"+strings.TrimPrefix(compact, "00"),
				"",
				true,
			); parseErr == nil {
				address.Original = original
				return address
			}
		}
		if address, parseErr := parseSubscriber(original, region, true); parseErr == nil {
			return address
		}
		if region != "" &&
			!strings.HasPrefix(compact, "+") &&
			!strings.HasPrefix(compact, "0") {
			if address, parseErr := parseSubscriber("+"+compact, "", true); parseErr == nil {
				address.Original = original
				return address
			}
		}
	}
	if validAlphanumericSender(value) {
		return Address{
			Original: value,
			Kind:     KindAlphanumeric,
			Dial:     value,
		}
	}
	return Address{
		Original: value,
		Kind:     KindUnknown,
		Dial:     value,
	}
}

func CanonicalNetworkAddress(value, defaultRegion string) string {
	address := ParseNetworkAddress(value, defaultRegion)
	if address.Kind == KindSubscriber {
		return address.E164
	}
	return address.Dial
}

// NetworkSubscriberE164 returns only a globally unambiguous subscriber
// identity. Short codes, alphanumeric senders, and unparseable raw values are
// intentionally excluded from global identity indexes.
func NetworkSubscriberE164(value, defaultRegion string) string {
	address := ParseNetworkAddress(value, defaultRegion)
	if address.Kind != KindSubscriber {
		return ""
	}
	return address.E164
}

// Normalize is retained for callers that explicitly provide a global number.
// National numbers must use ParseSubscriber with a region.
func Normalize(value string) (original string, canonical string, err error) {
	address, err := ParseSubscriber(value, "")
	if err != nil {
		return "", "", err
	}
	return address.Original, address.E164, nil
}

// NormalizeDialTarget is retained for explicit global numbers only. New call
// paths must use ParseDestination with the selected line's home region.
func NormalizeDialTarget(value string) (original string, normalized string, err error) {
	address, err := ParseDestination(value, "")
	if err != nil {
		return "", "", err
	}
	return address.Original, address.Dial, nil
}

// CanonicalRegion returns an uppercase supported ISO 3166-1 alpha-2 region.
// Invalid and absent values are both treated as unknown at network boundaries.
func CanonicalRegion(value string) string {
	region, err := normalizeRegion(value)
	if err != nil {
		return ""
	}
	return region
}

func normalizeSyntax(value string) (original, compact string, digitCount int, err error) {
	original = strings.TrimSpace(value)
	if original == "" {
		return "", "", 0, &Error{Code: CodeRequired}
	}
	if len([]rune(original)) > MaxNumberRunes {
		return "", "", 0, &Error{Code: CodeTooLong}
	}
	var normalized strings.Builder
	normalized.Grow(len(original))
	for index, character := range original {
		switch {
		case character >= '0' && character <= '9':
			normalized.WriteRune(character)
			digitCount++
		case character == '+' && index == 0:
			normalized.WriteRune(character)
		case unicode.IsControl(character):
			return "", "", 0, &Error{Code: CodeInvalidCharacter}
		case unicode.IsSpace(character) || strings.ContainsRune("().-/", character):
		default:
			return "", "", 0, &Error{Code: CodeInvalidCharacter}
		}
	}
	if digitCount == 0 || digitCount > MaxDialTargetDigits {
		return "", "", 0, &Error{Code: CodeInvalidLength}
	}
	return original, normalized.String(), digitCount, nil
}

func normalizeRegion(value string) (string, error) {
	region := strings.ToUpper(strings.TrimSpace(value))
	if region == "" {
		return "", nil
	}
	if len(region) != 2 || !phonenumbers.GetSupportedRegions()[region] {
		return "", &Error{Code: CodeInvalidRegion}
	}
	return region, nil
}

func validAlphanumericSender(value string) bool {
	if len([]rune(value)) > 32 {
		return false
	}
	hasLetter := false
	for _, character := range value {
		switch {
		case unicode.IsLetter(character):
			hasLetter = true
		case unicode.IsDigit(character), character == ' ', character == '-', character == '_', character == '.':
		default:
			return false
		}
	}
	return hasLetter
}
