package networkruntime

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"reflect"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/human-agent65535/modemdeck/internal/agentclient"
	"github.com/human-agent65535/modemdeck/internal/runtimeevents"
	"github.com/human-agent65535/modemdeck/internal/store"
)

const (
	DefaultInterval       = 5 * time.Second
	DefaultApplyAttempts  = 3
	maxProxyCount         = 64
	maxProxyIDLength      = 64
	maxProxyNameRunes     = 100
	maxLineIDLength       = 128
	maxUsernameBytes      = 128
	maxPasswordBytes      = 512
	maxSOCKS5SecretBytes  = 255
	generatedProxyIDBytes = 16
)

type Repository interface {
	Lines(context.Context) ([]store.LineSummary, error)
	ResolveLineEndpoint(context.Context, string) (string, error)
	ProxyInstances(context.Context) ([]store.ProxyInstanceRecord, error)
	ProxyInstance(context.Context, string) (store.ProxyInstanceRecord, error)
	CreateProxyInstance(
		context.Context,
		store.ProxyInstanceRecord,
	) (store.ProxyInstanceRecord, error)
	UpdateProxyInstance(
		context.Context,
		store.ProxyInstanceRecord,
		int64,
	) (store.ProxyInstanceRecord, error)
	DeleteProxyInstance(context.Context, string, int64) error
	FinalizeProxyApply(
		context.Context,
		[]store.ProxyApplyToken,
	) (bool, error)
	ApplyNetworkCounterSnapshot(
		context.Context,
		[]store.NetworkCounterSample,
		[]string,
	) error
	NetworkUsage(context.Context, string, string) ([]store.NetworkUsage, error)
	NetworkSelectionPolicy(
		context.Context,
		string,
	) (store.NetworkSelectionPolicyRecord, error)
	EnsureNetworkSelectionPolicy(
		context.Context,
		string,
	) (store.NetworkSelectionPolicyRecord, error)
	NetworkSelectionPolicies(context.Context) ([]store.NetworkSelectionPolicyRecord, error)
	UpdateNetworkSelectionPolicy(
		context.Context,
		string,
		string,
		string,
		int64,
	) (store.NetworkSelectionPolicyRecord, error)
	MarkNetworkSelectionApplied(
		context.Context,
		string,
		int64,
		string,
		time.Time,
	) (bool, error)
	MarkNetworkSelectionApplyFailed(context.Context, string, int64, string) error
}

type SecretBox interface {
	Seal([]byte, []byte) ([]byte, []byte, error)
	Open([]byte, []byte, []byte) ([]byte, error)
}

type Agent interface {
	Network(context.Context) (agentclient.NetworkSnapshot, error)
	Snapshot(context.Context) (agentclient.Snapshot, error)
	PutProxies(
		context.Context,
		[]agentclient.ProxyConfiguration,
	) (agentclient.NetworkSnapshot, error)
	ScanNetworks(
		context.Context,
		string,
		agentclient.NetworkScanRequest,
	) (agentclient.NetworkScanResult, error)
	SetNetworkSelection(
		context.Context,
		string,
		agentclient.ApplyNetworkSelectionRequest,
	) (agentclient.NetworkSelectionReceipt, error)
}

type Proxy struct {
	ID              string                `json:"id"`
	Name            string                `json:"name"`
	LineID          string                `json:"line_id"`
	Enabled         bool                  `json:"enabled"`
	Mode            agentclient.ProxyMode `json:"mode"`
	ListenAddress   string                `json:"listen_address"`
	ListenPort      uint16                `json:"listen_port"`
	AuthEnabled     bool                  `json:"auth_enabled"`
	Username        string                `json:"username"`
	HasPassword     bool                  `json:"has_password"`
	Revision        int64                 `json:"revision"`
	AppliedRevision int64                 `json:"applied_revision"`
	ApplyState      ProxyApplyState       `json:"apply_state"`
	CreatedAt       string                `json:"created_at"`
	UpdatedAt       string                `json:"updated_at"`
}

type ProxyApplyState string

const (
	ProxyApplyStateApplied       ProxyApplyState = "applied"
	ProxyApplyStatePendingCreate ProxyApplyState = "pending_create"
	ProxyApplyStatePendingUpdate ProxyApplyState = "pending_update"
	ProxyApplyStatePendingDelete ProxyApplyState = "pending_delete"
)

type CreateInput struct {
	ID            string
	Name          string
	LineID        string
	Enabled       bool
	Mode          agentclient.ProxyMode
	ListenAddress string
	ListenPort    uint16
	AuthEnabled   bool
	Username      string
	Password      string
}

type UpdateInput struct {
	Revision      int64
	Name          *string
	LineID        *string
	Enabled       *bool
	Mode          *agentclient.ProxyMode
	ListenAddress *string
	ListenPort    *uint16
	AuthEnabled   *bool
	Username      *string
	Password      *string
}

type ApplyStatus string

const (
	ApplyStatusPending            ApplyStatus = "pending"
	ApplyStatusApplied            ApplyStatus = "applied"
	ApplyStatusAgentUnavailable   ApplyStatus = "agent_unavailable"
	ApplyStatusAgentRejected      ApplyStatus = "agent_rejected"
	ApplyStatusRuntimeUnavailable ApplyStatus = "runtime_unavailable"
)

type ApplyResult struct {
	Applied bool        `json:"applied"`
	Status  ApplyStatus `json:"status"`
}

type ProxyMutation struct {
	Proxy Proxy `json:"proxy"`
	ApplyResult
}

type DeleteResult struct {
	ID string `json:"id"`
	ApplyResult
}

type Usage struct {
	ScopeKind store.NetworkScopeKind `json:"scope_kind"`
	ScopeID   string                 `json:"scope_id"`
	RXBytes   uint64                 `json:"rx_bytes"`
	TXBytes   uint64                 `json:"tx_bytes"`
}

