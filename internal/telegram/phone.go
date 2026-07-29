package telegram

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/human-agent65535/modemdeck/internal/phone"
)

// NormalizePhone is intentionally limited to an explicit global number.
// Commands containing national numbers are interpreted only after their line
// has been selected by the communication service.
func NormalizePhone(value string) (string, error) {
	_, canonical, err := phone.Normalize(value)
	if err != nil {
		return "", fmt.Errorf("phone number must be an explicit international number: %w", err)
	}
	return canonical, nil
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
