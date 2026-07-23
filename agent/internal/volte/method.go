package volte

import (
	"context"
	"fmt"
	"strings"
)

type StateDecoder[T string | []byte] func(T) (State, error)
type PolicyEncoder[T string | []byte] func(Policy) (T, error)
type ResponseValidator[T string | []byte] func(T) error

// ReadMethod can only be created by the protocol-specific constructors in this
// package. A profile therefore names exactly one transport for each operation.
type ReadMethod interface {
	Protocol() Protocol
	read(context.Context, execution) (State, error)
	validate() error
}

// WriteMethod can only be created by the protocol-specific constructors in
// this package. It executes one command and never attempts another protocol.
type WriteMethod interface {
	Protocol() Protocol
	write(context.Context, execution, Policy) error
	validate() error
}

type execution struct {
	profileID  string
	transports Transports
}

type atReadMethod struct {
	command string
	decode  StateDecoder[string]
}

func ATRead(command string, decode StateDecoder[string]) ReadMethod {
	return atReadMethod{command: command, decode: decode}
}

func (m atReadMethod) Protocol() Protocol {
	return ProtocolAT
}

func (m atReadMethod) validate() error {
	if strings.TrimSpace(m.command) == "" {
		return fmt.Errorf("AT read command is required")
	}
	if m.decode == nil {
		return fmt.Errorf("AT read decoder is required")
	}
	return nil
}

func (m atReadMethod) read(ctx context.Context, run execution) (State, error) {
	const operation = "read"
	if run.transports.AT == nil {
		return State{}, newError(
			ErrorUnavailable,
			operation,
			run.profileID,
			ProtocolAT,
			"AT transport is unavailable",
			nil,
		)
	}
	response, err := run.transports.AT.Command(ctx, m.command)
	if err != nil {
		return State{}, executionError(operation, run.profileID, ProtocolAT, "AT read command failed", err)
	}
	state, err := m.decode(response)
	if err != nil {
		return State{}, newError(
			ErrorDecode,
			operation,
			run.profileID,
			ProtocolAT,
			"AT read response was invalid",
			err,
		)
	}
	return state, nil
}

type atWriteMethod struct {
	encode           PolicyEncoder[string]
	validateResponse ResponseValidator[string]
}

func ATWrite(
	encode PolicyEncoder[string],
	validateResponse ResponseValidator[string],
) WriteMethod {
	return atWriteMethod{encode: encode, validateResponse: validateResponse}
}

func (m atWriteMethod) Protocol() Protocol {
	return ProtocolAT
}

func (m atWriteMethod) validate() error {
	if m.encode == nil {
		return fmt.Errorf("AT write encoder is required")
	}
	if m.validateResponse == nil {
		return fmt.Errorf("AT write response validator is required")
	}
	return nil
}

func (m atWriteMethod) write(ctx context.Context, run execution, policy Policy) error {
	const operation = "apply"
	if run.transports.AT == nil {
		return newError(
			ErrorUnavailable,
			operation,
			run.profileID,
			ProtocolAT,
			"AT transport is unavailable",
			nil,
		)
	}
	command, err := m.encode(policy)
	if err != nil {
		return newError(
			ErrorInvalidProfile,
			operation,
			run.profileID,
			ProtocolAT,
			"AT profile could not encode the policy",
			err,
		)
	}
	if strings.TrimSpace(command) == "" {
		return newError(
			ErrorInvalidProfile,
			operation,
			run.profileID,
			ProtocolAT,
			"AT profile produced an empty write command",
			nil,
		)
	}
	response, err := run.transports.AT.Command(ctx, command)
	if err != nil {
		return executionError(operation, run.profileID, ProtocolAT, "AT write command failed", err)
	}
	if err := m.validateResponse(response); err != nil {
		return newError(
			ErrorDecode,
			operation,
			run.profileID,
			ProtocolAT,
			"AT write response was invalid",
			err,
		)
	}
	return nil
}