type UsageTotal struct {
	RXBytes uint64 `json:"rx_bytes"`
	TXBytes uint64 `json:"tx_bytes"`
}

type Status struct {
	Available         bool                       `json:"available"`
	State             string                     `json:"state"`
	UnavailableReason string                     `json:"unavailable_reason,omitempty"`
	BootEpoch         string                     `json:"boot_epoch"`
	ObservedAt        *time.Time                 `json:"observed_at"`
	Lines             []agentclient.NetworkLine  `json:"lines"`
	Proxies           []agentclient.NetworkProxy `json:"proxies"`
	TodayTotal        UsageTotal                 `json:"today_total"`
	TodayUsage        []Usage                    `json:"today_usage"`
	MonthTotal        UsageTotal                 `json:"month_total"`
	MonthUsage        []Usage                    `json:"month_usage"`
	Stale             bool                       `json:"stale"`
	ApplyPending      bool                       `json:"apply_pending"`
	ApplyStatus       ApplyStatus                `json:"apply_status"`
	ApplyAttempts     int                        `json:"apply_attempts"`
	ApplyExhausted    bool                       `json:"apply_exhausted"`
}

type Options struct {
	Interval         time.Duration
	MaxApplyAttempts int
	Location         *time.Location
	Now              func() time.Time
	Random           io.Reader
	Report           func(error)
	RuntimeEvents    runtimeevents.Publisher
}

type Service struct {
	repository       Repository
	secrets          SecretBox
	agent            Agent
	interval         time.Duration
	maxApplyAttempts int
	location         *time.Location
	now              func() time.Time
	random           io.Reader
	report           func(error)
	runtimeEvents    runtimeevents.Publisher

	mutationsMu sync.Mutex
	randomMu    sync.Mutex
	reconcileMu sync.Mutex
	stateMu     sync.RWMutex
	state       Status
}

func New(
	repository Repository,
	secrets SecretBox,
	agent Agent,
	options Options,
) (*Service, error) {
	if repository == nil {
		return nil, operationError(
			CodeInvalidArgument,
			"",
			"Network settings repository is required",
			nil,
		)
	}
	if secrets == nil {
		return nil, operationError(
			CodeInvalidArgument,
			"",
			"Network settings encryption is required",
			nil,
		)
	}
	if agent == nil {
		return nil, operationError(
			CodeInvalidArgument,
			"",
			"Host agent client is required",
			nil,
		)
	}
	if options.Interval <= 0 {
		options.Interval = DefaultInterval
	}
	if options.MaxApplyAttempts <= 0 {
		options.MaxApplyAttempts = DefaultApplyAttempts
	}
	if options.Location == nil {
		options.Location = time.Local
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.Random == nil {
		options.Random = rand.Reader
	}
	if options.Report == nil {
		options.Report = func(error) {}
	}
	return &Service{
		repository:       repository,
		secrets:          secrets,
		agent:            agent,
		interval:         options.Interval,
		maxApplyAttempts: options.MaxApplyAttempts,
		location:         options.Location,
		now:              options.Now,
		random:           options.Random,
		report:           options.Report,
		runtimeEvents:    options.RuntimeEvents,
		state: Status{
			State:             "unavailable",
			UnavailableReason: "Network runtime has not synchronized",
			Lines:             []agentclient.NetworkLine{},
			Proxies:           []agentclient.NetworkProxy{},
			TodayUsage:        []Usage{},
			MonthUsage:        []Usage{},
			ApplyPending:      true,
			ApplyStatus:       ApplyStatusPending,
		},
	}, nil
}

func (s *Service) Run(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	s.Reconcile(ctx)
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if s.shouldApply() {
				s.Reconcile(ctx)
				continue
			}
			if err := s.Refresh(ctx); err != nil {
				s.report(err)
				continue
			}
			selectionPending, err := s.networkSelectionReplayPending(
				ctx,
				s.currentBootEpoch(),
			)
			if err != nil {
				s.report(err)
				continue
			}
			if selectionPending {
				s.markDirty()
				s.Reconcile(ctx)
			}
		}
	}
}

