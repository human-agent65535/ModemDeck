package main

import (
	"errors"
	"fmt"
	"time"

	"github.com/human-agent65535/modemdeck/internal/httpapi"
	"github.com/human-agent65535/modemdeck/internal/tlsmanager"
)

type tlsSettingsService struct {
	manager *tlsmanager.Manager
}

func (service tlsSettingsService) Status() httpapi.TLSSettingsStatus {
	return tlsStatus(service.manager.Status())
}

func (service tlsSettingsService) InstallUser(
	certificatePEM, privateKeyPEM []byte,
) (httpapi.TLSSettingsStatus, error) {
	status, err := service.manager.InstallUser(certificatePEM, privateKeyPEM)
	if err != nil {
		if errors.Is(err, tlsmanager.ErrInvalidUserMaterial) {
			return httpapi.TLSSettingsStatus{}, fmt.Errorf(
				"%w: %v",
				httpapi.ErrTLSSettingsInvalidInput,
				err,
			)
		}
		return httpapi.TLSSettingsStatus{}, err
	}
	return tlsStatus(status), nil
}

func (service tlsSettingsService) UseAutomatic() (
	httpapi.TLSSettingsStatus,
	error,
) {
	status, err := service.manager.UseAutomatic()
	if err != nil {
		return httpapi.TLSSettingsStatus{}, err
	}
	return tlsStatus(status), nil
}

func (service tlsSettingsService) AutomaticCAPEM() ([]byte, error) {
	return service.manager.AutomaticCAPEM()
}

func tlsStatus(status tlsmanager.Status) httpapi.TLSSettingsStatus {
	mode := ""
	switch status.Source {
	case tlsmanager.SourceAutomatic:
		mode = "automatic"
	case tlsmanager.SourceUser:
		mode = "user"
	}
	return httpapi.TLSSettingsStatus{
		Mode:                mode,
		Subject:             status.Subject,
		Issuer:              status.Issuer,
		DNSNames:            append([]string(nil), status.DNSNames...),
		IPAddresses:         append([]string(nil), status.IPAddresses...),
		NotBefore:           formatTLSTime(status.NotBefore),
		NotAfter:            formatTLSTime(status.NotAfter),
		FingerprintSHA256:   status.FingerprintSHA256,
		Expired:             status.Expired,
		RenewsAutomatically: status.RenewsAutomatically,
	}
}

func formatTLSTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}
