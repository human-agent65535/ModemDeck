// Package identity creates the application's random identifiers. An identifier
// is an opaque value: a fixed prefix naming its kind, followed by 128 random
// bits. Nothing may parse the part after the prefix.
package identity

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
)

const randomBytes = 16

// New returns prefix followed by 128 random bits as 32 hexadecimal digits.
func New(prefix string) (string, error) {
	return NewFrom(rand.Reader, prefix)
}

// NewFrom is New with the random source supplied by the caller.
func NewFrom(random io.Reader, prefix string) (string, error) {
	var value [randomBytes]byte
	if _, err := io.ReadFull(random, value[:]); err != nil {
		return "", fmt.Errorf("generate %sidentifier: %w", prefix, err)
	}
	return prefix + hex.EncodeToString(value[:]), nil
}

// NewLineID returns a stable line identifier. The migration that creates stable
// lines and the store that creates them afterwards must produce the same form.
func NewLineID() (string, error) {
	return New("line_")
}

// NewUUID returns a random (version 4) UUID in its canonical text form, for
// records whose identifier is exchanged with other systems as a UUID.
func NewUUID() (string, error) {
	var value [randomBytes]byte
	if _, err := io.ReadFull(rand.Reader, value[:]); err != nil {
		return "", fmt.Errorf("generate UUID: %w", err)
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	return fmt.Sprintf(
		"%x-%x-%x-%x-%x",
		value[0:4], value[4:6], value[6:8], value[8:10], value[10:16],
	), nil
}
