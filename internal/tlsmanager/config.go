package tlsmanager

import (
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

var defaultHosts = []string{"localhost", "127.0.0.1", "::1"}

type normalizedConfig struct {
	directory           string
	hosts               []string
	caValidity          time.Duration
	certificateValidity time.Duration
	renewalWindow       time.Duration
	now                 func() time.Time
}

func normalizeConfig(config Config) (normalizedConfig, error) {
	directory := strings.TrimSpace(config.Directory)
	if directory == "" {
		return normalizedConfig{}, fmt.Errorf(
			"%w: directory is required",
			ErrInvalidConfiguration,
		)
	}
	absolute, err := filepath.Abs(directory)
	if err != nil {
		return normalizedConfig{}, fmt.Errorf(
			"%w: resolve directory: %v",
			ErrInvalidConfiguration,
			err,
		)
	}

	hosts, err := normalizeHosts(config.Hosts)
	if err != nil {
		return normalizedConfig{}, err
	}
	caValidity := config.caValidity
	if caValidity == 0 {
		caValidity = defaultCAValidity
	}
	certificateValidity := config.certificateValidity
	if certificateValidity == 0 {
		certificateValidity = defaultCertificateValidity
	}
	renewalWindow := config.renewalWindow
	if renewalWindow == 0 {
		renewalWindow = defaultRenewalWindow
	}
	if caValidity <= 0 {
		return normalizedConfig{}, fmt.Errorf(
			"%w: CA validity must be positive",
			ErrInvalidConfiguration,
		)
	}
	if certificateValidity <= 0 {
		return normalizedConfig{}, fmt.Errorf(
			"%w: certificate validity must be positive",
			ErrInvalidConfiguration,
		)
	}
	if renewalWindow <= 0 || renewalWindow >= certificateValidity {
		return normalizedConfig{}, fmt.Errorf(
			"%w: renewal window must be positive and shorter than certificate validity",
			ErrInvalidConfiguration,
		)
	}
	now := config.clock
	if now == nil {
		now = time.Now
	}
	return normalizedConfig{
		directory:           absolute,
		hosts:               hosts,
		caValidity:          caValidity,
		certificateValidity: certificateValidity,
		renewalWindow:       renewalWindow,
		now:                 now,
	}, nil
}

func normalizeHosts(configured []string) ([]string, error) {
	unique := make(map[string]struct{}, len(defaultHosts)+len(configured))
	for _, host := range append(append([]string(nil), defaultHosts...), configured...) {
		normalized, err := normalizeHost(host)
		if err != nil {
			return nil, fmt.Errorf(
				"%w: invalid host %q: %v",
				ErrInvalidConfiguration,
				host,
				err,
			)
		}
		unique[normalized] = struct{}{}
	}
	hosts := make([]string, 0, len(unique))
	for host := range unique {
		hosts = append(hosts, host)
	}
	sort.Strings(hosts)
	return hosts, nil
}

func normalizeHost(host string) (string, error) {
	if host == "" || host != strings.TrimSpace(host) {
		return "", errors.New("host must not be empty or contain surrounding whitespace")
	}
	if address := net.ParseIP(host); address != nil {
		return address.String(), nil
	}
	if len(host) > 253 {
		return "", errors.New("DNS name is longer than 253 characters")
	}
	if strings.HasSuffix(host, ".") {
		host = strings.TrimSuffix(host, ".")
	}
	if host == "" {
		return "", errors.New("DNS name is empty")
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > 63 {
			return "", errors.New("DNS label length is invalid")
		}
		if label[0] == '-' || label[len(label)-1] == '-' {
			return "", errors.New("DNS label cannot start or end with a hyphen")
		}
		for _, character := range label {
			if character >= 'a' && character <= 'z' ||
				character >= 'A' && character <= 'Z' ||
				character >= '0' && character <= '9' ||
				character == '-' {
				continue
			}
			return "", errors.New("DNS name contains an invalid character")
		}
	}
	return strings.ToLower(host), nil
}
