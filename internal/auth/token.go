package auth

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"io"
)

const TokenBytes = 32

var rawURLBase64 = base64.RawURLEncoding.Strict()

type SessionToken string
type CSRFToken string

type SessionTokenDigest [sha256.Size]byte
type CSRFTokenDigest [sha256.Size]byte

func newOpaqueToken(random io.Reader) (string, error) {
	value := make([]byte, TokenBytes)
	if _, err := io.ReadFull(random, value); err != nil {
		return "", err
	}
	return rawURLBase64.EncodeToString(value), nil
}

func sessionTokenDigest(token SessionToken) (SessionTokenDigest, error) {
	if !validOpaqueToken(string(token)) {
		return SessionTokenDigest{}, newError("digest session token", CodeInvalidSessionToken, nil)
	}
	return SessionTokenDigest(sha256.Sum256([]byte(token))), nil
}

func csrfTokenDigest(token CSRFToken) (CSRFTokenDigest, error) {
	if !validOpaqueToken(string(token)) {
		return CSRFTokenDigest{}, newError("digest CSRF token", CodeInvalidCSRFToken, nil)
	}
	return CSRFTokenDigest(sha256.Sum256([]byte(token))), nil
}

func validOpaqueToken(token string) bool {
	if token == "" || len(token) != rawURLBase64.EncodedLen(TokenBytes) {
		return false
	}
	decoded, err := rawURLBase64.DecodeString(token)
	return err == nil &&
		len(decoded) == TokenBytes &&
		rawURLBase64.EncodeToString(decoded) == token
}

func equalCSRFDigests(left, right CSRFTokenDigest) bool {
	return subtle.ConstantTimeCompare(left[:], right[:]) == 1
}
