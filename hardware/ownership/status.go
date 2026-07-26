package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

type ownerStatus struct {
	Ready       bool               `json:"ready"`
	UpdatedAt   time.Time          `json:"updated_at"`
	Assignments []assignmentStatus `json:"assignments"`
}

type assignmentStatus struct {
	ID           string       `json:"id"`
	State        string       `json:"state"`
	Required     bool         `json:"required_at_startup"`
	SysfsPath    string       `json:"sysfs_path,omitempty"`
	USBSerial    string       `json:"usb_serial,omitempty"`
	USBVendorID  string       `json:"usb_vendor_id,omitempty"`
	USBProductID string       `json:"usb_product_id,omitempty"`
	Ports        []kernelPort `json:"ports,omitempty"`
	Detail       string       `json:"detail,omitempty"`
}

func writeOwnerStatus(path string, status ownerStatus) error {
	status.UpdatedAt = status.UpdatedAt.UTC()
	parent := filepath.Dir(path)
	if err := os.MkdirAll(parent, 0o770); err != nil {
		return fmt.Errorf("create owner status directory: %w", err)
	}

	file, err := os.CreateTemp(parent, ".device-owner-status-*")
	if err != nil {
		return fmt.Errorf("create owner status staging file: %w", err)
	}
	stagingPath := file.Name()
	defer os.Remove(stagingPath)

	encoder := json.NewEncoder(file)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(status); err != nil {
		file.Close()
		return fmt.Errorf("encode owner status: %w", err)
	}
	if err := file.Chmod(0o640); err != nil {
		file.Close()
		return fmt.Errorf("set owner status permissions: %w", err)
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return fmt.Errorf("sync owner status: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close owner status staging file: %w", err)
	}
	if err := os.Rename(stagingPath, path); err != nil {
		return fmt.Errorf("publish owner status: %w", err)
	}
	return nil
}

func checkOwnerStatus(path string, maxAge time.Duration, now time.Time) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open device owner status: %w", err)
	}
	defer file.Close()

	decoder := json.NewDecoder(io.LimitReader(file, 1<<20))
	decoder.DisallowUnknownFields()
	var status ownerStatus
	if err := decoder.Decode(&status); err != nil {
		return fmt.Errorf("decode device owner status: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("device owner status contains trailing data")
	}
	if !status.Ready {
		return errors.New("device owner has not completed initial reconciliation")
	}
	if status.UpdatedAt.IsZero() {
		return errors.New("device owner status has no update timestamp")
	}
	age := now.Sub(status.UpdatedAt)
	if age < -time.Second || age > maxAge {
		return fmt.Errorf("device owner status is stale by %s", age.Round(time.Second))
	}
	return nil
}
