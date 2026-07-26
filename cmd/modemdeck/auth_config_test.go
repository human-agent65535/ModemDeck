package main

import (
	"testing"
)

func TestEnvironmentBool(t *testing.T) {
	t.Setenv("MODEMDECK_TEST_BOOL", "yes")
	value, err := environmentBool("MODEMDECK_TEST_BOOL", false)
	if err != nil || !value {
		t.Fatalf("environmentBool() = %v, %v", value, err)
	}
	t.Setenv("MODEMDECK_TEST_BOOL", "invalid")
	if _, err := environmentBool("MODEMDECK_TEST_BOOL", false); err == nil {
		t.Fatal("environmentBool() error = nil, want invalid value error")
	}
}
