package auth

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
)

func TestHashPasswordAndVerify(t *testing.T) {
	hash, err := hashPassword(
		"correct horse battery staple",
		bytes.NewReader(bytes.Repeat([]byte{0x5a}, PasswordHashSaltBytes)),
	)
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}

	parameters, err := parsePasswordHash(hash)
	if err != nil {
		t.Fatalf("parsePasswordHash() error = %v", err)
	}
	if parameters.time != PasswordHashTime {
		t.Errorf("time = %d, want %d", parameters.time, PasswordHashTime)
	}
	if parameters.memoryKiB != PasswordHashMemoryKiB {
		t.Errorf("memory = %d, want %d", parameters.memoryKiB, PasswordHashMemoryKiB)
	}
	if parameters.threads != PasswordHashThreads {
		t.Errorf("threads = %d, want %d", parameters.threads, PasswordHashThreads)
	}
	if len(parameters.salt) != PasswordHashSaltBytes {
		t.Errorf("salt length = %d, want %d", len(parameters.salt), PasswordHashSaltBytes)
	}
	if len(parameters.key) != PasswordHashKeyBytes {
		t.Errorf("key length = %d, want %d", len(parameters.key), PasswordHashKeyBytes)
	}

	matches, err := VerifyPassword("correct horse battery staple", hash)
	if err != nil {
		t.Fatalf("VerifyPassword(correct) error = %v", err)
	}
	if !matches {
		t.Fatal("VerifyPassword(correct) = false, want true")
	}

	matches, err = VerifyPassword("wrong password", hash)
	if err != nil {
		t.Fatalf("VerifyPassword(wrong) error = %v", err)
	}
	if matches {
		t.Fatal("VerifyPassword(wrong) = true, want false")
	}
}

func TestHashPasswordRejectsEmptyPasswordAndRandomFailure(t *testing.T) {
	if _, err := HashPassword(""); !errors.Is(err, ErrPasswordRequired) {
		t.Fatalf("HashPassword(empty) error = %v, want ErrPasswordRequired", err)
	}

	randomFailure := errors.New("random unavailable")
	if _, err := hashPassword("password", failingReader{err: randomFailure}); !errors.Is(err, ErrRandomSource) {
		t.Fatalf("hashPassword(random failure) error = %v, want ErrRandomSource", err)
	} else if !errors.Is(err, randomFailure) {
		t.Fatalf("hashPassword(random failure) does not retain cause: %v", err)
	}
}

