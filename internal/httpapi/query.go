package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/human-agent65535/modemdeck/internal/store"
)

const (
	maxSearchLength     = 200
	maxIdentifierLength = 128
	maxPhoneLength      = 64
	maxJSONBodyBytes    = 64 << 10
)

type contactInputRequest struct {
	DisplayName         string                     `json:"display_name"`
	Notes               string                     `json:"notes"`
	PreferredDeviceIMEI string                     `json:"preferred_device_imei"`
	Favorite            bool                       `json:"favorite"`
	Revision            int64                      `json:"revision"`
	Phones              []contactPhoneInputRequest `json:"phones"`
}

type contactPhoneInputRequest struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Number  string `json:"number"`
	Primary bool   `json:"primary"`
}

func decodeContactInput(response http.ResponseWriter, request *http.Request) (store.ContactInput, bool) {
	var body contactInputRequest
	if !decodeJSONBody(response, request, &body) {
		return store.ContactInput{}, false
	}
	phones := make([]store.ContactPhoneInput, len(body.Phones))
	for index := range body.Phones {
		phones[index] = store.ContactPhoneInput{
			ID:      body.Phones[index].ID,
			Label:   body.Phones[index].Label,
			Number:  body.Phones[index].Number,
			Primary: body.Phones[index].Primary,
		}
	}
	return store.ContactInput{
		DisplayName:         body.DisplayName,
		Notes:               body.Notes,
		PreferredDeviceIMEI: body.PreferredDeviceIMEI,
		Favorite:            body.Favorite,
		Revision:            body.Revision,
		Phones:              phones,
	}, true
}

func decodeJSONBody(response http.ResponseWriter, request *http.Request, destination any) bool {
	mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeError(response, http.StatusUnsupportedMediaType, "unsupported_media_type", "Content-Type must be application/json", "")
		return false
	}
	request.Body = http.MaxBytesReader(response, request.Body, maxJSONBodyBytes)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		writeJSONDecodeError(response, err)
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeError(response, http.StatusBadRequest, "invalid_json", "Request body must contain one JSON object", "")
		return false
	}
	return true
}

func writeJSONDecodeError(response http.ResponseWriter, err error) {
	var maximum *http.MaxBytesError
	if errors.As(err, &maximum) {
		writeError(response, http.StatusRequestEntityTooLarge, "body_too_large", "Request body is too large", "")
		return
	}
	writeError(response, http.StatusBadRequest, "invalid_json", "Request body must be a valid JSON object", "")
}

func requiredPositiveInt64(response http.ResponseWriter, request *http.Request, field string) (int64, bool) {
	raw := strings.TrimSpace(request.URL.Query().Get(field))
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value <= 0 {
		writeError(response, http.StatusBadRequest, "invalid_argument", field+" must be a positive integer", field)
		return 0, false
	}
	return value, true
}

func requestLimit(response http.ResponseWriter, request *http.Request) (int, bool) {
	raw := strings.TrimSpace(request.URL.Query().Get("limit"))
	if raw == "" {
		return store.DefaultQueryLimit, true
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit <= 0 {
		writeError(response, http.StatusBadRequest, "invalid_argument", "limit must be a positive integer", "limit")
		return 0, false
	}
	if limit > store.MaxQueryLimit {
		limit = store.MaxQueryLimit
	}
	return limit, true
}

func requestSearch(response http.ResponseWriter, request *http.Request) (string, bool) {
	return boundedFilter(response, request.URL.Query().Get("q"), "q", maxSearchLength)
}

func boundedFilter(response http.ResponseWriter, value, field string, maximum int) (string, bool) {
	value = strings.TrimSpace(value)
	if utf8.RuneCountInString(value) > maximum {
		writeError(response, http.StatusBadRequest, "invalid_argument", field+" is too long", field)
		return "", false
	}
	return value, true
}
