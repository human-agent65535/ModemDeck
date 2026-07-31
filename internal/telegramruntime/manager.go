package telegramruntime

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/human-agent65535/modemdeck/internal/auth"
	"github.com/human-agent65535/modemdeck/internal/runtimeevents"
	"github.com/human-agent65535/modemdeck/internal/store"
	"github.com/human-agent65535/modemdeck/internal/telegram"
	"github.com/human-agent65535/modemdeck/internal/telegramsettings"
)

const (
	runtimeStatusTimeout        = 3 * time.Second
	defaultNotificationEvery    = time.Second
	notificationDeliveryLimit   = 20
	notificationDeliveryTimeout = 15 * time.Second
	defaultAPIRetryAfter        = 30 * time.Second
	retryReconcileBackoff       = 5 * time.Second
)

type Settings interface {
	List(context.Context) ([]telegramsettings.Unit, error)
	RuntimeConfig(context.Context, string) (telegram.Config, error)
	Changes() <-chan struct{}
}

type BotFactory func(string) (telegram.BotAPI, error)

type Options struct {
	BotFactory        BotFactory
	PollOptions       telegram.PollOptions
	Logger            *slog.Logger
	Now               func() time.Time
	NotificationEvery time.Duration
	RuntimeEvents     runtimeevents.Publisher
}

type Manager struct {
	settings          Settings
	communications    CommunicationService
	repository        Repository
	botFactory        BotFactory
	pollOptions       telegram.PollOptions
	logger            *slog.Logger
	now               func() time.Time
	notificationEvery time.Duration
	runtimeEvents     runtimeevents.Publisher
	commandMenus      map[string][sha256.Size]byte
}

type unitRuntime struct {
	cancel          context.CancelFunc
	done            chan struct{}
	service         *telegram.Service
	context         context.Context
	resolveContacts bool
	config          telegram.Config
}

func New(
	settings Settings,
	communications CommunicationService,
	repository Repository,
	options Options,
) (*Manager, error) {
	if settings == nil {
		return nil, errors.New("telegram runtime settings are required")
	}
	if communications == nil {
		return nil, errors.New("telegram runtime communications are required")
	}
	if repository == nil {
		return nil, errors.New("telegram runtime repository is required")
	}
	botFactory := options.BotFactory
	if botFactory == nil {
		botFactory = func(token string) (telegram.BotAPI, error) {
			return telegram.NewClient(token, telegram.ClientOptions{})
		}
	}
	logger := options.Logger
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	notificationEvery := options.NotificationEvery
	if notificationEvery <= 0 {
		notificationEvery = defaultNotificationEvery
	}
	return &Manager{
		settings:          settings,
		communications:    communications,
		repository:        repository,
		botFactory:        botFactory,
		pollOptions:       options.PollOptions,
		logger:            logger,
		now:               now,
		notificationEvery: notificationEvery,
		runtimeEvents:     options.RuntimeEvents,
		commandMenus:      make(map[string][sha256.Size]byte),
	}, nil
}

// Run reconciles enabled units once at startup and after committed settings
// or user-access changes. Telegram API rate limits are retried at their
// per-unit deadlines; other failures wait for an explicit settings boundary.
func (m *Manager) Run(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := m.repository.MarkSendingTelegramNotificationsIndeterminate(ctx); err != nil {
		return fmt.Errorf("recover Telegram notification outbox: %w", err)
	}
	retryDeadlines := make(map[string]time.Time)
	runtimes, err := m.reconcile(ctx, nil, retryDeadlines, false)
	if err != nil {
		return err
	}
	defer func() {
		stopRuntimes(runtimes)
	}()

	retryTimer := time.NewTimer(time.Hour)
	if !retryTimer.Stop() {
		<-retryTimer.C
	}
	defer retryTimer.Stop()
	retryEvents := resetRetryTimer(retryTimer, retryDeadlines)

	notifications := time.NewTicker(m.notificationEvery)
	defer notifications.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-m.settings.Changes():
			next, err := m.reconcile(ctx, runtimes, retryDeadlines, false)
			if err != nil {
				m.logger.Error("reload Telegram runtime", "error_class", classifyError(err))
			} else {
				runtimes = next
			}
			retryEvents = resetRetryTimer(retryTimer, retryDeadlines)
		case <-retryEvents:
			next, err := m.reconcile(ctx, runtimes, retryDeadlines, true)
			if err != nil {
				m.logger.Error("retry Telegram runtime", "error_class", classifyError(err))
				postponeDueRetries(retryDeadlines, retryReconcileBackoff)
			} else {
				runtimes = next
			}
			retryEvents = resetRetryTimer(retryTimer, retryDeadlines)
		case <-notifications.C:
			if err := m.dispatchNotifications(ctx, runtimes); err != nil {
				m.logger.Error("dispatch Telegram notifications", "error_class", classifyError(err))
			}
		}
	}
}

