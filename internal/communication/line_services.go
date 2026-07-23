package communication

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/human-agent65535/modemdeck/internal/agentclient"
	"github.com/human-agent65535/modemdeck/internal/store"
)

const connectionProfileResourcePrefix = "profile:"

type lineServiceAgent interface {
	SIMStatus(context.Context, string) (agentclient.SIMStatus, error)
	SIMCommand(
		context.Context,
		string,
		agentclient.SIMCommandRequest,
	) (agentclient.CommandReceipt, error)
	ConnectionProfiles(context.Context, string) ([]agentclient.ConnectionProfile, error)
	SaveConnectionProfile(
		context.Context,
		string,
		agentclient.SaveConnectionProfileRequest,
	) (agentclient.ConnectionProfile, error)
	DeleteConnectionProfile(
		context.Context,
		string,
		agentclient.DeleteConnectionProfileRequest,
	) (agentclient.CommandReceipt, error)
	USSDStatus(context.Context, string) (agentclient.USSDStatus, error)
	USSDCommand(
		context.Context,
		string,
		agentclient.USSDRequest,
	) (agentclient.USSDResponse, error)
}

func (s *Service) SIMStatus(
	ctx context.Context,
	lineSelector string,
) (agentclient.SIMStatus, error) {
	const operation = "read SIM status"
	line, agent, err := s.resolveLineService(ctx, lineSelector, operation)
	if err != nil {
		return agentclient.SIMStatus{}, err
	}
	readContext, cancel := context.WithTimeout(normalizeContext(ctx), snapshotTimeout)
	defer cancel()
	status, err := agent.SIMStatus(readContext, line.ID)
	if err != nil {
		return agentclient.SIMStatus{}, translateAgentError(operation, err)
	}
	if status.LineID != line.ID || status.ObservedAt.IsZero() {
		return agentclient.SIMStatus{}, operationError(
			CodeUnavailable,
			operation,
			"host agent returned an invalid SIM status",
			nil,
		)
	}
	if status.UnlockRetries == nil {
		status.UnlockRetries = map[string]uint32{}
	}
	return status, nil
}

func (s *Service) SIMCommand(
	ctx context.Context,
	lineSelector string,
	request agentclient.SIMCommandRequest,
) (agentclient.CommandReceipt, error) {
	const operation = "control SIM"
	line, agent, err := s.resolveLineService(ctx, lineSelector, operation)
	if err != nil {
		return agentclient.CommandReceipt{}, err
	}
	requestID, err := s.requestID(request.RequestID)
	if err != nil {
		return agentclient.CommandReceipt{}, operationError(
			CodeInvalidArgument,
			operation,
			"request id is invalid",
			err,
		)
	}
	request.RequestID = requestID
	enabled := ""
	if request.Enabled != nil {
		enabled = strconv.FormatBool(*request.Enabled)
	}
	command, replay, err := s.beginCommand(
		ctx,
		requestID,
		"sim_"+string(request.Operation),
		commandDigest("sim", line.ID, string(request.Operation), enabled),
	)
	if err != nil {
		return agentclient.CommandReceipt{}, err
	}
	if replay {
		return agentclient.CommandReceipt{
			RequestID:  requestID,
			ResourceID: command.ResourceID,
		}, nil
	}

	commandContext, cancel := context.WithTimeout(normalizeContext(ctx), commandTimeout)
	defer cancel()
	receipt, err := agent.SIMCommand(commandContext, line.ID, request)
	if err != nil {
		if finishErr := s.finishFailedCommand(ctx, command, err); finishErr != nil {
			return agentclient.CommandReceipt{}, finishErr
		}
		return agentclient.CommandReceipt{}, translateAgentError(operation, err)
	}
	if err := validateLineReceipt(receipt, requestID, line.ID); err != nil {
		if finishErr := s.finishIndeterminateCommand(ctx, command, err); finishErr != nil {
			return agentclient.CommandReceipt{}, finishErr
		}
		return agentclient.CommandReceipt{}, operationError(
			CodeUnavailable,
			operation,
			"host agent returned an invalid command receipt",
			err,
		)
	}
	if err := s.completeLineCommand(ctx, command, line.ID); err != nil {
		return agentclient.CommandReceipt{}, err
	}
	return receipt, nil
}

func (s *Service) ConnectionProfiles(
	ctx context.Context,
	lineSelector string,
) ([]agentclient.ConnectionProfile, error) {
	const operation = "list connection profiles"
	line, agent, err := s.resolveLineService(ctx, lineSelector, operation)
	if err != nil {
		return nil, err
	}
	readContext, cancel := context.WithTimeout(normalizeContext(ctx), snapshotTimeout)
	defer cancel()
	profiles, err := agent.ConnectionProfiles(readContext, line.ID)
	if err != nil {
		return nil, translateAgentError(operation, err)
	}
	if profiles == nil {
		profiles = []agentclient.ConnectionProfile{}
	}
	return profiles, nil
}

