package tlsmanager

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	sourceFilename          = "source"
	automaticCAFilename     = "automatic-ca.pem"
	automaticServerFilename = "automatic-server.pem"
	userBundleFilename      = "user.pem"
)

func ensureStorageDirectory(directory string) error {
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create TLS storage directory: %w", err)
	}
	info, err := os.Lstat(directory)
	if err != nil {
		return fmt.Errorf("inspect TLS storage directory: %w", err)
	}
	if !info.IsDir() {
		return errors.New("TLS storage path is not a directory")
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return errors.New("TLS storage directory must not be a symbolic link")
	}
	if err := os.Chmod(directory, 0o700); err != nil {
		return fmt.Errorf("secure TLS storage directory: %w", err)
	}
	return nil
}

func readSource(directory string) (Source, bool, error) {
	content, found, err := readOptionalRegularFile(
		filepath.Join(directory, sourceFilename),
	)
	if err != nil || !found {
		return "", found, err
	}
	switch Source(strings.TrimSpace(string(content))) {
	case SourceAutomatic:
		return SourceAutomatic, true, nil
	case SourceUser:
		return SourceUser, true, nil
	default:
		return "", true, errors.New("TLS certificate source metadata is invalid")
	}
}

func writeSource(directory string, source Source) error {
	switch source {
	case SourceAutomatic, SourceUser:
	default:
		return errors.New("TLS certificate source is invalid")
	}
	return writeAtomic(
		filepath.Join(directory, sourceFilename),
		[]byte(string(source)+"\n"),
	)
}

func readOptionalRegularFile(path string) ([]byte, bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("inspect %s: %w", filepath.Base(path), err)
	}
	if !info.Mode().IsRegular() {
		return nil, false, fmt.Errorf("%s is not a regular file", filepath.Base(path))
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, false, fmt.Errorf("read %s: %w", filepath.Base(path), err)
	}
	return content, true, nil
}

func writeAtomic(path string, content []byte) error {
	directory := filepath.Dir(path)
	file, err := os.CreateTemp(directory, "."+filepath.Base(path)+".tmp-")
	if err != nil {
		return fmt.Errorf("create temporary %s: %w", filepath.Base(path), err)
	}
	temporaryPath := file.Name()
	committed := false
	defer func() {
		_ = file.Close()
		if !committed {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := file.Chmod(0o600); err != nil {
		return fmt.Errorf("secure temporary %s: %w", filepath.Base(path), err)
	}
	if _, err := file.Write(content); err != nil {
		return fmt.Errorf("write temporary %s: %w", filepath.Base(path), err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync temporary %s: %w", filepath.Base(path), err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close temporary %s: %w", filepath.Base(path), err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace %s: %w", filepath.Base(path), err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("secure %s: %w", filepath.Base(path), err)
	}
	directoryHandle, err := os.Open(directory)
	if err != nil {
		return fmt.Errorf("open TLS storage directory for sync: %w", err)
	}
	syncErr := directoryHandle.Sync()
	closeErr := directoryHandle.Close()
	if syncErr != nil {
		return fmt.Errorf("sync TLS storage directory: %w", syncErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close TLS storage directory: %w", closeErr)
	}
	committed = true
	return nil
}
