package applepush

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/human-agent65535/modemdeck/internal/callevents"
	"github.com/human-agent65535/modemdeck/internal/messageevents"
	"github.com/human-agent65535/modemdeck/internal/store"
)

type fakePushRepository struct {
	targets map[store.IOSPushTokenKind][]store.IOSPushTarget
	cleared []clearedPushToken
	err     error
}

type clearedPushToken struct {
	userID string
	kind   store.IOSPushTokenKind
	token  string
}

func (repository *fakePushRepository) IOSPushTargetsForLine(
	_ context.Context,
	_ string,
	kind store.IOSPushTokenKind,
) ([]store.IOSPushTarget, error) {
	if repository.err != nil {
		return nil, repository.err
	}
	return append([]store.IOSPushTarget(nil), repository.targets[kind]...), nil
}

func (repository *fakePushRepository) IOSPushTargetForCredential(
	_ context.Context,
	userID, credentialID string,
	kind store.IOSPushTokenKind,
) (store.IOSPushTarget, bool, error) {
	if repository.err != nil {
		return store.IOSPushTarget{}, false, repository.err
	}
	for _, target := range repository.targets[kind] {
		if target.UserID == userID && target.CredentialID == credentialID {
			return target, true, nil
		}
	}
	return store.IOSPushTarget{}, false, nil
}

func (repository *fakePushRepository) ClearIOSPushToken(
	_ context.Context,
	userID string,
	kind store.IOSPushTokenKind,
	token string,
) error {
	repository.cleared = append(repository.cleared, clearedPushToken{userID: userID, kind: kind, token: token})
	return nil
}

type fakePushSender struct {
	bundleID      string
	notifications []Notification
	errors        []error
}

func (sender *fakePushSender) BundleID() string { return sender.bundleID }

func (sender *fakePushSender) Send(_ context.Context, notification Notification) error {
	sender.notifications = append(sender.notifications, notification)
	if len(sender.errors) == 0 {
		return nil
	}
	err := sender.errors[0]
	sender.errors = sender.errors[1:]
	return err
}

func TestRuntimeBuildsAlertAndVoIPPayloads(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 9, 5, 0, 0, 0, time.UTC)
	repository := &fakePushRepository{targets: map[store.IOSPushTokenKind][]store.IOSPushTarget{
		store.IOSPushTokenAPNS: {{
			UserID:      "user-example",
			Token:       "aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899",
			Environment: "production",
			BundleID:    "com.example.modemdeck",
		}},
		store.IOSPushTokenVoIP: {{
			UserID:      "user-example",
			Token:       "11223344556677889900aabbccddeeff00112233445566778899aabbccddeeff",
			Environment: "development",
			BundleID:    "com.example.modemdeck",
		}},
	}}
	sender := &fakePushSender{bundleID: "com.example.modemdeck"}
	runtime := testRuntime(t, repository, sender, now)

	runtime.deliverSMS(context.Background(), messageevents.IncomingSMS{
		MessageID: "42",
		ThreadKey: "line-example|peer-example",
		LineID:    "line-example",
		Peer:      "+12025550106",
		Content:   "Example message",
	})
	runtime.deliverCall(context.Background(), callevents.IncomingCall{
		CallID:       "call-example",
		LineID:       "line-example",
		RemoteNumber: "+12025550106",
		DisplayName:  "Example Contact",
		ObservedAt:   now,
	})
	if len(sender.notifications) != 2 {
		t.Fatalf("notifications = %+v", sender.notifications)
	}
	alert := sender.notifications[0]
	if alert.PushType != PushTypeAlert || alert.Environment != "production" || alert.DeviceToken == "" {
		t.Fatalf("alert = %+v", alert)
	}
	alertJSON, _ := json.Marshal(alert.Payload)
	if !jsonContains(t, alertJSON, "Example message") || !jsonContains(t, alertJSON, "modemdeck_message") {
		t.Fatalf("alert payload = %s", alertJSON)
	}
	voip := sender.notifications[1]
	if voip.PushType != PushTypeVoIP || voip.Environment != "development" || voip.CollapseID == "" {
		t.Fatalf("VoIP notification = %+v", voip)
	}
	voipJSON, _ := json.Marshal(voip.Payload)
	for _, expected := range []string{"modemdeck_call", "call-example", "Example Contact", voip.APNSID} {
		if !jsonContains(t, voipJSON, expected) {
			t.Fatalf("VoIP payload %s does not contain %q", voipJSON, expected)
		}
	}
}

