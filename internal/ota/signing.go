package ota

import (
	"crypto/ed25519"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"
)

// trustedReleaseSigningKeys are the Ed25519 public keys whose signatures the
// updater accepts on a release manifest. The matching private key lives only
// in the publish workflow's MODEMDECK_RELEASE_SIGNING_KEY secret. To rotate,
// add the new key here (and to deploy/release-signing-key.pub), ship a release
// signed with the old key, then switch the secret and drop the old key.
var trustedReleaseSigningKeys = []string{
	`-----BEGIN PUBLIC KEY-----
MCowBQYDK2VwAyEA1S7jfBMYc1vIbH7v7OtfjWro8ymlo2Mz+0P8VQp9FjI=
-----END PUBLIC KEY-----`,
}

const maximumSignatureBytes = 1 << 10

var errManifestSignature = errors.New("release manifest signature is invalid")

func parseReleaseSigningKeys(encoded []string) ([]ed25519.PublicKey, error) {
	keys := make([]ed25519.PublicKey, 0, len(encoded))
	for _, value := range encoded {
		block, rest := pem.Decode([]byte(strings.TrimSpace(value)))
		if block == nil || block.Type != "PUBLIC KEY" || len(strings.TrimSpace(string(rest))) != 0 {
			return nil, fmt.Errorf("release signing key is not a PEM public key")
		}
		parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("parse release signing key: %w", err)
		}
		key, ok := parsed.(ed25519.PublicKey)
		if !ok {
			return nil, fmt.Errorf("release signing key is not Ed25519")
		}
		keys = append(keys, key)
	}
	if len(keys) == 0 {
		return nil, fmt.Errorf("no release signing key is configured")
	}
	return keys, nil
}

// verifyManifestSignature checks a base64 Ed25519 signature over the exact
// manifest bytes. The bytes are verified before they are parsed.
func verifyManifestSignature(
	keys []ed25519.PublicKey,
	manifest []byte,
	encodedSignature []byte,
) error {
	signature, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(encodedSignature)))
	if err != nil || len(signature) != ed25519.SignatureSize {
		return errManifestSignature
	}
	for _, key := range keys {
		if ed25519.Verify(key, manifest, signature) {
			return nil
		}
	}
	return errManifestSignature
}
