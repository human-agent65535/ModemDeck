package mobilepairing

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"strings"
)

const (
	TokenPrefix = "md_ios_"
	TokenBytes  = 32
	PayloadType = "modemdeck.ios.pairing"
)

var (
	rawURLBase64    = base64.RawURLEncoding.Strict()
	ErrInvalidToken = errors.New("invalid iOS pairing token")
)

type Token string
type TokenDigest [sha256.Size]byte

func NewToken() (Token, TokenDigest, error) {
	return newToken(rand.Reader)
}

func newToken(random io.Reader) (Token, TokenDigest, error) {
	value := make([]byte, TokenBytes)
	if _, err := io.ReadFull(random, value); err != nil {
		return "", TokenDigest{}, err
	}
	token := Token(TokenPrefix + rawURLBase64.EncodeToString(value))
	return token, TokenDigest(sha256.Sum256([]byte(token))), nil
}

func Digest(token Token) (TokenDigest, error) {
	value := strings.TrimSpace(string(token))
	if !strings.HasPrefix(value, TokenPrefix) {
		return TokenDigest{}, ErrInvalidToken
	}
	encoded := strings.TrimPrefix(value, TokenPrefix)
	decoded, err := rawURLBase64.DecodeString(encoded)
	if err != nil ||
		len(decoded) != TokenBytes ||
		rawURLBase64.EncodeToString(decoded) != encoded {
		return TokenDigest{}, ErrInvalidToken
	}
	return TokenDigest(sha256.Sum256([]byte(value))), nil
}

type Payload struct {
	Version   int    `json:"version"`
	Type      string `json:"type"`
	ServerURL string `json:"server_url"`
	Token     Token  `json:"token"`
}

func NewPayload(serverURL string, token Token) Payload {
	return Payload{
		Version:   1,
		Type:      PayloadType,
		ServerURL: strings.TrimSpace(serverURL),
		Token:     token,
	}
}