func TestRuntimeRetriesTransientFailureOnce(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 9, 5, 30, 0, 0, time.UTC)
	repository := &fakePushRepository{targets: map[store.IOSPushTokenKind][]store.IOSPushTarget{
		store.IOSPushTokenAPNS: {{
			UserID:      "user-example",
			Token:       "aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899",
			Environment: "production",
			BundleID:    "com.example.modemdeck",
		}},
	}}
	sender := &fakePushSender{
		bundleID: "com.example.modemdeck",
		errors:   []error{&ResponseError{StatusCode: http.StatusServiceUnavailable, Reason: "Shutdown"}},
	}
	runtime := testRuntime(t, repository, sender, now)
	runtime.deliverSMS(context.Background(), messageevents.IncomingSMS{
		MessageID: "43",
		LineID:    "line-example",
		Content:   "Example",
	})
	if len(sender.notifications) != 2 {
		t.Fatalf("send attempts = %d, want 2", len(sender.notifications))
	}
}

func TestRuntimeClearsOnlyRejectedCurrentToken(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 9, 6, 0, 0, 0, time.UTC)
	token := strings.Repeat("ab", 32)
	repository := &fakePushRepository{targets: map[store.IOSPushTokenKind][]store.IOSPushTarget{
		store.IOSPushTokenAPNS: {{
			UserID:      "user-example",
			Token:       token,
			Environment: "production",
			BundleID:    "com.example.modemdeck",
		}},
	}}
	sender := &fakePushSender{
		bundleID: "com.example.modemdeck",
		errors:   []error{&ResponseError{StatusCode: http.StatusGone, Reason: "Unregistered"}},
	}
	runtime := testRuntime(t, repository, sender, now)
	runtime.deliverSMS(context.Background(), messageevents.IncomingSMS{
		MessageID: "44",
		LineID:    "line-example",
		Content:   "Example",
	})
	if len(sender.notifications) != 1 || len(repository.cleared) != 1 ||
		repository.cleared[0] != (clearedPushToken{
			userID: "user-example",
			kind:   store.IOSPushTokenAPNS,
			token:  token,
		}) {
		t.Fatalf("notifications=%d cleared=%+v", len(sender.notifications), repository.cleared)
	}
}

func TestRuntimeSkipsMismatchedTopicAndStaleCall(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 9, 6, 30, 0, 0, time.UTC)
	repository := &fakePushRepository{targets: map[store.IOSPushTokenKind][]store.IOSPushTarget{
		store.IOSPushTokenAPNS: {{
			UserID:      "user-example",
			Token:       "aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899",
			Environment: "production",
			BundleID:    "com.example.different",
		}},
	}}
	sender := &fakePushSender{bundleID: "com.example.modemdeck"}
	runtime := testRuntime(t, repository, sender, now)
	runtime.deliverSMS(context.Background(), messageevents.IncomingSMS{
		MessageID: "45",
		LineID:    "line-example",
		Content:   "Example",
	})
	runtime.deliverCall(context.Background(), callevents.IncomingCall{
		CallID:     "call-stale",
		LineID:     "line-example",
		ObservedAt: now.Add(-time.Minute),
	})
	if len(sender.notifications) != 0 {
		t.Fatalf("notifications = %+v, want none", sender.notifications)
	}
}