type desiredUnitRuntime struct {
	id     string
	config telegram.Config
}

func (m *Manager) reconcile(
	ctx context.Context,
	current map[string]unitRuntime,
	retryDeadlines map[string]time.Time,
	retryOnly bool,
) (map[string]unitRuntime, error) {
	units, err := m.settings.List(ctx)
	if err != nil {
		return current, fmt.Errorf("list Telegram runtime settings: %w", err)
	}
	sort.Slice(units, func(i, j int) bool {
		return units[i].ID < units[j].ID
	})

	knownUnitIDs := make(map[string]struct{}, len(units))
	desiredUnitIDs := make(map[string]struct{}, len(units))
	desired := make([]desiredUnitRuntime, 0, len(units))
	for _, unit := range units {
		knownUnitIDs[unit.ID] = struct{}{}
		if !unit.EffectiveEnabled {
			continue
		}
		config, err := m.settings.RuntimeConfig(ctx, unit.ID)
		if err != nil {
			return current, fmt.Errorf("load Telegram runtime config: %w", err)
		}
		desiredUnitIDs[unit.ID] = struct{}{}
		desired = append(desired, desiredUnitRuntime{
			id:     unit.ID,
			config: cloneTelegramConfig(config),
		})
	}

	for unitID := range retryDeadlines {
		if _, exists := desiredUnitIDs[unitID]; !exists {
			delete(retryDeadlines, unitID)
		}
	}
	for unitID := range m.commandMenus {
		if _, exists := knownUnitIDs[unitID]; !exists {
			delete(m.commandMenus, unitID)
		}
	}

	next := make(map[string]unitRuntime, len(desired))
	handledCurrent := make(map[string]struct{}, len(current))
	now := time.Now()
	for _, unit := range desired {
		existing, exists := current[unit.id]
		if retryOnly {
			deadline, scheduled := retryDeadlines[unit.id]
			if !scheduled || deadline.After(now) {
				if exists {
					handledCurrent[unit.id] = struct{}{}
					if unitRuntimeActive(existing) {
						next[unit.id] = existing
					} else {
						stopUnitRuntime(existing)
					}
				}
				continue
			}
		}
		if exists {
			handledCurrent[unit.id] = struct{}{}
			if unitRuntimeActive(existing) &&
				telegramConfigsEqual(existing.config, unit.config) {
				next[unit.id] = existing
				delete(retryDeadlines, unit.id)
				continue
			}
			stopUnitRuntime(existing)
		}

		if !retryOnly {
			delete(retryDeadlines, unit.id)
		}

		runtime, err := m.startUnit(ctx, unit.id, unit.config)
		if err != nil {
			m.recordFailure(unit.id, err)
			if delay, retry := telegramAPIRetryDelay(err); retry {
				retryDeadlines[unit.id] = time.Now().Add(delay)
			} else {
				delete(retryDeadlines, unit.id)
			}
			continue
		}
		delete(retryDeadlines, unit.id)
		next[unit.id] = runtime
	}
	for unitID, runtime := range current {
		if _, handled := handledCurrent[unitID]; handled {
			continue
		}
		stopUnitRuntime(runtime)
	}
	return next, nil
}

