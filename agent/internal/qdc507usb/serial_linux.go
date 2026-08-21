//go:build linux

package qdc507usb

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

type serialATClient struct{}

func newSerialATClient() ATClient {
	return serialATClient{}
}

func (serialATClient) Command(
	ctx context.Context,
	port string,
	command string,
	timeout time.Duration,
) (string, error) {
	if ctx == nil {
		return "", fmt.Errorf("serial AT context is required")
	}
	if port == "" || command == "" || len(command) > 512 || strings.ContainsAny(command, "\r\n\x00") {
		return "", fmt.Errorf("serial AT request is invalid")
	}
	bounded, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	fd, err := unix.Open(port, unix.O_RDWR|unix.O_NOCTTY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return "", err
	}
	defer unix.Close(fd)
	termios, err := unix.IoctlGetTermios(fd, unix.TCGETS)
	if err != nil {
		return "", err
	}
	termios.Iflag = unix.IGNPAR
	termios.Oflag = 0
	termios.Cflag = unix.B115200 | unix.CS8 | unix.CLOCAL | unix.CREAD
	termios.Lflag = 0
	termios.Cc[unix.VMIN] = 0
	termios.Cc[unix.VTIME] = 0
	if err := unix.IoctlSetTermios(fd, unix.TCSETS, termios); err != nil {
		return "", err
	}
	if err := unix.IoctlSetInt(fd, unix.TCFLSH, unix.TCIOFLUSH); err != nil {
		return "", err
	}
	request := append([]byte(command), '\r')
	for len(request) > 0 {
		written, writeErr := unix.Write(fd, request)
		if written > 0 {
			request = request[written:]
		}
		if writeErr == nil {
			continue
		}
		if !errors.Is(writeErr, unix.EAGAIN) && !errors.Is(writeErr, unix.EINTR) {
			return "", writeErr
		}
		if err := pollSerial(bounded, fd, unix.POLLOUT); err != nil {
			return "", err
		}
	}

	response := make([]byte, 0, 1024)
	buffer := make([]byte, 1024)
	for {
		read, readErr := unix.Read(fd, buffer)
		if read > 0 {
			response = append(response, buffer[:read]...)
			if len(response) > 32*1024 {
				return string(response), fmt.Errorf("serial AT response exceeded 32 KiB")
			}
			text := string(response)
			if atResponseSucceeded(text) || atResponseIsError(text) {
				return text, nil
			}
		}
		if readErr != nil && !errors.Is(readErr, unix.EAGAIN) && !errors.Is(readErr, unix.EINTR) {
			return string(response), readErr
		}
		if err := pollSerial(bounded, fd, unix.POLLIN); err != nil {
			return string(response), err
		}
	}
}

func pollSerial(ctx context.Context, fd int, events int16) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		pollFDs := []unix.PollFd{{Fd: int32(fd), Events: events}}
		count, err := unix.Poll(pollFDs, 100)
		if err != nil {
			if errors.Is(err, unix.EINTR) {
				continue
			}
			return err
		}
		if count == 0 {
			continue
		}
		if pollFDs[0].Revents&(unix.POLLERR|unix.POLLHUP|unix.POLLNVAL) != 0 {
			return fmt.Errorf("serial AT port disconnected")
		}
		if pollFDs[0].Revents&events != 0 {
			return nil
		}
	}
}

var _ ATClient = serialATClient{}
