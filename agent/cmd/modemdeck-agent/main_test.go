package main

import "testing"

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