func TestRuntimeSendsSyntheticTestCallOnlyToRequestedUser(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 9, 7, 0, 0, 0, time.UTC)
	repository := &fakePushRepository{targets: map[store.IOSPushTokenKind][]store.IOSPushTarget{
		store.IOSPushTokenVoIP: {
			{
				CredentialID: "ios-other",
				UserID:       "user-other",
				Token:        strings.Repeat("11", 32),
				Environment:  "production",
				BundleID:     "com.example.modemdeck",
			},
			{
				CredentialID: "ios-example",
				UserID:       "user-example",
				Token:        strings.Repeat("22", 32),
				Environment:  "production",
				BundleID:     "com.example.modemdeck",
			},
		},
	}}
	sender := &fakePushSender{bundleID: "com.example.modemdeck"}
	runtime := testRuntime(t, repository, sender, now)

	result, err := runtime.SendTestCall(
		context.Background(),
		"user-example",
		"ios-example",
	)
	if err != nil {
		t.Fatalf("SendTestCall() error = %v", err)
	}
	if result.ID == "" || !result.AcceptedAt.Equal(now) {
		t.Fatalf("result = %+v", result)
	}
	if len(sender.notifications) != 1 {
		t.Fatalf("notifications = %+v", sender.notifications)
	}
	notification := sender.notifications[0]
	if notification.DeviceToken != strings.Repeat("22", 32) ||
		notification.PushType != PushTypeVoIP ||
		notification.APNSID != result.ID {
		t.Fatalf("notification = %+v", notification)
	}
	payload, _ := json.Marshal(notification.Payload)
	for _, expected := range []string{`"test_call":true`, "ModemDeck Test Call", "test-" + result.ID} {
		if !jsonContains(t, payload, expected) {
			t.Fatalf("test call payload %s does not contain %q", payload, expected)
		}
	}
}

func TestRuntimeRejectsTestCallWithoutMatchingPushKitTarget(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.August, 9, 7, 30, 0, 0, time.UTC)
	tests := []struct {
		name    string
		targets []store.IOSPushTarget
		wantErr error
	}{
		{name: "missing", wantErr: ErrPushTargetUnavailable},
		{
			name: "topic mismatch",
			targets: []store.IOSPushTarget{{
				CredentialID: "ios-example",
				UserID:       "user-example",
				Token:        strings.Repeat("33", 32),
				Environment:  "production",
				BundleID:     "com.example.other",
			}},
			wantErr: ErrPushTopicMismatch,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository := &fakePushRepository{targets: map[store.IOSPushTokenKind][]store.IOSPushTarget{
				store.IOSPushTokenVoIP: test.targets,
			}}
			sender := &fakePushSender{bundleID: "com.example.modemdeck"}
			runtime := testRuntime(t, repository, sender, now)
			if _, err := runtime.SendTestCall(
				context.Background(),
				"user-example",
				"ios-example",
			); !errors.Is(err, test.wantErr) {
				t.Fatalf("SendTestCall() error = %v, want %v", err, test.wantErr)
			}
			if len(sender.notifications) != 0 {
				t.Fatalf("notifications = %+v", sender.notifications)
			}
		})
	}
}

func testRuntime(
	t *testing.T,
	repository Repository,
	sender Sender,
	now time.Time,
) *Runtime {
	t.Helper()
	runtime, err := NewRuntime(
		repository,
		sender,
		messageevents.NewBuffer(4),
		callevents.NewBuffer(4),
		Options{
			Logger:          slog.New(slog.NewTextHandler(io.Discard, nil)),
			Now:             func() time.Time { return now },
			DeliveryTimeout: time.Second,
			RetryDelay:      time.Millisecond,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	return runtime
}

func jsonContains(t *testing.T, contents []byte, expected string) bool {
	t.Helper()
	if !json.Valid(contents) {
		t.Fatalf("invalid JSON: %s", contents)
	}
	return bytes.Contains(contents, []byte(expected))
}
