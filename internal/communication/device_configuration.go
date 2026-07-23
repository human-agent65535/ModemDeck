package communication

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/human-agent65535/modemdeck/internal/agentclient"
	"github.com/human-agent65535/modemdeck/internal/store"
)

func (s *Service) DeviceConfiguration(
	ctx context.Context,
	lineSelector string,
) (agentclient.DeviceConfiguration, error) {
	const operation = "read device configuration"
	line, err := s.resolveAttachedLine(ctx, lineSelector)
	if err != nil {
		return agentclient.DeviceConfiguration{}, fmt.Errorf("%s: %w", operation, err)
	}
	readContext, cancel := context.WithTimeout(
		normalizeContext(ctx),
		snapshotTimeout,
	)
	defer cancel()
	configuration, err := s.agent.DeviceConfiguration(readContext, line.ID)
	if err != nil {
		return agentclient.DeviceConfiguration{}, translateAgentError(operation, err)
	}
	if err := validateDeviceConfiguration(configuration, line.ID); err != nil {
		return agentclient.DeviceConfiguration{}, operationError(
			CodeUnavailable,
			operation,
			"host agent returned an invalid device configuration",
			err,
		)
	}
	return configuration, nil
}

func (s *Service) ApplyDeviceConfiguration(
	ctx context.Context,
	lineSelector string,
	request agentclient.ApplyDeviceConfigurationRequest,
) (agentclient.DeviceConfiguration, error) {
	const operation = "apply device configuration"
	line, err := s.resolveAttachedLine(ctx, lineSelector)
	if err != nil {
		return agentclient.DeviceConfiguration{}, fmt.Errorf("%s: %w", operation, err)
	}
	requestID, err := s.requestID(request.RequestID)
	if err != nil {
		return agentclient.DeviceConfiguration{}, operationError(
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
		"device_configuration_"+string(request.Operation),
		deviceConfigurationDigest(line.ID, request),
	)
	if err != nil {
		return agentclient.DeviceConfiguration{}, err
	}
	if replay {
		return s.DeviceConfiguration(ctx, line.ID)
	}

	commandContext, cancel := context.WithTimeout(
		normalizeContext(ctx),
		deviceConfigurationTimeout,
	)
	defer cancel()
	configuration, err := s.agent.ApplyDeviceConfiguration(
		commandContext,
		line.ID,
		request,
	)
	if err != nil {
		if finishErr := s.finishFailedCommand(ctx, command, err); finishErr != nil {
			return agentclient.DeviceConfiguration{}, finishErr
		}
		return agentclient.DeviceConfiguration{}, translateAgentError(operation, err)
	}
	if err := validateDeviceConfiguration(configuration, line.ID); err != nil {
		if finishErr := s.finishIndeterminateCommand(ctx, command, err); finishErr != nil {
			return agentclient.DeviceConfiguration{}, finishErr
		}
		return agentclient.DeviceConfiguration{}, operationError(
			CodeUnavailable,
			operation,
			"host agent returned an invalid device configuration",
			err,
		)
	}
	outcomeContext, outcomeCancel := durableContext(ctx)
	defer outcomeCancel()
	if err := s.repository.FinishHardwareCommand(
		outcomeContext,
		requestID,
		store.HardwareCommandCompleted,
		configuration.Revision,
		"",
	); err != nil {
		return agentclient.DeviceConfiguration{}, operationError(
			CodeInternal,
			operation,
			"device configuration result could not be finalized",
			err,
		)
	}
	return configuration, nil
}

func (s *Service) resolveAttachedLine(
	ctx context.Context,
	lineSelector string,
) (store.LineSummary, error) {
	status, err := s.Status(ctx)
	if err != nil {
		return store.LineSummary{}, operationError(
			CodeUnavailable,
			"select line",
			"live lines are unavailable",
			err,
		)
	}
	return resolveLine(status.Lines, lineSelector, func(store.LineCapabilities) bool {
		return true
	})
}

func validateDeviceConfiguration(
	configuration agentclient.DeviceConfiguration,
	lineID string,
) error {
	if configuration.LineID != lineID {
		return fmt.Errorf("line id does not match")
	}
	if strings.TrimSpace(configuration.Revision) == "" {
		return fmt.Errorf("revision is missing")
	}
	if configuration.ObservedAt.IsZero() {
		return fmt.Errorf("observed_at is missing")
	}
	return nil
}

func deviceConfigurationDigest(
	lineID string,
	request agentclient.ApplyDeviceConfigurationRequest,
) []byte {
	radioEnabled := ""
	if request.RadioEnabled != nil {
		radioEnabled = strconv.FormatBool(*request.RadioEnabled)
	}
	return commandDigest(
		"device_configuration",
		lineID,
		string(request.Operation),
		request.ExpectedRevision,
		radioEnabled,
		request.APN,
		request.IPFamily,
		request.VoLTEPolicy,
	)
}
