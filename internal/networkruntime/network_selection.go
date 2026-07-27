package networkruntime

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/human-agent65535/modemdeck/internal/agentclient"
	"github.com/human-agent65535/modemdeck/internal/store"
)

const (
	NetworkScanTimeout      = 125 * time.Second
	NetworkSelectionTimeout = 50 * time.Second
	networkReadTimeout      = 10 * time.Second
)

type NetworkRegistration struct {
	Known        bool   `json:"known"`
	State        string `json:"state"`
	Roaming      bool   `json:"roaming"`
	OperatorCode string `json:"operator_code"`
	OperatorName string `json:"operator_name"`
}

type NetworkSelection struct {
	LineID       string                           `json:"line_id"`
	Mode         agentclient.NetworkSelectionMode `json:"mode"`
	OperatorCode string                           `json:"operator_code"`
	Revision     int64                            `json:"revision"`
	Applied      bool                             `json:"applied"`
	LastError    string                           `json:"last_error,omitempty"`
	AppliedAt    string                           `json:"applied_at,omitempty"`
	Registration NetworkRegistration              `json:"registration"`
}

type UpdateNetworkSelectionInput struct {
	ExpectedRevision int64
	Mode             agentclient.NetworkSelectionMode
	OperatorCode     string
}

type NetworkScan struct {
	LineID     string                      `json:"line_id"`
	ObservedAt time.Time                   `json:"observed_at"`
	Networks   []agentclient.MobileNetwork `json:"networks"`
}

func (s *Service) NetworkSelection(
	ctx context.Context,
	lineID string,
) (NetworkSelection, error) {
	lineID, err := normalizedNetworkLineID(lineID)
	if err != nil {
		return NetworkSelection{}, err
	}
	policy, err := s.repository.NetworkSelectionPolicy(ctx, lineID)
	var line agentclient.Line
	if errors.Is(err, store.ErrNetworkSelectionPolicyNotFound) {
		line, err = s.liveRegistrationLine(ctx, lineID)
		if err != nil {
			return NetworkSelection{}, err
		}
		policy, err = s.repository.EnsureNetworkSelectionPolicy(ctx, lineID)
	}
	if err != nil {
		return NetworkSelection{}, classifyNetworkSelectionStoreError(err)
	}
	if strings.TrimSpace(line.ID) == "" {
		line, err = s.liveRegistrationLine(ctx, lineID)
		if err != nil {
			line = agentclient.Line{ID: lineID}
		}
	}
	return s.publicNetworkSelection(policy, line), nil
}

