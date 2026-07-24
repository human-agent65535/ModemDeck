package tlsmanager

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"sort"
	"strings"
	"time"
)

const certificateBackdate = 5 * time.Minute

type certificateMaterial struct {
	certificate tls.Certificate
	chain       []*x509.Certificate
	key         crypto.Signer
	bundlePEM   []byte
}

type caMaterial struct {
	certificate    *x509.Certificate
	key            crypto.Signer
	certificatePEM []byte
	bundlePEM      []byte
}

func parseBundle(bundle []byte) (certificateMaterial, error) {
	var (
		certificateDER [][]byte
		certificates   []*x509.Certificate
		privateKey     crypto.Signer
	)
	rest := bundle
	for {
		rest = bytes.TrimSpace(rest)
		if len(rest) == 0 {
			break
		}
		block, remaining := pem.Decode(rest)
		if block == nil {
			return certificateMaterial{}, errors.New("PEM bundle contains undecodable data")
		}
		rest = remaining
		if len(block.Headers) != 0 {
			return certificateMaterial{}, errors.New("encrypted or annotated PEM blocks are unsupported")
		}
		switch block.Type {
		case "CERTIFICATE":
			certificate, err := x509.ParseCertificate(block.Bytes)
			if err != nil {
				return certificateMaterial{}, fmt.Errorf("parse certificate: %w", err)
			}
			certificateDER = append(certificateDER, append([]byte(nil), block.Bytes...))
			certificates = append(certificates, certificate)
		case "PRIVATE KEY", "RSA PRIVATE KEY", "EC PRIVATE KEY":
			if privateKey != nil {
				return certificateMaterial{}, errors.New("PEM bundle contains multiple private keys")
			}
			key, err := parsePrivateKey(block)
			if err != nil {
				return certificateMaterial{}, err
			}
			privateKey = key
		default:
			return certificateMaterial{}, fmt.Errorf(
				"unsupported PEM block %q",
				block.Type,
			)
		}
	}
	if len(certificates) == 0 {
		return certificateMaterial{}, errors.New("PEM bundle contains no certificates")
	}
	if privateKey == nil {
		return certificateMaterial{}, errors.New("PEM bundle contains no private key")
	}
	if err := validateKeyStrength(privateKey); err != nil {
		return certificateMaterial{}, err
	}
	if err := validateKeyMatchesCertificate(privateKey, certificates[0]); err != nil {
		return certificateMaterial{}, err
	}
	return certificateMaterial{
		certificate: tls.Certificate{
			Certificate: certificateDER,
			PrivateKey:  privateKey,
			Leaf:        certificates[0],
		},
		chain:     certificates,
		key:       privateKey,
		bundlePEM: append([]byte(nil), bundle...),
	}, nil
}

func parseUserMaterial(
	certificatePEM []byte,
	privateKeyPEM []byte,
) (certificateMaterial, error) {
	certificateBlocks, certificates, err := parseCertificates(certificatePEM)
	if err != nil {
		return certificateMaterial{}, err
	}
	key, err := parseSinglePrivateKey(privateKeyPEM)
	if err != nil {
		return certificateMaterial{}, err
	}
	if err := validateKeyStrength(key); err != nil {
		return certificateMaterial{}, err
	}
	if err := validateKeyMatchesCertificate(key, certificates[0]); err != nil {
		return certificateMaterial{}, err
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return certificateMaterial{}, fmt.Errorf("marshal private key: %w", err)
	}
	normalizedKey := pem.EncodeToMemory(&pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: keyDER,
	})
	bundle := append(append([]byte(nil), certificateBlocks...), normalizedKey...)
	certificateDER := make([][]byte, 0, len(certificates))
	for _, certificate := range certificates {
		certificateDER = append(
			certificateDER,
			append([]byte(nil), certificate.Raw...),
		)
	}
	return certificateMaterial{
		certificate: tls.Certificate{
			Certificate: certificateDER,
			PrivateKey:  key,
			Leaf:        certificates[0],
		},
		chain:     certificates,
		key:       key,
		bundlePEM: bundle,
	}, nil
}

