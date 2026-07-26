//go:build linux

package networking

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

var managedIPv6Sysctls = []string{"accept_ra", "autoconf"}

type sysctlController interface {
	Read(string, string) (int, error)
	Write(string, string, int) error
}

type procSysctl struct {
	root string
}

func newProcSysctl() sysctlController {
	return procSysctl{root: "/proc/sys/net/ipv6/conf"}
}

func (controller procSysctl) Read(interfaceName string, name string) (int, error) {
	path, err := controller.path(interfaceName, name)
	if err != nil {
		return 0, err
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return 0, fmt.Errorf("read %s: %w", path, err)
	}
	value, err := strconv.Atoi(strings.TrimSpace(string(content)))
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", path, err)
	}
	return value, nil
}

func (controller procSysctl) Write(
	interfaceName string,
	name string,
	value int,
) error {
	path, err := controller.path(interfaceName, name)
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(strconv.Itoa(value)+"\n"), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func (controller procSysctl) path(
	interfaceName string,
	name string,
) (string, error) {
	if err := validateInterfaceName(interfaceName); err != nil {
		return "", err
	}
	if !isManagedIPv6Sysctl(name) {
		return "", fmt.Errorf("IPv6 sysctl %q is not managed", name)
	}
	if strings.TrimSpace(controller.root) == "" {
		return "", errors.New("sysctl root is required")
	}
	return filepath.Join(controller.root, interfaceName, name), nil
}

func isManagedIPv6Sysctl(name string) bool {
	for _, candidate := range managedIPv6Sysctls {
		if candidate == name {
			return true
		}
	}
	return false
}
