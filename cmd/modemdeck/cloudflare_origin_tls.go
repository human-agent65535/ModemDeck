package main

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"sync"

	"github.com/human-agent65535/modemdeck/internal/httpapi"
	"github.com/human-agent65535/modemdeck/internal/mobilepairing"
	"github.com/human-agent65535/modemdeck/internal/tlsmanager"
)

type cloudflareOriginCertificateManager interface {
	Status() tlsmanager.CloudflareOriginStatus
	Install([]byte, []byte, []string) (tlsmanager.CloudflareOriginStatus, error)
	Disable() error
	CoversDNSNames([]string) bool
}

type cloudflareOriginTLSActivator func(context.Context, string) error

type cloudflareOriginTLSService struct {
	manager    cloudflareOriginCertificateManager
	cloudflare mobilepairing.Availability
	activate   cloudflareOriginTLSActivator
	mu         sync.Mutex
}

func (service *cloudflareOriginTLSService) Status(
	ctx context.Context,
) httpapi.CloudflareOriginTLSStatus {
	service.mu.Lock()
	defer service.mu.Unlock()

	return cloudflareOriginTLSStatus(
		service.manager,
		service.cloudflareStatus(ctx, false),
	)
}

func (service *cloudflareOriginTLSService) Install(
	ctx context.Context,
	certificatePEM []byte,
	privateKeyPEM []byte,
) (httpapi.CloudflareOriginTLSStatus, error) {
	service.mu.Lock()
	defer service.mu.Unlock()

	cloudflare := service.cloudflareStatus(ctx, true)
	_, err := service.manager.Install(
		certificatePEM,
		privateKeyPEM,
		cloudflareRouteDNSNames(cloudflare),
	)
	if err != nil {
		switch {
		case errors.Is(err, tlsmanager.ErrInvalidCloudflareOriginMaterial):
			return httpapi.CloudflareOriginTLSStatus{}, fmt.Errorf(
				"%w: %v",
				httpapi.ErrCloudflareOriginTLSInvalidInput,
				err,
			)
		case errors.Is(err, tlsmanager.ErrCloudflareOriginAlreadyEnabled):
			return httpapi.CloudflareOriginTLSStatus{}, fmt.Errorf(
				"%w: %v",
				httpapi.ErrCloudflareOriginTLSAlreadyEnabled,
				err,
			)
		}
		return httpapi.CloudflareOriginTLSStatus{}, err
	}
	status := cloudflareOriginTLSStatus(service.manager, cloudflare)
	activationErr := errors.New("origin activation probe is not configured")
	if service.activate != nil {
		activationErr = service.activate(ctx, status.FingerprintSHA256)
	}
	if activationErr == nil {
		return status, nil
	}
	if rollbackErr := service.manager.Disable(); rollbackErr != nil {
		return httpapi.CloudflareOriginTLSStatus{}, fmt.Errorf(
			"activate Cloudflare origin TLS: %v; remove inactive certificate: %w",
			activationErr,
			rollbackErr,
		)
	}
	return httpapi.CloudflareOriginTLSStatus{}, fmt.Errorf(
		"%w: %v",
		httpapi.ErrCloudflareOriginTLSActivation,
		activationErr,
	)
}

func (service *cloudflareOriginTLSService) Disable(
	ctx context.Context,
) (httpapi.CloudflareOriginTLSStatus, error) {
	service.mu.Lock()
	defer service.mu.Unlock()

	if err := service.manager.Disable(); err != nil {
		return httpapi.CloudflareOriginTLSStatus{}, err
	}
	return cloudflareOriginTLSStatus(
		service.manager,
		service.cloudflareStatus(ctx, false),
	), nil
}

func (service *cloudflareOriginTLSService) cloudflareStatus(
	ctx context.Context,
	refresh bool,
) mobilepairing.CloudflareStatus {
	if service.cloudflare == nil {
		return mobilepairing.CloudflareStatus{}
	}
	if refresh {
		if refresher, ok := service.cloudflare.(mobilepairing.Refresher); ok {
			return refresher.Refresh(ctx)
		}
	}
	return service.cloudflare.Status(ctx)
}

func cloudflareOriginTLSStatus(
	manager cloudflareOriginCertificateManager,
	cloudflare mobilepairing.CloudflareStatus,
) httpapi.CloudflareOriginTLSStatus {
	if manager == nil {
		return httpapi.CloudflareOriginTLSStatus{DNSNames: []string{}}
	}
	status := manager.Status()
	if !status.Enabled {
		return httpapi.CloudflareOriginTLSStatus{DNSNames: []string{}}
	}
	certificate := status.Certificate
	return httpapi.CloudflareOriginTLSStatus{
		Enabled:           true,
		CoversRoutes:      manager.CoversDNSNames(cloudflareRouteDNSNames(cloudflare)),
		Subject:           certificate.Subject,
		Issuer:            certificate.Issuer,
		DNSNames:          append([]string(nil), certificate.DNSNames...),
		NotBefore:         formatTLSTime(certificate.NotBefore),
		NotAfter:          formatTLSTime(certificate.NotAfter),
		FingerprintSHA256: certificate.FingerprintSHA256,
		Expired:           certificate.Expired,
	}
}

func cloudflareRouteDNSNames(
	status mobilepairing.CloudflareStatus,
) []string {
	unique := make(map[string]struct{}, len(status.APIURLs)+len(status.WebURLs))
	for _, rawURL := range append(
		append([]string(nil), status.APIURLs...),
		status.WebURLs...,
	) {
		parsed, err := url.Parse(rawURL)
		if err != nil {
			continue
		}
		hostname := strings.TrimSuffix(strings.ToLower(parsed.Hostname()), ".")
		if hostname != "" {
			unique[hostname] = struct{}{}
		}
	}
	names := make([]string, 0, len(unique))
	for name := range unique {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
