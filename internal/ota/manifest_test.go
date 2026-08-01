package ota

import "testing"

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
			name: "mutable cloudflared tag",
			mutate: func(manifest *ReleaseManifest) {
				manifest.Cloudflared.Version = "latest"
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