func (s *Service) SaveConnectionProfile(
	ctx context.Context,
	lineSelector string,
	request agentclient.SaveConnectionProfileRequest,
) (agentclient.ConnectionProfile, error) {
	const operation = "save connection profile"
	line, agent, err := s.resolveLineService(ctx, lineSelector, operation)
	if err != nil {
		return agentclient.ConnectionProfile{}, err
	}
	requestID, err := s.requestID(request.RequestID)
	if err != nil {
		return agentclient.ConnectionProfile{}, operationError(
			CodeInvalidArgument,
			operation,
			"request id is invalid",
			err,
		)
	}
	request.RequestID = requestID
	profileID := ""
	if request.ProfileID != nil {
		profileID = strconv.FormatInt(int64(*request.ProfileID), 10)
	}
	command, replay, err := s.beginCommand(
		ctx,
		requestID,
		"save_connection_profile",
		commandDigest(
			"save_connection_profile",
			line.ID,
			profileID,
			request.ProfileName,
			request.APN,
			request.IPFamily,
			strconv.FormatUint(uint64(request.APNType), 10),
			strconv.FormatUint(uint64(request.AllowedAuth), 10),
			request.User,
			strconv.FormatUint(uint64(request.AccessTypePreference), 10),
			strconv.FormatUint(uint64(request.RoamingAllowance), 10),
		),
	)
	if err != nil {
		return agentclient.ConnectionProfile{}, err
	}
	if replay {
		return s.replayedConnectionProfile(ctx, line.ID, command.ResourceID, agent)
	}

	commandContext, cancel := context.WithTimeout(normalizeContext(ctx), commandTimeout)
	defer cancel()
	profile, err := agent.SaveConnectionProfile(commandContext, line.ID, request)
	if err != nil {
		if finishErr := s.finishFailedCommand(ctx, command, err); finishErr != nil {
			return agentclient.ConnectionProfile{}, finishErr
		}
		return agentclient.ConnectionProfile{}, translateAgentError(operation, err)
	}
	if profile.ProfileID < 0 {
		protocolErr := errors.New("profile id is negative")
		if finishErr := s.finishIndeterminateCommand(ctx, command, protocolErr); finishErr != nil {
			return agentclient.ConnectionProfile{}, finishErr
		}
		return agentclient.ConnectionProfile{}, operationError(
			CodeUnavailable,
			operation,
			"host agent returned an invalid connection profile",
			protocolErr,
		)
	}
	resourceID := connectionProfileResourcePrefix + strconv.FormatInt(int64(profile.ProfileID), 10)
	if err := s.completeLineCommand(ctx, command, resourceID); err != nil {
		return agentclient.ConnectionProfile{}, err
	}
	return profile, nil
}

func (s *Service) DeleteConnectionProfile(
	ctx context.Context,
	lineSelector string,
	request agentclient.DeleteConnectionProfileRequest,
) (agentclient.CommandReceipt, error) {
	const operation = "delete connection profile"
	line, agent, err := s.resolveLineService(ctx, lineSelector, operation)
	if err != nil {
		return agentclient.CommandReceipt{}, err
	}
	requestID, err := s.requestID(request.RequestID)
	if err != nil {
		return agentclient.CommandReceipt{}, operationError(
			CodeInvalidArgument,
			operation,
			"request id is invalid",
			err,
		)
	}
	request.RequestID = requestID
	profileID := ""
	if request.ProfileID != nil {
		profileID = strconv.FormatInt(int64(*request.ProfileID), 10)
	}
	command, replay, err := s.beginCommand(
		ctx,
		requestID,
		"delete_connection_profile",
		commandDigest("delete_connection_profile", line.ID, profileID, request.ProfileName),
	)
	if err != nil {
		return agentclient.CommandReceipt{}, err
	}
	if replay {
		return agentclient.CommandReceipt{RequestID: requestID, ResourceID: line.ID}, nil
	}

	commandContext, cancel := context.WithTimeout(normalizeContext(ctx), commandTimeout)
	defer cancel()
	receipt, err := agent.DeleteConnectionProfile(commandContext, line.ID, request)
	if err != nil {
		if finishErr := s.finishFailedCommand(ctx, command, err); finishErr != nil {
			return agentclient.CommandReceipt{}, finishErr
		}
		return agentclient.CommandReceipt{}, translateAgentError(operation, err)
	}
	if err := validateLineReceipt(receipt, requestID, line.ID); err != nil {
		if finishErr := s.finishIndeterminateCommand(ctx, command, err); finishErr != nil {
			return agentclient.CommandReceipt{}, finishErr
		}
		return agentclient.CommandReceipt{}, operationError(
			CodeUnavailable,
			operation,
			"host agent returned an invalid command receipt",
			err,
		)
	}
	if err := s.completeLineCommand(ctx, command, line.ID); err != nil {
		return agentclient.CommandReceipt{}, err
	}
	return receipt, nil
}

