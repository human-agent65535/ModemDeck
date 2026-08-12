package applepush

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/human-agent65535/modemdeck/internal/callevents"
	"github.com/human-agent65535/modemdeck/internal/messageevents"
	"github.com/human-agent65535/modemdeck/internal/store"
)

type fakePushRepository struct {
	mu         sync.Mutex
	deliveries map[string]store.ApplePushDelivery
	statuses   map[string]string
	next       map[string]time.Time
	finished   []finishedPushDelivery
	cleared    []clearedPushToken
	targets    map[store.IOSPushTokenKind][]store.IOSPushTarget
	err        error
}

type finishedPushDelivery struct {
	key         string
	status      string
	errorClass  string
	nextAttempt time.Time
}

type clearedPushToken struct {
	userID string
	kind   store.IOSPushTokenKind
	token  string
}

func newFakePushRepository() *fakePushRepository {
	return &fakePushRepository{
		deliveries: make(map[string]store.ApplePushDelivery),
		statuses:   make(map[string]string),
		next:       make(map[string]time.Time),
		targets:    make(map[store.IOSPushTokenKind][]store.IOSPushTarget),
	}
}

func deliveryKey(eventKey, credentialID string, kind store.IOSPushTokenKind) string {
	return eventKey + "\x00" + credentialID + "\x00" + string(kind)
}

func (repository *fakePushRepository) add(
	delivery store.ApplePushDelivery,
	status string,
	next time.Time,
) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	key := deliveryKey(delivery.EventKey, delivery.CredentialID, delivery.TokenKind)
	repository.deliveries[key] = delivery
	repository.statuses[key] = status
	repository.next[key] = next
}

func (repository *fakePushRepository) PendingApplePushDeliveries(
	_ context.Context,
	now time.Time,
	limit int,
) ([]store.ApplePushDelivery, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if repository.err != nil {
		return nil, repository.err
	}
	result := make([]store.ApplePushDelivery, 0, limit)
	for key, delivery := range repository.deliveries {
		if repository.statuses[key] != store.NotificationPending ||
			repository.next[key].After(now) {
			continue
		}
		result = append(result, delivery)
		if len(result) == limit {
			break
		}
	}
	return result, nil
}

func (repository *fakePushRepository) NextApplePushDeliveryAttempt(
	_ context.Context,
) (time.Time, bool, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	var earliest time.Time
	for key, next := range repository.next {
		if repository.statuses[key] != store.NotificationPending {
			continue
		}
		if earliest.IsZero() || next.Before(earliest) {
			earliest = next
		}
	}
	return earliest, !earliest.IsZero(), repository.err
}

func (repository *fakePushRepository) RequeueSendingApplePushDeliveries(
	_ context.Context,
) error {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	for key, status := range repository.statuses {
		if status == store.NotificationSending {
			repository.statuses[key] = store.NotificationPending
			repository.next[key] = time.Time{}
		}
	}
	return repository.err
}

func (repository *fakePushRepository) ClaimApplePushDelivery(
	_ context.Context,
	eventKey, credentialID string,
	kind store.IOSPushTokenKind,
	_ string,
) (bool, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	key := deliveryKey(eventKey, credentialID, kind)
	if repository.statuses[key] != store.NotificationPending {
		return false, repository.err
	}
	repository.statuses[key] = store.NotificationSending
	delivery := repository.deliveries[key]
	delivery.AttemptCount++
	repository.deliveries[key] = delivery
	return true, repository.err
}

func (repository *fakePushRepository) FinishApplePushDelivery(
	_ context.Context,
	eventKey, credentialID string,
	kind store.IOSPushTokenKind,
	_, status, errorClass string,
	nextAttempt time.Time,
) error {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	key := deliveryKey(eventKey, credentialID, kind)
	repository.statuses[key] = status
	repository.next[key] = nextAttempt
	repository.finished = append(repository.finished, finishedPushDelivery{
		key:         key,
		status:      status,
		errorClass:  errorClass,
		nextAttempt: nextAttempt,
	})
	return repository.err
}

