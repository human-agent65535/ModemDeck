package auth

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"strings"
	"testing"
)

func TestOpaqueTokenUsesThirtyTwoRandomBytes(t *testing.T) {
	randomBytes := bytes.Repeat([]byte{0xa5}, TokenBytes)
	token, err := newOpaqueToken(bytes.NewReader(randomBytes))
	if err != nil {
		t.Fatalf("newOpaqueToken() error = %v", err)
	}
	if !validOpaqueToken(token) {
		t.Fatalf("newOpaqueToken() = %q, want canonical 32-byte token", token)
	}

	decoded, err := rawURLBase64.DecodeString(token)
	if err != nil {
		t.Fatalf("DecodeString() error = %v", err)
	}
	if !bytes.Equal(decoded, randomBytes) {
		t.Fatalf("decoded token = %x, want %x", decoded, randomBytes)
	}
}

func TestSessionTokenDigestHashesPresentedToken(t *testing.T) {
	tokenValue, err := newOpaqueToken(bytes.NewReader(bytes.Repeat([]byte{0x3c}, TokenBytes)))
	if err != nil {
		t.Fatalf("newOpaqueToken() error = %v", err)
	}
	token := SessionToken(tokenValue)

	digest, err := sessionTokenDigest(token)
	if err != nil {
		t.Fatalf("sessionTokenDigest() error = %v", err)
	}
	want := SessionTokenDigest(sha256.Sum256([]byte(token)))
	if digest != want {
		t.Fatalf("digest = %x, want %x", digest, want)
	}
}

func TestTokenDigestsRejectMalformedValues(t *testing.T) {
	valid, err := newOpaqueToken(bytes.NewReader(bytes.Repeat([]byte{0x7d}, TokenBytes)))
	if err != nil {
		t.Fatalf("newOpaqueToken() error = %v", err)
	}

	tests := []string{
		"",
		"short",
		valid + "=",
		strings.Repeat("a", len(valid)),
		valid[:len(valid)-1] + "*",
	}
	for _, token := range tests {
		t.Run(token, func(t *testing.T) {
			if _, err := sessionTokenDigest(SessionToken(token)); !errors.Is(err, ErrInvalidSessionToken) {
				t.Fatalf("sessionTokenDigest() error = %v, want ErrInvalidSessionToken", err)
			}
			if _, err := csrfTokenDigest(CSRFToken(token)); !errors.Is(err, ErrInvalidCSRFToken) {
				t.Fatalf("csrfTokenDigest() error = %v, want ErrInvalidCSRFToken", err)
			}
		})
	}
}
