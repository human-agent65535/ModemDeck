package main

import (
	"strings"
	"testing"
)

func TestResolveAssignmentsRejectsAmbiguousUSBMatch(t *testing.T) {
	assignments := []assignment{{
		ID: "rack-a",
		Match: matchSpec{USB: &usbMatch{
			VendorID:  "2c7c",
			ProductID: "0125",
			Serial:    "duplicate",
		}},
	}}
	devices := []physicalDevice{
		{
			SysfsPath: "/sys/devices/usb1/1-1",
			VendorID:  "2c7c",
			ProductID: "0125",
			Serial:    "duplicate",
		},
		{
			SysfsPath: "/sys/devices/usb1/1-2",
			VendorID:  "2c7c",
			ProductID: "0125",
			Serial:    "duplicate",
		},
	}

	_, err := resolveAssignments(assignments, "/sys", devices, nil)
	if err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("error = %v", err)
	}
}

func TestResolveAssignmentsRejectsDuplicatePhysicalOwnership(t *testing.T) {
	device := physicalDevice{SysfsPath: "/sys/devices/example"}
	assignments := []assignment{
		{ID: "rack-a", Match: matchSpec{SysfsPath: device.SysfsPath}},
		{ID: "rack-b", Match: matchSpec{SysfsPath: device.SysfsPath}},
	}
	physicalAt := func(string) (*physicalDevice, error) {
		copy := device
		return &copy, nil
	}

	_, err := resolveAssignments(assignments, "/sys", nil, physicalAt)
	if err == nil || !strings.Contains(err.Error(), "both match physical device") {
		t.Fatalf("error = %v", err)
	}
}

func TestAssignmentUIDIsStableAndIndependentOfKernelOrdinal(t *testing.T) {
	if got := assignmentUID("rack-a"); got != "modemdeck:rack-a" {
		t.Fatalf("uid = %q", got)
	}
}
