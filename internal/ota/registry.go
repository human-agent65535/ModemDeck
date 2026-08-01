package ota

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const manifestAccept = "application/vnd.oci.image.index.v1+json, application/vnd.docker.distribution.manifest.list.v2+json, application/vnd.oci.image.manifest.v1+json, application/vnd.docker.distribution.manifest.v2+json"

var ErrRegistryUnavailable = errors.New("container registry is unavailable")

type DigestResolver interface {
	Resolve(context.Context, string, string) (string, bool, error)
}

type GHCRResolver struct {
	registryURL string
	tokenURL    string
	client      HTTPClient
}

func NewGHCRResolver(registryURL, tokenURL string, client HTTPClient) *GHCRResolver {
	if strings.TrimSpace(registryURL) == "" {
		registryURL = "https://ghcr.io"
	}
	if strings.TrimSpace(tokenURL) == "" {
		tokenURL = "https://ghcr.io/token"
	}
	if client == nil {
		client = &http.Client{Timeout: defaultHTTPTimeout}
	}
	return &GHCRResolver{
		registryURL: strings.TrimRight(registryURL, "/"),
		tokenURL:    tokenURL,
		client:      client,
	}
}

func (resolver *GHCRResolver) Resolve(
	ctx context.Context,
	image, tag string,
) (string, bool, error) {
	const prefix = "ghcr.io/"
	if !strings.HasPrefix(image, prefix) || !validImageTag(tag) {
		return "", false, ErrRegistryUnavailable
	}
	repository := strings.TrimPrefix(image, prefix)
	if repository == "" || strings.Contains(repository, "..") {
		return "", false, ErrRegistryUnavailable
	}
	token, err := resolver.token(ctx, repository)
	if err != nil {
		return "", false, err
	}
	manifestURL := resolver.registryURL + "/v2/" + repository + "/manifests/" + url.PathEscape(tag)
	request, err := http.NewRequestWithContext(ctx, http.MethodHead, manifestURL, nil)
	if err != nil {
		return "", false, ErrRegistryUnavailable
	}
	request.Header.Set("Accept", manifestAccept)
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := resolver.client.Do(request)
	if err != nil {
		return "", false, ErrRegistryUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		return "", false, nil
	}
	if response.StatusCode != http.StatusOK {
		return "", false, ErrRegistryUnavailable
	}
	digest := strings.TrimSpace(response.Header.Get("Docker-Content-Digest"))
	if !digestPattern.MatchString(digest) {
		return "", false, ErrRegistryUnavailable
	}
	return digest, true, nil
}

func (resolver *GHCRResolver) token(ctx context.Context, repository string) (string, error) {
	tokenURL, err := url.Parse(resolver.tokenURL)
	if err != nil {
		return "", ErrRegistryUnavailable
	}
	query := tokenURL.Query()
	query.Set("service", "ghcr.io")
	query.Set("scope", "repository:"+repository+":pull")
	tokenURL.RawQuery = query.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, tokenURL.String(), nil)
	if err != nil {
		return "", ErrRegistryUnavailable
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "ModemDeck-Updater")
	response, err := resolver.client.Do(request)
	if err != nil {
		return "", ErrRegistryUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", ErrRegistryUnavailable
	}
	var payload struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&payload); err != nil {
		return "", ErrRegistryUnavailable
	}
	token := strings.TrimSpace(payload.Token)
	if token == "" {
		token = strings.TrimSpace(payload.AccessToken)
	}
	if token == "" {
		return "", fmt.Errorf("%w: missing registry token", ErrRegistryUnavailable)
	}
	return token, nil
}
