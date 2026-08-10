package applepush

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/human-agent65535/modemdeck/internal/callevents"
	"github.com/human-agent65535/modemdeck/internal/messageevents"
	"github.com/human-agent65535/modemdeck/internal/store"
)

const (
	defaultDeliveryTimeout = 12 * time.Second
	defaultRetryDelay      = 250 * time.Millisecond
	maximumCallEventAge    = 45 * time.Second
	maximumAlertRunes      = 240
)

var (
	ErrPushTargetUnavailable = errors.New("Apple push target is unavailable")
	ErrPushTopicMismatch     = errors.New("Apple push topic does not match the paired app")
)

type Repository interface {
	IOSPushTargetsForLine(
		context.Context,
		string,
		store.IOSPushTokenKind,
	) ([]store.IOSPushTarget, error)
	IOSPushTargetForCredential(
		context.Context,
		string,
		string,
		store.IOSPushTokenKind,
	) (store.IOSPushTarget, bool, error)
	ClearIOSPushToken(
		context.Context,
		string,
		store.IOSPushTokenKind,
		string,
	) error
}

type TestCallResult struct {
	ID         string
	AcceptedAt time.Time
}

type Sender interface {
	BundleID() string
	Send(context.Context, Notification) error
}

type Options struct {
	Logger          *slog.Logger
	Now             func() time.Time
	DeliveryTimeout time.Duration
	RetryDelay      time.Duration
}

type Runtime struct {
	repository      Repository
	sender          Sender
	messages        messageevents.Source
	calls           callevents.Source
	logger          *slog.Logger
	now             func() time.Time
	deliveryTimeout time.Duration
	retryDelay      time.Duration
}

func NewRuntime(
	repository Repository,
	sender Sender,
	messages messageevents.Source,
	calls callevents.Source,
	options Options,
) (*Runtime, error) {
	if repository == nil || sender == nil || messages == nil || calls == nil {
		return nil, errors.New("Apple push runtime dependencies are required")
	}
	logger := options.Logger
	if logger == nil {
		logger = slog.Default()
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	deliveryTimeout := options.DeliveryTimeout
	if deliveryTimeout <= 0 {
		deliveryTimeout = defaultDeliveryTimeout
	}
	retryDelay := options.RetryDelay
	if retryDelay <= 0 {
		retryDelay = defaultRetryDelay
	}
	return &Runtime{
		repository:      repository,
		sender:          sender,
		messages:        messages,
		calls:           calls,
		logger:          logger,
		now:             now,
		deliveryTimeout: deliveryTimeout,
		retryDelay:      retryDelay,
	}, nil
}

func (runtime *Runtime) Run(ctx context.Context) {
	runtime.run(ctx, nil)
}

func (runtime *Runtime) RunReady(ctx context.Context, ready chan<- struct{}) {
	runtime.run(ctx, ready)
}

func (runtime *Runtime) run(ctx context.Context, ready chan<- struct{}) {
	messageEvents, cancelMessages := runtime.messages.Subscribe()
	defer cancelMessages()
	callEvents, cancelCalls := runtime.calls.Subscribe()
	defer cancelCalls()
	if ready != nil {
		close(ready)
	}

	var workers sync.WaitGroup
	workers.Add(2)
	go func() {
		defer workers.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case event, open := <-messageEvents:
				if !open {
					return
				}
				runtime.deliverSMS(ctx, event)
			}
		}
	}()
	go func() {
		defer workers.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case event, open := <-callEvents:
				if !open {
					return
				}
				runtime.deliverCall(ctx, event)
			}
		}
	}()
	workers.Wait()
}

func (runtime *Runtime) deliverSMS(ctx context.Context, event messageevents.IncomingSMS) {
	payload := struct {
		APS struct {
			Alert struct {
				Title string `json:"title"`
				Body  string `json:"body"`
			} `json:"alert"`
			Sound    string `json:"sound"`
			ThreadID string `json:"thread-id,omitempty"`
		} `json:"aps"`
		Message struct {
			MessageID string `json:"message_id"`
			LineID    string `json:"line_id"`
			ThreadKey string `json:"thread_key"`
		} `json:"modemdeck_message"`
	}{}
	payload.APS.Alert.Title = truncateAlert(strings.TrimSpace(event.Peer))
	if payload.APS.Alert.Title == "" {
		payload.APS.Alert.Title = "New message"
	}
	payload.APS.Alert.Body = truncateAlert(event.Content)
	payload.APS.Sound = "default"
	payload.APS.ThreadID = truncateBytes(event.ThreadKey, 64)
	payload.Message.MessageID = strings.TrimSpace(event.MessageID)
	payload.Message.LineID = strings.TrimSpace(event.LineID)
	payload.Message.ThreadKey = strings.TrimSpace(event.ThreadKey)

	runtime.deliver(
		ctx,
		store.IOSPushTokenAPNS,
		event.LineID,
		"message",
		event.MessageID,
		Notification{
			PushType:   PushTypeAlert,
			APNSID:     deterministicUUID("message", event.MessageID),
			Expiration: runtime.now().UTC().Add(24 * time.Hour),
			Payload:    payload,
		},
	)
}