func (s *Service) UpdateNetworkSelection(
	ctx context.Context,
	lineID string,
	input UpdateNetworkSelectionInput,
) (NetworkSelection, error) {
	s.mutationsMu.Lock()
	defer s.mutationsMu.Unlock()

	lineID, err := normalizedNetworkLineID(lineID)
	if err != nil {
		return NetworkSelection{}, err
	}
	operatorCode := strings.TrimSpace(input.OperatorCode)
	if input.ExpectedRevision <= 0 {
		return NetworkSelection{}, invalid(
			"expected_revision",
			"A positive expected_revision is required",
		)
	}
	if !validSelectionInput(input.Mode, operatorCode) {
		if input.Mode == agentclient.NetworkSelectionModeManual {
			return NetworkSelection{}, invalid(
				"operator_code",
				"Manual network selection requires a 5 or 6 digit operator_code",
			)
		}
		return NetworkSelection{}, invalid(
			"mode",
			"Mode must be auto without operator_code, or manual with operator_code",
		)
	}
	current, err := s.repository.NetworkSelectionPolicy(ctx, lineID)
	if errors.Is(err, store.ErrNetworkSelectionPolicyNotFound) {
		if _, liveErr := s.liveRegistrationLine(ctx, lineID); liveErr != nil {
			return NetworkSelection{}, liveErr
		}
		current, err = s.repository.EnsureNetworkSelectionPolicy(ctx, lineID)
	}
	if err != nil {
		return NetworkSelection{}, classifyNetworkSelectionStoreError(err)
	}
	if current.Revision != input.ExpectedRevision {
		return NetworkSelection{}, classifyNetworkSelectionStoreError(
			store.ErrNetworkSelectionRevisionConflict,
		)
	}

	s.reconcileMu.Lock()
	defer s.reconcileMu.Unlock()
	updated, err := s.repository.UpdateNetworkSelectionPolicy(
		ctx,
		lineID,
		string(input.Mode),
		operatorCode,
		input.ExpectedRevision,
	)
	if err != nil {
		return NetworkSelection{}, classifyNetworkSelectionStoreError(err)
	}
	bootEpoch := s.currentBootEpoch()
	if bootEpoch == "" {
		snapshot, snapshotErr := s.agent.Network(ctx)
		if snapshotErr != nil {
			s.recordNetworkSelectionFailure(ctx, updated, snapshotErr)
			return NetworkSelection{}, translateNetworkSelectionAgentError(snapshotErr)
		}
		bootEpoch = snapshot.BootEpoch
		publicSnapshot, endpointIDs, bindErr := s.bindNetworkSnapshot(ctx, snapshot)
		if bindErr != nil {
			s.recordNetworkSelectionFailure(ctx, updated, bindErr)
			return NetworkSelection{}, operationError(
				CodeUnavailable,
				"line_id",
				"Network line identities are unavailable",
				bindErr,
			)
		}
		if observeErr := s.observeSnapshot(ctx, publicSnapshot, endpointIDs); observeErr != nil {
			s.report(observeErr)
		}
		s.setSnapshot(publicSnapshot)
	}
	requestID, err := s.newNetworkRequestID("selection")
	if err != nil {
		s.recordNetworkSelectionFailure(ctx, updated, err)
		return NetworkSelection{}, operationError(
			CodeUnavailable,
			"",
			"Network selection request could not be generated",
			err,
		)
	}
	bounded, cancel := context.WithTimeout(
		normalizeNetworkContext(ctx),
		NetworkSelectionTimeout,
	)
	defer cancel()
	endpointID, err := s.repository.ResolveLineEndpoint(ctx, lineID)
	if err != nil {
		s.recordNetworkSelectionFailure(ctx, updated, err)
		return NetworkSelection{}, operationError(
			CodeNotFound,
			"line_id",
			"Line is not attached",
			err,
		)
	}
	receipt, err := s.agent.SetNetworkSelection(
		bounded,
		endpointID,
		agentclient.ApplyNetworkSelectionRequest{
			RequestID:    requestID,
			Mode:         input.Mode,
			OperatorCode: operatorCode,
		},
	)
	if err != nil {
		s.recordNetworkSelectionFailure(ctx, updated, err)
		return NetworkSelection{}, translateNetworkSelectionAgentError(err)
	}
	changed, err := s.repository.MarkNetworkSelectionApplied(
		ctx,
		lineID,
		updated.Revision,
		bootEpoch,
		receipt.AppliedAt,
	)
	if err != nil {
		s.markDirty()
		return NetworkSelection{}, classifyNetworkSelectionStoreError(err)
	}
	if changed {
		s.markDirty()
	}
	updated, err = s.repository.NetworkSelectionPolicy(ctx, lineID)
	if err != nil {
		return NetworkSelection{}, classifyNetworkSelectionStoreError(err)
	}
	line, snapshotErr := s.liveRegistrationLine(ctx, lineID)
	if snapshotErr != nil {
		return s.publicNetworkSelection(updated, agentclient.Line{ID: lineID}), nil
	}
	return s.publicNetworkSelection(updated, line), nil
}

