package mobilepairing

import (
	"bytes"
	"testing"
)

func TestTokenRoundTrip(t *testing.T) {
	t.Parallel()

	token, digest, err := newToken(bytes.NewReader(bytes.Repeat([]byte{0x2a}, TokenBytes)))
	if err != nil {
		t.Fatalf("newToken() error = %v", err)
	}
	if token != "md_ios_KioqKioqKioqKioqKioqKioqKioqKioqKioqKioqKio" {
		t.Fatalf("token = %q", token)
	}
	actual, err := Digest(token)
	if err != nil {
		t.Fatalf("Digest() error = %v", err)
	}
	if actual != digest {
		t.Fatalf("digest = %x, want %x", actual, digest)
	}
	payload := NewPayload(" https://phone.example.com ", token)
	if payload.Version != 1 ||
		payload.Type != PayloadType ||
		payload.ServerURL != "https://phone.example.com" ||
		payload.Token != token {
		t.Fatalf("payload = %+v", payload)
	}
}

func TestDigestRejectsMalformedTokens(t *testing.T) {
	t.Parallel()

	for _, token := range []Token{
		"",
		"md_ios_short",
		"wrong_KioqKioqKioqKioqKioqKioqKioqKioqKioqKioqKio",
		"md_ios_KioqKioqKioqKioqKioqKioqKioqKioqKioqKioqKi!",
	} {
		if _, err := Digest(token); err == nil {
			t.Fatalf("Digest(%q) error = nil", token)
		}
	}
}