func (m *Manager) startUnit(
	parent context.Context,
	unitID string,
	config telegram.Config,
) (unitRuntime, error) {
	runtimeParent := parent
	if config.Principal != nil {
		runtimeParent = auth.ContextWithPrincipal(parent, *config.Principal)
	}
	bot, err := m.botFactory(config.BotToken)
	if err != nil {
		return unitRuntime{}, err
	}
	dependencies := adapters{
		communications:              m.communications,
		repository:                  m.repository,
		runtimeEvents:               m.runtimeEvents,
		resolveContacts:             config.ResolveContacts,
		contactResolutionConfigured: true,
	}
	service, err := telegram.NewService(config, telegram.Dependencies{
		Bot:       bot,
		Lines:     dependencies,
		SMS:       dependencies,
		Calls:     dependencies,
		SMSSender: dependencies,
		Replies:   dependencies,
		Read:      dependencies,
		CallRead:  dependencies,
		Observer: telegram.ObserverFunc(func(_ context.Context, event telegram.Event) {
			m.logger.Info(
				"Telegram runtime event",
				"kind", event.Kind,
				"operation", event.Operation,
				"error_class", event.ErrorClass,
			)
		}),
	})
	if err != nil {
		return unitRuntime{}, err
	}
	verifyContext, verifyCancel := context.WithTimeout(runtimeParent, 10*time.Second)
	tokenFingerprint := sha256.Sum256([]byte(config.BotToken))
	registeredFingerprint, commandsRegistered := m.commandMenus[unitID]
	var user telegram.BotUser
	if commandsRegistered && registeredFingerprint == tokenFingerprint {
		user, err = service.VerifyBot(verifyContext)
	} else {
		user, err = service.InitializeBot(verifyContext)
		if err == nil {
			m.commandMenus[unitID] = tokenFingerprint
		}
	}
	verifyCancel()
	if err != nil {
		return unitRuntime{}, err
	}
	statusContext, statusCancel := context.WithTimeout(context.WithoutCancel(parent), runtimeStatusTimeout)
	err = m.repository.UpdateTelegramRuntimeStatus(
		statusContext,
		unitID,
		user.Username,
		m.now().UTC().Format(time.RFC3339Nano),
		"",
	)
	statusCancel()
	if err != nil {
		return unitRuntime{}, err
	}

	poller, err := telegram.NewPoller(
		bot,
		service,
		checkpoint{unitID: unitID, repository: m.repository},
		telegram.ObserverFunc(func(_ context.Context, event telegram.Event) {
			m.logger.Info(
				"Telegram polling event",
				"kind", event.Kind,
				"operation", event.Operation,
				"error_class", event.ErrorClass,
			)
		}),
		m.pollOptions,
	)
	if err != nil {
		return unitRuntime{}, err
	}

	unitContext, cancel := context.WithCancel(runtimeParent)
	done := make(chan struct{})
	go func() {
		defer close(done)
		err := poller.Run(unitContext)
		if err == nil || errors.Is(err, context.Canceled) || unitContext.Err() != nil {
			return
		}
		m.recordFailure(unitID, err)
	}()
	return unitRuntime{
		cancel:          cancel,
		done:            done,
		service:         service,
		context:         unitContext,
		resolveContacts: config.ResolveContacts,
		config:          cloneTelegramConfig(config),
	}, nil
}

func (m *Manager) recordFailure(unitID string, err error) {
	errorClass := classifyError(err)
	statusContext, cancel := context.WithTimeout(context.Background(), runtimeStatusTimeout)
	defer cancel()
	if statusErr := m.repository.UpdateTelegramRuntimeStatus(
		statusContext,
		unitID,
		"",
		"",
		errorClass,
	); statusErr != nil {
		m.logger.Error("record Telegram runtime status", "error_class", classifyError(statusErr))
	}
	logFields := []any{"error_class", errorClass}
	var operationError *telegram.OperationError
	if errors.As(err, &operationError) {
		logFields = append(logFields, "operation", operationError.Operation)
	}
	var apiError *telegram.APIError
	if errors.As(err, &apiError) {
		logFields = append(logFields, "api_code", apiError.Code)
		if apiError.RetryAfter > 0 {
			logFields = append(
				logFields,
				"retry_after_seconds",
				int64(apiError.RetryAfter/time.Second),
			)
		}
	}
	m.logger.Warn("Telegram unit stopped", logFields...)
}