func (s *Service) ScanNetworks(
	ctx context.Context,
	lineID string,
) (NetworkScan, error) {
	lineID, err := normalizedNetworkLineID(lineID)
	if err != nil {
		return NetworkScan{}, err
	}
	requestID, err := s.newNetworkRequestID("scan")
	if err != nil {
		return NetworkScan{}, operationError(
			CodeUnavailable,
			"",
			"Network scan request could not be generated",
			err,
		)
	}
	bounded, cancel := context.WithTimeout(normalizeNetworkContext(ctx), NetworkScanTimeout)
	defer cancel()
	endpointID, err := s.repository.ResolveLineEndpoint(ctx, lineID)
	if err != nil {
		return NetworkScan{}, operationError(
			CodeNotFound,
			"line_id",
			"Line is not attached",
			err,
		)
	}
	result, err := s.agent.ScanNetworks(
		bounded,
		endpointID,
		agentclient.NetworkScanRequest{RequestID: requestID},
	)
	if err != nil {
		return NetworkScan{}, translateNetworkSelectionAgentError(err)
	}
	networks := append([]agentclient.MobileNetwork(nil), result.Networks...)
	if networks == nil {
		networks = []agentclient.MobileNetwork{}
	}
	return NetworkScan{
		LineID:     lineID,
		ObservedAt: result.ObservedAt.UTC(),
		Networks:   networks,
	}, nil
}

func (s *Service) reconcileNetworkSelections(
	ctx context.Context,
	snapshot agentclient.NetworkSnapshot,
	fullSnapshot agentclient.Snapshot,
) (bool, error) {
	active, policies, err := s.networkSelectionPolicies(ctx, fullSnapshot)
	if err != nil {
		return true, err
	}
	pending := false
	failures := make([]error, 0)
	for _, policy := range policies {
		endpointID, attached := active[policy.LineID]
		if !attached {
			continue
		}
		if !policy.Configured {
			continue
		}
		if policy.AppliedRevision == policy.Revision &&
			policy.AppliedBootEpoch == snapshot.BootEpoch {
			continue
		}
		requestID, err := s.newNetworkRequestID("reconcile")
		if err != nil {
			pending = true
			failures = append(failures, fmt.Errorf(
				"generate network selection request for %q: %w",
				policy.LineID,
				err,
			))
			continue
		}
		bounded, cancel := context.WithTimeout(
			normalizeNetworkContext(ctx),
			NetworkSelectionTimeout,
		)
		receipt, err := s.agent.SetNetworkSelection(
			bounded,
			endpointID,
			agentclient.ApplyNetworkSelectionRequest{
				RequestID:    requestID,
				Mode:         agentclient.NetworkSelectionMode(policy.Mode),
				OperatorCode: policy.OperatorCode,
			},
		)
		cancel()
		if err != nil {
			pending = true
			if markErr := s.repository.MarkNetworkSelectionApplyFailed(
				ctx,
				policy.LineID,
				policy.Revision,
				networkSelectionErrorMessage(err),
			); markErr != nil {
				failures = append(failures, errors.Join(err, markErr))
				continue
			}
			failures = append(failures, err)
			continue
		}
		changed, err := s.repository.MarkNetworkSelectionApplied(
			ctx,
			policy.LineID,
			policy.Revision,
			snapshot.BootEpoch,
			receipt.AppliedAt,
		)
		if err != nil {
			pending = true
			failures = append(failures, err)
			continue
		}
		pending = pending || changed
	}
	return pending, errors.Join(failures...)
}

func (s *Service) networkSelectionsPending(
	ctx context.Context,
	snapshot agentclient.NetworkSnapshot,
	fullSnapshot agentclient.Snapshot,
) (bool, error) {
	active, policies, err := s.networkSelectionPolicies(ctx, fullSnapshot)
	if err != nil {
		return false, err
	}
	for _, policy := range policies {
		if _, attached := active[policy.LineID]; !attached {
			continue
		}
		if !policy.Configured {
			continue
		}
		if policy.AppliedRevision != policy.Revision ||
			policy.AppliedBootEpoch != snapshot.BootEpoch {
			return true, nil
		}
	}
	return false, nil
}

