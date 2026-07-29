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
	MCC         string `json:"mcc"`
	MNC         string `json:"mnc"`
	ISO         string `json:"iso"`
	Country     string `json:"country"`
	CountryCode string `json:"country_code"`
	Network     string `json:"network"`
}

type Details struct {
	Name               string
	CountryISO         string
	Country            string
	CountryCallingCode string
}

type database struct {
	rows   []row
	byPLMN map[string]Details
	byMCC  map[string]Details
}

var operators = mustLoadDatabase(mccMNCData)

// Name returns the source-provided network name for a five- or six-digit PLMN.
func Name(plmn string) (string, bool) {
	details, ok := Lookup(plmn)
	return details.Name, ok && details.Name != ""
}

// Lookup returns the source-provided operator and home-country metadata for a
// five- or six-digit PLMN.
func Lookup(plmn string) (Details, bool) {
	plmn, ok := normalizePLMN(plmn)
	if !ok {
		return Details{}, false
	}
	details, ok := operators.byPLMN[plmn]
	return details, ok
}

// CountryForIMSI returns country metadata from the IMSI's MCC. It deliberately
// does not infer an operator because the MNC length is not encoded separately.
func CountryForIMSI(imsi string) (Details, bool) {
	imsi = strings.TrimSpace(imsi)
	if !isDecimalWithLength(imsi, 3, 32) {
		return Details{}, false
	}
	details, ok := operators.byMCC[imsi[:3]]
	return details, ok
}

func mustLoadDatabase(data []byte) database {
	var rows []row
	if err := json.Unmarshal(data, &rows); err != nil {
		panic("decode embedded MCC/MNC database: " + err.Error())
	}
	if len(rows) == 0 {
		panic("embedded MCC/MNC database is empty")
	}

	byPLMN := make(map[string]Details, len(rows))
	byMCC := make(map[string]Details)
	for _, row := range rows {
		mcc := strings.TrimSpace(row.MCC)
		mnc := strings.TrimSpace(row.MNC)
		name := strings.TrimSpace(row.Network)
		if !isDecimalWithLength(mcc, 3, 3) ||
			!isDecimalWithLength(mnc, 2, 3) {
			continue
		}
		plmn := mcc + mnc
		details := byPLMN[plmn]
		if details.Name == "" {
			details.Name = name
		}
		if details.CountryISO == "" {
			details.CountryISO = strings.ToUpper(strings.TrimSpace(row.ISO))
		}
		if details.Country == "" {
			details.Country = strings.TrimSpace(row.Country)
		}
		if details.CountryCallingCode == "" {
			details.CountryCallingCode = strings.TrimSpace(row.CountryCode)
		}
		byPLMN[plmn] = details

		country := byMCC[mcc]
		if country.CountryISO == "" {
			country.CountryISO = details.CountryISO
		}
		if country.Country == "" {
			country.Country = details.Country
		}
		if country.CountryCallingCode == "" {
			country.CountryCallingCode = details.CountryCallingCode
		}
		byMCC[mcc] = country
	}
	return database{rows: rows, byPLMN: byPLMN, byMCC: byMCC}
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