func telegramAPIRetryDelay(err error) (time.Duration, bool) {
	var apiError *telegram.APIError
	if !errors.As(err, &apiError) || apiError.Code != 429 {
		return 0, false
	}
	if apiError.RetryAfter > 0 {
		return apiError.RetryAfter, true
	}
	return defaultAPIRetryAfter, true
}

func resetRetryTimer(
	timer *time.Timer,
	deadlines map[string]time.Time,
) <-chan time.Time {
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
	deadline, exists := earliestRetryDeadline(deadlines)
	if !exists {
		return nil
	}
	delay := time.Until(deadline)
	if delay < 0 {
		delay = 0
	}
	timer.Reset(delay)
	return timer.C
}

func earliestRetryDeadline(deadlines map[string]time.Time) (time.Time, bool) {
	var earliest time.Time
	for _, deadline := range deadlines {
		if earliest.IsZero() || deadline.Before(earliest) {
			earliest = deadline
		}
	}
	return earliest, !earliest.IsZero()
}

func postponeDueRetries(deadlines map[string]time.Time, delay time.Duration) {
	now := time.Now()
	next := now.Add(delay)
	for unitID, deadline := range deadlines {
		if !deadline.After(now) {
			deadlines[unitID] = next
		}
	}
}

func telegramConfigsEqual(left, right telegram.Config) bool {
	return left.Enabled == right.Enabled &&
		left.BotToken == right.BotToken &&
		left.ChatID == right.ChatID &&
		left.AdminID == right.AdminID &&
		left.LineScopeMode == right.LineScopeMode &&
		left.ResolveContacts == right.ResolveContacts &&
		left.Notifications == right.Notifications &&
		slices.Equal(left.LineScopes, right.LineScopes) &&
		telegramPrincipalsEqual(left.Principal, right.Principal)
}

func telegramPrincipalsEqual(left, right *auth.Principal) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.UserID == right.UserID &&
		left.Username == right.Username &&
		left.Role == right.Role &&
		left.ProfileContactID == right.ProfileContactID &&
		slices.Equal(left.AllowedLineIDs, right.AllowedLineIDs)
}

func cloneTelegramConfig(config telegram.Config) telegram.Config {
	config.LineScopes = slices.Clone(config.LineScopes)
	if config.Principal != nil {
		principal := config.Principal.Copy()
		config.Principal = &principal
	}
	return config
}

func unitRuntimeActive(runtime unitRuntime) bool {
	select {
	case <-runtime.done:
		return false
	default:
		return true
	}
}

func stopUnitRuntime(runtime unitRuntime) {
	runtime.cancel()
	<-runtime.done
}

func stopRuntimes(runtimes map[string]unitRuntime) {
	if len(runtimes) == 0 {
		return
	}
	for _, runtime := range runtimes {
		runtime.cancel()
	}
	var wait sync.WaitGroup
	for _, runtime := range runtimes {
		wait.Add(1)
		go func(done <-chan struct{}) {
			defer wait.Done()
			<-done
		}(runtime.done)
	}
	wait.Wait()
}

func classifyError(err error) string {
	if err == nil {
		return ""
	}
	var operationError *telegram.OperationError
	var apiError *telegram.APIError
	var transportError *telegram.TransportError
	var protocolError *telegram.ProtocolError
	var pollingError *telegram.PollingError
	switch {
	case errors.As(err, &operationError):
		return operationError.Kind
	case errors.As(err, &apiError):
		return "telegram_api"
	case errors.As(err, &transportError):
		return "transport"
	case errors.As(err, &protocolError):
		return "protocol"
	case errors.As(err, &pollingError):
		return "polling"
	default:
		return "dependency"
	}
}

