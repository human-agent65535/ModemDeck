package tlsmanager

import (
	"crypto/tls"
	"errors"
	"time"
)

const (
	defaultCAValidity          = 10 * 365 * 24 * time.Hour
	defaultCertificateValidity = 90 * 24 * time.Hour
	defaultRenewalWindow       = 30 * 24 * time.Hour
)

var (
	ErrInvalidConfiguration = errors.New("invalid TLS manager configuration")
	ErrInvalidUserMaterial  = errors.New("invalid user TLS certificate material")
)

type Source string

const (
	SourceAutomatic Source = "auto"
	SourceUser      Source = "user"
)

type Config struct {
	Directory string
	Hosts     []string

	caValidity          time.Duration
	certificateValidity time.Duration
	renewalWindow       time.Duration
	clock               func() time.Time
}

type Status struct {
	Source              Source
	Subject             string
	Issuer              string
	SANs                []string
	DNSNames            []string
	IPAddresses         []string
	NotBefore           time.Time
	NotAfter            time.Time
	Expired             bool
	RenewalDue          bool
	RenewsAutomatically bool
	FingerprintSHA256   string
}

func Open(config Config) (*Manager, error) {
	normalized, err := normalizeConfig(config)
	if err != nil {
		return nil, err
	}
	manager := &Manager{config: normalized}
	if err := manager.open(); err != nil {
		return nil, err
	}
	return manager, nil
}

func (m *Manager) TLSConfig() *tls.Config {
	return &tls.Config{
		MinVersion:     tls.VersionTLS12,
		GetCertificate: m.getCertificate,
	}
}

func cloneStatus(status Status) Status {
	status.SANs = append([]string(nil), status.SANs...)
	status.DNSNames = append([]string(nil), status.DNSNames...)
	status.IPAddresses = append([]string(nil), status.IPAddresses...)
	return status
}
