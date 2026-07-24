package store

import (
	"strings"
)

func MessageThreadKey(localPhone, imsi, iccid, peer string) string {
	identity := messageLineIdentity(localPhone, imsi, iccid)
	return identity + "|" + strings.TrimSpace(peer)
}

func NormalizeLinePhone(value string) string {
	return normalizePhoneIdentity(value)
}

func messageLineIdentity(localPhone, imsi, iccid string) string {
	if normalized := normalizePhoneIdentity(localPhone); normalized != "" {
		return "phone:" + normalized
	}
	if normalized := strings.TrimSpace(imsi); normalized != "" {
		return "imsi:" + normalized
	}
	return "iccid:" + strings.TrimSpace(iccid)
}

func normalizePhoneIdentity(value string) string {
	var digits strings.Builder
	digits.Grow(len(value))
	for _, character := range strings.TrimSpace(value) {
		if character >= '0' && character <= '9' {
			digits.WriteRune(character)
		}
	}
	return digits.String()
}

func normalizedPhoneSQL(column string) string {
	return "REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(TRIM(" + column +
		"), '+', ''), ' ', ''), '-', ''), '(', ''), ')', '')"
}