func (repository *fakePushRepository) IOSPushTargetForCredential(
	_ context.Context,
	userID, credentialID string,
	kind store.IOSPushTokenKind,
) (store.IOSPushTarget, bool, error) {
	for _, target := range repository.targets[kind] {
		if target.UserID == userID && target.CredentialID == credentialID {
			return target, true, repository.err
		}
	}
	return store.IOSPushTarget{}, false, repository.err
}

func (repository *fakePushRepository) ClearIOSPushToken(
	_ context.Context,
	userID string,
	kind store.IOSPushTokenKind,
	token string,
) error {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	repository.cleared = append(repository.cleared, clearedPushToken{
		userID: userID,
		kind:   kind,
		token:  token,
	})
	return repository.err
}

type fakePushSender struct {
	mu            sync.Mutex
	bundleID      string
	notifications []Notification
	errors        []error
	wake          chan struct{}
}

func (sender *fakePushSender) BundleID() string { return sender.bundleID }

func (sender *fakePushSender) Send(_ context.Context, notification Notification) error {
	sender.mu.Lock()
	sender.notifications = append(sender.notifications, notification)
	var err error
	if len(sender.errors) > 0 {
		err = sender.errors[0]
		sender.errors = sender.errors[1:]
	}
	sender.mu.Unlock()
	if sender.wake != nil {
		select {
		case sender.wake <- struct{}{}:
		default:
		}
	}
	return err
}

func (sender *fakePushSender) sent() []Notification {
	sender.mu.Lock()
	defer sender.mu.Unlock()
	return append([]Notification(nil), sender.notifications...)
}

func TestRuntimeBuildsDurableAlertAndVoIPPayloads(t *testing.T) {
	now := time.Date(2026, time.August, 9, 5, 0, 0, 0, time.UTC)
	repository := newFakePushRepository()
	sender := &fakePushSender{bundleID: "com.example.modemdeck"}
	runtime := testRuntime(t, repository, sender, now)

	sms := exampleDelivery(now, store.NotificationIncomingSMS, store.IOSPushTokenAPNS)
	sms.EventKey = "sms:42"
	sms.ResourceID = "42"
	sms.Body = "Example message"
	call := exampleDelivery(now, store.NotificationIncomingCall, store.IOSPushTokenVoIP)
	call.EventKey = "incoming-call:call-example"
	call.ResourceID = "call-example"
	call.Body = "Example Contact"
	for _, delivery := range []store.ApplePushDelivery{sms, call} {
		repository.add(delivery, store.NotificationPending, now)
		if !runtime.deliverOutboxEntry(context.Background(), delivery) {
			t.Fatalf("delivery was not claimed: %+v", delivery)
		}
	}

	notifications := sender.sent()
	if len(notifications) != 2 {
		t.Fatalf("notifications = %+v", notifications)
	}
	alertJSON, _ := json.Marshal(notifications[0].Payload)
	for _, expected := range []string{"Example message", "modemdeck_message", `"message_id":"42"`} {
		if !jsonContains(t, alertJSON, expected) {
			t.Fatalf("alert payload %s does not contain %q", alertJSON, expected)
		}
	}
	voipJSON, _ := json.Marshal(notifications[1].Payload)
	for _, expected := range []string{"modemdeck_call", `"event":"incoming"`, "call-example", "Example Contact"} {
		if !jsonContains(t, voipJSON, expected) {
			t.Fatalf("VoIP payload %s does not contain %q", voipJSON, expected)
		}
	}
	if notifications[1].CollapseID != deterministicUUID("callkit", "call-example") ||
		notifications[1].APNSID == notifications[1].CollapseID {
		t.Fatalf("VoIP identities = %+v", notifications[1])
	}
	for _, finished := range repository.finished {
		if finished.status != store.NotificationAccepted {
			t.Fatalf("finished = %+v", repository.finished)
		}
	}
}