func parseCertificates(content []byte) ([]byte, []*x509.Certificate, error) {
	var (
		normalized   []byte
		certificates []*x509.Certificate
	)
	rest := content
	for {
		rest = bytes.TrimSpace(rest)
		if len(rest) == 0 {
			break
		}
		block, remaining := pem.Decode(rest)
		if block == nil {
			return nil, nil, errors.New("certificate chain contains undecodable data")
		}
		rest = remaining
		if block.Type != "CERTIFICATE" || len(block.Headers) != 0 {
			return nil, nil, errors.New("certificate chain must contain only CERTIFICATE PEM blocks")
		}
		certificate, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, nil, fmt.Errorf("parse certificate: %w", err)
		}
		certificates = append(certificates, certificate)
		normalized = append(normalized, pem.EncodeToMemory(&pem.Block{
			Type:  "CERTIFICATE",
			Bytes: certificate.Raw,
		})...)
	}
	if len(certificates) == 0 {
		return nil, nil, errors.New("certificate chain is empty")
	}
	return normalized, certificates, nil
}

func parseSinglePrivateKey(content []byte) (crypto.Signer, error) {
	rest := bytes.TrimSpace(content)
	block, remaining := pem.Decode(rest)
	if block == nil {
		return nil, errors.New("private key is not PEM encoded")
	}
	if len(bytes.TrimSpace(remaining)) != 0 {
		return nil, errors.New("private key input must contain exactly one PEM block")
	}
	if len(block.Headers) != 0 {
		return nil, errors.New("encrypted private keys are unsupported")
	}
	return parsePrivateKey(block)
}

func parsePrivateKey(block *pem.Block) (crypto.Signer, error) {
	var (
		key any
		err error
	)
	switch block.Type {
	case "PRIVATE KEY":
		key, err = x509.ParsePKCS8PrivateKey(block.Bytes)
	case "RSA PRIVATE KEY":
		key, err = x509.ParsePKCS1PrivateKey(block.Bytes)
	case "EC PRIVATE KEY":
		key, err = x509.ParseECPrivateKey(block.Bytes)
	default:
		return nil, fmt.Errorf("unsupported private key PEM block %q", block.Type)
	}
	if err != nil {
		return nil, fmt.Errorf("parse private key: %w", err)
	}
	signer, ok := key.(crypto.Signer)
	if !ok {
		return nil, errors.New("private key type cannot sign TLS handshakes")
	}
	return signer, nil
}

func validateKeyStrength(key crypto.Signer) error {
	switch value := key.(type) {
	case *rsa.PrivateKey:
		if value.N.BitLen() < 2048 {
			return errors.New("RSA private key must be at least 2048 bits")
		}
	case *ecdsa.PrivateKey:
		if value.Curve == nil ||
			value.Curve.Params() == nil ||
			value.Curve.Params().BitSize < 256 {
			return errors.New("ECDSA private key must use a curve of at least 256 bits")
		}
	case ed25519.PrivateKey:
		if len(value) != ed25519.PrivateKeySize {
			return errors.New("Ed25519 private key has an invalid size")
		}
	default:
		return fmt.Errorf("unsupported private key type %T", key)
	}
	return nil
}

func validateKeyMatchesCertificate(
	key crypto.Signer,
	certificate *x509.Certificate,
) error {
	publicDER, err := x509.MarshalPKIXPublicKey(key.Public())
	if err != nil {
		return fmt.Errorf("marshal private key public component: %w", err)
	}
	if !bytes.Equal(publicDER, certificate.RawSubjectPublicKeyInfo) {
		return errors.New("private key does not match leaf certificate")
	}
	return nil
}

