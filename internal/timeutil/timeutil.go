// Package timeutil holds the application's rules for reading times it did not
// format itself in the current process: values stored in SQLite and timestamps
// reported by ModemManager.
package timeutil

import (
	"strings"
	"time"
)

// SQLiteTimestampLayout is the text SQLite's CURRENT_TIMESTAMP produces, in UTC.
const SQLiteTimestampLayout = "2006-01-02 15:04:05"

// databaseLayouts are every form a time column has been written in: RFC 3339
// by the application, and the SQLite forms by column defaults and by older
// releases.
var databaseLayouts = [...]string{
	time.RFC3339Nano,
	"2006-01-02 15:04:05.999999999-07:00",
	"2006-01-02 15:04:05.999999999",
	SQLiteTimestampLayout,
}

// ParseDatabaseTime reads a time stored in the database. A form without an
// offset is UTC.
func ParseDatabaseTime(value string) (time.Time, bool) {
	value = strings.TrimSpace(value)
	for _, layout := range databaseLayouts {
		parsed, err := time.Parse(layout, value)
		if err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}

// modemLayouts are the timestamp forms modems report through ModemManager.
var modemLayouts = [...]string{
	time.RFC3339Nano,
	"2006-01-02T15:04:05Z07",
	"2006-01-02T15:04:05Z0700",
	"060102150405Z07",
}

// ParseModemTimestamp reads a timestamp reported by ModemManager.
func ParseModemTimestamp(value string) (time.Time, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, false
	}
	for _, layout := range modemLayouts {
		parsed, err := time.Parse(layout, value)
		if err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}
