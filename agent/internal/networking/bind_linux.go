//go:build linux

package networking

import "syscall"

func bindSocketToInterface(fileDescriptor uintptr, interfaceName string) error {
	return syscall.SetsockoptString(
		int(fileDescriptor),
		syscall.SOL_SOCKET,
		syscall.SO_BINDTODEVICE,
		interfaceName,
	)
}