func (s *Service) Reconcile(ctx context.Context) ApplyResult {
	s.reconcileMu.Lock()
	defer s.reconcileMu.Unlock()
	s.noteApplyAttempt()

	records, err := s.repository.ProxyInstances(ctx)
	if err != nil {
		return s.applyFailedWithRefresh(
			ctx,
			ApplyStatusRuntimeUnavailable,
			"load desired proxy settings",
			err,
		)
	}
	desired := make([]agentclient.ProxyConfiguration, 0, len(records))
	tokens := make([]store.ProxyApplyToken, 0, len(records))
	for _, record := range records {
		tokens = append(tokens, store.ProxyApplyToken{
			ID:             record.ID,
			Revision:       record.Revision,
			DesiredDeleted: record.DesiredDeleted,
		})
		if record.DesiredDeleted {
			continue
		}
		proxy, err := s.desiredProxy(ctx, record)
		if err != nil {
			return s.applyFailedWithRefresh(
				ctx,
				ApplyStatusRuntimeUnavailable,
				"load stored proxy credentials",
				err,
			)
		}
		desired = append(desired, proxy)
	}
	snapshot, err := s.agent.PutProxies(ctx, desired)
	if err != nil {
		return s.applyFailedWithRefresh(
			ctx,
			classifyAgentApplyError(err),
			"reconcile host proxy state",
			err,
		)
	}
	if err := validateReconciledSnapshot(desired, snapshot); err != nil {
		return s.applyFailedWithRefresh(
			ctx,
			ApplyStatusAgentRejected,
			"validate reconciled proxy state",
			err,
		)
	}
	pending, err := s.repository.FinalizeProxyApply(ctx, tokens)
	if err != nil {
		publicSnapshot, endpointIDs, bindErr := s.bindNetworkSnapshot(ctx, snapshot)
		if bindErr != nil {
			s.report(bindErr)
		} else if observeErr := s.observeSnapshot(
			ctx,
			publicSnapshot,
			endpointIDs,
		); observeErr != nil {
			s.report(observeErr)
		}
		if bindErr == nil {
			s.setSnapshot(publicSnapshot)
		}
		s.markApplyFailure(ApplyStatusRuntimeUnavailable)
		s.report(fmt.Errorf("finalize applied proxy state: %w", err))
		return ApplyResult{Status: ApplyStatusRuntimeUnavailable}
	}
	fullSnapshot, err := s.agent.Snapshot(ctx)
	if err != nil {
		publicSnapshot, endpointIDs, bindErr := s.bindNetworkSnapshot(ctx, snapshot)
		if bindErr != nil {
			s.report(bindErr)
		} else if observeErr := s.observeSnapshot(
			ctx,
			publicSnapshot,
			endpointIDs,
		); observeErr != nil {
			s.report(observeErr)
		}
		if bindErr == nil {
			s.setSnapshot(publicSnapshot)
		}
		status := classifyAgentApplyError(err)
		s.markApplyFailure(status)
		s.report(fmt.Errorf("read lines for network selection reconciliation: %w", err))
		return ApplyResult{Status: status}
	}
	selectionPending, err := s.reconcileNetworkSelections(ctx, snapshot, fullSnapshot)
	if err != nil {
		publicSnapshot, endpointIDs, bindErr := s.bindNetworkSnapshot(ctx, snapshot)
		if bindErr != nil {
			s.report(bindErr)
		} else if observeErr := s.observeSnapshot(
			ctx,
			publicSnapshot,
			endpointIDs,
		); observeErr != nil {
			s.report(observeErr)
		}
		if bindErr == nil {
			s.setSnapshot(publicSnapshot)
		}
		status := classifyAgentApplyError(err)
		s.markApplyFailure(status)
		s.report(fmt.Errorf("reconcile network selection policies: %w", err))
		return ApplyResult{Status: status}
	}
	publicSnapshot, endpointIDs, err := s.bindNetworkSnapshot(ctx, snapshot)
	if err != nil {
		return s.applyFailedWithRefresh(
			ctx,
			ApplyStatusRuntimeUnavailable,
			"resolve host network line identities",
			err,
		)
	}
	if err := s.observeSnapshot(ctx, publicSnapshot, endpointIDs); err != nil {
		s.report(err)
	}
	s.setSnapshot(publicSnapshot)
	if pending || selectionPending {
		s.markApplyPending()
		return ApplyResult{Status: ApplyStatusPending}
	}
	s.markApplySuccess()
	return ApplyResult{Applied: true, Status: ApplyStatusApplied}
}

func (s *Service) Refresh(ctx context.Context) error {
	s.reconcileMu.Lock()
	defer s.reconcileMu.Unlock()
	return s.refreshLocked(ctx)
}

func (s *Service) refreshLocked(ctx context.Context) error {
	s.stateMu.RLock()
	previousBootEpoch := s.state.BootEpoch
	wasAvailable := s.state.Available
	s.stateMu.RUnlock()
	snapshot, err := s.agent.Network(ctx)
	if err != nil {
		s.setUnavailable("Host agent is unavailable")
		return fmt.Errorf("refresh host network state: %w", err)
	}
	publicSnapshot, endpointIDs, err := s.bindNetworkSnapshot(ctx, snapshot)
	if err != nil {
		s.setUnavailable("Network line identities are unavailable")
		return fmt.Errorf("resolve host network line identities: %w", err)
	}
	if err := s.observeSnapshot(ctx, publicSnapshot, endpointIDs); err != nil {
		s.report(err)
	}
	s.setSnapshot(publicSnapshot)
	if snapshot.BootEpoch != previousBootEpoch || !wasAvailable {
		pending, pendingErr := s.networkSelectionReplayPending(
			ctx,
			snapshot.BootEpoch,
		)
		if pendingErr != nil {
			return fmt.Errorf("inspect network selection replay state: %w", pendingErr)
		}
		if pending {
			s.markDirty()
		}
	}
	return nil
}

func (s *Service) applyFailedWithRefresh(
	ctx context.Context,
	status ApplyStatus,
	operation string,
	err error,
) ApplyResult {
	s.markApplyFailure(status)
	s.report(fmt.Errorf("%s: %w", operation, err))
	if refreshErr := s.refreshLocked(ctx); refreshErr != nil {
		s.report(refreshErr)
	}
	return ApplyResult{Status: status}
}

func (s *Service) Status(ctx context.Context) (Status, error) {
	s.stateMu.RLock()
	status := cloneStatus(s.state)
	s.stateMu.RUnlock()

	now := s.now().In(s.location)
	today := now.Format(time.DateOnly)
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, s.location).
		Format(time.DateOnly)
	todayUsage, err := s.repository.NetworkUsage(ctx, today, today)
	if err != nil {
		return Status{}, operationError(
			CodeInternal,
			"",
			"Today's network usage could not be loaded",
			err,
		)
	}
	monthUsage, err := s.repository.NetworkUsage(ctx, monthStart, today)
	if err != nil {
		return Status{}, operationError(
			CodeInternal,
			"",
			"Monthly network usage could not be loaded",
			err,
		)
	}
	status.TodayUsage = publicUsage(todayUsage)
	status.TodayTotal = totalLineUsage(status.TodayUsage)
	status.MonthUsage = publicUsage(monthUsage)
	status.MonthTotal = totalLineUsage(status.MonthUsage)
	return status, nil
}