func TestRuntimePersistsRetryInsteadOfSendingTwiceImmediately(t *testing.T) {
	now := time.Date(2026, time.August, 9, 5, 30, 0, 0, time.UTC)
	repository := newFakePushRepository()
	sender := &fakePushSender{
		bundleID: "com.example.modemdeck",
		errors: []error{&ResponseError{
			StatusCode: http.StatusServiceUnavailable,
			Reason:     "Shutdown",
		}},
	}
	runtime := testRuntime(t, repository, sender, now)
	delivery := exampleDelivery(now, store.NotificationIncomingSMS, store.IOSPushTokenAPNS)
	repository.add(delivery, store.NotificationPending, now)
	runtime.deliverOutboxEntry(context.Background(), delivery)

	if len(sender.sent()) != 1 || len(repository.finished) != 1 {
		t.Fatalf("sends=%d finished=%+v", len(sender.sent()), repository.finished)
	}
	finished := repository.finished[0]
	if finished.status != store.NotificationPending || !finished.nextAttempt.After(now) {
		t.Fatalf("finished = %+v", finished)
	}
}

func TestRuntimeRequeuesAmbiguousTransportWithStableAPNSID(t *testing.T) {
	now := time.Date(2026, time.August, 9, 5, 45, 0, 0, time.UTC)
	repository := newFakePushRepository()
	sender := &fakePushSender{
		bundleID: "com.example.modemdeck",
		errors: []error{&transportError{cause: &net.DNSError{
			Err:         "temporary",
			IsTemporary: true,
		}}},
	}
	runtime := testRuntime(t, repository, sender, now)
	delivery := exampleDelivery(now, store.NotificationIncomingSMS, store.IOSPushTokenAPNS)
	repository.add(delivery, store.NotificationPending, now)
	runtime.deliverOutboxEntry(context.Background(), delivery)
	firstID := sender.sent()[0].APNSID

	repository.mu.Lock()
	repository.statuses[deliveryKey(delivery.EventKey, delivery.CredentialID, delivery.TokenKind)] = store.NotificationPending
	repository.mu.Unlock()
	sender.errors = nil
	runtime.deliverOutboxEntry(context.Background(), delivery)
	notifications := sender.sent()
	if len(notifications) != 2 || notifications[1].APNSID != firstID {
		t.Fatalf("notifications = %+v", notifications)
	}
}

func TestRuntimeClearsRejectedCurrentToken(t *testing.T) {
	now := time.Date(2026, time.August, 9, 6, 0, 0, 0, time.UTC)
	repository := newFakePushRepository()
	sender := &fakePushSender{
		bundleID: "com.example.modemdeck",
		errors: []error{&ResponseError{
			StatusCode: http.StatusGone,
			Reason:     "Unregistered",
		}},
	}
	runtime := testRuntime(t, repository, sender, now)
	delivery := exampleDelivery(now, store.NotificationIncomingSMS, store.IOSPushTokenAPNS)
	repository.add(delivery, store.NotificationPending, now)
	runtime.deliverOutboxEntry(context.Background(), delivery)

	if len(repository.cleared) != 1 || repository.cleared[0] != (clearedPushToken{
		userID: delivery.UserID,
		kind:   delivery.TokenKind,
		token:  delivery.DeviceToken,
	}) || repository.finished[0].status != store.NotificationFailed {
		t.Fatalf("cleared=%+v finished=%+v", repository.cleared, repository.finished)
	}
}

func TestRuntimeExpiresCallThatIsNoLongerRinging(t *testing.T) {
	now := time.Date(2026, time.August, 9, 6, 30, 0, 0, time.UTC)
	repository := newFakePushRepository()
	sender := &fakePushSender{bundleID: "com.example.modemdeck"}
	runtime := testRuntime(t, repository, sender, now)
	delivery := exampleDelivery(now, store.NotificationIncomingCall, store.IOSPushTokenVoIP)
	delivery.ResourceLive = false
	repository.add(delivery, store.NotificationPending, now)
	runtime.deliverOutboxEntry(context.Background(), delivery)
	if len(sender.sent()) != 0 || repository.finished[0].status != store.NotificationExpired {
		t.Fatalf("notifications=%+v finished=%+v", sender.sent(), repository.finished)
	}
}

