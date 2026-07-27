package modemidentity

import "testing"

func TestDisplayModelUsesConcreteQuectelIdentityForGenericModels(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		model            string
		firmware         string
		hardwareRevision string
		want             string
	}{
		{
			name:     "baiwang firmware",
			model:    "QUECTEL Mobile Broadband Module",
			firmware: "QDC507GLEFM21",
			want:     "QDC507",
		},
		{
			name:     "eg25 firmware",
			model:    "QUECTEL Mobile Broadband Module",
			firmware: "EG25GGCR07A02M1G",
			want:     "EG25-G",
		},
		{
			name:             "hardware revision wins",
			model:            "Mobile Broadband Module",
			firmware:         "unknown",
			hardwareRevision: "EG25G-MINIPCIE",
			want:             "EG25-G",
		},
		{
			name:     "ec25 regional variant",
			model:    "",
			firmware: "EC25EUXGAR08A01M1G",
			want:     "EC25-E",
		},
		{
			name:     "reported concrete model is authoritative",
			model:    "EG25-G",
			firmware: "QDC507GLEFM21",
			want:     "EG25-G",
		},
		{
			name:     "unknown generic model remains available",
			model:    "Vendor Mobile Broadband Module",
			firmware: "unknown",
			want:     "Vendor Mobile Broadband Module",
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := DisplayModel(test.model, test.firmware, test.hardwareRevision); got != test.want {
				t.Fatalf("DisplayModel() = %q, want %q", got, test.want)
			}
		})
	}
}