func (s *Service) Proxies(ctx context.Context) ([]Proxy, error) {
	records, err := s.repository.ProxyInstances(ctx)
	if err != nil {
		return nil, classifyStoreError(err)
	}
	proxies := make([]Proxy, 0, len(records))
	for _, record := range records {
		proxies = append(proxies, publicProxy(record))
	}
	return proxies, nil
}

func (s *Service) Create(ctx context.Context, input CreateInput) (ProxyMutation, error) {
	s.mutationsMu.Lock()
	defer s.mutationsMu.Unlock()

	id := strings.TrimSpace(input.ID)
	if id == "" {
		var err error
		id, err = s.newProxyID()
		if err != nil {
			return ProxyMutation{}, operationError(
				CodeUnavailable,
				"id",
				"Proxy ID could not be generated",
				err,
			)
		}
	}
	if !validProxyID(id) {
		return ProxyMutation{}, invalid("id", "Proxy ID is invalid")
	}
	if _, err := s.repository.ProxyInstance(ctx, id); err == nil {
		return ProxyMutation{}, operationError(
			CodeConflict,
			"id",
			"Proxy ID is already in use",
			nil,
		)
	} else if !errors.Is(err, store.ErrProxyInstanceNotFound) {
		return ProxyMutation{}, classifyStoreError(err)
	}

	record, err := s.buildRecord(id, store.ProxyInstanceRecord{}, proxyFields{
		Name:          input.Name,
		LineID:        input.LineID,
		Enabled:       input.Enabled,
		Mode:          input.Mode,
		ListenAddress: input.ListenAddress,
		ListenPort:    input.ListenPort,
		AuthEnabled:   input.AuthEnabled,
		Username:      input.Username,
		Password:      &input.Password,
	})
	if err != nil {
		return ProxyMutation{}, err
	}
	records, err := s.repository.ProxyInstances(ctx)
	if err != nil {
		return ProxyMutation{}, classifyStoreError(err)
	}
	if err := validateProxyCollection(record, records, true); err != nil {
		return ProxyMutation{}, err
	}
	created, err := s.repository.CreateProxyInstance(ctx, record)
	if err != nil {
		return ProxyMutation{}, classifyStoreError(err)
	}
	s.markDirty()
	applyResult := s.Reconcile(ctx)
	if latest, err := s.repository.ProxyInstance(ctx, created.ID); err == nil {
		created = latest
	} else if applyResult.Applied {
		return ProxyMutation{}, classifyStoreError(err)
	}
	return ProxyMutation{
		Proxy:       publicProxy(created),
		ApplyResult: applyResult,
	}, nil
}

func (s *Service) Update(
	ctx context.Context,
	id string,
	input UpdateInput,
) (ProxyMutation, error) {
	s.mutationsMu.Lock()
	defer s.mutationsMu.Unlock()

	id = strings.TrimSpace(id)
	if !validProxyID(id) {
		return ProxyMutation{}, invalid("id", "Proxy ID is invalid")
	}
	if input.Revision <= 0 {
		return ProxyMutation{}, invalid("revision", "A positive revision is required")
	}
	if !hasUpdate(input) {
		return ProxyMutation{}, invalid("", "At least one proxy field must be updated")
	}
	current, err := s.repository.ProxyInstance(ctx, id)
	if err != nil {
		return ProxyMutation{}, classifyStoreError(err)
	}
	if current.DesiredDeleted {
		return ProxyMutation{}, operationError(
			CodeConflict,
			"revision",
			"Proxy is pending deletion",
			store.ErrProxyInstanceRevisionConflict,
		)
	}
	if current.Revision != input.Revision {
		return ProxyMutation{}, operationError(
			CodeConflict,
			"revision",
			"Proxy changed since it was loaded",
			store.ErrProxyInstanceRevisionConflict,
		)
	}
	fields := proxyFields{
		Name:          current.Name,
		LineID:        current.LineID,
		Enabled:       current.Enabled,
		Mode:          agentclient.ProxyMode(current.Mode),
		ListenAddress: current.ListenAddress,
		ListenPort:    current.ListenPort,
		AuthEnabled:   current.AuthEnabled,
		Username:      current.Username,
		Password:      input.Password,
	}
	if input.Name != nil {
		fields.Name = *input.Name
	}
	if input.LineID != nil {
		fields.LineID = *input.LineID
	}
	if input.Enabled != nil {
		fields.Enabled = *input.Enabled
	}
	if input.Mode != nil {
		fields.Mode = *input.Mode
	}
	if input.ListenAddress != nil {
		fields.ListenAddress = *input.ListenAddress
	}
	if input.ListenPort != nil {
		fields.ListenPort = *input.ListenPort
	}
	if input.AuthEnabled != nil {
		fields.AuthEnabled = *input.AuthEnabled
		if !*input.AuthEnabled && input.Username == nil {
			fields.Username = ""
		}
	}
	if input.Username != nil {
		fields.Username = *input.Username
	}
	updatedRecord, err := s.buildRecord(id, current, fields)
	if err != nil {
		return ProxyMutation{}, err
	}
	records, err := s.repository.ProxyInstances(ctx)
	if err != nil {
		return ProxyMutation{}, classifyStoreError(err)
	}
	if err := validateProxyCollection(updatedRecord, records, false); err != nil {
		return ProxyMutation{}, err
	}
	updated, err := s.repository.UpdateProxyInstance(ctx, updatedRecord, input.Revision)
	if err != nil {
		return ProxyMutation{}, classifyStoreError(err)
	}
	s.markDirty()
	applyResult := s.Reconcile(ctx)
	if latest, err := s.repository.ProxyInstance(ctx, updated.ID); err == nil {
		updated = latest
	} else if applyResult.Applied {
		return ProxyMutation{}, classifyStoreError(err)
	}
	return ProxyMutation{
		Proxy:       publicProxy(updated),
		ApplyResult: applyResult,
	}, nil
}

