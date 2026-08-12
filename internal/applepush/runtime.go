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
	defaultDeliveryTimeout  = 12 * time.Second
	defaultRetryDelay       = 250 * time.Millisecond
	defaultSweepInterval    = 30 * time.Second
	subscriptionRetryDelay  = 100 * time.Millisecond
	maximumCallEventAge     = 45 * time.Second
	maximumAlertRunes       = 240
	deliveryBatchSize       = 100
	maximumDeliveryAttempts = 5
)

var (
	ErrPushTargetUnavailable = errors.New("Apple push target is unavailable")
	ErrPushTopicMismatch     = errors.New("Apple push topic does not match the paired app")
)

type Repository interface {
	PendingApplePushDeliveries(context.Context, time.Time, int) ([]store.ApplePushDelivery, error)
	NextApplePushDeliveryAttempt(context.Context) (time.Time, bool, error)
	RequeueSendingApplePushDeliveries(context.Context) error
	ClaimApplePushDelivery(
		context.Context,
		string,
		string,
		store.IOSPushTokenKind,
		string,
	) (bool, error)
	FinishApplePushDelivery(
		context.Context,
		string,
		string,
		store.IOSPushTokenKind,
		string,
		string,
		string,
		time.Time,
	) error
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
	SweepInterval   time.Duration
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
	sweepInterval   time.Duration
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
	sweepInterval := options.SweepInterval
	if sweepInterval <= 0 {
		sweepInterval = defaultSweepInterval
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
		sweepInterval:   sweepInterval,
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
	callEvents, cancelCalls := runtime.calls.Subscribe()
	if err := runtime.repository.RequeueSendingApplePushDeliveries(ctx); err != nil {
		runtime.logger.Error(
			"Apple push outbox recovery failed",
			"component", "apple_push",
			"error", err,
		)
	}
	runtime.drainOutbox(ctx)
	if ready != nil {
		close(ready)
	}

	wake := make(chan struct{}, 1)
	var workers sync.WaitGroup
	workers.Add(3)
	go func() {
		defer workers.Done()
		runtime.consumeMessageEvents(ctx, messageEvents, cancelMessages, wake)
	}()
	go func() {
		defer workers.Done()
		runtime.consumeCallEvents(ctx, callEvents, cancelCalls, wake)
	}()
	go func() {
		defer workers.Done()
		runtime.sweepOutbox(ctx, wake)
	}()
	workers.Wait()
}

func (runtime *Runtime) consumeMessageEvents(
	ctx context.Context,
	events <-chan messageevents.IncomingSMS,
	cancel func(),
	wake chan<- struct{},
) {
	for {
		select {
		case <-ctx.Done():
			cancel()
			return
		case event, open := <-events:
			if open {
				_ = event
				runtime.wakeOutbox(wake)
				continue
			}
		}
		cancel()
		if !runtime.waitToResubscribe(ctx, "message") {
			return
		}
		events, cancel = runtime.messages.Subscribe()
	}
}

func (runtime *Runtime) consumeCallEvents(
	ctx context.Context,
	events <-chan callevents.Event,
	cancel func(),
	wake chan<- struct{},
) {
	for {
		select {
		case <-ctx.Done():
			cancel()
			return
		case event, open := <-events:
			if open {
				_ = event
				runtime.wakeOutbox(wake)
				continue
			}
		}
		cancel()
		if !runtime.waitToResubscribe(ctx, "call") {
			return
		}
		events, cancel = runtime.calls.Subscribe()
	}
}

func (runtime *Runtime) wakeOutbox(wake chan<- struct{}) {
	select {
	case wake <- struct{}{}:
	default:
	}
}

func (runtime *Runtime) sweepOutbox(ctx context.Context, wake <-chan struct{}) {
	timer := time.NewTimer(runtime.nextSweepDelay(ctx))
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-wake:
			runtime.drainOutbox(ctx)
		case <-timer.C:
			runtime.drainOutbox(ctx)
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(runtime.nextSweepDelay(ctx))
	}
}

func (runtime *Runtime) nextSweepDelay(ctx context.Context) time.Duration {
	next, found, err := runtime.repository.NextApplePushDeliveryAttempt(ctx)
	if err != nil || !found {
		return runtime.sweepInterval
	}
	delay := next.Sub(runtime.now().UTC())
	if delay <= 0 {
		return 100 * time.Millisecond
	}
	return min(delay, runtime.sweepInterval)
}

