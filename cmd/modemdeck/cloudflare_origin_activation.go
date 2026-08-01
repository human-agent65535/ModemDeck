package main

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	cloudflareOriginActivationTimeout = 12 * time.Second
	cloudflareOriginProbeInterval     = 250 * time.Millisecond
)

func newCloudflareOriginTLSActivator(
	rawURLs string,
) (cloudflareOriginTLSActivator, error) {
	endpoints, err := cloudflareOriginProbeURLs(rawURLs)
	if err != nil {
		return nil, err
	}
	if len(endpoints) == 0 {
		return nil, nil
	}
	return func(ctx context.Context, fingerprint string) error {
		expectedFingerprint, err := parseSHA256Fingerprint(fingerprint)
		if err != nil {
			return err
		}
		return waitForCloudflareOriginTLS(ctx, endpoints, expectedFingerprint)
	}, nil
}

func cloudflareOriginProbeURLs(rawURLs string) ([]*url.URL, error) {
	parts := strings.Split(rawURLs, ",")
	endpoints := make([]*url.URL, 0, len(parts))
	for _, part := range parts {
		rawURL := strings.TrimSpace(part)
		if rawURL == "" {
			continue
		}
		endpoint, err := url.Parse(rawURL)
		if err != nil || endpoint.Scheme != "https" || endpoint.Hostname() == "" ||
			endpoint.User != nil || endpoint.Fragment != "" {
			return nil, fmt.Errorf("invalid Cloudflare origin probe URL %q", rawURL)
		}
		endpoints = append(endpoints, endpoint)
	}
	return endpoints, nil
}

func parseSHA256Fingerprint(value string) ([sha256.Size]byte, error) {
	var fingerprint [sha256.Size]byte
	decoded, err := hex.DecodeString(strings.ReplaceAll(strings.TrimSpace(value), ":", ""))
	if err != nil || len(decoded) != sha256.Size {
		return fingerprint, errors.New("installed certificate has an invalid SHA-256 fingerprint")
	}
	copy(fingerprint[:], decoded)
	return fingerprint, nil
}

func waitForCloudflareOriginTLS(
	ctx context.Context,
	endpoints []*url.URL,
	expectedFingerprint [sha256.Size]byte,
) error {
	activationContext, cancel := context.WithTimeout(
		ctx,
		cloudflareOriginActivationTimeout,
	)
	defer cancel()

	transport := &http.Transport{
		ForceAttemptHTTP2:     true,
		DialContext:           (&net.Dialer{Timeout: 2 * time.Second}).DialContext,
		TLSHandshakeTimeout:   2 * time.Second,
		ResponseHeaderTimeout: 2 * time.Second,
		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
			// The Origin CA is intentionally not in the system trust store. The
			// manager has already validated it; the probe authenticates the exact
			// installed leaf certificate instead of relying on a DNS name.
			InsecureSkipVerify: true, //nolint:gosec -- exact leaf pinning below
			VerifyConnection: func(state tls.ConnectionState) error {
				if len(state.PeerCertificates) == 0 {
					return errors.New("origin returned no certificate")
				}
				actual := sha256.Sum256(state.PeerCertificates[0].Raw)
				if subtle.ConstantTimeCompare(actual[:], expectedFingerprint[:]) != 1 {
					return errors.New("origin served a different certificate")
				}
				return nil
			},
		},
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{
		Transport: transport,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return errors.New("origin probe must not redirect")
		},
	}

	var lastErr error
	for {
		allReady := true
		for _, endpoint := range endpoints {
			if err := probeCloudflareOriginTLS(
				activationContext,
				client,
				endpoint,
			); err != nil {
				lastErr = fmt.Errorf("%s: %w", endpoint.Host, err)
				allReady = false
				break
			}
		}
		if allReady {
			return nil
		}

		timer := time.NewTimer(cloudflareOriginProbeInterval)
		select {
		case <-activationContext.Done():
			timer.Stop()
			return fmt.Errorf("%w; last probe: %v", activationContext.Err(), lastErr)
		case <-timer.C:
		}
	}
}

func probeCloudflareOriginTLS(
	ctx context.Context,
	client *http.Client,
	endpoint *url.URL,
) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return err
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	closeErr := response.Body.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if response.ProtoMajor != 2 {
		return fmt.Errorf("negotiated HTTP/%d instead of HTTP/2", response.ProtoMajor)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("returned HTTP status %d", response.StatusCode)
	}
	return nil
}
