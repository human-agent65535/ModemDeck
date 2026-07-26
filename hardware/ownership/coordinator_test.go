package main

import (
	"context"
	"errors"
	"testing"
	"time"
)

type scannerStub struct {
	resolutions [][]resolution
	index       int
	readyError  error
}

func (scanner *scannerStub) resolve([]assignment) ([]resolution, error) {
	index := scanner.index
	if index >= len(scanner.resolutions) {
		index = len(scanner.resolutions) - 1
	}
	result := scanner.resolutions[index]
	scanner.index++
	return result, nil
}

func (scanner *scannerStub) checkPortReady(kernelPort) error {
	return scanner.readyError
}

type guardStub struct {
	err    error
	checks int
}

func (guard *guardStub) Check(context.Context, []physicalDevice) error {
	guard.checks++
	return guard.err
}

type reporterStub struct {
	events []kernelEvent
}

func (reporter *reporterStub) Report(_ context.Context, event kernelEvent) error {
	reporter.events = append(reporter.events, event)
	return nil
}

func TestCoordinatorReportsRealPortAddAndRemove(t *testing.T) {
	item := assignment{ID: "rack-a", RequiredAtStartup: true}
	device := physicalDevice{
		SysfsPath: "/sys/devices/example",
		Ports: []kernelPort{{
			Subsystem: "tty",
			Name:      "ttyUSB2",
			DevNode:   "/dev/ttyUSB2",
			UDevKey:   "c188:2",
		}},
	}
	scanner := &scannerStub{resolutions: [][]resolution{
		{{Assignment: item, Device: &device}},
		{{Assignment: item, Device: nil}},
	}}
	guard := &guardStub{}
	reporter := &reporterStub{}
	coordinator := newCoordinator(
		config{
			Assignments:     []assignment{item},
			settleDuration:  0,
			portReadyWindow: time.Second,
			pollInterval:    time.Second,
		},
		scanner,
		guard,
		reporter,
		t.TempDir()+"/status.json",
		time.Second,
	)

	statuses, complete, err := coordinator.reconcile(context.Background(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !complete || len(statuses) != 1 || statuses[0].State != "active" {
		t.Fatalf("complete=%v statuses=%+v", complete, statuses)
	}
	_, _, err = coordinator.reconcile(context.Background(), time.Now().Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}

	if len(reporter.events) != 2 {
		t.Fatalf("events = %+v", reporter.events)
	}
	if reporter.events[0] != (kernelEvent{
		Action: "add", Subsystem: "tty", Name: "ttyUSB2", UID: "modemdeck:rack-a",
	}) {
		t.Fatalf("add event = %+v", reporter.events[0])
	}
	if reporter.events[1] != (kernelEvent{
		Action: "remove", Subsystem: "tty", Name: "ttyUSB2", UID: "modemdeck:rack-a",
	}) {
		t.Fatalf("remove event = %+v", reporter.events[1])
	}
	if guard.checks != 2 {
		t.Fatalf("guard checks = %d", guard.checks)
	}
}

func TestCoordinatorFailsClosedOnOwnershipGuardError(t *testing.T) {
	item := assignment{ID: "rack-a"}
	device := physicalDevice{SysfsPath: "/sys/devices/example"}
	coordinator := newCoordinator(
		config{
			Assignments:     []assignment{item},
			settleDuration:  0,
			portReadyWindow: time.Second,
			pollInterval:    time.Second,
		},
		&scannerStub{resolutions: [][]resolution{{{
			Assignment: item,
			Device:     &device,
		}}}},
		&guardStub{err: errors.New("host owns port")},
		&reporterStub{},
		t.TempDir()+"/status.json",
		time.Second,
	)

	_, _, err := coordinator.reconcile(context.Background(), time.Now())
	if err == nil {
		t.Fatal("expected ownership guard failure")
	}
}

func TestOptionalAbsentAssignmentAllowsHealthyStartup(t *testing.T) {
	item := assignment{ID: "hotplug", RequiredAtStartup: false}
	coordinator := newCoordinator(
		config{
			Assignments:     []assignment{item},
			settleDuration:  0,
			portReadyWindow: time.Second,
			pollInterval:    time.Second,
		},
		&scannerStub{resolutions: [][]resolution{{{
			Assignment: item,
		}}}},
		&guardStub{},
		&reporterStub{},
		t.TempDir()+"/status.json",
		time.Second,
	)

	statuses, complete, err := coordinator.reconcile(context.Background(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !complete || len(statuses) != 1 || statuses[0].State != "absent" {
		t.Fatalf("complete=%v statuses=%+v", complete, statuses)
	}
}

func TestOptionalAssignmentReportsRuntimeHotplug(t *testing.T) {
	item := assignment{ID: "hotplug", RequiredAtStartup: false}
	device := physicalDevice{
		SysfsPath: "/sys/devices/hotplug",
		Ports: []kernelPort{{
			Subsystem: "usbmisc",
			Name:      "cdc-wdm9",
			DevNode:   "/dev/cdc-wdm9",
			UDevKey:   "c180:9",
		}},
	}
	reporter := &reporterStub{}
	coordinator := newCoordinator(
		config{
			Assignments:     []assignment{item},
			settleDuration:  0,
			portReadyWindow: time.Second,
			pollInterval:    time.Second,
		},
		&scannerStub{resolutions: [][]resolution{
			{{Assignment: item}},
			{{Assignment: item, Device: &device}},
		}},
		&guardStub{},
		reporter,
		t.TempDir()+"/status.json",
		time.Second,
	)

	statuses, complete, err := coordinator.reconcile(context.Background(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !complete || statuses[0].State != "absent" {
		t.Fatalf("complete=%v statuses=%+v", complete, statuses)
	}

	statuses, complete, err = coordinator.reconcile(
		context.Background(),
		time.Now().Add(time.Second),
	)
	if err != nil {
		t.Fatal(err)
	}
	if !complete || statuses[0].State != "active" {
		t.Fatalf("complete=%v statuses=%+v", complete, statuses)
	}
	if len(reporter.events) != 1 || reporter.events[0] != (kernelEvent{
		Action:    "add",
		Subsystem: "usbmisc",
		Name:      "cdc-wdm9",
		UID:       "modemdeck:hotplug",
	}) {
		t.Fatalf("events = %+v", reporter.events)
	}
}