func (s *Service) Delete(ctx context.Context, id string, revision int64) (DeleteResult, error) {
	s.mutationsMu.Lock()
	defer s.mutationsMu.Unlock()

	id = strings.TrimSpace(id)
	if !validProxyID(id) {
		return DeleteResult{}, invalid("id", "Proxy ID is invalid")
	}
	if revision <= 0 {
		return DeleteResult{}, invalid("revision", "A positive revision is required")
	}
	if err := s.repository.DeleteProxyInstance(ctx, id, revision); err != nil {
		return DeleteResult{}, classifyStoreError(err)
	}
	s.markDirty()
	return DeleteResult{ID: id, ApplyResult: s.Reconcile(ctx)}, nil
}

type proxyFields struct {
	Name          string
	LineID        string
	Enabled       bool
	Mode          agentclient.ProxyMode
	ListenAddress string
	ListenPort    uint16
	AuthEnabled   bool
	Username      string
	Password      *string
}

func (s *Service) buildRecord(
	id string,
	current store.ProxyInstanceRecord,
	fields proxyFields,
) (store.ProxyInstanceRecord, error) {
	name := strings.TrimSpace(fields.Name)
	if name == "" ||
		utf8.RuneCountInString(name) > maxProxyNameRunes ||
		containsControl(name) {
		return store.ProxyInstanceRecord{}, invalid(
			"name",
			"Proxy name must contain 1 to 100 printable characters",
		)
	}
	lineID := strings.TrimSpace(fields.LineID)
	if lineID == "" || len(lineID) > maxLineIDLength || containsControl(lineID) {
		return store.ProxyInstanceRecord{}, invalid("line_id", "Line ID is invalid")
	}
	if fields.Mode != agentclient.ProxyModeSOCKS5 &&
		fields.Mode != agentclient.ProxyModeHTTP {
		return store.ProxyInstanceRecord{}, invalid(
			"mode",
			"Proxy mode must be socks5 or http",
		)
	}
	if fields.ListenPort < 1024 {
		return store.ProxyInstanceRecord{}, invalid(
			"listen_port",
			"Listen port must be between 1024 and 65535",
		)
	}
	listenIP := net.ParseIP(strings.TrimSpace(fields.ListenAddress))
	if listenIP == nil {
		return store.ProxyInstanceRecord{}, invalid(
			"listen_address",
			"Listen address must be an IP address",
		)
	}
	if !listenIP.IsLoopback() && !fields.AuthEnabled {
		return store.ProxyInstanceRecord{}, invalid(
			"auth_enabled",
			"Authentication is required outside loopback",
		)
	}

	record := current
	record.ID = id
	record.Name = name
	record.LineID = lineID
	record.Enabled = fields.Enabled
	record.Mode = string(fields.Mode)
	record.ListenAddress = listenIP.String()
	record.ListenPort = fields.ListenPort
	record.AuthEnabled = fields.AuthEnabled

	if !fields.AuthEnabled {
		if strings.TrimSpace(fields.Username) != "" ||
			(fields.Password != nil && *fields.Password != "") {
			return store.ProxyInstanceRecord{}, invalid(
				"auth_enabled",
				"Credentials require authentication to be enabled",
			)
		}
		record.Username = ""
		record.PasswordNonce = nil
		record.PasswordCiphertext = nil
		return record, nil
	}

	username := strings.TrimSpace(fields.Username)
	if username == "" ||
		len(username) > maxUsernameBytes ||
		containsControl(username) {
		return store.ProxyInstanceRecord{}, invalid(
			"username",
			"Username must contain 1 to 128 printable bytes",
		)
	}
	if fields.Mode == agentclient.ProxyModeHTTP && strings.Contains(username, ":") {
		return store.ProxyInstanceRecord{}, invalid(
			"username",
			"HTTP proxy username must not contain a colon",
		)
	}
	record.Username = username
	if fields.Password != nil {
		password := *fields.Password
		if password == "" ||
			len(password) > maxPasswordBytes ||
			containsControl(password) {
			return store.ProxyInstanceRecord{}, invalid(
				"password",
				"Password must contain 1 to 512 printable bytes",
			)
		}
		if fields.Mode == agentclient.ProxyModeSOCKS5 &&
			len(password) > maxSOCKS5SecretBytes {
			return store.ProxyInstanceRecord{}, invalid(
				"password",
				"SOCKS5 password must contain at most 255 bytes",
			)
		}
		nonce, ciphertext, err := s.secrets.Seal(
			[]byte(password),
			passwordAssociatedData(id),
		)
		if err != nil {
			return store.ProxyInstanceRecord{}, operationError(
				CodeUnavailable,
				"password",
				"Proxy password could not be encrypted",
				err,
			)
		}
		record.PasswordNonce = nonce
		record.PasswordCiphertext = ciphertext
	} else if fields.Mode == agentclient.ProxyModeSOCKS5 {
		plaintext, err := s.secrets.Open(
			record.PasswordNonce,
			record.PasswordCiphertext,
			passwordAssociatedData(id),
		)
		if err != nil {
			return store.ProxyInstanceRecord{}, operationError(
				CodeUnavailable,
				"password",
				"Existing proxy password could not be validated",
				err,
			)
		}
		defer clear(plaintext)
		if len(plaintext) > maxSOCKS5SecretBytes {
			return store.ProxyInstanceRecord{}, invalid(
				"password",
				"SOCKS5 password must contain at most 255 bytes",
			)
		}
	}
	if len(record.PasswordNonce) == 0 || len(record.PasswordCiphertext) == 0 {
		return store.ProxyInstanceRecord{}, invalid(
			"password",
			"Authentication requires an existing or new password",
		)
	}
	return record, nil
}

