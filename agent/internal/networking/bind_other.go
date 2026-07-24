//go:build !linux

package networking

import "fmt"

func bindSocketToInterface(_ uintptr, _ string) error {
	return fmt.Errorf("SO_BINDTODEVICE is available only on Linux")
}
