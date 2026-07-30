package main

import (
	"errors"
	"testing"

	"github.com/human-agent65535/modemdeck/internal/httpapi"
	"github.com/human-agent65535/modemdeck/internal/tlsmanager"
)

func TestParseCommaSeparatedList(t *testing.T) {
	t.Parallel()
	got := parseCommaSeparatedList(
		" modem.example.test, 192.0.2.10, ,2001:db8::10 ",
	)
	want := []string{
		"modem.example.test",
		"192.0.2.10",
		"2001:db8::10",
	}
	if len(got) != len(want) {
		t.Fatalf("parseCommaSeparatedList() = %v, want %v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("parseCommaSeparatedList() = %v, want %v", got, want)
		}
	}
}

func TestTLSSettingsServiceMapsAutomaticStatus(t *testing.T) {
	t.Parallel()
	manager, err := tlsmanager.Open(tlsmanager.Config{
		Directory: t.TempDir(),
		Hosts:     []string{"modem.example.test", "192.0.2.10"},
	})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	service := tlsSettingsService{manager: manager}
	status := service.Status()
	if status.Mode != "automatic" ||
		status.Subject == "" ||
		status.Issuer == "" ||
		status.NotBefore == "" ||
		status.NotAfter == "" ||
		status.FingerprintSHA256 == "" ||
		!status.RenewsAutomatically ||
		status.Expired {
		t.Fatalf("Status() = %+v", status)
	}
	if len(status.DNSNames) == 0 || len(status.IPAddresses) == 0 {
		t.Fatalf("Status() SANs = DNS %v, IP %v", status.DNSNames, status.IPAddresses)
	}
	certificate, err := service.AutomaticCAPEM()
	if err != nil || len(certificate) == 0 {
		t.Fatalf("AutomaticCAPEM() = %d bytes, %v", len(certificate), err)
	}
}

func TestTLSSettingsServiceMapsInvalidUserMaterial(t *testing.T) {
	t.Parallel()
	manager, err := tlsmanager.Open(tlsmanager.Config{
		Directory: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	service := tlsSettingsService{manager: manager}
	_, err = service.InstallUser([]byte("not a certificate"), []byte("not a key"))
	if !errors.Is(err, httpapi.ErrTLSSettingsInvalidInput) {
		t.Fatalf(
			"InstallUser() error = %v, want ErrTLSSettingsInvalidInput",
			err,
		)
	}
}
