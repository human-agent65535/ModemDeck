package secretbox

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/crypto/chacha20poly1305"
)

const KeySize = chacha20poly1305.KeySize

var (
	ErrInvalidKey       = errors.New("settings encryption key is invalid")
	ErrDecryptionFailed = errors.New("settings secret cannot be decrypted")
)

type Box struct {
	aead cipherAEAD
	rand io.Reader
}

type cipherAEAD interface {
	NonceSize() int
	Seal([]byte, []byte, []byte, []byte) []byte
	Open([]byte, []byte, []byte, []byte) ([]byte, error)
}

func New(key []byte) (*Box, error) {
	if len(key) != KeySize {
		return nil, fmt.Errorf("%w: expected %d bytes", ErrInvalidKey, KeySize)
	}
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, fmt.Errorf("create settings secret box: %w", err)
	}
	return &Box{aead: aead, rand: rand.Reader}, nil
}

func OpenFile(path string) (*Box, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("%w: key file path is required", ErrInvalidKey)
	}
	encoded, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read settings encryption key: %w", err)
	}
	key, err := DecodeKey(encoded)
	if err != nil {
		return nil, err
	}
	return New(key)
}

func DecodeKey(encoded []byte) ([]byte, error) {
	trimmed := []byte(strings.TrimSpace(string(encoded)))
	if len(encoded) == KeySize && len(trimmed) != KeySize {
		return append([]byte(nil), encoded...), nil
	}
	if len(trimmed) == KeySize {
		return append([]byte(nil), trimmed...), nil
	}
	encodings := []*base64.Encoding{
		base64.StdEncoding,
		base64.RawStdEncoding,
		base64.URLEncoding,
		base64.RawURLEncoding,
	}
	for _, encoding := range encodings {
		decoded, err := encoding.DecodeString(string(trimmed))
		if err == nil && len(decoded) == KeySize {
			return decoded, nil
		}
	}
	return nil, fmt.Errorf("%w: expected 32 raw or base64-encoded bytes", ErrInvalidKey)
}

func (box *Box) Seal(plaintext, associatedData []byte) (nonce, ciphertext []byte, err error) {
	if box == nil || box.aead == nil || box.rand == nil {
		return nil, nil, errors.New("settings secret box is not initialized")
	}
	nonce = make([]byte, box.aead.NonceSize())
	if _, err := io.ReadFull(box.rand, nonce); err != nil {
		return nil, nil, fmt.Errorf("generate settings secret nonce: %w", err)
	}
	ciphertext = box.aead.Seal(nil, nonce, plaintext, associatedData)
	return nonce, ciphertext, nil
}

func (box *Box) Open(nonce, ciphertext, associatedData []byte) ([]byte, error) {
	if box == nil || box.aead == nil {
		return nil, errors.New("settings secret box is not initialized")
	}
	if len(nonce) != box.aead.NonceSize() || len(ciphertext) == 0 {
		return nil, ErrDecryptionFailed
	}
	plaintext, err := box.aead.Open(nil, nonce, ciphertext, associatedData)
	if err != nil {
		return nil, ErrDecryptionFailed
	}
	return plaintext, nil
}
