package store

import "testing"

func TestMessageThreadKeyUsesStableLineIdentity(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		localPhone string
		imsi       string
		iccid      string
		peer       string
		want       string
	}{
		{
			name:       "phone formatting",
			localPhone: "+81 (90) 1234-5678",
			imsi:       "imsi-ignored",
			iccid:      "iccid-ignored",
			peer:       " +818012345678 ",
			want:       "phone:819012345678|+818012345678",
		},
		{
			name:  "IMSI fallback",
			imsi:  " imsi-main ",
			iccid: "iccid-ignored",
			peer:  "10690",
			want:  "imsi:imsi-main|10690",
		},
		{
			name:  "ICCID fallback",
			iccid: " iccid-main ",
			peer:  "10690",
			want:  "iccid:iccid-main|10690",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got := MessageThreadKey(
				test.localPhone,
				test.imsi,
				test.iccid,
				test.peer,
			)
			if got != test.want {
				t.Fatalf("MessageThreadKey() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestNormalizeLinePhoneKeepsOnlyASCIIDigits(t *testing.T) {
	t.Parallel()

	if got := NormalizeLinePhone("+81 90-1234-5678"); got != "819012345678" {
		t.Fatalf("NormalizeLinePhone() = %q, want 819012345678", got)
	}
}