func (s *Service) networkSelectionPolicies(
	ctx context.Context,
	snapshot agentclient.Snapshot,
) (map[string]string, []store.NetworkSelectionPolicyRecord, error) {
	active := make(map[string]string, len(snapshot.Lines))
	for _, line := range snapshot.Lines {
		endpointID := strings.TrimSpace(line.ID)
		if endpointID == "" || !line.SIMPresent || !line.SavedPolicySupported {
			continue
		}
		lineID, err := s.lineIDForEndpoint(ctx, endpointID)
		if err != nil {
			return nil, nil, err
		}
		active[lineID] = endpointID
		if _, err := s.repository.EnsureNetworkSelectionPolicy(ctx, lineID); err != nil {
			return nil, nil, fmt.Errorf(
				"initialize network selection policy for %q: %w",
				lineID,
				err,
			)
		}
	}
	policies, err := s.repository.NetworkSelectionPolicies(ctx)
	if err != nil {
		return nil, nil, err
	}
	return active, policies, nil
}

func (s *Service) liveRegistrationLine(
	ctx context.Context,
	lineID string,
) (agentclient.Line, error) {
	bounded, cancel := context.WithTimeout(normalizeNetworkContext(ctx), networkReadTimeout)
	defer cancel()
	snapshot, err := s.agent.Snapshot(bounded)
	if err != nil {
		return agentclient.Line{}, translateNetworkSelectionAgentError(err)
	}
	endpointID, err := s.repository.ResolveLineEndpoint(ctx, lineID)
	if err != nil {
		return agentclient.Line{}, operationError(
			CodeNotFound,
			"line_id",
			"Line is not attached",
			err,
		)
	}
	for _, line := range snapshot.Lines {
		if strings.TrimSpace(line.ID) == endpointID {
			if !line.SIMPresent || !line.SavedPolicySupported {
				return agentclient.Line{}, operationError(
					CodeFailedPrecondition,
					"line_id",
					"Line has no SIM-backed persistent identity",
					nil,
				)
			}
			return line, nil
		}
	}
	return agentclient.Line{}, operationError(
		CodeNotFound,
		"line_id",
		"Line is not attached",
		nil,
	)
}

func (s *Service) lineIDForEndpoint(
	ctx context.Context,
	endpointID string,
) (string, error) {
	lines, err := s.repository.Lines(ctx)
	if err != nil {
		return "", fmt.Errorf("load stable line identities: %w", err)
	}
	endpointID = strings.TrimSpace(endpointID)
	for _, line := range lines {
		if strings.TrimSpace(line.EndpointID) == endpointID &&
			strings.TrimSpace(line.ID) != "" {
			return strings.TrimSpace(line.ID), nil
		}
	}
	return "", operationError(
		CodeNotFound,
		"line_id",
		"Endpoint is not bound to a stable line",
		nil,
	)
}

func (s *Service) publicNetworkSelection(
	policy store.NetworkSelectionPolicyRecord,
	line agentclient.Line,
) NetworkSelection {
	bootEpoch := s.currentBootEpoch()
	lastError := ""
	if policy.Configured {
		lastError = policy.LastError
	}
	return NetworkSelection{
		LineID:       policy.LineID,
		Mode:         agentclient.NetworkSelectionMode(policy.Mode),
		OperatorCode: policy.OperatorCode,
		Revision:     policy.Revision,
		Applied: !policy.Configured ||
			(policy.AppliedRevision == policy.Revision &&
				bootEpoch != "" &&
				policy.AppliedBootEpoch == bootEpoch),
		LastError:    lastError,
		AppliedAt:    policy.AppliedAt,
		Registration: registrationFromLine(line),
	}
}

