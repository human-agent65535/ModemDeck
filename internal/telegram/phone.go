package telegram

import (
	"fmt"
	"strings"
	"unicode"
)

// NormalizePhone returns strict E.164. It strips common presentation
// separators and converts an international 00 prefix to +. It never guesses a
// country code for national-format numbers.
func NormalizePhone(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("phone number is required")
	}

	var normalized strings.Builder
	normalized.Grow(len(value))
	for i, ch := range value {
		switch {
		case ch >= '0' && ch <= '9':
			normalized.WriteRune(ch)
		case ch == '+' && i == 0:
			normalized.WriteRune(ch)
		case ch == ' ' || ch == '-' || ch == '(' || ch == ')' || ch == '.':
			continue
		default:
			return "", fmt.Errorf("phone number contains unsupported characters")
		}
	}

	result := normalized.String()
	if strings.HasPrefix(result, "00") {
		result = "+" + result[2:]
	}
	if !strings.HasPrefix(result, "+") {
		return "", fmt.Errorf("phone number must include an international country code")
	}

	digits := result[1:]
	if len(digits) < 8 || len(digits) > 15 {
		return "", fmt.Errorf("phone number must contain 8 to 15 digits")
	}
	if digits[0] == '0' {
		return "", fmt.Errorf("country code must not start with zero")
	}
	for _, ch := range digits {
		if ch < '0' || ch > '9' {
			return "", fmt.Errorf("phone number contains unsupported characters")
		}
	}
	return result, nil
}

// MaskPhone is intended only for user-facing diagnostics. Event/log fields do
// not contain even masked phone numbers.
func MaskPhone(value string) string {
	normalized, err := NormalizePhone(value)
	if err != nil {
		return "[invalid]"
	}
	if len(normalized) <= 5 {
		return "****"
	}
	return "+***" + normalized[len(normalized)-4:]
}

func notificationPeer(value string) (display string, normalized string, replyable bool) {
	if normalized, err := NormalizePhone(value); err == nil {
		return normalized, normalized, true
	}

	fields := strings.FieldsFunc(value, func(ch rune) bool {
		return unicode.IsSpace(ch) || unicode.IsControl(ch)
	})
	display = strings.Join(fields, " ")
	if display == "" {
		return "未知号码", "", false
	}
	return truncateRunes(display, 128), "", false
}