func TestRuntimeStartupRecoversAndDrainsSendingDelivery(t *testing.T) {
	now := time.Date(2026, time.August, 9, 6, 45, 0, 0, time.UTC)
	repository := newFakePushRepository()
	delivery := exampleDelivery(now, store.NotificationIncomingSMS, store.IOSPushTokenAPNS)
	repository.add(delivery, store.NotificationSending, now)
	sender := &fakePushSender{
		bundleID: "com.example.modemdeck",
		wake:     make(chan struct{}, 1),
	}
	messages := messageevents.NewBuffer(2)
	calls := callevents.NewBuffer(2)
	runtime, err := NewRuntime(repository, sender, messages, calls, Options{
		Logger:          slog.New(slog.NewTextHandler(io.Discard, nil)),
		Now:             func() time.Time { return now },
		DeliveryTimeout: time.Second,
		SweepInterval:   time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	ready := make(chan struct{})
	go func() {
		defer close(done)
		runtime.RunReady(ctx, ready)
	}()
	<-ready
	select {
	case <-sender.wake:
	case <-time.After(time.Second):
		t.Fatal("recovered delivery was not sent")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("runtime did not stop")
	}
}

func TestRuntimeSendsSyntheticTestCallOnlyToRequestedCredential(t *testing.T) {
	now := time.Date(2026, time.August, 9, 7, 0, 0, 0, time.UTC)
	repository := newFakePushRepository()
	repository.targets[store.IOSPushTokenVoIP] = []store.IOSPushTarget{
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
	}
	sender := &fakePushSender{bundleID: "com.example.modemdeck"}
	runtime := testRuntime(t, repository, sender, now)
	result, err := runtime.SendTestCall(context.Background(), "user-example", "ios-example")
	if err != nil {
		t.Fatal(err)
	}
	notifications := sender.sent()
	if len(notifications) != 1 || notifications[0].DeviceToken != strings.Repeat("22", 32) ||
		notifications[0].APNSID != result.ID {
		t.Fatalf("result=%+v notifications=%+v", result, notifications)
	}
	payload, _ := json.Marshal(notifications[0].Payload)
	if !jsonContains(t, payload, `"test_call":true`) {
		t.Fatalf("payload = %s", payload)
	}
}

func TestRuntimeRejectsTestCallWithoutMatchingTarget(t *testing.T) {
	now := time.Date(2026, time.August, 9, 7, 30, 0, 0, time.UTC)
	repository := newFakePushRepository()
	sender := &fakePushSender{bundleID: "com.example.modemdeck"}
	runtime := testRuntime(t, repository, sender, now)
	if _, err := runtime.SendTestCall(
		context.Background(),
		"user-example",
		"ios-example",
	); !errors.Is(err, ErrPushTargetUnavailable) {
		t.Fatalf("error = %v", err)
	}
}

func exampleDelivery(
	now time.Time,
	eventType string,
	kind store.IOSPushTokenKind,
) store.ApplePushDelivery {
	return store.ApplePushDelivery{
		EventKey:     "sms:1",
		CredentialID: "ios-example",
		UserID:       "user-example",
		TokenKind:    kind,
		DeviceToken:  strings.Repeat("ab", 32),
		Environment:  "production",
		BundleID:     "com.example.modemdeck",
		EventType:    eventType,
		ResourceID:   "1",
		LineID:       "line-example",
		Peer:         "+12025550106",
		Body:         "Example",
		OccurredAt:   now,
		ExpiresAt:    now.Add(24 * time.Hour),
		Eligible:     true,
		ResourceLive: true,
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
			SweepInterval:   time.Second,
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