func registrationFromLine(line agentclient.Line) NetworkRegistration {
	registration := NetworkRegistration{
		Known:   line.RegistrationStateKnown,
		State:   strings.TrimSpace(line.RegistrationState),
		Roaming: line.Roaming,
	}
	if !registration.Known {
		return registration
	}
	switch line.RegistrationStateCode {
	case 1, 5, 6, 7, 9, 10, 11:
		registration.OperatorCode = strings.TrimSpace(line.ServingOperatorCode)
		registration.OperatorName = strings.TrimSpace(line.ServingOperatorName)
		if registration.OperatorCode == "" {
			registration.OperatorCode = strings.TrimSpace(line.OperatorIdentifier)
		}
		if registration.OperatorName == "" {
			registration.OperatorName = strings.TrimSpace(line.OperatorName)
		}
	}
	return registration
}

func (s *Service) currentBootEpoch() string {
	s.stateMu.RLock()
	defer s.stateMu.RUnlock()
	return strings.TrimSpace(s.state.BootEpoch)
}

func (s *Service) newNetworkRequestID(kind string) (string, error) {
	s.randomMu.Lock()
	defer s.randomMu.Unlock()
	random := make([]byte, generatedProxyIDBytes)
	if _, err := io.ReadFull(s.random, random); err != nil {
		return "", err
	}
	return "network_" + kind + "_" + hex.EncodeToString(random), nil
}

func (s *Service) recordNetworkSelectionFailure(
	ctx context.Context,
	policy store.NetworkSelectionPolicyRecord,
	err error,
) {
	s.report(fmt.Errorf("apply network selection for %q: %w", policy.LineID, err))
	if markErr := s.repository.MarkNetworkSelectionApplyFailed(
		ctx,
		policy.LineID,
		policy.Revision,
		networkSelectionErrorMessage(err),
	); markErr != nil {
		s.report(errors.Join(err, markErr))
	}
	s.markDirty()
}

func normalizedNetworkLineID(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > maxLineIDLength || containsControl(value) {
		return "", invalid("line_id", "Line ID is invalid")
	}
	return value, nil
}

func validSelectionInput(
	mode agentclient.NetworkSelectionMode,
	operatorCode string,
) bool {
	switch mode {
	case agentclient.NetworkSelectionModeAuto:
		return operatorCode == ""
	case agentclient.NetworkSelectionModeManual:
		return validMCCMNC(operatorCode)
	default:
		return false
	}
}

func validMCCMNC(value string) bool {
	if len(value) != 5 && len(value) != 6 {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

func translateNetworkSelectionAgentError(err error) error {
	if errors.Is(err, agentclient.ErrInvalidRequest) {
		return operationError(
			CodeInvalidArgument,
			"",
			"Host agent rejected the network selection request",
			err,
		)
	}
	var operationErrorValue *agentclient.OperationError
	if !errors.As(err, &operationErrorValue) {
		return operationError(
			CodeUnavailable,
			"",
			"Host agent is unavailable",
			err,
		)
	}
	code := CodeInternal
	switch operationErrorValue.Code {
	case "invalid_argument":
		code = CodeInvalidArgument
	case "not_found":
		code = CodeNotFound
	case "conflict":
		code = CodeOperationConflict
	case "not_supported":
		code = CodeNotSupported
	case "failed_precondition":
		code = CodeFailedPrecondition
	case "network_rejected":
		code = CodeNetworkRejected
	case "unavailable":
		code = CodeUnavailable
	}
	return operationError(code, "", operationErrorValue.Message, err)
}

func networkSelectionErrorMessage(err error) string {
	var operationErrorValue *agentclient.OperationError
	if errors.As(err, &operationErrorValue) &&
		strings.TrimSpace(operationErrorValue.Message) != "" {
		return strings.TrimSpace(operationErrorValue.Message)
	}
	return "Host agent is unavailable"
}

func normalizeNetworkContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}
