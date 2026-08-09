package mobilepairing

import (
	"encoding/hex"
	"errors"
	"strings"
)

var ErrInvalidPushToken = errors.New("invalid Apple push token")

type PushRegistration struct {
	APNSToken   string `json:"apns_token"`
	VoIPToken   string `json:"voip_token"`
	Environment string `json:"environment"`
	BundleID    string `json:"bundle_id"`
}

func (registration PushRegistration) Normalize() (PushRegistration, error) {
	registration.APNSToken = strings.ToLower(strings.TrimSpace(registration.APNSToken))
	registration.VoIPToken = strings.ToLower(strings.TrimSpace(registration.VoIPToken))
	registration.Environment = strings.ToLower(strings.TrimSpace(registration.Environment))
	registration.BundleID = strings.TrimSpace(registration.BundleID)
	if registration.Environment == "" {
		registration.Environment = "development"
	}
	if registration.Environment != "development" && registration.Environment != "production" {
		return PushRegistration{}, ErrInvalidPushToken
	}
	if registration.APNSToken == "" && registration.VoIPToken == "" {
		return PushRegistration{}, ErrInvalidPushToken
	}
	if !ValidBundleID(registration.BundleID) {
		return PushRegistration{}, ErrInvalidPushToken
	}
	for _, token := range []string{registration.APNSToken, registration.VoIPToken} {
		if token == "" {
			continue
		}
		decoded, err := hex.DecodeString(token)
		if err != nil || len(decoded) != 32 {
			return PushRegistration{}, ErrInvalidPushToken
		}
	}
	return registration, nil
}

func ValidBundleID(value string) bool {
	if len(value) == 0 || len(value) > 255 || !strings.Contains(value, ".") {
		return false
	}
	for _, segment := range strings.Split(value, ".") {
		if segment == "" || segment[0] == '-' || segment[len(segment)-1] == '-' {
			return false
		}
		for _, character := range []byte(segment) {
			if character == '-' ||
				(character >= 'A' && character <= 'Z') ||
				(character >= 'a' && character <= 'z') ||
				(character >= '0' && character <= '9') {
				continue
			}
			return false
		}
	}
	return true
}
