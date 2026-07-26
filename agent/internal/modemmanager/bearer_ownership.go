package modemmanager

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/godbus/dbus/v5"
)

const bearerOwnershipVersion = 1

type ownedBearer struct {
	LineID        string          `json:"line_id"`
	ProviderEpoch string          `json:"provider_epoch"`
	ModemPath     dbus.ObjectPath `json:"modem_path"`
	BearerPath    dbus.ObjectPath `json:"bearer_path"`
	APN           string          `json:"apn,omitempty"`
	IPFamily      uint32          `json:"ip_family,omitempty"`
}

func (bearer ownedBearer) valid() bool {
	return strings.TrimSpace(bearer.LineID) != "" &&
		strings.TrimSpace(bearer.ProviderEpoch) != "" &&
		bearer.ModemPath.IsValid() &&
		bearer.ModemPath != "/" &&
		bearer.BearerPath.IsValid() &&
		bearer.BearerPath != "/"
}

type bearerOwnershipDocument struct {
	Version int           `json:"version"`
	Bearers []ownedBearer `json:"bearers"`
}

type bearerOwnershipStore struct {
	mu     sync.Mutex
	path   string
	byLine map[string]ownedBearer
}

func newBearerOwnershipStore(path string) (*bearerOwnershipStore, error) {
	store := &bearerOwnershipStore{
		path:   strings.TrimSpace(path),
		byLine: make(map[string]ownedBearer),
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
	document := bearerOwnershipDocument{}
	if err := json.Unmarshal(content, &document); err != nil {
		return nil, fmt.Errorf("decode %s: %w", store.path, err)
	}
	if document.Version != bearerOwnershipVersion {
		return nil, fmt.Errorf(
			"decode %s: unsupported version %d",
			store.path,
			document.Version,
		)
	}
	for _, bearer := range document.Bearers {
		if !bearer.valid() {
			return nil, fmt.Errorf("decode %s: invalid owned bearer entry", store.path)
		}
		if _, duplicate := store.byLine[bearer.LineID]; duplicate {
			return nil, fmt.Errorf(
				"decode %s: duplicate line %q",
				store.path,
				bearer.LineID,
			)
		}
		store.byLine[bearer.LineID] = bearer
	}
	return store, nil
}

func (store *bearerOwnershipStore) get(
	lineID string,
	providerEpoch string,
) (ownedBearer, bool) {
	bearer, found := store.getAny(lineID)
	if !found || bearer.ProviderEpoch != strings.TrimSpace(providerEpoch) {
		return ownedBearer{}, false
	}
	return bearer, true
}

func (store *bearerOwnershipStore) getAny(lineID string) (ownedBearer, bool) {
	if store == nil {
		return ownedBearer{}, false
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	bearer, found := store.byLine[strings.TrimSpace(lineID)]
	return bearer, found
}

func (store *bearerOwnershipStore) list() []ownedBearer {
	if store == nil {
		return nil
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	bearers := make([]ownedBearer, 0, len(store.byLine))
	for _, bearer := range store.byLine {
		bearers = append(bearers, bearer)
	}
	sort.Slice(bearers, func(i, j int) bool {
		return bearers[i].LineID < bearers[j].LineID
	})
	return bearers
}

func (store *bearerOwnershipStore) put(bearer ownedBearer) error {
	if store == nil {
		return errors.New("owned bearer store is unavailable")
	}
	if !bearer.valid() {
		return errors.New("owned bearer entry is invalid")
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	next := cloneOwnedBearers(store.byLine)
	next[bearer.LineID] = bearer
	if err := store.persist(next); err != nil {
		return err
	}
	store.byLine = next
	return nil
}

func (store *bearerOwnershipStore) remove(lineID string) error {
	if store == nil {
		return errors.New("owned bearer store is unavailable")
	}
	lineID = strings.TrimSpace(lineID)
	store.mu.Lock()
	defer store.mu.Unlock()
	if _, found := store.byLine[lineID]; !found {
		return nil
	}
	next := cloneOwnedBearers(store.byLine)
	delete(next, lineID)
	if err := store.persist(next); err != nil {
		return err
	}
	store.byLine = next
	return nil
}

func cloneOwnedBearers(source map[string]ownedBearer) map[string]ownedBearer {
	cloned := make(map[string]ownedBearer, len(source))
	for lineID, bearer := range source {
		cloned[lineID] = bearer
	}
	return cloned
}

func (store *bearerOwnershipStore) persist(bearers map[string]ownedBearer) error {
	if store.path == "" {
		return nil
	}
	document := bearerOwnershipDocument{
		Version: bearerOwnershipVersion,
		Bearers: make([]ownedBearer, 0, len(bearers)),
	}
	for _, bearer := range bearers {
		document.Bearers = append(document.Bearers, bearer)
	}
	sort.Slice(document.Bearers, func(i, j int) bool {
		return document.Bearers[i].LineID < document.Bearers[j].LineID
	})
	content, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return err
	}
	content = append(content, '\n')

	directory := filepath.Dir(store.path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(directory, ".bearers-*.tmp")
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