func (s *Service) USSDStatus(
	ctx context.Context,
	lineSelector string,
) (agentclient.USSDStatus, error) {
	const operation = "read USSD status"
	line, agent, err := s.resolveLineService(ctx, lineSelector, operation)
	if err != nil {
		return agentclient.USSDStatus{}, err
	}
	readContext, cancel := context.WithTimeout(normalizeContext(ctx), snapshotTimeout)
	defer cancel()
	status, err := agent.USSDStatus(readContext, line.ID)
	if err != nil {
		return agentclient.USSDStatus{}, translateAgentError(operation, err)
	}
	if status.LineID != line.ID || status.ObservedAt.IsZero() {
		return agentclient.USSDStatus{}, operationError(
			CodeUnavailable,
			operation,
			"host agent returned an invalid USSD status",
			nil,
		)
	}
	return status, nil
}

func (s *Service) USSDCommand(
	ctx context.Context,
	lineSelector string,
	request agentclient.USSDRequest,
) (agentclient.USSDResponse, error) {
	const operation = "send USSD command"
	line, agent, err := s.resolveLineService(ctx, lineSelector, operation)
	if err != nil {
		return agentclient.USSDResponse{}, err
	}
	requestID, err := s.requestID(request.RequestID)
	if err != nil {
		return agentclient.USSDResponse{}, operationError(
			CodeInvalidArgument,
			operation,
			"request id is invalid",
			err,
		)
	}
	request.RequestID = requestID
	command, replay, err := s.beginCommand(
		ctx,
		requestID,
		"ussd_"+string(request.Action),
		commandDigest("ussd", line.ID, string(request.Action)),
	)
	if err != nil {
		return agentclient.USSDResponse{}, err
	}
	if replay {
		return agentclient.USSDResponse{}, nil
	}

	commandContext, cancel := context.WithTimeout(normalizeContext(ctx), commandTimeout)
	defer cancel()
	result, err := agent.USSDCommand(commandContext, line.ID, request)
	if err != nil {
		if finishErr := s.finishFailedCommand(ctx, command, err); finishErr != nil {
			return agentclient.USSDResponse{}, finishErr
		}
		return agentclient.USSDResponse{}, translateAgentError(operation, err)
	}
	if err := s.completeLineCommand(ctx, command, line.ID); err != nil {
		return agentclient.USSDResponse{}, err
	}
	return result, nil
}

func (s *Service) resolveLineService(
	ctx context.Context,
	lineSelector string,
	operation string,
) (store.LineSummary, lineServiceAgent, error) {
	line, err := s.resolveAttachedLine(ctx, lineSelector)
	if err != nil {
		return store.LineSummary{}, nil, fmt.Errorf("%s: %w", operation, err)
	}
	agent, ok := s.agent.(lineServiceAgent)
	if !ok {
		return store.LineSummary{}, nil, operationError(
			CodeNotSupported,
			operation,
			"host agent does not implement this line service",
			nil,
		)
	}
	return line, agent, nil
}

func (s *Service) replayedConnectionProfile(
	ctx context.Context,
	lineID string,
	resourceID string,
	agent lineServiceAgent,
) (agentclient.ConnectionProfile, error) {
	const operation = "save connection profile"
	if !strings.HasPrefix(resourceID, connectionProfileResourcePrefix) {
		return agentclient.ConnectionProfile{}, operationError(
			CodeInternal,
			operation,
			"completed profile command has no stored result",
			nil,
		)
	}
	profileID, err := strconv.ParseInt(
		strings.TrimPrefix(resourceID, connectionProfileResourcePrefix),
		10,
		32,
	)
	if err != nil {
		return agentclient.ConnectionProfile{}, operationError(
			CodeInternal,
			operation,
			"completed profile command has an invalid stored result",
			err,
		)
	}
	readContext, cancel := context.WithTimeout(normalizeContext(ctx), snapshotTimeout)
	defer cancel()
	profiles, err := agent.ConnectionProfiles(readContext, lineID)
	if err != nil {
		return agentclient.ConnectionProfile{}, translateAgentError(operation, err)
	}
	for _, profile := range profiles {
		if profile.ProfileID == int32(profileID) {
			return profile, nil
		}
	}
	return agentclient.ConnectionProfile{}, operationError(
		CodeNotFound,
		operation,
		"saved connection profile is no longer present",
		nil,
	)
}

func (s *Service) completeLineCommand(
	ctx context.Context,
	command store.HardwareCommand,
	resourceID string,
) error {
	outcomeContext, cancel := durableContext(ctx)
	defer cancel()
	if err := s.repository.FinishHardwareCommand(
		outcomeContext,
		command.RequestID,
		store.HardwareCommandCompleted,
		resourceID,
		"",
	); err != nil {
		return operationError(
			CodeInternal,
			command.Operation,
			"line command result could not be finalized",
			err,
		)
	}
	return nil
}

func validateLineReceipt(
	receipt agentclient.CommandReceipt,
	requestID string,
	lineID string,
) error {
	if err := validateReceipt(receipt, requestID); err != nil {
		return err
	}
	if receipt.ResourceID != lineID {
		return errors.New("receipt resource does not match the selected line")
	}
	return nil
}