func (s *Service) desiredProxy(
	ctx context.Context,
	record store.ProxyInstanceRecord,
) (agentclient.ProxyConfiguration, error) {
	endpointID, err := s.repository.ResolveLineEndpoint(ctx, record.LineID)
	if err != nil {
		return agentclient.ProxyConfiguration{}, fmt.Errorf(
			"resolve proxy %q line endpoint: %w",
			record.ID,
			err,
		)
	}
	proxy := agentclient.ProxyConfiguration{
		ID:            record.ID,
		LineID:        endpointID,
		Enabled:       record.Enabled,
		Mode:          agentclient.ProxyMode(record.Mode),
		ListenAddress: record.ListenAddress,
		ListenPort:    record.ListenPort,
		AuthEnabled:   record.AuthEnabled,
		Username:      record.Username,
	}
	if !record.AuthEnabled {
		return proxy, nil
	}
	plaintext, err := s.secrets.Open(
		record.PasswordNonce,
		record.PasswordCiphertext,
		passwordAssociatedData(record.ID),
	)
	if err != nil {
		return agentclient.ProxyConfiguration{}, fmt.Errorf(
			"decrypt proxy %q password: %w",
			record.ID,
			err,
		)
	}
	defer clear(plaintext)
	proxy.Password = string(plaintext)
	return proxy, nil
}

func (s *Service) observeSnapshot(
	ctx context.Context,
	snapshot agentclient.NetworkSnapshot,
	endpointIDs map[string]string,
) error {
	samples := make([]store.NetworkCounterSample, 0, len(snapshot.Lines)+len(snapshot.Proxies))
	interfaceOwners := make(map[string]int, len(snapshot.Lines))
	for _, line := range snapshot.Lines {
		interfaceName := strings.TrimSpace(line.Interface)
		if line.Connected && interfaceName != "" {
			interfaceOwners[interfaceName]++
		}
	}
	activeLineIDs := make([]string, 0, len(snapshot.Lines))
	for _, line := range snapshot.Lines {
		lineID := strings.TrimSpace(line.LineID)
		interfaceName := strings.TrimSpace(line.Interface)
		if !line.Connected ||
			lineID == "" ||
			interfaceName == "" ||
			interfaceOwners[interfaceName] != 1 {
			continue
		}
		endpointID := strings.TrimSpace(endpointIDs[lineID])
		if endpointID == "" {
			return fmt.Errorf(
				"network counter line %q has no snapshot endpoint",
				lineID,
			)
		}
		activeLineIDs = append(activeLineIDs, lineID)
		samples = append(samples, store.NetworkCounterSample{
			ScopeKind:       store.NetworkScopeLine,
			ScopeID:         lineID,
			EndpointScopeID: endpointID,
			Epoch:           snapshot.BootEpoch + "\x00" + interfaceName,
			RXBytes:         line.RXBytes,
			TXBytes:         line.TXBytes,
			ObservedAt:      snapshot.ObservedAt,
			Location:        s.location,
		})
	}
	for _, proxy := range snapshot.Proxies {
		if strings.TrimSpace(proxy.ID) == "" || strings.TrimSpace(proxy.RuntimeEpoch) == "" {
			continue
		}
		samples = append(samples, store.NetworkCounterSample{
			ScopeKind:       store.NetworkScopeProxy,
			ScopeID:         proxy.ID,
			EndpointScopeID: proxy.ID,
			Epoch:           proxy.RuntimeEpoch,
			RXBytes:         proxy.BytesDown,
			TXBytes:         proxy.BytesUp,
			ObservedAt:      snapshot.ObservedAt,
			Location:        s.location,
		})
	}
	if err := s.repository.ApplyNetworkCounterSnapshot(ctx, samples, activeLineIDs); err != nil {
		return fmt.Errorf("persist network usage sample: %w", err)
	}
	return nil
}

func (s *Service) bindNetworkSnapshot(
	ctx context.Context,
	snapshot agentclient.NetworkSnapshot,
) (agentclient.NetworkSnapshot, map[string]string, error) {
	lineIDByEndpoint, err := s.stableLineIDsByEndpoint(ctx)
	if err != nil {
		return agentclient.NetworkSnapshot{}, nil, err
	}

	publicLines := make([]agentclient.NetworkLine, 0, len(snapshot.Lines))
	snapshotEndpointIDByLine := make(map[string]string, len(snapshot.Lines))
	for _, line := range snapshot.Lines {
		endpointID := strings.TrimSpace(line.LineID)
		lineID := lineIDByEndpoint[endpointID]
		if lineID == "" {
			continue
		}
		line.LineID = lineID
		publicLines = append(publicLines, line)
		snapshotEndpointIDByLine[lineID] = endpointID
	}
	publicProxies := make([]agentclient.NetworkProxy, 0, len(snapshot.Proxies))
	for _, proxy := range snapshot.Proxies {
		endpointID := strings.TrimSpace(proxy.LineID)
		lineID := lineIDByEndpoint[endpointID]
		if lineID == "" {
			return agentclient.NetworkSnapshot{}, nil, fmt.Errorf(
				"proxy %q references unattached endpoint %q",
				proxy.ID,
				endpointID,
			)
		}
		proxy.LineID = lineID
		publicProxies = append(publicProxies, proxy)
	}
	snapshot.Lines = publicLines
	snapshot.Proxies = publicProxies
	return snapshot, snapshotEndpointIDByLine, nil
}

