package tlsmanager

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestAutomaticCertificateInitializesAndPersists(t *testing.T) {
	t.Parallel()
	clock := newTestClock()
	config := testConfig(t.TempDir(), clock)

	manager, err := Open(config)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	status := manager.Status()
	if status.Source != SourceAutomatic ||
		status.Subject == "" ||
		status.Issuer == "" ||
		status.FingerprintSHA256 == "" ||
		status.Expired ||
		status.RenewalDue ||
		!status.RenewsAutomatically {
		t.Fatalf("automatic status = %+v", status)
	}
	for _, host := range defaultHosts {
		if !contains(status.SANs, host) {
			t.Fatalf("automatic SANs = %v, missing %q", status.SANs, host)
		}
	}
	certificate, err := manager.TLSConfig().GetCertificate(nil)
	if err != nil || certificate.Leaf == nil {
		t.Fatalf("GetCertificate() = %+v, %v", certificate, err)
	}

	caPEM, err := manager.AutomaticCAPEM()
	if err != nil {
		t.Fatalf("AutomaticCAPEM() error = %v", err)
	}
	if bytes.Contains(caPEM, []byte("PRIVATE KEY")) {
		t.Fatal("AutomaticCAPEM() exposed a private key")
	}
	if block, rest := pem.Decode(caPEM); block == nil ||
		block.Type != "CERTIFICATE" ||
		len(bytes.TrimSpace(rest)) != 0 {
		t.Fatalf("AutomaticCAPEM() = %q", caPEM)
	}

	reopened, err := Open(config)
	if err != nil {
		t.Fatalf("second Open() error = %v", err)
	}
	if reopened.Status().FingerprintSHA256 != status.FingerprintSHA256 {
		t.Fatalf(
			"automatic certificate changed before renewal: %s -> %s",
			status.FingerprintSHA256,
			reopened.Status().FingerprintSHA256,
		)
	}
	reopenedCA, err := reopened.AutomaticCAPEM()
	if err != nil || !bytes.Equal(reopenedCA, caPEM) {
		t.Fatalf("automatic CA was not reused: equal=%t error=%v", bytes.Equal(reopenedCA, caPEM), err)
	}
}

func TestAutomaticCertificateRenewsAndReusesCA(t *testing.T) {
	t.Parallel()
	clock := newTestClock()
	config := testConfig(t.TempDir(), clock)
	manager, err := Open(config)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	firstStatus := manager.Status()
	firstCA, err := manager.AutomaticCAPEM()
	if err != nil {
		t.Fatalf("AutomaticCAPEM() error = %v", err)
	}

	clock.Advance(19 * time.Hour)
	reopened, err := Open(config)
	if err != nil {
		t.Fatalf("Open() in renewal window error = %v", err)
	}
	secondStatus := reopened.Status()
	secondCA, err := reopened.AutomaticCAPEM()
	if err != nil {
		t.Fatalf("second AutomaticCAPEM() error = %v", err)
	}
	if firstStatus.FingerprintSHA256 == secondStatus.FingerprintSHA256 {
		t.Fatal("automatic server certificate did not renew")
	}
	if !bytes.Equal(firstCA, secondCA) {
		t.Fatal("automatic CA changed during server certificate renewal")
	}
}

