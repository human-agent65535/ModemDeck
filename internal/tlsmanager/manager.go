package tlsmanager

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
)

type certificateState struct {
	source      Source
	certificate tls.Certificate
	leaf        *x509.Certificate
}

type Manager struct {
	config normalizedConfig

	mu sync.Mutex
	ca *caMaterial

	active atomic.Pointer[certificateState]
}

func (m *Manager) open() error {
	if err := ensureStorageDirectory(m.config.directory); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, _, err := m.ensureAutomaticCALocked(); err != nil {
		return fmt.Errorf("prepare automatic certificate authority: %w", err)
	}
	source, found, err := readSource(m.config.directory)
	if err != nil {
		return fmt.Errorf("read TLS certificate source: %w", err)
	}
	if !found {
		source = SourceAutomatic
	}

	var material certificateMaterial
	switch source {
	case SourceAutomatic:
		material, err = m.ensureAutomaticServerLocked()
	case SourceUser:
		material, err = m.loadUserLocked()
	default:
		err = errors.New("TLS certificate source is invalid")
	}
	if err != nil {
		return fmt.Errorf("load %s TLS certificate: %w", source, err)
	}
	if !found {
		if err := writeSource(m.config.directory, source); err != nil {
			return fmt.Errorf("persist TLS certificate source: %w", err)
		}
	}
	m.storeActive(source, material)
	return nil
}

func (m *Manager) Status() Status {
	state := m.active.Load()
	if state == nil || state.leaf == nil {
		return Status{}
	}
	return cloneStatus(statusFor(
		state.source,
		state.leaf,
		m.config.now(),
		m.config.renewalWindow,
	))
}