func (runtime *Runtime) deliverCall(ctx context.Context, event callevents.IncomingCall) {
	now := runtime.now().UTC()
	observedAt := event.ObservedAt.UTC()
	if !observedAt.IsZero() && now.Sub(observedAt) > maximumCallEventAge {
		runtime.logger.Warn(
			"discarded stale incoming call push",
			"component", "apple_push",
			"call_id", event.CallID,
		)
		return
	}
	callUUID := deterministicUUID("callkit", event.CallID)
	payload := struct {
		APS  struct{} `json:"aps"`
		Call struct {
			CallID       string `json:"call_id"`
			UUID         string `json:"uuid"`
			LineID       string `json:"line_id"`
			RemoteNumber string `json:"remote_number"`
			DisplayName  string `json:"display_name,omitempty"`
		} `json:"modemdeck_call"`
	}{}
	payload.Call.CallID = strings.TrimSpace(event.CallID)
	payload.Call.UUID = callUUID
	payload.Call.LineID = strings.TrimSpace(event.LineID)
	payload.Call.RemoteNumber = strings.TrimSpace(event.RemoteNumber)
	payload.Call.DisplayName = strings.TrimSpace(event.DisplayName)

	runtime.deliver(
		ctx,
		store.IOSPushTokenVoIP,
		event.LineID,
		"call",
		event.CallID,
		Notification{
			PushType:   PushTypeVoIP,
			APNSID:     callUUID,
			CollapseID: callUUID,
			Expiration: now.Add(30 * time.Second),
			Payload:    payload,
		},
	)
}

// SendTestCall sends a synthetic CallKit wake-up to the current user's paired
// iPhone. It never creates a modem call or a durable call record.
func (runtime *Runtime) SendTestCall(
	ctx context.Context,
	userID, credentialID string,
) (TestCallResult, error) {
	userID = strings.TrimSpace(userID)
	credentialID = strings.TrimSpace(credentialID)
	if userID == "" || credentialID == "" {
		return TestCallResult{}, ErrPushTargetUnavailable
	}
	target, found, err := runtime.repository.IOSPushTargetForCredential(
		ctx,
		userID,
		credentialID,
		store.IOSPushTokenVoIP,
	)
	if err != nil {
		return TestCallResult{}, fmt.Errorf("query test call target: %w", err)
	}
	if !found || strings.TrimSpace(target.Token) == "" {
		return TestCallResult{}, ErrPushTargetUnavailable
	}
	if target.BundleID != runtime.sender.BundleID() {
		return TestCallResult{}, ErrPushTopicMismatch
	}

	now := runtime.now().UTC()
	testID := deterministicUUID(
		"callkit-test",
		fmt.Sprintf("%s\x00%d", userID, now.UnixNano()),
	)
	payload := struct {
		APS  struct{} `json:"aps"`
		Call struct {
			CallID       string `json:"call_id"`
			UUID         string `json:"uuid"`
			RemoteNumber string `json:"remote_number"`
			DisplayName  string `json:"display_name"`
			TestCall     bool   `json:"test_call"`
		} `json:"modemdeck_call"`
	}{}
	payload.Call.CallID = "test-" + testID
	payload.Call.UUID = testID
	payload.Call.RemoteNumber = "ModemDeck Test"
	payload.Call.DisplayName = "ModemDeck Test Call"
	payload.Call.TestCall = true

	notification := Notification{
		DeviceToken: target.Token,
		Environment: target.Environment,
		PushType:    PushTypeVoIP,
		APNSID:      testID,
		CollapseID:  testID,
		Expiration:  now.Add(10 * time.Second),
		Payload:     payload,
	}
	deliveryContext, cancel := context.WithTimeout(ctx, runtime.deliveryTimeout)
	err = runtime.sendWithRetry(deliveryContext, notification)
	cancel()
	if err != nil {
		if InvalidatesToken(err) {
			clearContext, clearCancel := context.WithTimeout(ctx, 5*time.Second)
			clearErr := runtime.repository.ClearIOSPushToken(
				clearContext,
				target.UserID,
				store.IOSPushTokenVoIP,
				target.Token,
			)
			clearCancel()
			if clearErr != nil {
				runtime.logger.Warn(
					"invalid test call token could not be cleared",
					"component", "apple_push",
					"user_id", target.UserID,
					"error", clearErr,
				)
			}
		}
		return TestCallResult{}, fmt.Errorf("send test call: %w", err)
	}
	runtime.logger.Info(
		"test CallKit push accepted by APNs",
		"component", "apple_push",
		"user_id", target.UserID,
		"credential_id", target.CredentialID,
		"test_call_id", testID,
	)
	return TestCallResult{ID: testID, AcceptedAt: now}, nil
}

