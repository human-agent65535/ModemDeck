package communication

import (
	"strings"
	"time"
)

var modemManagerTimestampLayouts = [...]string{
	time.RFC3339Nano,
	"2006-01-02T15:04:05Z07",
	"2006-01-02T15:04:05Z0700",
	"060102150405Z07",
}

func parseModemManagerTimestamp(value string) (time.Time, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, false
	}
	for _, layout := range modemManagerTimestampLayouts {
		parsed, err := time.Parse(layout, value)
		if err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}
