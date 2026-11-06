package ota

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
)

func TestReleaseManifestRejectsMutableOrUnexpectedImages(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		mutate func(*ReleaseManifest)
	}{
		{
			name: "unexpected updater repository",
			mutate: func(manifest *ReleaseManifest) {
				manifest.Images.Updater = "ghcr.io/example/modemdeck-updater"
			},
		},
		{
			name: "unsupported schema",
			mutate: func(manifest *ReleaseManifest) {
				manifest.SchemaVersion = 1
			},
		},
		{
			name: "mutable cloudflared tag",
			mutate: func(manifest *ReleaseManifest) {
				manifest.Cloudflared.Version = "latest"
			},
		},
		{
			name: "missing image digest",
			mutate: func(manifest *ReleaseManifest) {
				manifest.Digests.API = ""
			},
		},
		{
			name: "mutable release version",
			mutate: func(manifest *ReleaseManifest) {
				manifest.Release = "latest"
			},
		},
		{
			name: "invalid cloudflared digest",
			mutate: func(manifest *ReleaseManifest) {
				manifest.Cloudflared.Digest = "sha256:invalid"
			},
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			manifest := testManifest(testDigest("d"))
			test.mutate(&manifest)
			if err := manifest.Validate(); err == nil {
				t.Fatal("Validate() accepted an unsafe release manifest")
			}
		})
	}
}

func TestTrustedReleaseSigningKeyMatchesPublishedKey(t *testing.T) {
	t.Parallel()
	published, err := os.ReadFile("../../deploy/release-signing-key.pub")
	if err != nil {
		t.Fatalf("read published key: %v", err)
	}
	if strings.TrimSpace(string(published)) != strings.TrimSpace(trustedReleaseSigningKeys[0]) {
		t.Fatal("deploy/release-signing-key.pub differs from the updater's trusted key")
	}
	if _, err := parseReleaseSigningKeys(trustedReleaseSigningKeys); err != nil {
		t.Fatalf("parseReleaseSigningKeys() error = %v", err)
	}
}

type staticManifestClient map[string][]byte

func (client staticManifestClient) Do(request *http.Request) (*http.Response, error) {
	body, ok := client[request.URL.String()]
	if !ok {
		return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(bytes.NewReader(nil))}, nil
	}
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(body))}, nil
}

func TestHTTPManifestLoaderRequiresTrustedSignature(t *testing.T) {
	t.Parallel()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	_, otherPrivate, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	const template = "https://releases.example.test/%s/release-manifest.json"
	const manifestURL = "https://releases.example.test/v2.0.0/release-manifest.json"
	encode := func(manifest ReleaseManifest) []byte {
		body, err := json.Marshal(manifest)
		if err != nil {
			t.Fatal(err)
		}
		return body
	}
	sign := func(key ed25519.PrivateKey, body []byte) []byte {
		return []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(key, body)) + "\n")
	}
	valid := encode(testManifest(testDigest("d")))
	other := testManifest(testDigest("d"))
	other.Release = "v1.0.0"
	replayed := encode(other)

	tests := []struct {
		name      string
		body      []byte
		signature []byte
		wantErr   bool
	}{
		{name: "valid", body: valid, signature: sign(private, valid)},
		{name: "untrusted key", body: valid, signature: sign(otherPrivate, valid), wantErr: true},
		{name: "tampered body", body: bytes.Replace(valid, []byte(testDigest("a")), []byte(testDigest("f")), 1), signature: sign(private, valid), wantErr: true},
		{name: "missing signature", body: valid, wantErr: true},
		{name: "replayed release", body: replayed, signature: sign(private, replayed), wantErr: true},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			client := staticManifestClient{manifestURL: test.body}
			if test.signature != nil {
				client[manifestURL+".sig"] = test.signature
			}
			loader := newHTTPManifestLoader(template, client, []ed25519.PublicKey{public})
			manifest, err := loader.Load(context.Background(), "v2.0.0")
			if test.wantErr {
				if !errors.Is(err, ErrManifestUnavailable) {
					t.Fatalf("Load() error = %v, want ErrManifestUnavailable", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if manifest.Digests.API != testDigest("a") {
				t.Fatalf("Load() digests = %+v", manifest.Digests)
			}
		})
	}
}