func TestAutomaticCertificateRenewsOnHandshakeWithoutReopen(t *testing.T) {
	t.Parallel()
	clock := newTestClock()
	config := testConfig(t.TempDir(), clock)
	manager, err := Open(config)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	tlsConfig := manager.TLSConfig()
	first, err := tlsConfig.GetCertificate(nil)
	if err != nil {
		t.Fatalf("first GetCertificate() error = %v", err)
	}
	firstFingerprint := manager.Status().FingerprintSHA256
	firstCA, _ := manager.AutomaticCAPEM()

	clock.Advance(19 * time.Hour)
	const handshakes = 16
	certificates := make(chan *tls.Certificate, handshakes)
	errorsSeen := make(chan error, handshakes)
	var wait sync.WaitGroup
	for range handshakes {
		wait.Add(1)
		go func() {
			defer wait.Done()
			certificate, callErr := tlsConfig.GetCertificate(nil)
			certificates <- certificate
			errorsSeen <- callErr
		}()
	}
	wait.Wait()
	close(certificates)
	close(errorsSeen)
	for callErr := range errorsSeen {
		if callErr != nil {
			t.Fatalf("concurrent GetCertificate() error = %v", callErr)
		}
	}
	var renewedRaw []byte
	for certificate := range certificates {
		if certificate == nil || certificate.Leaf == nil {
			t.Fatal("GetCertificate() returned an empty certificate")
		}
		if renewedRaw == nil {
			renewedRaw = certificate.Leaf.Raw
			continue
		}
		if !bytes.Equal(renewedRaw, certificate.Leaf.Raw) {
			t.Fatal("concurrent handshakes observed multiple renewed leaves")
		}
	}
	if bytes.Equal(first.Leaf.Raw, renewedRaw) ||
		manager.Status().FingerprintSHA256 == firstFingerprint {
		t.Fatal("handshake did not rotate automatic certificate in renewal window")
	}
	secondCA, _ := manager.AutomaticCAPEM()
	if !bytes.Equal(firstCA, secondCA) {
		t.Fatal("handshake renewal replaced a still-valid automatic CA")
	}
}

func TestAutomaticCertificateRenewsForSANChange(t *testing.T) {
	t.Parallel()
	clock := newTestClock()
	directory := t.TempDir()
	firstConfig := testConfig(directory, clock)
	first, err := Open(firstConfig)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	firstStatus := first.Status()
	firstCA, _ := first.AutomaticCAPEM()

	secondConfig := firstConfig
	secondConfig.Hosts = []string{"gateway.example.test", "198.51.100.18"}
	second, err := Open(secondConfig)
	if err != nil {
		t.Fatalf("Open() after SAN change error = %v", err)
	}
	secondStatus := second.Status()
	secondCA, _ := second.AutomaticCAPEM()
	if firstStatus.FingerprintSHA256 == secondStatus.FingerprintSHA256 {
		t.Fatal("automatic server certificate did not change with SANs")
	}
	for _, host := range append(defaultHosts, secondConfig.Hosts...) {
		if !contains(secondStatus.SANs, host) {
			t.Fatalf("renewed SANs = %v, missing %q", secondStatus.SANs, host)
		}
	}
	if !bytes.Equal(firstCA, secondCA) {
		t.Fatal("automatic CA changed with SAN set")
	}
}

func TestAutomaticCARebuildsOnlyAfterExpiry(t *testing.T) {
	t.Parallel()
	clock := newTestClock()
	config := testConfig(t.TempDir(), clock)
	config.caValidity = 48 * time.Hour
	manager, err := Open(config)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	firstCA, _ := manager.AutomaticCAPEM()

	clock.Advance(47 * time.Hour)
	beforeExpiry, err := Open(config)
	if err != nil {
		t.Fatalf("Open() before CA expiry error = %v", err)
	}
	stillSameCA, _ := beforeExpiry.AutomaticCAPEM()
	if !bytes.Equal(firstCA, stillSameCA) {
		t.Fatal("automatic CA changed before expiry")
	}

	clock.Advance(2 * time.Hour)
	afterExpiry, err := Open(config)
	if err != nil {
		t.Fatalf("Open() after CA expiry error = %v", err)
	}
	replacementCA, _ := afterExpiry.AutomaticCAPEM()
	if bytes.Equal(firstCA, replacementCA) {
		t.Fatal("automatic CA was not rebuilt after expiry")
	}
}