func validateServerMaterial(
	material certificateMaterial,
	now time.Time,
	requireCurrent bool,
) error {
	leaf := material.chain[0]
	if leaf.IsCA {
		return errors.New("leaf certificate must not be a certificate authority")
	}
	if len(leaf.DNSNames) == 0 && len(leaf.IPAddresses) == 0 {
		return errors.New("leaf certificate must contain a DNS or IP subject alternative name")
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

	roots, err := x509.SystemCertPool()
	if err != nil || roots == nil {
		roots = x509.NewCertPool()
	}
	intermediates := x509.NewCertPool()
	if selfSigned(leaf) {
		roots.AddCert(leaf)
	}
	for _, certificate := range material.chain[1:] {
		if selfSigned(certificate) {
			roots.AddCert(certificate)
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
		return fmt.Errorf("verify server certificate chain: %w", err)
	}
	return nil
}

func selfSigned(certificate *x509.Certificate) bool {
	return bytes.Equal(certificate.RawSubject, certificate.RawIssuer) &&
		certificate.CheckSignature(
			certificate.SignatureAlgorithm,
			certificate.RawTBSCertificate,
			certificate.Signature,
		) == nil
}

func createCA(
	now time.Time,
	validity time.Duration,
) (caMaterial, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return caMaterial{}, fmt.Errorf("generate automatic CA key: %w", err)
	}
	serial, err := randomSerial()
	if err != nil {
		return caMaterial{}, err
	}
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:   "ModemDeck Local CA",
			Organization: []string{"ModemDeck"},
		},
		NotBefore:             now.Add(-certificateBackdate),
		NotAfter:              now.Add(validity),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLen:            0,
	}
	der, err := x509.CreateCertificate(
		rand.Reader,
		template,
		template,
		&key.PublicKey,
		key,
	)
	if err != nil {
		return caMaterial{}, fmt.Errorf("create automatic CA certificate: %w", err)
	}
	certificate, err := x509.ParseCertificate(der)
	if err != nil {
		return caMaterial{}, fmt.Errorf("parse generated automatic CA certificate: %w", err)
	}
	certificatePEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: der,
	})
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return caMaterial{}, fmt.Errorf("marshal automatic CA key: %w", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: keyDER,
	})
	return caMaterial{
		certificate:    certificate,
		key:            key,
		certificatePEM: certificatePEM,
		bundlePEM:      append(append([]byte(nil), certificatePEM...), keyPEM...),
	}, nil
}

func parseCA(bundle []byte) (caMaterial, error) {
	material, err := parseBundle(bundle)
	if err != nil {
		return caMaterial{}, err
	}
	if len(material.chain) != 1 {
		return caMaterial{}, errors.New("automatic CA bundle must contain exactly one certificate")
	}
	certificate := material.chain[0]
	if !certificate.IsCA ||
		!certificate.BasicConstraintsValid ||
		certificate.KeyUsage&x509.KeyUsageCertSign == 0 {
		return caMaterial{}, errors.New("automatic CA certificate lacks CA constraints")
	}
	if !selfSigned(certificate) {
		return caMaterial{}, errors.New("automatic CA certificate is not self-signed")
	}
	certificatePEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certificate.Raw,
	})
	return caMaterial{
		certificate:    certificate,
		key:            material.key,
		certificatePEM: certificatePEM,
		bundlePEM:      append([]byte(nil), bundle...),
	}, nil
}

func createAutomaticServer(
	config normalizedConfig,
	ca caMaterial,
) (certificateMaterial, error) {
	now := config.now()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return certificateMaterial{}, fmt.Errorf(
			"generate automatic server key: %w",
			err,
		)
	}
	serial, err := randomSerial()
	if err != nil {
		return certificateMaterial{}, err
	}
	dnsNames, ipAddresses := splitHosts(config.hosts)
	notAfter := now.Add(config.certificateValidity)
	if notAfter.After(ca.certificate.NotAfter) {
		notAfter = ca.certificate.NotAfter
	}
	if !notAfter.After(now) {
		return certificateMaterial{}, errors.New(
			"automatic CA expires before a server certificate can be issued",
		)
	}
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:   "ModemDeck Local Server",
			Organization: []string{"ModemDeck"},
		},
		NotBefore:   now.Add(-certificateBackdate),
		NotAfter:    notAfter,
		KeyUsage:    x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:    dnsNames,
		IPAddresses: ipAddresses,
	}
	der, err := x509.CreateCertificate(
		rand.Reader,
		template,
		ca.certificate,
		&key.PublicKey,
		ca.key,
	)
	if err != nil {
		return certificateMaterial{}, fmt.Errorf(
			"create automatic server certificate: %w",
			err,
		)
	}
	certificate, err := x509.ParseCertificate(der)
	if err != nil {
		return certificateMaterial{}, fmt.Errorf(
			"parse generated automatic server certificate: %w",
			err,
		)
	}
	certificatePEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: der,
	})
	caPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: ca.certificate.Raw,
	})
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return certificateMaterial{}, fmt.Errorf(
			"marshal automatic server key: %w",
			err,
		)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: keyDER,
	})
	bundle := make([]byte, 0, len(certificatePEM)+len(caPEM)+len(keyPEM))
	bundle = append(bundle, certificatePEM...)
	bundle = append(bundle, caPEM...)
	bundle = append(bundle, keyPEM...)
	return certificateMaterial{
		certificate: tls.Certificate{
			Certificate: [][]byte{
				append([]byte(nil), certificate.Raw...),
				append([]byte(nil), ca.certificate.Raw...),
			},
			PrivateKey: key,
			Leaf:       certificate,
		},
		chain:     []*x509.Certificate{certificate, ca.certificate},
		key:       key,
		bundlePEM: bundle,
	}, nil
}

