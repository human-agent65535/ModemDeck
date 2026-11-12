// Package jsonrequest holds the one rule for reading a JSON request body.
// Every HTTP entry point of the application accepts exactly one JSON value of
// the declared type, sent as application/json, within a size limit.
package jsonrequest

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
)

var (
	// ErrMediaType: the Content-Type is not application/json.
	ErrMediaType = errors.New("content type must be application/json")
	// ErrTooLarge: the body exceeds the limit given to Decode.
	ErrTooLarge = errors.New("request body is too large")
	// ErrInvalid: the body is not one JSON value of the expected shape, or it
	// names a field the destination does not have.
	ErrInvalid = errors.New("request body is not a valid JSON value")
	// ErrTrailingData: something follows the JSON value.
	ErrTrailingData = errors.New("request body must contain one JSON value")
)

// Decode reads the request body into destination. It fails with one of the
// package errors; the caller chooses the response for each.
func Decode(
	response http.ResponseWriter,
	request *http.Request,
	destination any,
	maximumBytes int64,
) error {
	mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return ErrMediaType
	}
	request.Body = http.MaxBytesReader(response, request.Body, maximumBytes)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return classify(err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if classified := classify(err); errors.Is(classified, ErrTooLarge) {
			return classified
		}
		return ErrTrailingData
	}
	return nil
}

func classify(err error) error {
	var maximum *http.MaxBytesError
	if errors.As(err, &maximum) {
		return ErrTooLarge
	}
	return ErrInvalid
}