func TestUserCertificateHotSwitchAndAutomaticRestore(t *testing.T) {
	t.Parallel()
	clock := newTestClock()
	config := testConfig(t.TempDir(), clock)
	manager, err := Open(config)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	tlsConfig := manager.TLSConfig()
	automatic, err := tlsConfig.GetCertificate(nil)
	if err != nil {
		t.Fatalf("automatic GetCertificate() error = %v", err)
	}
	certificatePEM, keyPEM := createUserCertificate(
		t,
		clock.Now(),
		clock.Now().Add(12*time.Hour),
		"user.example.test",
	)
	userStatus, err := manager.InstallUser(certificatePEM, keyPEM)
	if err != nil {
		t.Fatalf("InstallUser() error = %v", err)
	}
	userCertificate, err := tlsConfig.GetCertificate(nil)
	if err != nil {
		t.Fatalf("user GetCertificate() error = %v", err)
	}
	if userStatus.Source != SourceUser ||
		userStatus.RenewsAutomatically ||
		userStatus.Expired ||
		userCertificate.Leaf == nil ||
		userCertificate.Leaf == automatic.Leaf {
		t.Fatalf("user switch status=%+v certificate=%+v", userStatus, userCertificate)
	}

	automaticStatus, err := manager.UseAutomatic()
	if err != nil {
		t.Fatalf("UseAutomatic() error = %v", err)
	}
	restored, err := tlsConfig.GetCertificate(nil)
	if err != nil {
		t.Fatalf("restored GetCertificate() error = %v", err)
	}
	if automaticStatus.Source != SourceAutomatic ||
		restored.Leaf == nil ||
		restored.Leaf.RawSubjectPublicKeyInfo == nil ||
		bytes.Equal(
			restored.Leaf.RawSubjectPublicKeyInfo,
			userCertificate.Leaf.RawSubjectPublicKeyInfo,
		) {
		t.Fatalf("automatic restore status=%+v certificate=%+v", automaticStatus, restored)
	}
}

func TestExpiredUserCertificateRemainsSelected(t *testing.T) {
	t.Parallel()
	clock := newTestClock()
	config := testConfig(t.TempDir(), clock)
	manager, err := Open(config)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	certificatePEM, keyPEM := createUserCertificate(
		t,
		clock.Now(),
		clock.Now().Add(2*time.Hour),
		"expired.example.test",
	)
	installed, err := manager.InstallUser(certificatePEM, keyPEM)
	if err != nil {
		t.Fatalf("InstallUser() error = %v", err)
	}
	automaticCA, _ := manager.AutomaticCAPEM()

	clock.Advance(3 * time.Hour)
	reopened, err := Open(config)
	if err != nil {
		t.Fatalf("Open() with expired user certificate error = %v", err)
	}
	status := reopened.Status()
	if status.Source != SourceUser ||
		!status.Expired ||
		status.RenewsAutomatically ||
		status.FingerprintSHA256 != installed.FingerprintSHA256 {
		t.Fatalf("expired user status = %+v; installed = %+v", status, installed)
	}
	served, err := reopened.TLSConfig().GetCertificate(nil)
	if err != nil {
		t.Fatalf("expired user GetCertificate() error = %v", err)
	}
	if served.Leaf == nil ||
		formatFingerprint(sha256Sum(served.Leaf.Raw)) != installed.FingerprintSHA256 {
		t.Fatal("expired user certificate was replaced during handshake")
	}
	reopenedCA, _ := reopened.AutomaticCAPEM()
	if !bytes.Equal(reopenedCA, automaticCA) {
		t.Fatal("reading expired user mode unexpectedly replaced automatic CA")
	}
}

func TestMismatchedPersistedUserBundleFailsOpen(t *testing.T) {
	t.Parallel()
	clock := newTestClock()
	config := testConfig(t.TempDir(), clock)
	manager, err := Open(config)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	certificatePEM, keyPEM := createUserCertificate(
		t,
		clock.Now(),
		clock.Now().Add(time.Hour),
		"user.example.test",
	)
	if _, err := manager.InstallUser(certificatePEM, keyPEM); err != nil {
		t.Fatalf("InstallUser() error = %v", err)
	}
	_, otherKey := createUserCertificate(
		t,
		clock.Now(),
		clock.Now().Add(time.Hour),
		"other.example.test",
	)
	mismatched := append(append([]byte(nil), certificatePEM...), otherKey...)
	if err := os.WriteFile(
		filepath.Join(config.Directory, userBundleFilename),
		mismatched,
		0o600,
	); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if _, err := Open(config); err == nil {
		t.Fatal("Open() succeeded with mismatched selected user bundle")
	}
}

