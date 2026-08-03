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