func (s *Service) stableLineIDsByEndpoint(
	ctx context.Context,
) (map[string]string, error) {
	lines, err := s.repository.Lines(ctx)
	if err != nil {
		return nil, fmt.Errorf(
			"load stable line identities: %w",
			err,
		)
	}
	lineIDByEndpoint := make(map[string]string, len(lines))
	attachedEndpointIDByLine := make(map[string]string, len(lines))
	for _, line := range lines {
		lineID := strings.TrimSpace(line.ID)
		endpointID := strings.TrimSpace(line.EndpointID)
		if lineID == "" || endpointID == "" {
			continue
		}
		if existing := lineIDByEndpoint[endpointID]; existing != "" && existing != lineID {
			return nil, fmt.Errorf(
				"endpoint %q is bound to multiple lines",
				endpointID,
			)
		}
		if existing := attachedEndpointIDByLine[lineID]; existing != "" && existing != endpointID {
			return nil, fmt.Errorf(
				"line %q is attached to multiple endpoints",
				lineID,
			)
		}
		lineIDByEndpoint[endpointID] = lineID
		attachedEndpointIDByLine[lineID] = endpointID
	}
	return lineIDByEndpoint, nil
}

func (s *Service) setUnavailable(reason string) {
	s.stateMu.Lock()
	changed := s.state.Available || s.state.UnavailableReason != reason
	s.state.Available = false
	s.state.State = "unavailable"
	s.state.UnavailableReason = reason
	s.state.Stale = s.state.ObservedAt != nil
	s.stateMu.Unlock()
	if changed {
		s.publishNetworkChange(s.now().UTC())
	}
}

func (s *Service) setSnapshot(snapshot agentclient.NetworkSnapshot) {
	lines := cloneLines(snapshot.Lines)
	proxies := cloneProxies(snapshot.Proxies)
	s.stateMu.Lock()
	changed := !s.state.Available ||
		s.state.State != "available" ||
		s.state.UnavailableReason != "" ||
		s.state.Stale ||
		s.state.BootEpoch != snapshot.BootEpoch ||
		!reflect.DeepEqual(s.state.Lines, lines) ||
		!reflect.DeepEqual(s.state.Proxies, proxies)
	observedAt := snapshot.ObservedAt
	s.state.Available = true
	s.state.State = "available"
	s.state.UnavailableReason = ""
	s.state.Stale = false
	s.state.BootEpoch = snapshot.BootEpoch
	s.state.ObservedAt = &observedAt
	s.state.Lines = lines
	s.state.Proxies = proxies
	s.stateMu.Unlock()
	if changed {
		s.publishNetworkChange(snapshot.ObservedAt)
	}
}

func (s *Service) markDirty() {
	s.updateApplyState(func(status *Status) {
		status.ApplyPending = true
		status.ApplyStatus = ApplyStatusPending
		status.ApplyAttempts = 0
		status.ApplyExhausted = false
	})
}

func (s *Service) noteApplyAttempt() {
	s.updateApplyState(func(status *Status) {
		status.ApplyPending = true
		status.ApplyAttempts++
	})
}

func (s *Service) markApplyFailure(status ApplyStatus) {
	s.updateApplyState(func(state *Status) {
		state.ApplyPending = true
		state.ApplyStatus = status
		state.ApplyExhausted = state.ApplyAttempts >= s.maxApplyAttempts
	})
}

func (s *Service) markApplyPending() {
	s.updateApplyState(func(status *Status) {
		status.ApplyPending = true
		status.ApplyStatus = ApplyStatusPending
		status.ApplyAttempts = 0
		status.ApplyExhausted = false
	})
}

func (s *Service) markApplySuccess() {
	s.updateApplyState(func(status *Status) {
		status.ApplyPending = false
		status.ApplyStatus = ApplyStatusApplied
		status.ApplyAttempts = 0
		status.ApplyExhausted = false
	})
}

type applyState struct {
	pending   bool
	status    ApplyStatus
	attempts  int
	exhausted bool
}

func currentApplyState(status *Status) applyState {
	return applyState{
		pending:   status.ApplyPending,
		status:    status.ApplyStatus,
		attempts:  status.ApplyAttempts,
		exhausted: status.ApplyExhausted,
	}
}

func (s *Service) updateApplyState(update func(*Status)) {
	s.stateMu.Lock()
	previous := currentApplyState(&s.state)
	update(&s.state)
	changed := currentApplyState(&s.state) != previous
	s.stateMu.Unlock()
	if changed {
		s.publishNetworkChange(s.now().UTC())
	}
}

func (s *Service) publishNetworkChange(observedAt time.Time) {
	if s.runtimeEvents == nil {
		return
	}
	s.runtimeEvents.Publish(runtimeevents.Change{
		ObservedAt: observedAt,
		Sections:   runtimeevents.SectionNetwork,
	})
}

func (s *Service) shouldApply() bool {
	s.stateMu.RLock()
	defer s.stateMu.RUnlock()
	return s.state.ApplyPending && !s.state.ApplyExhausted
}

func (s *Service) newProxyID() (string, error) {
	s.randomMu.Lock()
	defer s.randomMu.Unlock()
	random := make([]byte, generatedProxyIDBytes)
	if _, err := io.ReadFull(s.random, random); err != nil {
		return "", err
	}
	return "proxy_" + hex.EncodeToString(random), nil
}

func publicProxy(record store.ProxyInstanceRecord) Proxy {
	return Proxy{
		ID:              record.ID,
		Name:            record.Name,
		LineID:          record.LineID,
		Enabled:         record.Enabled,
		Mode:            agentclient.ProxyMode(record.Mode),
		ListenAddress:   record.ListenAddress,
		ListenPort:      record.ListenPort,
		AuthEnabled:     record.AuthEnabled,
		Username:        record.Username,
		HasPassword:     len(record.PasswordNonce) > 0 && len(record.PasswordCiphertext) > 0,
		Revision:        record.Revision,
		AppliedRevision: record.AppliedRevision,
		ApplyState:      proxyApplyState(record),
		CreatedAt:       record.CreatedAt,
		UpdatedAt:       record.UpdatedAt,
	}
}

func proxyApplyState(record store.ProxyInstanceRecord) ProxyApplyState {
	switch {
	case record.DesiredDeleted:
		return ProxyApplyStatePendingDelete
	case record.AppliedRevision == record.Revision:
		return ProxyApplyStateApplied
	case record.AppliedRevision == 0:
		return ProxyApplyStatePendingCreate
	default:
		return ProxyApplyStatePendingUpdate
	}
}

