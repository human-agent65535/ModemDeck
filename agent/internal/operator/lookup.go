package operator

import (
	_ "embed"
	"encoding/json"
	"strings"
)

const (
	databaseSource   = "https://github.com/musalbas/mcc-mnc-table"
	databaseRevision = "45b06a15ee614ba3d71ddf2b45717225894d0dd8"
	databaseSHA256   = "8a09618ef23ef1f7018ef256f5e889a61098e2024fb9e9016381155e6f7ad03b"
)

// mccMNCData is a fixed snapshot of databaseSource at databaseRevision.
// The source is MIT licensed; see LICENSE.mcc-mnc-table.
//
//go:embed mcc-mnc-table.json
var mccMNCData []byte

type row struct {
	MCC     string `json:"mcc"`
	MNC     string `json:"mnc"`
	Network string `json:"network"`
}

type database struct {
	rows   []row
	byPLMN map[string]string
}

var operators = mustLoadDatabase(mccMNCData)

// Name returns the source-provided network name for a five- or six-digit PLMN.
func Name(plmn string) (string, bool) {
	plmn, ok := normalizePLMN(plmn)
	if !ok {
		return "", false
	}
	name, ok := operators.byPLMN[plmn]
	return name, ok
}

func mustLoadDatabase(data []byte) database {
	var rows []row
	if err := json.Unmarshal(data, &rows); err != nil {
		panic("decode embedded MCC/MNC database: " + err.Error())
	}
	if len(rows) == 0 {
		panic("embedded MCC/MNC database is empty")
	}

	byPLMN := make(map[string]string, len(rows))
	for _, row := range rows {
		mcc := strings.TrimSpace(row.MCC)
		mnc := strings.TrimSpace(row.MNC)
		name := strings.TrimSpace(row.Network)
		if !isDecimalWithLength(mcc, 3, 3) ||
			!isDecimalWithLength(mnc, 2, 3) ||
			name == "" {
			continue
		}
		plmn := mcc + mnc
		if _, exists := byPLMN[plmn]; !exists {
			byPLMN[plmn] = name
		}
	}
	return database{rows: rows, byPLMN: byPLMN}
}

func normalizePLMN(raw string) (string, bool) {
	plmn := strings.TrimSpace(raw)
	if !isDecimalWithLength(plmn, 5, 6) {
		return "", false
	}
	return plmn, true
}

func isDecimalWithLength(value string, minimum, maximum int) bool {
	if len(value) < minimum || len(value) > maximum {
		return false
	}
	for index := range value {
		if value[index] < '0' || value[index] > '9' {
			return false
		}
	}
	return true
}