func (m *Manager) dispatchNotifications(
	ctx context.Context,
	runtimes map[string]unitRuntime,
) error {
	if len(runtimes) == 0 {
		return nil
	}
	deliveries, err := m.repository.PendingTelegramNotificationDeliveries(
		ctx,
		notificationDeliveryLimit,
	)
	if err != nil {
		return err
	}
	for _, delivery := range deliveries {
		runtime, ok := runtimes[delivery.UnitID]
		if !ok || runtime.service == nil {
			continue
		}
		attemptToken, err := newAttemptToken()
		if err != nil {
			return err
		}
		claimed, err := m.repository.ClaimTelegramNotificationDelivery(
			ctx,
			delivery.EventKey,
			delivery.UnitID,
			attemptToken,
		)
		if err != nil {
			return err
		}
		if !claimed {
			continue
		}

		deliveryParent := runtime.context
		if deliveryParent == nil {
			deliveryParent = ctx
		}
		deliveryContext, cancel := context.WithTimeout(deliveryParent, notificationDeliveryTimeout)
		deliveryErr := notify(
			deliveryContext,
			runtime.service,
			m.repository,
			runtime.resolveContacts,
			delivery,
		)
		cancel()
		status := store.NotificationSent
		errorClass := ""
		if deliveryErr != nil {
			errorClass = classifyError(deliveryErr)
			status = notificationFailureStatus(deliveryErr)
		}
		finishContext, finishCancel := context.WithTimeout(context.WithoutCancel(ctx), runtimeStatusTimeout)
		finishErr := m.repository.FinishTelegramNotificationDelivery(
			finishContext,
			delivery.EventKey,
			delivery.UnitID,
			attemptToken,
			status,
			errorClass,
		)
		finishCancel()
		if finishErr != nil {
			return finishErr
		}
		if deliveryErr != nil {
			m.logger.Warn(
				"Telegram notification not confirmed",
				"status", status,
				"error_class", errorClass,
			)
		}
	}
	return nil
}

func notify(
	ctx context.Context,
	service *telegram.Service,
	repository Repository,
	resolveContacts bool,
	delivery store.TelegramNotificationDelivery,
) error {
	contactName := ""
	if resolveContacts {
		if resolver, ok := repository.(contactNameRepository); ok {
			var err error
			contactName, err = resolver.ContactNameForNumber(ctx, delivery.Peer)
			if err != nil {
				return err
			}
		}
	}
	switch delivery.EventType {
	case store.NotificationIncomingSMS:
		return service.NotifyIncomingSMS(ctx, telegram.IncomingSMS{
			MessageID:   delivery.ResourceID,
			LineID:      delivery.LineID,
			From:        delivery.Peer,
			ContactName: contactName,
			Body:        delivery.Body,
			ReceivedAt:  delivery.OccurredAt,
		})
	case store.NotificationMissedCall:
		return service.NotifyMissedCall(ctx, telegram.MissedCall{
			CallID:      delivery.ResourceID,
			LineID:      delivery.LineID,
			From:        delivery.Peer,
			ContactName: contactName,
			CalledAt:    delivery.OccurredAt,
		})
	default:
		return fmt.Errorf("unsupported Telegram notification event type")
	}
}

func notificationFailureStatus(err error) string {
	var apiError *telegram.APIError
	var capabilityError *telegram.CapabilityUnavailableError
	switch {
	case errors.As(err, &apiError) && apiError.Code >= 400 && apiError.Code < 500 && apiError.Code != 429:
		return store.NotificationFailed
	case errors.As(err, &capabilityError):
		return store.NotificationFailed
	default:
		return store.NotificationIndeterminate
	}
}

func newAttemptToken() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", fmt.Errorf("generate Telegram notification attempt: %w", err)
	}
	return hex.EncodeToString(value[:]), nil
}