type qmiReadMethod struct {
	request QMIRequest
	decode  StateDecoder[[]byte]
}

func QMIRead(
	service string,
	method string,
	payload []byte,
	decode StateDecoder[[]byte],
) ReadMethod {
	return qmiReadMethod{
		request: QMIRequest{
			Service: service,
			Method:  method,
			Payload: append([]byte(nil), payload...),
		},
		decode: decode,
	}
}

func (m qmiReadMethod) Protocol() Protocol {
	return ProtocolQMI
}

func (m qmiReadMethod) validate() error {
	if strings.TrimSpace(m.request.Service) == "" {
		return fmt.Errorf("QMI read service is required")
	}
	if strings.TrimSpace(m.request.Method) == "" {
		return fmt.Errorf("QMI read method is required")
	}
	if m.decode == nil {
		return fmt.Errorf("QMI read decoder is required")
	}
	return nil
}

func (m qmiReadMethod) read(ctx context.Context, run execution) (State, error) {
	const operation = "read"
	if run.transports.QMI == nil {
		return State{}, newError(
			ErrorUnavailable,
			operation,
			run.profileID,
			ProtocolQMI,
			"QMI transport is unavailable",
			nil,
		)
	}
	response, err := run.transports.QMI.Request(ctx, cloneQMIRequest(m.request))
	if err != nil {
		return State{}, executionError(operation, run.profileID, ProtocolQMI, "QMI read request failed", err)
	}
	state, err := m.decode(append([]byte(nil), response...))
	if err != nil {
		return State{}, newError(
			ErrorDecode,
			operation,
			run.profileID,
			ProtocolQMI,
			"QMI read response was invalid",
			err,
		)
	}
	return state, nil
}

type qmiWriteMethod struct {
	service          string
	method           string
	encode           PolicyEncoder[[]byte]
	validateResponse ResponseValidator[[]byte]
}

func QMIWrite(
	service string,
	method string,
	encode PolicyEncoder[[]byte],
	validateResponse ResponseValidator[[]byte],
) WriteMethod {
	return qmiWriteMethod{
		service:          service,
		method:           method,
		encode:           encode,
		validateResponse: validateResponse,
	}
}

func (m qmiWriteMethod) Protocol() Protocol {
	return ProtocolQMI
}

func (m qmiWriteMethod) validate() error {
	if strings.TrimSpace(m.service) == "" {
		return fmt.Errorf("QMI write service is required")
	}
	if strings.TrimSpace(m.method) == "" {
		return fmt.Errorf("QMI write method is required")
	}
	if m.encode == nil {
		return fmt.Errorf("QMI write encoder is required")
	}
	if m.validateResponse == nil {
		return fmt.Errorf("QMI write response validator is required")
	}
	return nil
}

func (m qmiWriteMethod) write(ctx context.Context, run execution, policy Policy) error {
	const operation = "apply"
	if run.transports.QMI == nil {
		return newError(
			ErrorUnavailable,
			operation,
			run.profileID,
			ProtocolQMI,
			"QMI transport is unavailable",
			nil,
		)
	}
	payload, err := m.encode(policy)
	if err != nil {
		return newError(
			ErrorInvalidProfile,
			operation,
			run.profileID,
			ProtocolQMI,
			"QMI profile could not encode the policy",
			err,
		)
	}
	response, err := run.transports.QMI.Request(ctx, QMIRequest{
		Service: m.service,
		Method:  m.method,
		Payload: append([]byte(nil), payload...),
	})
	if err != nil {
		return executionError(operation, run.profileID, ProtocolQMI, "QMI write request failed", err)
	}
	if err := m.validateResponse(append([]byte(nil), response...)); err != nil {
		return newError(
			ErrorDecode,
			operation,
			run.profileID,
			ProtocolQMI,
			"QMI write response was invalid",
			err,
		)
	}
	return nil
}

func cloneQMIRequest(request QMIRequest) QMIRequest {
	request.Payload = append([]byte(nil), request.Payload...)
	return request
}
