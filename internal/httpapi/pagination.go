package httpapi

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

const (
	pageCursorVersion   = 1
	maxPageCursorLength = 2048
)

type pageCursorPayload struct {
	Version     int             `json:"v"`
	Scope       string          `json:"s"`
	QueryDigest string          `json:"q"`
	Position    json.RawMessage `json:"p"`
}

type pageCursorEnvelope struct {
	Payload   pageCursorPayload `json:"d"`
	Signature string            `json:"h"`
}

var pageCursorSigningKey = func() []byte {
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		panic(fmt.Sprintf("generate page cursor signing key: %v", err))
	}
	return key
}()

func decodePageCursor(
	response http.ResponseWriter,
	request *http.Request,
	scope string,
	queryIdentity string,
	position any,
) (bool, bool) {
	value := strings.TrimSpace(request.URL.Query().Get("cursor"))
	if value == "" {
		return false, true
	}
	if len(value) > maxPageCursorLength {
		writeInvalidPageCursor(response)
		return false, false
	}
	encoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		writeInvalidPageCursor(response)
		return false, false
	}
	var envelope pageCursorEnvelope
	if err := json.Unmarshal(encoded, &envelope); err != nil ||
		envelope.Payload.Version != pageCursorVersion ||
		envelope.Payload.Scope != scope ||
		envelope.Payload.QueryDigest != pageQueryDigest(scope, queryIdentity) ||
		len(envelope.Payload.Position) == 0 {
		writeInvalidPageCursor(response)
		return false, false
	}
	signature, err := base64.RawURLEncoding.DecodeString(envelope.Signature)
	if err != nil || !hmac.Equal(
		signature,
		signPageCursor(envelope.Payload),
	) {
		writeInvalidPageCursor(response)
		return false, false
	}
	if err := json.Unmarshal(envelope.Payload.Position, position); err != nil {
		writeInvalidPageCursor(response)
		return false, false
	}
	return true, true
}

func encodePageCursor(scope, queryIdentity string, position any) (string, error) {
	encodedPosition, err := json.Marshal(position)
	if err != nil {
		return "", fmt.Errorf("encode cursor position: %w", err)
	}
	payload := pageCursorPayload{
		Version:     pageCursorVersion,
		Scope:       scope,
		QueryDigest: pageQueryDigest(scope, queryIdentity),
		Position:    encodedPosition,
	}
	encodedEnvelope, err := json.Marshal(pageCursorEnvelope{
		Payload: payload,
		Signature: base64.RawURLEncoding.EncodeToString(
			signPageCursor(payload),
		),
	})
	if err != nil {
		return "", fmt.Errorf("encode cursor: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(encodedEnvelope), nil
}

func signPageCursor(payload pageCursorPayload) []byte {
	encoded, err := json.Marshal(payload)
	if err != nil {
		panic(fmt.Sprintf("marshal page cursor signing payload: %v", err))
	}
	mac := hmac.New(sha256.New, pageCursorSigningKey)
	_, _ = mac.Write(encoded)
	return mac.Sum(nil)
}

func pageQueryDigest(scope, queryIdentity string) string {
	digest := sha256.Sum256([]byte(scope + "\x00" + queryIdentity))
	return base64.RawURLEncoding.EncodeToString(digest[:16])
}

func writeInvalidPageCursor(response http.ResponseWriter) {
	writeError(
		response,
		http.StatusBadRequest,
		"invalid_argument",
		"cursor is invalid for this query",
		"cursor",
	)
}

func pageItems[T any](items []T, limit int) ([]T, bool) {
	if len(items) <= limit {
		return items, false
	}
	return items[:limit], true
}