func (runtime *Runtime) waitToResubscribe(ctx context.Context, eventKind string) bool {
	runtime.logger.Warn(
		"Apple push event subscription closed; resubscribing",
		"component", "apple_push",
		"event_kind", eventKind,
	)
	timer := time.NewTimer(subscriptionRetryDelay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (runtime *Runtime) drainOutbox(ctx context.Context) {
	for ctx.Err() == nil {
		deliveries, err := runtime.repository.PendingApplePushDeliveries(
			ctx,
			runtime.now().UTC(),
			deliveryBatchSize,
		)
		if err != nil {
			runtime.logger.Warn(
				"Apple push outbox unavailable",
				"component", "apple_push",
				"error", err,
			)
			return
		}
		if len(deliveries) == 0 {
			return
		}
		claimed := 0
		for _, delivery := range deliveries {
			if runtime.deliverOutboxEntry(ctx, delivery) {
				claimed++
			}
		}
		if len(deliveries) < deliveryBatchSize || claimed == 0 {
			return
		}
	}
}

func (runtime *Runtime) deliverOutboxEntry(
	ctx context.Context,
	delivery store.ApplePushDelivery,
) bool {
	attemptToken := deterministicUUID(
		"apple-push-attempt",
		fmt.Sprintf(
			"%s\x00%s\x00%s\x00%d",
			delivery.EventKey,
			delivery.CredentialID,
			delivery.TokenKind,
			runtime.now().UnixNano(),
		),
	)
	claimed, err := runtime.repository.ClaimApplePushDelivery(
		ctx,
		delivery.EventKey,
		delivery.CredentialID,
		delivery.TokenKind,
		attemptToken,
	)
	if err != nil {
		runtime.logger.Warn(
			"Apple push delivery could not be claimed",
			"component", "apple_push",
			"event_key", delivery.EventKey,
			"credential_id", delivery.CredentialID,
			"error", err,
		)
		return false
	}
	if !claimed {
		return false
	}

	status, errorClass, nextAttempt, deliveryErr := runtime.sendOutboxDelivery(ctx, delivery)
	finishContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	finishErr := runtime.repository.FinishApplePushDelivery(
		finishContext,
		delivery.EventKey,
		delivery.CredentialID,
		delivery.TokenKind,
		attemptToken,
		status,
		errorClass,
		nextAttempt,
	)
	cancel()
	if finishErr != nil {
		runtime.logger.Error(
			"Apple push delivery result could not be recorded",
			"component", "apple_push",
			"event_key", delivery.EventKey,
			"credential_id", delivery.CredentialID,
			"error", finishErr,
		)
		return true
	}
	if deliveryErr != nil {
		runtime.logger.Warn(
			"Apple push delivery not confirmed",
			"component", "apple_push",
			"event_key", delivery.EventKey,
			"credential_id", delivery.CredentialID,
			"status", status,
			"error_class", errorClass,
			"error", deliveryErr,
		)
	}
	return true
}

func (runtime *Runtime) sendOutboxDelivery(
	ctx context.Context,
	delivery store.ApplePushDelivery,
) (string, string, time.Time, error) {
	now := runtime.now().UTC()
	if !delivery.ExpiresAt.After(now) {
		return store.NotificationExpired, "delivery_expired", time.Time{}, nil
	}
	if !delivery.Eligible {
		return store.NotificationCancelled, "target_ineligible", time.Time{}, nil
	}
	if !delivery.ResourceLive {
		return store.NotificationExpired, "call_no_longer_ringing", time.Time{}, nil
	}
	if delivery.AttemptCount >= maximumDeliveryAttempts {
		return store.NotificationFailed, "attempt_limit", time.Time{}, nil
	}
	if delivery.BundleID != runtime.sender.BundleID() {
		return store.NotificationFailed, "topic_mismatch", time.Time{}, ErrPushTopicMismatch
	}
	if strings.TrimSpace(delivery.DeviceToken) == "" {
		return store.NotificationCancelled, "target_unavailable", time.Time{}, ErrPushTargetUnavailable
	}
	notification, err := runtime.notificationForDelivery(delivery)
	if err != nil {
		return store.NotificationFailed, "invalid_delivery", time.Time{}, err
	}
	notification.DeviceToken = delivery.DeviceToken
	notification.Environment = delivery.Environment
	notification.APNSID = deterministicUUID(
		"apple-delivery",
		delivery.EventKey+"\x00"+delivery.CredentialID+"\x00"+string(delivery.TokenKind),
	)

	deliveryContext, cancel := context.WithTimeout(ctx, runtime.deliveryTimeout)
	err = runtime.sender.Send(deliveryContext, notification)
	cancel()
	if err == nil {
		return store.NotificationAccepted, "", time.Time{}, nil
	}
	if InvalidatesToken(err) {
		clearContext, clearCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		clearErr := runtime.repository.ClearIOSPushToken(
			clearContext,
			delivery.UserID,
			delivery.TokenKind,
			delivery.DeviceToken,
		)
		clearCancel()
		if clearErr != nil {
			runtime.logger.Warn(
				"invalid Apple push token could not be cleared",
				"component", "apple_push",
				"user_id", delivery.UserID,
				"credential_id", delivery.CredentialID,
				"error", clearErr,
			)
		}
		return store.NotificationFailed, "invalid_device_token", time.Time{}, err
	}
	if explicitAPNSRetry(err) || ambiguousAPNSDelivery(err) {
		errorClass := applePushErrorClass(err)
		if ambiguousAPNSDelivery(err) {
			errorClass = "transport_ambiguous"
		}
		nextAttempt := now.Add(applePushRetryDelay(delivery.AttemptCount + 1))
		if delivery.AttemptCount+1 >= maximumDeliveryAttempts ||
			!nextAttempt.Before(delivery.ExpiresAt) {
			return store.NotificationExpired, "retry_window_expired", time.Time{}, err
		}
		return store.NotificationPending, errorClass, nextAttempt, err
	}
	return store.NotificationFailed, applePushErrorClass(err), time.Time{}, err
}

func applePushRetryDelay(attempt int) time.Duration {
	delays := [...]time.Duration{
		1 * time.Second,
		5 * time.Second,
		30 * time.Second,
		2 * time.Minute,
		10 * time.Minute,
	}
	if attempt <= 0 {
		return delays[0]
	}
	return delays[min(attempt-1, len(delays)-1)]
}

func (runtime *Runtime) notificationForDelivery(
	delivery store.ApplePushDelivery,
) (Notification, error) {
	now := runtime.now().UTC()
	switch delivery.EventType {
	case store.NotificationIncomingSMS:
		expiresAt := delivery.OccurredAt.UTC().Add(24 * time.Hour)
		if !expiresAt.After(now) {
			return Notification{}, errors.New("incoming message push expired")
		}
		messageID := strings.TrimPrefix(delivery.EventKey, "sms:")
		if messageID == "" || messageID == delivery.EventKey {
			messageID = strings.TrimSpace(delivery.ResourceID)
		}
		threadKey := store.MessageThreadKey(delivery.LineID, delivery.Peer)
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
		payload.APS.Alert.Title = truncateAlert(delivery.Peer)
		if payload.APS.Alert.Title == "" {
			payload.APS.Alert.Title = "New message"
		}
		payload.APS.Alert.Body = truncateAlert(delivery.Body)
		payload.APS.Sound = "default"
		payload.APS.ThreadID = truncateBytes(threadKey, 64)
		payload.Message.MessageID = messageID
		payload.Message.LineID = strings.TrimSpace(delivery.LineID)
		payload.Message.ThreadKey = threadKey
		return Notification{
			PushType:   PushTypeAlert,
			APNSID:     deterministicUUID("message", messageID),
			Expiration: expiresAt,
			Payload:    payload,
		}, nil
	case store.NotificationIncomingCall:
		observedAt := delivery.OccurredAt.UTC()
		if !observedAt.IsZero() && now.Sub(observedAt) > maximumCallEventAge {
			return Notification{}, errors.New("incoming call push expired")
		}
		callID := strings.TrimSpace(delivery.ResourceID)
		if callID == "" {
			return Notification{}, errors.New("incoming call has no call ID")
		}
		callUUID := deterministicUUID("callkit", callID)
		displayName := strings.TrimSpace(delivery.Body)
		if displayName == "" {
			displayName = strings.TrimSpace(delivery.Peer)
		}
		payload := struct {
			APS  struct{} `json:"aps"`
			Call struct {
				Event        string `json:"event"`
				CallID       string `json:"call_id"`
				UUID         string `json:"uuid"`
				LineID       string `json:"line_id"`
				RemoteNumber string `json:"remote_number"`
				DisplayName  string `json:"display_name,omitempty"`
			} `json:"modemdeck_call"`
		}{}
		payload.Call.Event = string(callevents.KindIncoming)
		payload.Call.CallID = callID
		payload.Call.UUID = callUUID
		payload.Call.LineID = strings.TrimSpace(delivery.LineID)
		payload.Call.RemoteNumber = strings.TrimSpace(delivery.Peer)
		payload.Call.DisplayName = displayName
		return Notification{
			PushType:   PushTypeVoIP,
			APNSID:     callUUID,
			CollapseID: callUUID,
			Expiration: observedAt.Add(30 * time.Second),
			Payload:    payload,
		}, nil
	default:
		return Notification{}, fmt.Errorf(
			"unsupported Apple push event type %q",
			delivery.EventType,
		)
	}
}

func explicitAPNSRetry(err error) bool {
	var responseError *ResponseError
	return errors.As(err, &responseError) &&
		(responseError.StatusCode == 429 || responseError.StatusCode >= 500)
}

func ambiguousAPNSDelivery(err error) bool {
	var transport *transportError
	return errors.As(err, &transport) ||
		errors.Is(err, context.Canceled) ||
		errors.Is(err, context.DeadlineExceeded)
}

func applePushErrorClass(err error) string {
	var responseError *ResponseError
	if errors.As(err, &responseError) {
		reason := strings.ToLower(strings.TrimSpace(responseError.Reason))
		if reason != "" {
			return truncateBytes("apns_"+reason, 64)
		}
		return fmt.Sprintf("apns_http_%d", responseError.StatusCode)
	}
	return "request_rejected"
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
			Event        string `json:"event"`
			CallID       string `json:"call_id"`
			UUID         string `json:"uuid"`
			RemoteNumber string `json:"remote_number"`
			DisplayName  string `json:"display_name"`
			TestCall     bool   `json:"test_call"`
		} `json:"modemdeck_call"`
	}{}
	payload.Call.Event = string(callevents.KindIncoming)
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
