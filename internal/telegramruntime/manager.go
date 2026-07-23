package telegramruntime

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/human-agent65535/modemdeck/internal/store"
	"github.com/human-agent65535/modemdeck/internal/telegram"
	"github.com/human-agent65535/modemdeck/internal/telegramsettings"
)

const (
	runtimeStatusTimeout        = 3 * time.Second
	defaultNotificationEvery    = time.Second
	notificationDeliveryLimit   = 20
	notificationDeliveryTimeout = 15 * time.Second
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
}

type unitRuntime struct {
	cancel  context.CancelFunc
	done    chan struct{}
	service *telegram.Service
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
	}, nil
}

// Run reconciles enabled units once at startup and after committed settings
// changes. A failed poller is not restarted until one of those explicit
// boundaries, preventing an unbounded recovery loop around invalid settings or
// credentials.
func (m *Manager) Run(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := m.repository.MarkSendingTelegramNotificationsIndeterminate(ctx); err != nil {
		return fmt.Errorf("recover Telegram notification outbox: %w", err)
	}
	runtimes, err := m.reconcile(ctx, nil)
	if err != nil {
		return err
	}
	defer stopRuntimes(runtimes)
	notifications := time.NewTicker(m.notificationEvery)
	defer notifications.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-m.settings.Changes():
			next, err := m.reconcile(ctx, runtimes)
			if err != nil {
				m.logger.Error("reload Telegram runtime", "error_class", classifyError(err))
				continue
			}
			runtimes = next
		case <-notifications.C:
			if err := m.dispatchNotifications(ctx, runtimes); err != nil {
				m.logger.Error("dispatch Telegram notifications", "error_class", classifyError(err))
			}
		}
	}
}

func (m *Manager) reconcile(
	ctx context.Context,
	current map[string]unitRuntime,
) (map[string]unitRuntime, error) {
	stopRuntimes(current)
	units, err := m.settings.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list Telegram runtime settings: %w", err)
	}
	sort.Slice(units, func(i, j int) bool {
		return units[i].ID < units[j].ID
	})
	next := make(map[string]unitRuntime)
	for _, unit := range units {
		if !unit.Enabled {
			continue
		}
		runtime, err := m.startUnit(ctx, unit.ID)
		if err != nil {
			m.recordFailure(unit.ID, err)
			continue
		}
		next[unit.ID] = runtime
	}
	return next, nil
}

func (m *Manager) startUnit(parent context.Context, unitID string) (unitRuntime, error) {
	config, err := m.settings.RuntimeConfig(parent, unitID)
	if err != nil {
		return unitRuntime{}, err
	}
	bot, err := m.botFactory(config.BotToken)
	if err != nil {
		return unitRuntime{}, err
	}
	dependencies := adapters{
		communications: m.communications,
		repository:     m.repository,
	}
	service, err := telegram.NewService(config, telegram.Dependencies{
		Bot:       bot,
		Lines:     dependencies,
		SMS:       dependencies,
		SMSSender: dependencies,
		Dialer:    dependencies,
		Replies:   dependencies,
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
	verifyContext, verifyCancel := context.WithTimeout(parent, 10*time.Second)
	user, err := service.VerifyBot(verifyContext)
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

	unitContext, cancel := context.WithCancel(parent)
	done := make(chan struct{})
	go func() {
		defer close(done)
		err := poller.Run(unitContext)
		if err == nil || errors.Is(err, context.Canceled) || unitContext.Err() != nil {
			return
		}
		m.recordFailure(unitID, err)
	}()
	return unitRuntime{cancel: cancel, done: done, service: service}, nil
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
	m.logger.Warn("Telegram unit stopped", "error_class", errorClass)
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

		deliveryContext, cancel := context.WithTimeout(ctx, notificationDeliveryTimeout)
		deliveryErr := notify(deliveryContext, runtime.service, delivery)
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
	delivery store.TelegramNotificationDelivery,
) error {
	switch delivery.EventType {
	case store.NotificationIncomingSMS:
		return service.NotifyIncomingSMS(ctx, telegram.IncomingSMS{
			MessageID:  delivery.ResourceID,
			LineID:     delivery.LineID,
			From:       delivery.Peer,
			Body:       delivery.Body,
			ReceivedAt: delivery.OccurredAt,
		})
	case store.NotificationMissedCall:
		return service.NotifyMissedCall(ctx, telegram.MissedCall{
			CallID:   delivery.ResourceID,
			LineID:   delivery.LineID,
			From:     delivery.Peer,
			CalledAt: delivery.OccurredAt,
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
