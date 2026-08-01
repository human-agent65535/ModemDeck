package ota

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/human-agent65535/modemdeck/internal/updatecheck"
)

type FileOperationStore struct {
	path string
}

func NewFileOperationStore(directory string) *FileOperationStore {
	return &FileOperationStore{path: filepath.Join(directory, "operation.json")}
}

func (store *FileOperationStore) Load() (*updatecheck.Operation, error) {
	contents, err := os.ReadFile(store.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read update operation: %w", err)
	}
	var operation updatecheck.Operation
	if err := json.Unmarshal(contents, &operation); err != nil {
		return nil, fmt.Errorf("decode update operation: %w", err)
	}
	return &operation, nil
}

func (store *FileOperationStore) Save(operation updatecheck.Operation) error {
	if err := os.MkdirAll(filepath.Dir(store.path), 0o700); err != nil {
		return fmt.Errorf("create updater state directory: %w", err)
	}
	contents, err := json.Marshal(operation)
	if err != nil {
		return fmt.Errorf("encode update operation: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(store.path), ".operation-*.json")
	if err != nil {
		return fmt.Errorf("create update operation: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err == nil {
		_, err = temporary.Write(contents)
	}
	if err == nil {
		err = temporary.Sync()
	}
	closeErr := temporary.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("write update operation: %w", err)
	}
	if err := os.Rename(temporaryPath, store.path); err != nil {
		return fmt.Errorf("commit update operation: %w", err)
	}
	return nil
}
