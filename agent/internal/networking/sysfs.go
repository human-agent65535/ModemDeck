package networking

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const defaultSysfsNetworkRoot = "/sys/class/net"

type InterfaceCounters struct {
	RXBytes uint64
	TXBytes uint64
}

type InterfaceStatsReader interface {
	Read(string) (InterfaceCounters, error)
}

type SysfsStatsReader struct {
	Root string
}

func (reader SysfsStatsReader) Read(interfaceName string) (InterfaceCounters, error) {
	if err := validateInterfaceName(interfaceName); err != nil {
		return InterfaceCounters{}, err
	}
	root := reader.Root
	if root == "" {
		root = defaultSysfsNetworkRoot
	}
	rx, err := readUint64File(
		filepath.Join(root, interfaceName, "statistics", "rx_bytes"),
	)
	if err != nil {
		return InterfaceCounters{}, fmt.Errorf("read rx_bytes for %s: %w", interfaceName, err)
	}
	tx, err := readUint64File(
		filepath.Join(root, interfaceName, "statistics", "tx_bytes"),
	)
	if err != nil {
		return InterfaceCounters{}, fmt.Errorf("read tx_bytes for %s: %w", interfaceName, err)
	}
	return InterfaceCounters{RXBytes: rx, TXBytes: tx}, nil
}

func validateInterfaceName(value string) error {
	if value == "" || len(value) > 15 {
		return fmt.Errorf("interface must be 1-15 characters")
	}
	for _, character := range value {
		if character >= 'a' && character <= 'z' ||
			character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' ||
			character == '_' || character == '-' ||
			character == '.' || character == ':' {
			continue
		}
		return fmt.Errorf("interface %q contains unsupported characters", value)
	}
	if value == "." || value == ".." {
		return fmt.Errorf("interface %q is invalid", value)
	}
	return nil
}

func readUint64File(path string) (uint64, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	value, err := strconv.ParseUint(strings.TrimSpace(string(content)), 10, 64)
	if err != nil {
		return 0, err
	}
	return value, nil
}
