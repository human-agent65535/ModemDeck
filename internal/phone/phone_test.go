package phone

import (
	"errors"
	"testing"

	"github.com/nyaruka/phonenumbers/v2"
)

func TestParseSubscriberUsesLineHomeRegion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		value      string
		region     string
		e164       string
		numberHome string
	}{
		{
			name:       "Japanese national mobile",
			value:      "080-1234-5678",
			region:     "JP",
			e164:       "+818012345678",
			numberHome: "JP",
		},
		{
			name:       "Chinese national mobile",
			value:      "138 0013 8000",
			region:     "CN",
			e164:       "+8613800138000",
			numberHome: "CN",
		},
		{
			name:       "Vietnamese national mobile",
			value:      "0912 345 678",
			region:     "VN",
			e164:       "+84912345678",
			numberHome: "VN",
		},
		{
			name:       "Explicit international ignores roaming region",
			value:      "+84 912 345 678",
			region:     "JP",
			e164:       "+84912345678",
			numberHome: "VN",
		},
		{
			name:       "China international prefix",
			value:      "00 81 80 1234 5678",
			region:     "CN",
			e164:       "+818012345678",
			numberHome: "JP",
		},
		{
			name:       "Japan international prefix",
			value:      "010 84 912 345 678",
			region:     "JP",
			e164:       "+84912345678",
			numberHome: "VN",
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			address, err := ParseSubscriber(test.value, test.region)
			if err != nil {
				t.Fatalf("ParseSubscriber(%q, %q) error = %v", test.value, test.region, err)
			}
			if address.Kind != KindSubscriber ||
				address.E164 != test.e164 ||
				address.Dial != test.e164 ||
				address.Region != test.numberHome {
				t.Fatalf("ParseSubscriber(%q, %q) = %#v", test.value, test.region, address)
			}
		})
	}
}

func TestParseSubscriberLeavesNumberAssignmentToTheNetwork(t *testing.T) {
	t.Parallel()

	address, err := ParseSubscriber("+1 200 123 0101", "")
	if err != nil {
		t.Fatalf("ParseSubscriber() error = %v", err)
	}
	number, err := phonenumbers.Parse(address.E164, "")
	if err != nil {
		t.Fatalf("parse canonical number: %v", err)
	}
	if address.E164 != "+12001230101" || phonenumbers.IsValidNumber(number) {
		t.Fatalf("ParseSubscriber() = %#v, want possible but unassigned number", address)
	}
}

func TestParseSubscriberUsesTheSelectedRegionsIDD(t *testing.T) {
	t.Parallel()

	_, err := ParseSubscriber("00818012345678", "")
	assertPhoneError(t, err, CodeRegionRequired)

	_, err = ParseSubscriber("00818012345678", "JP")
	assertPhoneError(t, err, CodeInvalidLength)
}

func TestParseSubscriberRejectsCountryCodeWithoutInternationalPrefix(t *testing.T) {
	t.Parallel()

	_, err := ParseSubscriber("8613800138000", "CN")
	assertPhoneError(t, err, CodeInternationalPrefixRequired)

	_, err = ParseSubscriber("819012345678", "JP")
	assertPhoneError(t, err, CodeInternationalPrefixRequired)
}

func TestParseDestinationKeepsShortCodesLineLocal(t *testing.T) {
	t.Parallel()

	for _, region := range []string{"CN", ""} {
		address, err := ParseDestination("10010", region)
		if err != nil {
			t.Fatalf("ParseDestination(10010, %q) error = %v", region, err)
		}
		if address.Kind != KindShortCode || address.Dial != "10010" || address.E164 != "" {
			t.Fatalf("ParseDestination(10010, %q) = %#v", region, address)
		}
	}
}

func TestParseDestinationRejectsAmbiguousNationalNumberWithoutRegion(t *testing.T) {
	t.Parallel()

	_, err := ParseDestination("08012345678", "")
	assertPhoneError(t, err, CodeRegionRequired)
}

func TestCanonicalRegionAcceptsOnlySupportedRegions(t *testing.T) {
	t.Parallel()

	if actual := CanonicalRegion(" jp "); actual != "JP" {
		t.Fatalf("CanonicalRegion(jp) = %q, want JP", actual)
	}
	for _, value := range []string{"", "JPN", "XX", "日本"} {
		if actual := CanonicalRegion(value); actual != "" {
			t.Fatalf("CanonicalRegion(%q) = %q, want empty", value, actual)
		}
	}
}

