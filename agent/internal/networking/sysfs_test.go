package networking

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSysfsStatsReaderReadsAbsoluteCounters(t *testing.T) {
	root := t.TempDir()
	statistics := filepath.Join(root, "wwan0", "statistics")
	if err := os.MkdirAll(statistics, 0o755); err != nil {
		t.Fatalf("create statistics directory: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(statistics, "rx_bytes"),
		[]byte("123456\n"),
		0o644,
	); err != nil {
		t.Fatalf("write rx_bytes: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(statistics, "tx_bytes"),
		[]byte("654321\n"),
		0o644,
	); err != nil {
		t.Fatalf("write tx_bytes: %v", err)
	}

	counters, err := (SysfsStatsReader{Root: root}).Read("wwan0")
	if err != nil {
		t.Fatalf("read counters: %v", err)
	}
	if counters.RXBytes != 123456 || counters.TXBytes != 654321 {
		t.Fatalf("unexpected counters: %+v", counters)
	}
}

func TestSysfsStatsReaderRejectsInterfaceTraversal(t *testing.T) {
	for _, value := range []string{"", "..", "../eth0", "wwan0/statistics", "interface-name-too-long"} {
		if _, err := (SysfsStatsReader{Root: t.TempDir()}).Read(value); err == nil {
			t.Fatalf("Read(%q) succeeded", value)
		}
	}
}
