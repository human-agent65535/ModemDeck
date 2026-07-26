//go:build linux

package usbrecovery

import (
	"context"
	"os"

	"golang.org/x/sys/unix"
)

// USBDEVFS_RESET is _IO('U', 20) from linux/usbdevice_fs.h. x/sys does not
// currently export the request number.
const usbdevfsReset = 0x5514

func deviceWritable(path string) error {
	device, err := os.OpenFile(path, os.O_RDWR|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	return device.Close()
}

func resetUSBDevice(ctx context.Context, path string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	device, err := os.OpenFile(path, os.O_RDWR|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	defer device.Close()
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, device.Fd(), usbdevfsReset, 0)
	if errno != 0 {
		return errno
	}
	return ctx.Err()
}
