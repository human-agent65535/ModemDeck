//go:build !linux

package usbrecovery

import (
	"context"
	"errors"
)

func deviceWritable(string) error {
	return errors.New("USB reset is only supported on Linux")
}

func resetUSBDevice(context.Context, string) error {
	return errors.New("USB reset is only supported on Linux")
}
