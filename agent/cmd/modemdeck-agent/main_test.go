package main

import (
	"os"
	"strings"
	"testing"
)

func TestParseSocketMode(t *testing.T) {
	mode, err := parseSocketMode("0660")
	if err != nil {
		t.Fatalf("parse mode: %v", err)
	}
	if mode.Perm() != 0o660 {
		t.Fatalf("mode = %04o", mode.Perm())
	}

	for _, value := range []string{"", "888", "1000", "-1"} {
		if _, err := parseSocketMode(value); err == nil {
			t.Fatalf("parseSocketMode(%q) succeeded", value)
		}
	}
}

func TestSystemdNetworkSandboxIsNarrow(t *testing.T) {
	content, err := os.ReadFile("../../../packaging/systemd/modemdeck-agent.service")
	if err != nil {
		t.Fatalf("read systemd service: %v", err)
	}
	service := string(content)
	for _, required := range []string{
		"CapabilityBoundingSet=CAP_NET_RAW",
		"AmbientCapabilities=CAP_NET_RAW",
		"RestrictAddressFamilies=AF_UNIX AF_NETLINK AF_INET AF_INET6",
		"NoNewPrivileges=yes",
	} {
		if !strings.Contains(service, required) {
			t.Fatalf("systemd service is missing %q", required)
		}
	}
	for _, forbidden := range []string{
		"CAP_NET_ADMIN",
		"Privileged=yes",
	} {
		if strings.Contains(service, forbidden) {
			t.Fatalf("systemd service contains forbidden permission %q", forbidden)
		}
	}
}
