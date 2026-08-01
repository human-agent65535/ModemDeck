package tlsmanager

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCloudflareOriginManagerUsesBundlePresenceAsEnablement(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC)
	root, rootKey := createOriginTestRoot(t, now)
	config := CloudflareOriginConfig{
		Directory: t.TempDir(),
		clock:     func() time.Time { return now },
		roots:     []*x509.Certificate{root},
	}
	manager, err := OpenCloudflareOrigin(config)
	if err != nil {
		t.Fatalf("OpenCloudflareOrigin() error = %v", err)
	}
	if status := manager.Status(); status.Enabled {
		t.Fatalf("initial status = %+v", status)
	}

	certificatePEM, keyPEM := createOriginTestCertificate(
		t,
		root,
		rootKey,
		now,
		now.Add(365*24*time.Hour),
		[]string{"callsapi.example.com", "call.example.com"},
	)
	status, err := manager.Install(
		certificatePEM,
		keyPEM,
		[]string{"callsapi.example.com", "call.example.com"},
	)
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	if !status.Enabled || status.Certificate.Expired ||
		!manager.CoversDNSNames([]string{"call.example.com"}) {
		t.Fatalf("installed status = %+v", status)
	}
	if _, err := manager.Install(
		certificatePEM,
		keyPEM,
		[]string{"callsapi.example.com", "call.example.com"},
	); !errors.Is(err, ErrCloudflareOriginAlreadyEnabled) {
		t.Fatalf("second Install() error = %v, want already enabled", err)
	}
	info, err := os.Stat(filepath.Join(config.Directory, cloudflareOriginFilename))
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if permission := info.Mode().Perm(); permission != 0o640 {
		t.Fatalf("bundle permission = %o, want 640", permission)
	}

	reopened, err := OpenCloudflareOrigin(config)
	if err != nil {
		t.Fatalf("reopen error = %v", err)
	}
	if !reopened.Status().Enabled {
		t.Fatal("reopened manager did not load the installed certificate")
	}
	if err := reopened.Disable(); err != nil {
		t.Fatalf("Disable() error = %v", err)
	}
	if reopened.Status().Enabled {
		t.Fatal("manager remained enabled after removing the certificate")
	}
	if _, err := os.Stat(
		filepath.Join(config.Directory, cloudflareOriginFilename),
	); !os.IsNotExist(err) {
		t.Fatalf("disabled bundle Stat() error = %v, want not exist", err)
	}
	if err := reopened.Disable(); err != nil {
		t.Fatalf("second Disable() error = %v", err)
	}
}

func TestCloudflareOriginManagerRejectsWrongAuthorityAndRouteCoverage(
	t *testing.T,
) {
	t.Parallel()
	now := time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC)
	trustedRoot, _ := createOriginTestRoot(t, now)
	untrustedRoot, untrustedKey := createOriginTestRoot(t, now)
	manager, err := OpenCloudflareOrigin(CloudflareOriginConfig{
		Directory: t.TempDir(),
		clock:     func() time.Time { return now },
		roots:     []*x509.Certificate{trustedRoot},
	})
	if err != nil {
		t.Fatal(err)
	}
	certificatePEM, keyPEM := createOriginTestCertificate(
		t,
		untrustedRoot,
		untrustedKey,
		now,
		now.Add(time.Hour),
		[]string{"call.example.com"},
	)
	if _, err := manager.Install(
		certificatePEM,
		keyPEM,
		[]string{"call.example.com"},
	); !errorsIsInvalidOrigin(err) {
		t.Fatalf("untrusted Install() error = %v", err)
	}

	trustedRoot, trustedKey := createOriginTestRoot(t, now)
	manager, err = OpenCloudflareOrigin(CloudflareOriginConfig{
		Directory: t.TempDir(),
		clock:     func() time.Time { return now },
		roots:     []*x509.Certificate{trustedRoot},
	})
	if err != nil {
		t.Fatal(err)
	}
	certificatePEM, keyPEM = createOriginTestCertificate(
		t,
		trustedRoot,
		trustedKey,
		now,
		now.Add(time.Hour),
		[]string{"call.example.com"},
	)
	if _, err := manager.Install(
		certificatePEM,
		keyPEM,
		[]string{"call.example.com", "callsapi.example.com"},
	); !errorsIsInvalidOrigin(err) {
		t.Fatalf("route mismatch Install() error = %v", err)
	}
	if manager.Status().Enabled {
		t.Fatal("invalid upload enabled origin TLS")
	}
}

func TestCloudflareOriginManagerKeepsExpiredInstalledMode(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC)
	current := now
	root, rootKey := createOriginTestRoot(t, now)
	config := CloudflareOriginConfig{
		Directory: t.TempDir(),
		clock:     func() time.Time { return current },
		roots:     []*x509.Certificate{root},
	}
	manager, err := OpenCloudflareOrigin(config)
	if err != nil {
		t.Fatal(err)
	}
	certificatePEM, keyPEM := createOriginTestCertificate(
		t,
		root,
		rootKey,
		now,
		now.Add(time.Hour),
		[]string{"call.example.com"},
	)
	if _, err := manager.Install(
		certificatePEM,
		keyPEM,
		[]string{"call.example.com"},
	); err != nil {
		t.Fatal(err)
	}
	current = now.Add(2 * time.Hour)
	reopened, err := OpenCloudflareOrigin(config)
	if err != nil {
		t.Fatalf("reopen expired certificate error = %v", err)
	}
	status := reopened.Status()
	if !status.Enabled || !status.Certificate.Expired {
		t.Fatalf("expired status = %+v", status)
	}
}

func errorsIsInvalidOrigin(err error) bool {
	return err != nil && errors.Is(err, ErrInvalidCloudflareOriginMaterial)
}

func createOriginTestRoot(
	t *testing.T,
	now time.Time,
) (*x509.Certificate, *ecdsa.PrivateKey) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Cloudflare Origin Test CA"},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(10 * 365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(
		rand.Reader,
		template,
		template,
		&key.PublicKey,
		key,
	)
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return certificate, key
}

func createOriginTestCertificate(
	t *testing.T,
	root *x509.Certificate,
	rootKey *ecdsa.PrivateKey,
	notBefore time.Time,
	notAfter time.Time,
	dnsNames []string,
) ([]byte, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: dnsNames[0]},
		NotBefore:    notBefore.Add(-time.Minute),
		NotAfter:     notAfter,
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     append([]string(nil), dnsNames...),
	}
	der, err := x509.CreateCertificate(
		rand.Reader,
		template,
		root,
		&key.PublicKey,
		rootKey,
	)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
}