func randomSerial() (*big.Int, error) {
	limit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(rand.Reader, limit)
	if err != nil {
		return nil, fmt.Errorf("generate certificate serial: %w", err)
	}
	if serial.Sign() == 0 {
		serial.SetInt64(1)
	}
	return serial, nil
}

func splitHosts(hosts []string) ([]string, []net.IP) {
	var (
		dnsNames    []string
		ipAddresses []net.IP
	)
	for _, host := range hosts {
		if address := net.ParseIP(host); address != nil {
			ipAddresses = append(ipAddresses, address)
			continue
		}
		dnsNames = append(dnsNames, host)
	}
	return dnsNames, ipAddresses
}

func automaticServerNeedsRenewal(
	material certificateMaterial,
	ca caMaterial,
	config normalizedConfig,
) bool {
	leaf := material.chain[0]
	now := config.now()
	if now.Before(leaf.NotBefore) || !now.Before(leaf.NotAfter) {
		return true
	}
	if leaf.NotAfter.Sub(now) <= config.renewalWindow {
		return true
	}
	if leaf.CheckSignatureFrom(ca.certificate) != nil {
		return true
	}
	return !sameSANs(leaf, config.hosts)
}

func sameSANs(certificate *x509.Certificate, hosts []string) bool {
	actual := make([]string, 0, len(certificate.DNSNames)+len(certificate.IPAddresses))
	for _, name := range certificate.DNSNames {
		actual = append(actual, strings.ToLower(name))
	}
	for _, address := range certificate.IPAddresses {
		actual = append(actual, address.String())
	}
	sort.Strings(actual)
	return slicesEqual(actual, hosts)
}

func slicesEqual(first, second []string) bool {
	if len(first) != len(second) {
		return false
	}
	for index := range first {
		if first[index] != second[index] {
			return false
		}
	}
	return true
}

func statusFor(
	source Source,
	certificate *x509.Certificate,
	now time.Time,
	renewalWindow time.Duration,
) Status {
	dnsNames := append([]string(nil), certificate.DNSNames...)
	ipAddresses := make([]string, 0, len(certificate.IPAddresses))
	sans := make([]string, 0, len(dnsNames)+len(certificate.IPAddresses))
	sans = append(sans, dnsNames...)
	for _, address := range certificate.IPAddresses {
		value := address.String()
		ipAddresses = append(ipAddresses, value)
		sans = append(sans, value)
	}
	sum := sha256.Sum256(certificate.Raw)
	return Status{
		Source:              source,
		Subject:             certificate.Subject.String(),
		Issuer:              certificate.Issuer.String(),
		SANs:                sans,
		DNSNames:            dnsNames,
		IPAddresses:         ipAddresses,
		NotBefore:           certificate.NotBefore,
		NotAfter:            certificate.NotAfter,
		Expired:             !now.Before(certificate.NotAfter),
		RenewalDue:          source == SourceAutomatic && certificate.NotAfter.Sub(now) <= renewalWindow,
		RenewsAutomatically: source == SourceAutomatic,
		FingerprintSHA256:   formatFingerprint(sum[:]),
	}
}

func formatFingerprint(value []byte) string {
	encoded := strings.ToUpper(hex.EncodeToString(value))
	parts := make([]string, 0, len(encoded)/2)
	for index := 0; index < len(encoded); index += 2 {
		parts = append(parts, encoded[index:index+2])
	}
	return strings.Join(parts, ":")
}
