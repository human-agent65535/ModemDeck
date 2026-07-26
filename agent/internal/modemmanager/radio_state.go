package modemmanager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/human-agent65535/modemdeck/agent/internal/domain"
)

const radioStateVersion = 1

type radioStateDocument struct {
	Version       int      `json:"version"`
	DisabledLines []string `json:"disabled_lines"`
}

type radioStateStore struct {
	mu       sync.Mutex
	path     string
	disabled map[string]struct{}
}

func newRadioStateStore(path string) (*radioStateStore, error) {
	store := &radioStateStore{
		path:     strings.TrimSpace(path),
		disabled: make(map[string]struct{}),
	}
	if store.path == "" {
		return store, nil
	}
	content, err := os.ReadFile(store.path)
	if errors.Is(err, os.ErrNotExist) {
		return store, nil
	}
	if err != nil {
		return nil, err
	}
	document := radioStateDocument{}
	if err := json.Unmarshal(content, &document); err != nil {
		return nil, fmt.Errorf("decode %s: %w", store.path, err)
	}
	if document.Version != radioStateVersion {
		return nil, fmt.Errorf(
			"decode %s: unsupported version %d",
			store.path,
			document.Version,
		)
	}
	for _, lineID := range document.DisabledLines {
		lineID = strings.TrimSpace(lineID)
		if lineID == "" {
			return nil, fmt.Errorf("decode %s: disabled line id is empty", store.path)
		}
		if _, duplicate := store.disabled[lineID]; duplicate {
			return nil, fmt.Errorf("decode %s: duplicate disabled line %q", store.path, lineID)
		}
		store.disabled[lineID] = struct{}{}
	}
	return store, nil
}

func (store *radioStateStore) enabled(lineID string) bool {
	if store == nil {
		return true
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	_, disabled := store.disabled[strings.TrimSpace(lineID)]
	return !disabled
}

func (store *radioStateStore) setEnabled(lineID string, enabled bool) error {
	if store == nil {
		return errors.New("radio state store is unavailable")
	}
	lineID = strings.TrimSpace(lineID)
	if lineID == "" {
		return errors.New("radio state line id is empty")
	}

	store.mu.Lock()
	defer store.mu.Unlock()
	next := cloneDisabledLines(store.disabled)
	if enabled {
		delete(next, lineID)
	} else {
		next[lineID] = struct{}{}
	}
	if err := store.persist(next); err != nil {
		return err
	}
	store.disabled = next
	return nil
}

func cloneDisabledLines(source map[string]struct{}) map[string]struct{} {
	cloned := make(map[string]struct{}, len(source))
	for lineID := range source {
		cloned[lineID] = struct{}{}
	}
	return cloned
}

func (store *radioStateStore) persist(disabled map[string]struct{}) error {
	if store.path == "" {
		return nil
	}
	document := radioStateDocument{
		Version:       radioStateVersion,
		DisabledLines: make([]string, 0, len(disabled)),
	}
	for lineID := range disabled {
		document.DisabledLines = append(document.DisabledLines, lineID)
	}
	sort.Strings(document.DisabledLines)
	content, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return err
	}
	content = append(content, '\n')

	directory := filepath.Dir(store.path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(directory, ".radio-state-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(content); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, store.path); err != nil {
		return err
	}
	directoryHandle, err := os.Open(directory)
	if err != nil {
		return err
	}
	defer directoryHandle.Close()
	return directoryHandle.Sync()
}

// ReconcileRadioState applies the persisted user intent to every currently
// discovered modem. New stable lines default to enabled.
func (p *Provider) ReconcileRadioState(ctx context.Context) error {
	const operation = "reconcile_radio_state"
	if ctx == nil {
		return domain.InvalidArgument(operation, "request context is required")
	}

	p.configMu.Lock()
	defer p.configMu.Unlock()

	identity, err := p.resolveProviderIdentity(ctx, operation)
	if err != nil {
		return err
	}
	objects, err := p.managedObjects(ctx, operation)
	if err != nil {
		return err
	}
	parsed := ParseManagedObjects(objects, identity)
	sort.Slice(parsed.Lines, func(i, j int) bool {
		return parsed.Lines[i].ID < parsed.Lines[j].ID
	})

	var failures []error
	for _, line := range parsed.Lines {
		if line.ID == "" {
			continue
		}
		desiredEnabled := p.radioStates.enabled(line.ID)
		currentEnabled, currentKnown := modemEnabled(line.StateCode)
		if currentKnown && currentEnabled == desiredEnabled {
			continue
		}
		if desiredEnabled && line.StateCode != modemStateDisabled {
			continue
		}
		if !desiredEnabled && line.StateCode < modemStateEnabled {
			continue
		}
		modemPath, found := parsed.LinePaths[line.ID]
		if !found {
			failures = append(
				failures,
				fmt.Errorf("line %s has no ModemManager path", line.ID),
			)
			continue
		}
		if _, err := p.call(
			ctx,
			modemPath,
			modemInterface+".Enable",
			operation,
			"ModemManager failed to reconcile the modem radio state",
			desiredEnabled,
		); err != nil {
			failures = append(failures, fmt.Errorf("line %s: %w", line.ID, err))
			continue
		}
		slog.Info(
			"modem radio state reconciled",
			"component", "modemmanager",
			"line_id", line.ID,
			"enabled", desiredEnabled,
		)
	}
	return errors.Join(failures...)
}
