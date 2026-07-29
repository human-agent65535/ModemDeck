package telegram

import "testing"

func TestNormalizePhone(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input   string
		want    string
		wantErr bool
	}{
		{input: "+81 80-1234-5678", want: "+818012345678"},
		{input: "0081 (80) 1234.5678", wantErr: true},
		{input: "+1 (415) 555-0123", want: "+14155550123"},
		{input: "08012345678", wantErr: true},
		{input: "+0123456789", wantErr: true},
		{input: "+123", wantErr: true},
		{input: "+1234567890123456", wantErr: true},
		{input: "+81/8012345678", want: "+818012345678"},
		{input: "＋818012345678", wantErr: true},
	}
	for _, test := range tests {
		test := test
		t.Run(test.input, func(t *testing.T) {
			t.Parallel()
			got, err := NormalizePhone(test.input)
			if test.wantErr {
				if err == nil {
					t.Fatalf("NormalizePhone(%q) returned nil error", test.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("NormalizePhone(%q) error = %v", test.input, err)
			}
			if got != test.want {
				t.Fatalf("NormalizePhone(%q) = %q, want %q", test.input, got, test.want)
			}
		})
	}
}

func TestMaskPhone(t *testing.T) {
	t.Parallel()

	if got := MaskPhone("+81 80-1234-5678"); got != "+***5678" {
		t.Fatalf("MaskPhone() = %q", got)
	}
	if got := MaskPhone("not a number"); got != "[invalid]" {
		t.Fatalf("MaskPhone(invalid) = %q", got)
	}
}

func TestNotificationPeer(t *testing.T) {
	t.Parallel()

	display, normalized, replyable := notificationPeer("+81 80-1234-5678")
	if display != "+818012345678" || normalized != display || !replyable {
		t.Fatalf("E.164 peer = %q %q %v", display, normalized, replyable)
	}
	display, normalized, replyable = notificationPeer("  BANK\nALERT\x00 ")
	if display != "BANK ALERT" || normalized != "" || replyable {
		t.Fatalf("alphanumeric peer = %q %q %v", display, normalized, replyable)
	}
	display, normalized, replyable = notificationPeer("\n\x00")
	if display != "未知号码" || normalized != "" || replyable {
		t.Fatalf("unknown peer = %q %q %v", display, normalized, replyable)
	}
}
