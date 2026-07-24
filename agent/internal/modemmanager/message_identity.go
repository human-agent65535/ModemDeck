package modemmanager

import (
	"crypto/sha256"
	"encoding/base64"
	"strconv"
	"strings"
)

func stableIncomingMessageID(
	lineID string,
	properties Properties,
) (string, bool) {
	pduType, known := uint32Property(properties, "PduType")
	if !known || messageDirectionName(pduType) != "incoming" {
		return "", false
	}
	timestamp, _ := stringProperty(properties, "Timestamp")
	timestamp = strings.TrimSpace(timestamp)
	if timestamp == "" {
		return "", false
	}

	number, _ := stringProperty(properties, "Number")
	text, _ := stringProperty(properties, "Text")
	smsc, _ := stringProperty(properties, "SMSC")
	dischargeTimestamp, _ := stringProperty(properties, "DischargeTimestamp")
	messageReference, _ := uint32Property(properties, "MessageReference")
	teleserviceID, _ := uint32Property(properties, "TeleserviceId")
	serviceCategory, _ := uint32Property(properties, "ServiceCategory")
	fields := []string{
		strings.TrimSpace(lineID),
		strconv.FormatUint(uint64(pduType), 10),
		strings.TrimSpace(number),
		text,
		timestamp,
		strings.TrimSpace(smsc),
		strings.TrimSpace(dischargeTimestamp),
		strconv.FormatUint(uint64(messageReference), 10),
		strconv.FormatUint(uint64(teleserviceID), 10),
		strconv.FormatUint(uint64(serviceCategory), 10),
	}

	var identity strings.Builder
	for _, field := range fields {
		identity.WriteString(strconv.Itoa(len(field)))
		identity.WriteByte(':')
		identity.WriteString(field)
	}
	sum := sha256.Sum256([]byte("incoming-message\x00" + identity.String()))
	return "message_" + base64.RawURLEncoding.EncodeToString(sum[:18]), true
}
