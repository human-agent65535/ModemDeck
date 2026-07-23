package unixsocket

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

type Config struct {
	Path string
	Mode os.FileMode
	UID  int
	GID  int
}

func Listen(config Config) (*net.UnixListener, error) {
	if config.Path == "" || !filepath.IsAbs(config.Path) {
		return nil, errors.New("socket path must be absolute")
	}
	if config.UID < -1 || config.GID < -1 {
		return nil, errors.New("socket uid and gid must be -1 or non-negative")
	}
	if err := os.MkdirAll(filepath.Dir(config.Path), 0o755); err != nil {
		return nil, fmt.Errorf("create socket directory: %w", err)
	}
	if err := removeStaleSocket(config.Path); err != nil {
		return nil, err
	}

	address, err := net.ResolveUnixAddr("unix", config.Path)
	if err != nil {
		return nil, fmt.Errorf("resolve unix socket: %w", err)
	}
	listener, err := net.ListenUnix("unix", address)
	if err != nil {
		return nil, fmt.Errorf("listen on unix socket: %w", err)
	}
	listener.SetUnlinkOnClose(true)

	if err := os.Chmod(config.Path, config.Mode.Perm()); err != nil {
		_ = listener.Close()
		return nil, fmt.Errorf("set socket mode: %w", err)
	}
	if config.UID != -1 || config.GID != -1 {
		if err := os.Chown(config.Path, config.UID, config.GID); err != nil {
			_ = listener.Close()
			return nil, fmt.Errorf("set socket ownership: %w", err)
		}
	}
	return listener, nil
}

func removeStaleSocket(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect socket path: %w", err)
	}
	if info.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("socket path exists and is not a unix socket: %s", path)
	}

	connection, dialErr := net.DialTimeout("unix", path, 200*time.Millisecond)
	if dialErr == nil {
		_ = connection.Close()
		return fmt.Errorf("unix socket is already active: %s", path)
	}
	if !errors.Is(dialErr, syscall.ECONNREFUSED) && !errors.Is(dialErr, os.ErrNotExist) {
		return fmt.Errorf("cannot verify existing unix socket: %w", dialErr)
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove stale unix socket: %w", err)
	}
	return nil
}
