package tlsmanager

import (
	"bytes"
	"crypto/x509"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Cloudflare publishes these roots at
// https://developers.cloudflare.com/ssl/origin-configuration/origin-ca/.
//
//go:embed cloudflare_origin_roots.pem
var cloudflareOriginRootsPEM []byte

type CloudflareOriginConfig struct {
	Directory string

	clock func() time.Time
	roots []*x509.Certificate
}

type CloudflareOriginStatus struct {
	Enabled     bool
	Certificate Status
}

type cloudflareOriginState struct {
	leaf *x509.Certificate
}

type CloudflareOriginManager struct {
	directory string
	now       func() time.Time
	roots     []*x509.Certificate

	mu     sync.Mutex
	active atomic.Pointer[cloudflareOriginState]
}

func OpenCloudflareOrigin(
	config CloudflareOriginConfig,
) (*CloudflareOriginManager, error) {
	directory := strings.TrimSpace(config.Directory)
	if directory == "" {
		return nil, fmt.Errorf(
			"%w: Cloudflare origin directory is required",
			ErrInvalidConfiguration,
		)
	}
	absolute, err := filepath.Abs(directory)
	if err != nil {
		return nil, fmt.Errorf(
			"%w: resolve Cloudflare origin directory: %v",
			ErrInvalidConfiguration,
			err,
		)
	}
	now := config.clock
	if now == nil {
		now = time.Now
	}
	roots := append([]*x509.Certificate(nil), config.roots...)
	if len(roots) == 0 {
		_, parsed, parseErr := parseCertificates(cloudflareOriginRootsPEM)
		if parseErr != nil {
			return nil, fmt.Errorf("parse embedded Cloudflare Origin CA roots: %w", parseErr)
		}
		roots = parsed
	}
	manager := &CloudflareOriginManager{
		directory: absolute,
		now:       now,
		roots:     roots,
	}
	if err := manager.open(); err != nil {
		return nil, err
	}
	return manager, nil
}

func (m *CloudflareOriginManager) open() error {
	if err := ensureStorageDirectory(m.directory); err != nil {
		return err
	}
	bundle, found, err := readOptionalRegularFile(
		filepath.Join(m.directory, cloudflareOriginFilename),
	)
	if err != nil || !found {
		return err
	}
	material, err := parseBundle(bundle)
	if err != nil {
		return fmt.Errorf("Cloudflare origin TLS bundle is invalid: %w", err)
	}
	if err := validateCloudflareOriginMaterial(
		material,
		m.now(),
		false,
		m.roots,
	); err != nil {
		return fmt.Errorf("Cloudflare origin TLS certificate is invalid: %w", err)
	}
	m.active.Store(&cloudflareOriginState{leaf: material.chain[0]})
	return nil
}

func (m *CloudflareOriginManager) Status() CloudflareOriginStatus {
	state := m.active.Load()
	if state == nil || state.leaf == nil {
		return CloudflareOriginStatus{}
	}
	return CloudflareOriginStatus{
		Enabled: true,
		Certificate: statusFor(
			SourceUser,
			state.leaf,
			m.now(),
			0,
		),
	}
}

func (m *CloudflareOriginManager) Install(
	certificateChainPEM []byte,
	privateKeyPEM []byte,
	requiredDNSNames []string,
) (CloudflareOriginStatus, error) {
	material, err := parseUserMaterial(certificateChainPEM, privateKeyPEM)
	if err != nil {
		return CloudflareOriginStatus{}, invalidCloudflareOriginMaterial(err)
	}
	if err := validateCloudflareOriginMaterial(
		material,
		m.now(),
		true,
		m.roots,
	); err != nil {
		return CloudflareOriginStatus{}, invalidCloudflareOriginMaterial(err)
	}
	if err := certificateCoversDNSNames(
		material.chain[0],
		requiredDNSNames,
	); err != nil {
		return CloudflareOriginStatus{}, invalidCloudflareOriginMaterial(err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.active.Load() != nil {
		return CloudflareOriginStatus{}, ErrCloudflareOriginAlreadyEnabled
	}
	if err := writeAtomic(
		filepath.Join(m.directory, cloudflareOriginFilename),
		material.bundlePEM,
	); err != nil {
		return CloudflareOriginStatus{}, fmt.Errorf(
			"persist Cloudflare origin TLS certificate: %w",
			err,
		)
	}
	m.active.Store(&cloudflareOriginState{leaf: material.chain[0]})
	return m.Status(), nil
}

func (m *CloudflareOriginManager) Disable() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	path := filepath.Join(m.directory, cloudflareOriginFilename)
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		m.active.Store(nil)
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect Cloudflare origin TLS certificate: %w", err)
	}
	if !info.Mode().IsRegular() {
		return errors.New("Cloudflare origin TLS bundle is not a regular file")
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("remove Cloudflare origin TLS certificate: %w", err)
	}
	directory, err := os.Open(m.directory)
	if err != nil {
		return fmt.Errorf("open TLS storage directory for sync: %w", err)
	}
	syncErr := directory.Sync()
	closeErr := directory.Close()
	if syncErr != nil {
		return fmt.Errorf("sync TLS storage directory: %w", syncErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close TLS storage directory: %w", closeErr)
	}
	m.active.Store(nil)
	return nil
}

func (m *CloudflareOriginManager) CoversDNSNames(names []string) bool {
	state := m.active.Load()
	if state == nil || state.leaf == nil || len(names) == 0 {
		return false
	}
	return certificateCoversDNSNames(state.leaf, names) == nil
}

func validateCloudflareOriginMaterial(
	material certificateMaterial,
	now time.Time,
	requireCurrent bool,
	trustedRoots []*x509.Certificate,
) error {
	leaf := material.chain[0]
	if leaf.IsCA {
		return errors.New("leaf certificate must not be a certificate authority")
	}
	if len(leaf.DNSNames) == 0 {
		return errors.New("leaf certificate must contain a DNS subject alternative name")
	}
	if len(leaf.IPAddresses) != 0 {
		return errors.New("Cloudflare Origin CA certificates must not contain IP addresses")
	}
	if leaf.NotAfter.IsZero() || !leaf.NotAfter.After(leaf.NotBefore) {
		return errors.New("leaf certificate validity interval is invalid")
	}
	if requireCurrent &&
		(now.Before(leaf.NotBefore) || !now.Before(leaf.NotAfter)) {
		return errors.New("leaf certificate is not currently valid")
	}
	verificationTime := now
	if verificationTime.Before(leaf.NotBefore) {
		verificationTime = leaf.NotBefore.Add(time.Second)
	}
	if !verificationTime.Before(leaf.NotAfter) {
		verificationTime = leaf.NotAfter.Add(-time.Second)
	}
	if !verificationTime.After(leaf.NotBefore) ||
		!verificationTime.Before(leaf.NotAfter) {
		return errors.New("leaf certificate has no usable validation instant")
	}

	roots := x509.NewCertPool()
	for _, root := range trustedRoots {
		if root == nil || !root.IsCA || !selfSigned(root) {
			return errors.New("trusted Cloudflare Origin CA root is invalid")
		}
		roots.AddCert(root)
	}
	intermediates := x509.NewCertPool()
	for _, certificate := range material.chain[1:] {
		if selfSigned(certificate) {
			if !trustedCertificate(certificate, trustedRoots) {
				return errors.New("certificate chain contains an untrusted root")
			}
			continue
		}
		intermediates.AddCert(certificate)
	}
	if _, err := leaf.Verify(x509.VerifyOptions{
		Roots:         roots,
		Intermediates: intermediates,
		CurrentTime:   verificationTime,
		KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}); err != nil {
		return fmt.Errorf("verify certificate against Cloudflare Origin CA: %w", err)
	}
	return nil
}

func trustedCertificate(
	candidate *x509.Certificate,
	trusted []*x509.Certificate,
) bool {
	for _, certificate := range trusted {
		if certificate != nil && bytes.Equal(candidate.Raw, certificate.Raw) {
			return true
		}
	}
	return false
}

func certificateCoversDNSNames(
	certificate *x509.Certificate,
	names []string,
) error {
	for _, name := range names {
		name = strings.TrimSuffix(strings.TrimSpace(name), ".")
		if name == "" {
			return errors.New("required origin hostname is empty")
		}
		if err := certificate.VerifyHostname(name); err != nil {
			return fmt.Errorf("certificate does not cover origin hostname %q", name)
		}
	}
	return nil
}

func invalidCloudflareOriginMaterial(err error) error {
	return fmt.Errorf("%w: %v", ErrInvalidCloudflareOriginMaterial, err)
}