func TestInstallUserRejectsInvalidAndWeakMaterial(t *testing.T) {
	t.Parallel()
	clock := newTestClock()
	manager, err := Open(testConfig(t.TempDir(), clock))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	certificatePEM, _ := createUserCertificate(
		t,
		clock.Now(),
		clock.Now().Add(time.Hour),
		"valid.example.test",
	)
	_, otherKey := createUserCertificate(
		t,
		clock.Now(),
		clock.Now().Add(time.Hour),
		"other.example.test",
	)
	if _, err := manager.InstallUser(certificatePEM, otherKey); !errors.Is(err, ErrInvalidUserMaterial) {
		t.Fatalf("mismatched InstallUser() error = %v", err)
	}

	weakCertificate, weakKey := createWeakUserCertificate(t, clock.Now())
	if _, err := manager.InstallUser(weakCertificate, weakKey); !errors.Is(err, ErrInvalidUserMaterial) {
		t.Fatalf("weak InstallUser() error = %v", err)
	}
}

func TestCorruptUserBundleFailsOpenWithoutAutomaticFallback(t *testing.T) {
	t.Parallel()
	clock := newTestClock()
	config := testConfig(t.TempDir(), clock)
	manager, err := Open(config)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	certificatePEM, keyPEM := createUserCertificate(
		t,
		clock.Now(),
		clock.Now().Add(time.Hour),
		"user.example.test",
	)
	if _, err := manager.InstallUser(certificatePEM, keyPEM); err != nil {
		t.Fatalf("InstallUser() error = %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(config.Directory, userBundleFilename),
		[]byte("not a PEM bundle"),
		0o600,
	); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if _, err := Open(config); err == nil {
		t.Fatal("Open() succeeded with corrupt selected user bundle")
	}
	source, found, err := readSource(config.Directory)
	if err != nil || !found || source != SourceUser {
		t.Fatalf("selected source = %q, %t, %v; want user", source, found, err)
	}
}

func TestStoredKeyAndMetadataPermissions(t *testing.T) {
	t.Parallel()
	clock := newTestClock()
	config := testConfig(t.TempDir(), clock)
	manager, err := Open(config)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	certificatePEM, keyPEM := createUserCertificate(
		t,
		clock.Now(),
		clock.Now().Add(time.Hour),
		"user.example.test",
	)
	if _, err := manager.InstallUser(certificatePEM, keyPEM); err != nil {
		t.Fatalf("InstallUser() error = %v", err)
	}
	for _, name := range []string{
		sourceFilename,
		automaticCAFilename,
		automaticServerFilename,
		userBundleFilename,
	} {
		info, err := os.Stat(filepath.Join(config.Directory, name))
		if err != nil {
			t.Fatalf("Stat(%s) error = %v", name, err)
		}
		if permission := info.Mode().Perm(); permission != 0o600 {
			t.Fatalf("%s permission = %o, want 600", name, permission)
		}
	}
}

func TestConcurrentTLSReadsDuringHotSwitch(t *testing.T) {
	t.Parallel()
	clock := newTestClock()
	config := testConfig(t.TempDir(), clock)
	manager, err := Open(config)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	certificatePEM, keyPEM := createUserCertificate(
		t,
		clock.Now(),
		clock.Now().Add(12*time.Hour),
		"user.example.test",
	)
	tlsConfig := manager.TLSConfig()

	const readers = 12
	const iterations = 80
	start := make(chan struct{})
	errorsSeen := make(chan error, readers)
	var wait sync.WaitGroup
	for range readers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			for range iterations {
				status := manager.Status()
				if status.Source != SourceAutomatic && status.Source != SourceUser {
					errorsSeen <- errors.New("unexpected status source")
					return
				}
				if _, err := tlsConfig.GetCertificate(nil); err != nil {
					errorsSeen <- err
					return
				}
			}
			errorsSeen <- nil
		}()
	}
	close(start)
	for range 8 {
		if _, err := manager.InstallUser(certificatePEM, keyPEM); err != nil {
			t.Fatalf("InstallUser() error = %v", err)
		}
		if _, err := manager.UseAutomatic(); err != nil {
			t.Fatalf("UseAutomatic() error = %v", err)
		}
	}
	wait.Wait()
	close(errorsSeen)
	for err := range errorsSeen {
		if err != nil {
			t.Fatalf("concurrent reader error = %v", err)
		}
	}
}