func TestVerifyPasswordRejectsMalformedHashes(t *testing.T) {
	salt := base64.RawStdEncoding.EncodeToString(bytes.Repeat([]byte{0x11}, PasswordHashSaltBytes))
	key := base64.RawStdEncoding.EncodeToString(bytes.Repeat([]byte{0x22}, PasswordHashKeyBytes))
	validCosts := fmt.Sprintf("m=%d,t=%d,p=%d", PasswordHashMemoryKiB, PasswordHashTime, PasswordHashThreads)

	tests := map[string]string{
		"empty":                  "",
		"wrong algorithm":        fmt.Sprintf("$argon2i$v=%d$%s$%s$%s", argon2Version, validCosts, salt, key),
		"wrong version":          fmt.Sprintf("$argon2id$v=18$%s$%s$%s", validCosts, salt, key),
		"missing version prefix": fmt.Sprintf("$argon2id$19$%s$%s$%s", validCosts, salt, key),
		"missing parameter":      fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d$%s$%s", argon2Version, PasswordHashMemoryKiB, PasswordHashTime, salt, key),
		"duplicate parameter":    fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,t=%d$%s$%s", argon2Version, PasswordHashMemoryKiB, PasswordHashTime, PasswordHashTime, salt, key),
		"unknown parameter":      fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,x=%d$%s$%s", argon2Version, PasswordHashMemoryKiB, PasswordHashTime, PasswordHashThreads, salt, key),
		"zero parameter":         fmt.Sprintf("$argon2id$v=%d$m=%d,t=0,p=%d$%s$%s", argon2Version, PasswordHashMemoryKiB, PasswordHashThreads, salt, key),
		"memory too small":       fmt.Sprintf("$argon2id$v=%d$m=8,t=1,p=2$%s$%s", argon2Version, salt, key),
		"bad salt base64":        fmt.Sprintf("$argon2id$v=%d$%s$*$%s", argon2Version, validCosts, key),
		"short salt":             fmt.Sprintf("$argon2id$v=%d$%s$%s$%s", argon2Version, validCosts, base64.RawStdEncoding.EncodeToString([]byte("short")), key),
		"short key":              fmt.Sprintf("$argon2id$v=%d$%s$%s$%s", argon2Version, validCosts, salt, base64.RawStdEncoding.EncodeToString([]byte("short"))),
		"padded salt":            fmt.Sprintf("$argon2id$v=%d$%s$%s=$%s", argon2Version, validCosts, salt, key),
		"extra field":            fmt.Sprintf("$argon2id$v=%d$%s$%s$%s$extra", argon2Version, validCosts, salt, key),
		"oversized PHC":          strings.Repeat("x", maxPasswordHashPHCLen+1),
	}

	for name, encodedHash := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := VerifyPassword("password", encodedHash); !errors.Is(err, ErrInvalidPasswordHash) {
				t.Fatalf("VerifyPassword() error = %v, want ErrInvalidPasswordHash", err)
			}
		})
	}
}

func TestVerifyPasswordRejectsExcessiveCostBeforeArgon2(t *testing.T) {
	salt := base64.RawStdEncoding.EncodeToString(bytes.Repeat([]byte{0x11}, PasswordHashSaltBytes))
	key := base64.RawStdEncoding.EncodeToString(bytes.Repeat([]byte{0x22}, PasswordHashKeyBytes))

	tests := map[string]string{
		"time": fmt.Sprintf(
			"$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
			argon2Version,
			PasswordHashMemoryKiB,
			uint64(MaxPasswordHashTime)+1,
			PasswordHashThreads,
			salt,
			key,
		),
		"memory": fmt.Sprintf(
			"$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
			argon2Version,
			uint64(MaxPasswordHashMemoryKiB)+1,
			PasswordHashTime,
			PasswordHashThreads,
			salt,
			key,
		),
		"threads": fmt.Sprintf(
			"$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
			argon2Version,
			PasswordHashMemoryKiB,
			PasswordHashTime,
			uint64(MaxPasswordHashThreads)+1,
			salt,
			key,
		),
	}

	for name, encodedHash := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := VerifyPassword("password", encodedHash); !errors.Is(err, ErrPasswordHashTooExpensive) {
				t.Fatalf("VerifyPassword() error = %v, want ErrPasswordHashTooExpensive", err)
			}
		})
	}
}

func TestVerifyPasswordRejectsEmptyPassword(t *testing.T) {
	if _, err := VerifyPassword("", "not inspected"); !errors.Is(err, ErrPasswordRequired) {
		t.Fatalf("VerifyPassword(empty) error = %v, want ErrPasswordRequired", err)
	}
}

func FuzzVerifyPasswordDoesNotPanic(f *testing.F) {
	f.Add("")
	f.Add("$argon2id$v=19$m=65536,t=3,p=4$c2FsdHNhbHRzYWx0c2FsdA$a2V5a2V5a2V5a2V5a2V5a2V5a2V5a2V5a2V5a2V5a2U")
	f.Add("$argon2id$v=19$m=4294967295,t=4294967295,p=255$AA$AA")

	f.Fuzz(func(t *testing.T, encodedHash string) {
		_, _ = VerifyPassword("password", encodedHash)
	})
}

type failingReader struct {
	err error
}

func (r failingReader) Read(_ []byte) (int, error) {
	return 0, r.err
}

var _ io.Reader = failingReader{}
