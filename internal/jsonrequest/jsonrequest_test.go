package jsonrequest

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDecodeAcceptsOneJSONValueOfTheDeclaredType(t *testing.T) {
	type input struct {
		Name string `json:"name"`
	}
	for _, test := range []struct {
		name, contentType, body string
		want                    error
	}{
		{"valid", "application/json", `{"name":"a"}`, nil},
		{"charset parameter", "application/json; charset=utf-8", `{"name":"a"}`, nil},
		{"trailing whitespace", "application/json", "{\"name\":\"a\"}\n ", nil},
		{"missing content type", "", `{"name":"a"}`, ErrMediaType},
		{"other content type", "text/plain", `{"name":"a"}`, ErrMediaType},
		{"unknown field", "application/json", `{"name":"a","extra":1}`, ErrInvalid},
		{"malformed", "application/json", `{"name":`, ErrInvalid},
		{"empty", "application/json", ``, ErrInvalid},
		{"second value", "application/json", `{"name":"a"}{"name":"b"}`, ErrTrailingData},
		{"trailing text", "application/json", `{"name":"a"} x`, ErrTrailingData},
		{"too large", "application/json", `{"name":"` + strings.Repeat("a", 64) + `"}`, ErrTooLarge},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(test.body))
			if test.contentType != "" {
				request.Header.Set("Content-Type", test.contentType)
			}
			var destination input
			err := Decode(httptest.NewRecorder(), request, &destination, 32)
			if !errors.Is(err, test.want) || (test.want == nil && err != nil) {
				t.Fatalf("Decode() error = %v, want %v", err, test.want)
			}
			if test.want == nil && destination.Name != "a" {
				t.Fatalf("decoded = %+v", destination)
			}
		})
	}
}