func TestConfigRejectsInvalidHosts(t *testing.T) {
	t.Parallel()
	for _, host := range []string{
		"https://example.test",
		"example.test:443",
		"*.example.test",
		"bad_name.example",
		" leading.example",
		"[::1]",
	} {
		host := host
		t.Run(host, func(t *testing.T) {
			t.Parallel()
			config := testConfig(t.TempDir(), newTestClock())
			config.Hosts = []string{host}
			if _, err := Open(config); !errors.Is(err, ErrInvalidConfiguration) {
				t.Fatalf("Open() error = %v, want ErrInvalidConfiguration", err)
			}
		})
	}
}

type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func newTestClock() *testClock {
	return &testClock{
		now: time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC),
	}
}

func (clock *testClock) Now() time.Time {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	return clock.now
}

func (clock *testClock) Advance(duration time.Duration) {
	clock.mu.Lock()
	clock.now = clock.now.Add(duration)
	clock.mu.Unlock()
}

func testConfig(directory string, clock *testClock) Config {
	return Config{
		Directory:           directory,
		caValidity:          365 * 24 * time.Hour,
		certificateValidity: 24 * time.Hour,
		renewalWindow:       6 * time.Hour,
		clock:               clock.Now,
	}
}

func createUserCertificate(
	t *testing.T,
	notBefore time.Time,
	notAfter time.Time,
	host string,
) ([]byte, []byte) {
	t.Helper()
	rootKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey(root) error = %v", err)
	}
	rootTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "Test Root"},
		NotBefore:             notBefore.Add(-time.Hour),
		NotAfter:              notAfter.Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	rootDER, err := x509.CreateCertificate(
		rand.Reader,
		rootTemplate,
		rootTemplate,
		&rootKey.PublicKey,
		rootKey,
	)
	if err != nil {
		t.Fatalf("CreateCertificate(root) error = %v", err)
	}
	root, err := x509.ParseCertificate(rootDER)
	if err != nil {
		t.Fatalf("ParseCertificate(root) error = %v", err)
	}
	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey(leaf) error = %v", err)
	}
	leafTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: host},
		NotBefore:    notBefore.Add(-time.Minute),
		NotAfter:     notAfter,
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{host},
	}
	leafDER, err := x509.CreateCertificate(
		rand.Reader,
		leafTemplate,
		root,
		&leafKey.PublicKey,
		rootKey,
	)
	if err != nil {
		t.Fatalf("CreateCertificate(leaf) error = %v", err)
	}
	certificatePEM := append(
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leafDER}),
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: rootDER})...,
	)
	keyDER, err := x509.MarshalPKCS8PrivateKey(leafKey)
	if err != nil {
		t.Fatalf("MarshalPKCS8PrivateKey() error = %v", err)
	}
	return certificatePEM, pem.EncodeToMemory(&pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: keyDER,
	})
}

func createWeakUserCertificate(
	t *testing.T,
	now time.Time,
) ([]byte, []byte) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "weak.example.test"},
		NotBefore:             now.Add(-time.Minute),
		NotAfter:              now.Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:              []string{"weak.example.test"},
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(
		rand.Reader,
		template,
		template,
		&key.PublicKey,
		key,
	)
	if err != nil {
		t.Fatalf("CreateCertificate() error = %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{
			Type:  "CERTIFICATE",
			Bytes: der,
		}), pem.EncodeToMemory(&pem.Block{
			Type:  "RSA PRIVATE KEY",
			Bytes: x509.MarshalPKCS1PrivateKey(key),
		})
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func sha256Sum(value []byte) []byte {
	sum := sha256.Sum256(value)
	return sum[:]
}
