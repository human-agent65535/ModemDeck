//go:build !linux

package qdc507usb

import (
	"context"
	"fmt"
	"time"
)

type unsupportedSerialATClient struct{}

func newSerialATClient() ATClient {
	return unsupportedSerialATClient{}
}

func (unsupportedSerialATClient) Command(
	context.Context,
	string,
	string,
	time.Duration,
) (string, error) {
	return "", fmt.Errorf("direct QDC507 serial provisioning requires Linux")
}

var _ ATClient = unsupportedSerialATClient{}