func (m *Manager) InstallUser(
	certificateChainPEM []byte,
	privateKeyPEM []byte,
) (Status, error) {
	material, err := parseUserMaterial(certificateChainPEM, privateKeyPEM)
	if err != nil {
		return Status{}, invalidUserMaterial(err)
	}
	if err := validateServerMaterial(material, m.config.now(), true); err != nil {
		return Status{}, invalidUserMaterial(err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if err := writeAtomic(
		filepath.Join(m.config.directory, userBundleFilename),
		material.bundlePEM,
	); err != nil {
		return Status{}, fmt.Errorf("persist user TLS certificate: %w", err)
	}
	current := m.active.Load()
	if current == nil || current.source != SourceUser {
		if err := writeSource(m.config.directory, SourceUser); err != nil {
			return Status{}, fmt.Errorf("select user TLS certificate: %w", err)
		}
	}
	m.storeActive(SourceUser, material)
	return m.Status(), nil
}

func (m *Manager) UseAutomatic() (Status, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, _, err := m.ensureAutomaticCALocked(); err != nil {
		return Status{}, fmt.Errorf("prepare automatic certificate authority: %w", err)
	}
	material, err := m.ensureAutomaticServerLocked()
	if err != nil {
		return Status{}, fmt.Errorf("prepare automatic TLS certificate: %w", err)
	}
	current := m.active.Load()
	if current == nil || current.source != SourceAutomatic {
		if err := writeSource(m.config.directory, SourceAutomatic); err != nil {
			return Status{}, fmt.Errorf("select automatic TLS certificate: %w", err)
		}
	}
	m.storeActive(SourceAutomatic, material)
	return m.Status(), nil
}

func (m *Manager) AutomaticCAPEM() ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, changed, err := m.ensureAutomaticCALocked()
	if err != nil {
		return nil, fmt.Errorf("prepare automatic certificate authority: %w", err)
	}
	if changed {
		current := m.active.Load()
		if current != nil && current.source == SourceAutomatic {
			material, issueErr := m.ensureAutomaticServerLocked()
			if issueErr != nil {
				return nil, fmt.Errorf(
					"renew automatic TLS certificate after CA replacement: %w",
					issueErr,
				)
			}
			m.storeActive(SourceAutomatic, material)
		}
	}
	if m.ca == nil {
		return nil, errors.New("automatic certificate authority is unavailable")
	}
	return append([]byte(nil), m.ca.certificatePEM...), nil
}

func (m *Manager) getCertificate(
	*tls.ClientHelloInfo,
) (*tls.Certificate, error) {
	state := m.active.Load()
	if state == nil {
		return nil, errors.New("TLS certificate is unavailable")
	}
	if state.source != SourceAutomatic ||
		!m.automaticStateNeedsRenewal(state) {
		return &state.certificate, nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	state = m.active.Load()
	if state == nil {
		return nil, errors.New("TLS certificate is unavailable")
	}
	if state.source != SourceAutomatic ||
		!m.automaticStateNeedsRenewal(state) {
		return &state.certificate, nil
	}
	if _, _, err := m.ensureAutomaticCALocked(); err != nil {
		return m.automaticRenewalFailure(state, err)
	}
	material, err := m.ensureAutomaticServerLocked()
	if err != nil {
		return m.automaticRenewalFailure(state, err)
	}
	m.storeActive(SourceAutomatic, material)
	state = m.active.Load()
	return &state.certificate, nil
}

func (m *Manager) automaticStateNeedsRenewal(state *certificateState) bool {
	now := m.config.now()
	return now.Before(state.leaf.NotBefore) ||
		!now.Before(state.leaf.NotAfter) ||
		state.leaf.NotAfter.Sub(now) <= m.config.renewalWindow
}

func (m *Manager) automaticRenewalFailure(
	state *certificateState,
	err error,
) (*tls.Certificate, error) {
	now := m.config.now()
	if !now.Before(state.leaf.NotBefore) &&
		now.Before(state.leaf.NotAfter) {
		return &state.certificate, nil
	}
	return nil, fmt.Errorf("renew automatic TLS certificate: %w", err)
}

func (m *Manager) storeActive(
	source Source,
	material certificateMaterial,
) {
	m.active.Store(&certificateState{
		source:      source,
		certificate: material.certificate,
		leaf:        material.chain[0],
	})
}

func (m *Manager) ensureAutomaticCALocked() (
	caMaterial,
	bool,
	error,
) {
	now := m.config.now()
	if m.ca != nil {
		if now.Before(m.ca.certificate.NotBefore) {
			return caMaterial{}, false, errors.New(
				"automatic CA certificate is not valid yet",
			)
		}
		if now.Before(m.ca.certificate.NotAfter) {
			return *m.ca, false, nil
		}
		return m.createAutomaticCALocked()
	}

	path := filepath.Join(m.config.directory, automaticCAFilename)
	bundle, found, err := readOptionalRegularFile(path)
	if err != nil {
		return caMaterial{}, false, err
	}
	if !found {
		return m.createAutomaticCALocked()
	}
	material, err := parseCA(bundle)
	if err != nil {
		return caMaterial{}, false, fmt.Errorf(
			"automatic CA bundle is invalid: %w",
			err,
		)
	}
	if now.Before(material.certificate.NotBefore) {
		return caMaterial{}, false, errors.New(
			"automatic CA certificate is not valid yet",
		)
	}
	if !now.Before(material.certificate.NotAfter) {
		return m.createAutomaticCALocked()
	}
	m.ca = &material
	return material, false, nil
}

func (m *Manager) createAutomaticCALocked() (
	caMaterial,
	bool,
	error,
) {
	material, err := createCA(m.config.now(), m.config.caValidity)
	if err != nil {
		return caMaterial{}, false, err
	}
	if err := writeAtomic(
		filepath.Join(m.config.directory, automaticCAFilename),
		material.bundlePEM,
	); err != nil {
		return caMaterial{}, false, err
	}
	m.ca = &material
	return material, true, nil
}

func (m *Manager) ensureAutomaticServerLocked() (
	certificateMaterial,
	error,
) {
	if m.ca == nil {
		return certificateMaterial{}, errors.New(
			"automatic certificate authority is unavailable",
		)
	}
	path := filepath.Join(m.config.directory, automaticServerFilename)
	bundle, found, err := readOptionalRegularFile(path)
	if err != nil {
		return certificateMaterial{}, err
	}
	if !found {
		return m.createAutomaticServerLocked()
	}
	material, err := parseBundle(bundle)
	if err != nil {
		return certificateMaterial{}, fmt.Errorf(
			"automatic server bundle is invalid: %w",
			err,
		)
	}
	if err := validateServerMaterial(material, m.config.now(), false); err != nil {
		return certificateMaterial{}, fmt.Errorf(
			"automatic server certificate is invalid: %w",
			err,
		)
	}
	if automaticServerNeedsRenewal(material, *m.ca, m.config) {
		return m.createAutomaticServerLocked()
	}
	return material, nil
}

func (m *Manager) createAutomaticServerLocked() (
	certificateMaterial,
	error,
) {
	if m.ca == nil {
		return certificateMaterial{}, errors.New(
			"automatic certificate authority is unavailable",
		)
	}
	material, err := createAutomaticServer(m.config, *m.ca)
	if err != nil {
		return certificateMaterial{}, err
	}
	if err := writeAtomic(
		filepath.Join(m.config.directory, automaticServerFilename),
		material.bundlePEM,
	); err != nil {
		return certificateMaterial{}, err
	}
	return material, nil
}

func (m *Manager) loadUserLocked() (certificateMaterial, error) {
	path := filepath.Join(m.config.directory, userBundleFilename)
	bundle, found, err := readOptionalRegularFile(path)
	if err != nil {
		return certificateMaterial{}, err
	}
	if !found {
		return certificateMaterial{}, os.ErrNotExist
	}
	material, err := parseBundle(bundle)
	if err != nil {
		return certificateMaterial{}, fmt.Errorf(
			"user certificate bundle is invalid: %w",
			err,
		)
	}
	if err := validateServerMaterial(material, m.config.now(), false); err != nil {
		return certificateMaterial{}, fmt.Errorf(
			"user certificate is invalid: %w",
			err,
		)
	}
	return material, nil
}

func invalidUserMaterial(err error) error {
	return fmt.Errorf("%w: %v", ErrInvalidUserMaterial, err)
}