func TestParseNetworkAddressClassifiesAlphanumericSenders(t *testing.T) {
	t.Parallel()

	address := ParseNetworkAddress("VTMONEY.VN", "VN")
	if address.Kind != KindAlphanumeric || address.Dial != "VTMONEY.VN" {
		t.Fatalf("ParseNetworkAddress() = %#v", address)
	}
}

func TestCanonicalNetworkAddressUsesReportedLineContext(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"080-1234-5678":      "+818012345678",
		"+81 (80) 1234-5678": "+818012345678",
		"10655526":           "10655526",
		"VTMONEY.VN":         "VTMONEY.VN",
	}
	for input, expected := range tests {
		if actual := CanonicalNetworkAddress(input, "JP"); actual != expected {
			t.Fatalf("CanonicalNetworkAddress(%q) = %q, want %q", input, actual, expected)
		}
	}
}

func TestCanonicalNetworkAddressUnifiesHomeCountryRepresentations(t *testing.T) {
	t.Parallel()

	const expected = "+8613800138000"
	for _, input := range []string{
		"+8613800138000",
		"8613800138000",
		"008613800138000",
	} {
		if actual := CanonicalNetworkAddress(input, "CN"); actual != expected {
			t.Fatalf("CanonicalNetworkAddress(%q, CN) = %q, want %q", input, actual, expected)
		}
	}
}

func TestCanonicalNetworkAddressUnifies00AcrossHomeRegions(t *testing.T) {
	t.Parallel()

	const expected = "+819012345678"
	for _, region := range []string{"", "JP", "CN"} {
		if actual := CanonicalNetworkAddress("00819012345678", region); actual != expected {
			t.Fatalf(
				"CanonicalNetworkAddress(00819012345678, %q) = %q, want %q",
				region,
				actual,
				expected,
			)
		}
	}
}

func TestCanonicalNetworkAddressAcceptsSharedCountryCallingCode(t *testing.T) {
	t.Parallel()

	const canadianNumber = "14165550123"
	if actual := CanonicalNetworkAddress(canadianNumber, "US"); actual != "+"+canadianNumber {
		t.Fatalf(
			"CanonicalNetworkAddress(%q, US) = %q, want %q",
			canadianNumber,
			actual,
			"+"+canadianNumber,
		)
	}
}

func TestCanonicalNetworkAddressRecoversBareForeignCountryCode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		reported string
		lineHome string
		expected string
	}{
		{
			reported: "8613800138000",
			lineHome: "JP",
			expected: "+8613800138000",
		},
		{
			reported: "819012345678",
			lineHome: "CN",
			expected: "+819012345678",
		},
	}
	for _, test := range tests {
		if actual := CanonicalNetworkAddress(test.reported, test.lineHome); actual != test.expected {
			t.Fatalf(
				"CanonicalNetworkAddress(%q, %q) = %q, want %q",
				test.reported,
				test.lineHome,
				actual,
				test.expected,
			)
		}
	}
}

func TestNetworkSubscriberE164ExcludesLineLocalAddresses(t *testing.T) {
	t.Parallel()

	for _, value := range []string{"10010", "VTMONEY.VN", "unknown"} {
		if actual := NetworkSubscriberE164(value, "CN"); actual != "" {
			t.Fatalf("NetworkSubscriberE164(%q, CN) = %q, want empty", value, actual)
		}
	}
	if actual := NetworkSubscriberE164("13800138000", "CN"); actual != "+8613800138000" {
		t.Fatalf("NetworkSubscriberE164() = %q, want +8613800138000", actual)
	}
}

func TestParseRejectsInvalidInput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		value  string
		region string
		code   ErrorCode
	}{
		{value: "", region: "JP", code: CodeRequired},
		{value: "Aiko", region: "JP", code: CodeInvalidCharacter},
		{value: "*123#", region: "JP", code: CodeInvalidCharacter},
		{value: "81+8012345678", region: "JP", code: CodeInvalidCharacter},
		{value: "08012345678", region: "XX", code: CodeInvalidRegion},
		{value: "+123", region: "", code: CodeInvalidLength},
		{value: "+0123456789", region: "", code: CodeInvalidCountryCode},
	}
	for _, test := range tests {
		_, err := ParseDestination(test.value, test.region)
		assertPhoneError(t, err, test.code)
	}
}

func assertPhoneError(t *testing.T, err error, code ErrorCode) {
	t.Helper()
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("error = %v, want ErrInvalid", err)
	}
	var numberError *Error
	if !errors.As(err, &numberError) || numberError.Code != code {
		t.Fatalf("error = %v, want code %q", err, code)
	}
}