func publicUsage(items []store.NetworkUsage) []Usage {
	usage := make([]Usage, 0, len(items))
	for _, item := range items {
		usage = append(usage, Usage(item))
	}
	return usage
}

func totalLineUsage(items []Usage) UsageTotal {
	var total UsageTotal
	for _, item := range items {
		if item.ScopeKind != store.NetworkScopeLine {
			continue
		}
		total.RXBytes += item.RXBytes
		total.TXBytes += item.TXBytes
	}
	return total
}

func cloneStatus(source Status) Status {
	result := source
	if source.ObservedAt != nil {
		observedAt := *source.ObservedAt
		result.ObservedAt = &observedAt
	}
	result.Lines = cloneLines(source.Lines)
	result.Proxies = cloneProxies(source.Proxies)
	result.TodayUsage = append([]Usage(nil), source.TodayUsage...)
	result.MonthUsage = append([]Usage(nil), source.MonthUsage...)
	if result.TodayUsage == nil {
		result.TodayUsage = []Usage{}
	}
	if result.MonthUsage == nil {
		result.MonthUsage = []Usage{}
	}
	return result
}

func cloneLines(source []agentclient.NetworkLine) []agentclient.NetworkLine {
	lines := append([]agentclient.NetworkLine(nil), source...)
	if lines == nil {
		return []agentclient.NetworkLine{}
	}
	for index := range lines {
		lines[index].Addresses = append([]string(nil), lines[index].Addresses...)
		if lines[index].Addresses == nil {
			lines[index].Addresses = []string{}
		}
		lines[index].DNS = append([]string(nil), lines[index].DNS...)
		if lines[index].DNS == nil {
			lines[index].DNS = []string{}
		}
	}
	return lines
}

func cloneProxies(source []agentclient.NetworkProxy) []agentclient.NetworkProxy {
	proxies := append([]agentclient.NetworkProxy(nil), source...)
	if proxies == nil {
		return []agentclient.NetworkProxy{}
	}
	for index := range proxies {
		if proxies[index].StartedAt != nil {
			startedAt := *proxies[index].StartedAt
			proxies[index].StartedAt = &startedAt
		}
	}
	return proxies
}

func validateReconciledSnapshot(
	desired []agentclient.ProxyConfiguration,
	snapshot agentclient.NetworkSnapshot,
) error {
	expected := make(map[string]agentclient.ProxyConfiguration, len(desired))
	for _, proxy := range desired {
		expected[proxy.ID] = proxy
	}
	if len(snapshot.Proxies) != len(expected) {
		return fmt.Errorf(
			"%w: reconciled proxy collection does not match desired state",
			agentclient.ErrProtocol,
		)
	}
	for _, runtime := range snapshot.Proxies {
		proxy, exists := expected[runtime.ID]
		if !exists ||
			runtime.LineID != proxy.LineID ||
			runtime.Mode != proxy.Mode ||
			runtime.ListenAddress != proxy.ListenAddress ||
			runtime.ListenPort != proxy.ListenPort {
			return fmt.Errorf(
				"%w: reconciled proxy does not match desired state",
				agentclient.ErrProtocol,
			)
		}
		if !proxy.Enabled && runtime.State != agentclient.ProxyStateDisabled {
			return fmt.Errorf(
				"%w: disabled proxy is active in host agent",
				agentclient.ErrProtocol,
			)
		}
		if proxy.Enabled && runtime.State == agentclient.ProxyStateDisabled {
			return fmt.Errorf(
				"%w: enabled proxy is disabled in host agent",
				agentclient.ErrProtocol,
			)
		}
	}
	return nil
}

func classifyAgentApplyError(err error) ApplyStatus {
	if errors.Is(err, agentclient.ErrProtocol) ||
		errors.Is(err, agentclient.ErrInvalidRequest) {
		return ApplyStatusAgentRejected
	}
	var operationError *agentclient.OperationError
	if errors.As(err, &operationError) {
		return ApplyStatusAgentRejected
	}
	return ApplyStatusAgentUnavailable
}

func passwordAssociatedData(id string) []byte {
	return []byte("modemdeck:proxy-password:" + id)
}

func validProxyID(id string) bool {
	if id == "" || len(id) > maxProxyIDLength {
		return false
	}
	for _, character := range id {
		if (character >= 'a' && character <= 'z') ||
			(character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') ||
			character == '-' ||
			character == '_' ||
			character == '.' {
			continue
		}
		return false
	}
	return true
}

func validateProxyCollection(
	candidate store.ProxyInstanceRecord,
	records []store.ProxyInstanceRecord,
	creating bool,
) error {
	count := len(records)
	if creating {
		count++
	}
	if count > maxProxyCount {
		return invalid("id", "No more than 64 proxies may be configured")
	}
	for _, record := range records {
		if record.ID == candidate.ID {
			continue
		}
		if record.ListenAddress == candidate.ListenAddress &&
			record.ListenPort == candidate.ListenPort {
			return invalid(
				"listen_port",
				"Listen address and port are already used by another proxy",
			)
		}
	}
	return nil
}

func containsControl(value string) bool {
	for _, character := range value {
		if unicode.IsControl(character) {
			return true
		}
	}
	return false
}

func hasUpdate(input UpdateInput) bool {
	return input.Name != nil ||
		input.LineID != nil ||
		input.Enabled != nil ||
		input.Mode != nil ||
		input.ListenAddress != nil ||
		input.ListenPort != nil ||
		input.AuthEnabled != nil ||
		input.Username != nil ||
		input.Password != nil
}

func invalid(field, message string) error {
	return operationError(CodeInvalidArgument, field, message, nil)
}
