package httpapi

import (
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
)

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
