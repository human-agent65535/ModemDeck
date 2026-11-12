package identity

import (
	"bytes"
	"regexp"
	"testing"
)

func TestNewKeepsThePrefixAndAdds128RandomBits(t *testing.T) {
	form := regexp.MustCompile(`^user_[0-9a-f]{32}$`)
	first, err := New("user_")
	if err != nil || !form.MatchString(first) {
		t.Fatalf("New() = %q, %v", first, err)
	}
	second, err := New("user_")
	if err != nil || second == first {
		t.Fatalf("second New() = %q, %v; want a different identifier", second, err)
	}
	if bare, err := New(""); err != nil || len(bare) != 32 {
		t.Fatalf("New(\"\") = %q, %v", bare, err)
	}
}

func TestNewFromUsesTheSuppliedSource(t *testing.T) {
	got, err := NewFrom(bytes.NewReader(bytes.Repeat([]byte{0xab}, 16)), "proxy_")
	if err != nil || got != "proxy_abababababababababababababababab" {
		t.Fatalf("NewFrom() = %q, %v", got, err)
	}
	if got, err := NewFrom(bytes.NewReader([]byte{1, 2, 3}), "proxy_"); err == nil || got != "" {
		t.Fatalf("NewFrom() with a short source = %q, %v; want a failure", got, err)
	}
}

func TestNewLineIDAndNewUUIDForms(t *testing.T) {
	line, err := NewLineID()
	if err != nil || !regexp.MustCompile(`^line_[0-9a-f]{32}$`).MatchString(line) {
		t.Fatalf("NewLineID() = %q, %v", line, err)
	}
	uuid, err := NewUUID()
	if err != nil || !regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`).MatchString(uuid) {
		t.Fatalf("NewUUID() = %q, %v", uuid, err)
	}
}
