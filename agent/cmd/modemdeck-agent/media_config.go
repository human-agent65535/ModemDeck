package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/human-agent65535/modemdeck/agent/internal/media"
)

const (
	maxMediaBindingsFileBytes = 64 << 10
	maxMediaBindings          = 64
)

type mediaBindingsConfig struct {
	Bindings []media.Binding `json:"bindings"`
}

func loadMediaBindings(path string) ([]media.Binding, error) {
	if path == "" {
		return []media.Binding{}, nil
	}
	path = filepath.Clean(path)
	if !filepath.IsAbs(path) {
		return nil, errors.New("media bindings file path must be absolute")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open media bindings file: %w", err)
	}
	defer file.Close()
	contents, err := io.ReadAll(io.LimitReader(file, maxMediaBindingsFileBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read media bindings file: %w", err)
	}
	if len(contents) > maxMediaBindingsFileBytes {
		return nil, fmt.Errorf(
			"media bindings file exceeds %d bytes",
			maxMediaBindingsFileBytes,
		)
	}
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	var config mediaBindingsConfig
	if err := decoder.Decode(&config); err != nil {
		return nil, fmt.Errorf("decode media bindings file: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, errors.New("media bindings file must contain one JSON object")
	}
	if len(config.Bindings) > maxMediaBindings {
		return nil, fmt.Errorf("media bindings file contains more than %d bindings", maxMediaBindings)
	}
	return append([]media.Binding(nil), config.Bindings...), nil
}

func configureMediaBackends(
	bindings []media.Binding,
) (map[media.BackendKind]media.Backend, error) {
	backends := map[media.BackendKind]media.Backend{
		media.BackendCharPCM: media.NewCharPCMBackend(nil),
	}
	requiresALSA := false
	for _, binding := range bindings {
		if binding.Backend == media.BackendALSAPCM {
			requiresALSA = true
			break
		}
	}
	if !requiresALSA {
		return backends, nil
	}
	opener, err := media.NewALSACommandOpener()
	if err != nil {
		return nil, err
	}
	backends[media.BackendALSAPCM] = media.NewALSAPCMBackend(opener)
	return backends, nil
}
