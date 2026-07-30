package auth

import (
	"bytes"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"io"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
)

const (
	PasswordHashTime      uint32 = 3
	PasswordHashMemoryKiB uint32 = 64 * 1024
	PasswordHashThreads   uint8  = 4
	PasswordHashSaltBytes        = 16
	PasswordHashKeyBytes         = 32

	MaxPasswordHashTime      uint32 = PasswordHashTime
	MaxPasswordHashMemoryKiB uint32 = PasswordHashMemoryKiB
	MaxPasswordHashThreads   uint8  = PasswordHashThreads
	MaxPasswordHashSaltBytes        = 64
	MaxPasswordHashKeyBytes         = 64
)

const (
	argon2Version         = argon2.Version
	minPasswordHashSalt   = 8
	minPasswordHashKey    = 16
	maxPasswordHashPHCLen = 512
)

var rawBase64 = base64.RawStdEncoding.Strict()

// dummyPasswordHash is a valid current-cost hash used to keep login password
// work equivalent when an account is missing, disabled, or named incorrectly.
// The key is deliberately arbitrary: a dummy comparison can never authenticate.
var dummyPasswordHash = encodePasswordHash(
	bytes.Repeat([]byte{0xa5}, PasswordHashSaltBytes),
	bytes.Repeat([]byte{0x5a}, PasswordHashKeyBytes),
)

type passwordHashParameters struct {
	time      uint32
	memoryKiB uint32
	threads   uint8
	salt      []byte
	key       []byte
}

// HashPassword creates an Argon2id password hash in PHC string format.
func HashPassword(password string) (string, error) {
	return hashPassword(password, rand.Reader)
}

func hashPassword(password string, random io.Reader) (string, error) {
	const op = "hash password"

	if password == "" {
		return "", newError(op, CodePasswordRequired, nil)
	}

	salt := make([]byte, PasswordHashSaltBytes)
	if _, err := io.ReadFull(random, salt); err != nil {
		return "", newError(op, CodeRandomSource, err)
	}

	key := argon2.IDKey(
		[]byte(password),
		salt,
		PasswordHashTime,
		PasswordHashMemoryKiB,
		PasswordHashThreads,
		PasswordHashKeyBytes,
	)

	return encodePasswordHash(salt, key), nil
}

func encodePasswordHash(salt, key []byte) string {
	return fmt.Sprintf(
		"$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2Version,
		PasswordHashMemoryKiB,
		PasswordHashTime,
		PasswordHashThreads,
		rawBase64.EncodeToString(salt),
		rawBase64.EncodeToString(key),
	)
}

// VerifyPassword checks a password against an Argon2id PHC string. It rejects
// hashes whose cost parameters exceed the package limits before invoking
// Argon2, preventing persisted input from forcing unbounded work.
func VerifyPassword(password, encodedHash string) (bool, error) {
	const op = "verify password"

	if password == "" {
		return false, newError(op, CodePasswordRequired, nil)
	}

	parameters, err := parsePasswordHash(encodedHash)
	if err != nil {
		return false, err
	}

	actual := argon2.IDKey(
		[]byte(password),
		parameters.salt,
		parameters.time,
		parameters.memoryKiB,
		parameters.threads,
		uint32(len(parameters.key)),
	)

	return subtle.ConstantTimeCompare(actual, parameters.key) == 1, nil
}

func parsePasswordHash(encodedHash string) (passwordHashParameters, error) {
	const op = "parse password hash"

	if encodedHash == "" || len(encodedHash) > maxPasswordHashPHCLen {
		return passwordHashParameters{}, invalidPasswordHash(op, "invalid PHC length")
	}

	fields := strings.Split(encodedHash, "$")
	if len(fields) != 6 || fields[0] != "" || fields[1] != "argon2id" {
		return passwordHashParameters{}, invalidPasswordHash(op, "expected Argon2id PHC format")
	}

	versionText, ok := strings.CutPrefix(fields[2], "v=")
	if !ok {
		return passwordHashParameters{}, invalidPasswordHash(op, "missing Argon2 version")
	}
	version, err := strconv.ParseUint(versionText, 10, 32)
	if err != nil || version != uint64(argon2Version) {
		return passwordHashParameters{}, invalidPasswordHash(op, "unsupported Argon2 version")
	}

	timeCost, memoryKiB, threads, err := parsePasswordHashCosts(fields[3])
	if err != nil {
		return passwordHashParameters{}, err
	}

	salt, err := decodePasswordHashField(op, "salt", fields[4], minPasswordHashSalt, MaxPasswordHashSaltBytes)
	if err != nil {
		return passwordHashParameters{}, err
	}
	key, err := decodePasswordHashField(op, "key", fields[5], minPasswordHashKey, MaxPasswordHashKeyBytes)
	if err != nil {
		return passwordHashParameters{}, err
	}

	return passwordHashParameters{
		time:      timeCost,
		memoryKiB: memoryKiB,
		threads:   threads,
		salt:      salt,
		key:       key,
	}, nil
}

func parsePasswordHashCosts(encoded string) (uint32, uint32, uint8, error) {
	const op = "parse password hash"

	parts := strings.Split(encoded, ",")
	if len(parts) != 3 {
		return 0, 0, 0, invalidPasswordHash(op, "expected m, t, and p parameters")
	}

	values := make(map[string]uint64, len(parts))
	for _, part := range parts {
		name, valueText, ok := strings.Cut(part, "=")
		if !ok || name == "" || valueText == "" {
			return 0, 0, 0, invalidPasswordHash(op, "invalid cost parameter")
		}
		if name != "m" && name != "t" && name != "p" {
			return 0, 0, 0, invalidPasswordHash(op, "unknown cost parameter")
		}
		if _, duplicate := values[name]; duplicate {
			return 0, 0, 0, invalidPasswordHash(op, "duplicate cost parameter")
		}

		value, err := strconv.ParseUint(valueText, 10, 64)
		if err != nil {
			return 0, 0, 0, invalidPasswordHash(op, "invalid cost value")
		}
		values[name] = value
	}

	memory := values["m"]
	timeCost := values["t"]
	parallelism := values["p"]
	if memory == 0 || timeCost == 0 || parallelism == 0 {
		return 0, 0, 0, invalidPasswordHash(op, "cost parameters must be positive")
	}
	if memory > uint64(MaxPasswordHashMemoryKiB) ||
		timeCost > uint64(MaxPasswordHashTime) ||
		parallelism > uint64(MaxPasswordHashThreads) {
		return 0, 0, 0, newError(op, CodePasswordHashTooExpensive, nil)
	}
	if memory < 8*parallelism {
		return 0, 0, 0, invalidPasswordHash(op, "memory is too small for parallelism")
	}

	return uint32(timeCost), uint32(memory), uint8(parallelism), nil
}

func decodePasswordHashField(op, name, encoded string, minimum, maximum int) ([]byte, error) {
	if encoded == "" || len(encoded) > base64.RawStdEncoding.EncodedLen(maximum) {
		return nil, invalidPasswordHash(op, name+" length is invalid")
	}

	decoded, err := rawBase64.DecodeString(encoded)
	if err != nil || len(decoded) < minimum || len(decoded) > maximum {
		return nil, invalidPasswordHash(op, name+" is invalid")
	}
	if rawBase64.EncodeToString(decoded) != encoded {
		return nil, invalidPasswordHash(op, name+" encoding is not canonical")
	}
	return decoded, nil
}