func (runtime *Runtime) deliver(
	ctx context.Context,
	kind store.IOSPushTokenKind,
	lineID string,
	eventKind string,
	eventID string,
	notification Notification,
) {
	targets, err := runtime.repository.IOSPushTargetsForLine(ctx, strings.TrimSpace(lineID), kind)
	if err != nil {
		runtime.logger.Warn(
			"Apple push targets unavailable",
			"component", "apple_push",
			"event_kind", eventKind,
			"event_id", eventID,
			"error", err,
		)
		return
	}
	for _, target := range targets {
		if target.BundleID != runtime.sender.BundleID() {
			runtime.logger.Warn(
				"skipped Apple push with mismatched app topic",
				"component", "apple_push",
				"event_kind", eventKind,
				"event_id", eventID,
				"user_id", target.UserID,
			)
			continue
		}
		notification.DeviceToken = target.Token
		notification.Environment = target.Environment
		deliveryContext, cancel := context.WithTimeout(ctx, runtime.deliveryTimeout)
		err := runtime.sendWithRetry(deliveryContext, notification)
		cancel()
		if err == nil {
			continue
		}
		if InvalidatesToken(err) {
			clearContext, clearCancel := context.WithTimeout(ctx, 5*time.Second)
			clearErr := runtime.repository.ClearIOSPushToken(
				clearContext,
				target.UserID,
				kind,
				target.Token,
			)
			clearCancel()
			if clearErr != nil {
				runtime.logger.Warn(
					"invalid Apple push token could not be cleared",
					"component", "apple_push",
					"event_kind", eventKind,
					"event_id", eventID,
					"user_id", target.UserID,
					"error", clearErr,
				)
			}
		}
		runtime.logger.Warn(
			"Apple push delivery failed",
			"component", "apple_push",
			"event_kind", eventKind,
			"event_id", eventID,
			"user_id", target.UserID,
			"error", err,
		)
	}
}

func (runtime *Runtime) sendWithRetry(ctx context.Context, notification Notification) error {
	err := runtime.sender.Send(ctx, notification)
	if err == nil || !IsRetryable(err) {
		return err
	}
	timer := time.NewTimer(runtime.retryDelay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return errors.Join(err, ctx.Err())
	case <-timer.C:
	}
	return runtime.sender.Send(ctx, notification)
}

func deterministicUUID(namespace, value string) string {
	digest := sha256.Sum256([]byte(namespace + "\x00" + strings.TrimSpace(value)))
	bytes := append([]byte(nil), digest[:16]...)
	bytes[6] = (bytes[6] & 0x0f) | 0x50
	bytes[8] = (bytes[8] & 0x3f) | 0x80
	encoded := hex.EncodeToString(bytes)
	return fmt.Sprintf(
		"%s-%s-%s-%s-%s",
		encoded[0:8],
		encoded[8:12],
		encoded[12:16],
		encoded[16:20],
		encoded[20:32],
	)
}

func truncateAlert(value string) string {
	value = strings.TrimSpace(value)
	if utf8.RuneCountInString(value) <= maximumAlertRunes {
		return value
	}
	runes := []rune(value)
	return strings.TrimSpace(string(runes[:maximumAlertRunes-1])) + "…"
}

func truncateBytes(value string, maximum int) string {
	value = strings.TrimSpace(value)
	if len(value) <= maximum {
		return value
	}
	for len(value) > maximum {
		_, size := utf8.DecodeLastRuneInString(value)
		value = value[:len(value)-size]
	}
	return value
}
