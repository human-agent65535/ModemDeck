package secretbox

import (
	"bytes"
	"encoding/base64"
	"errors"
	"testing"
)

func TestSealBindsCiphertextToAssociatedData(t *testing.T) {
	t.Parallel()

	key := bytes.Repeat([]byte{0x42}, KeySize)
	box, err := New(key)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	nonce, ciphertext, err := box.Seal([]byte("secret-token"), []byte("telegram:unit-1"))
	if err != nil {
		t.Fatalf("Seal() error = %v", err)
	}
	plaintext, err := box.Open(nonce, ciphertext, []byte("telegram:unit-1"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if string(plaintext) != "secret-token" {
		t.Fatalf("plaintext = %q", plaintext)
	}
	if _, err := box.Open(nonce, ciphertext, []byte("telegram:unit-2")); !errors.Is(err, ErrDecryptionFailed) {
		t.Fatalf("Open() with wrong associated data error = %v", err)
	}
}

func TestDecodeKeyAcceptsOperationalFormats(t *testing.T) {
	t.Parallel()

	key := bytes.Repeat([]byte{0x24}, KeySize)
	tests := [][]byte{
		key,
		[]byte(base64.StdEncoding.EncodeToString(key) + "\n"),
		[]byte(base64.RawURLEncoding.EncodeToString(key)),
	}
	for _, encoded := range tests {
		decoded, err := DecodeKey(encoded)
		if err != nil {
			t.Fatalf("DecodeKey(%q) error = %v", encoded, err)
		}
		if !bytes.Equal(decoded, key) {
			t.Fatalf("decoded key differs")
		}
	}
	if _, err := DecodeKey([]byte("short")); !errors.Is(err, ErrInvalidKey) {
		t.Fatalf("DecodeKey(short) error = %v", err)
	}
}
