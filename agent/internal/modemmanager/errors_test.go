package modemmanager

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/godbus/dbus/v5"
	"github.com/human-agent65535/modemdeck/agent/internal/domain"
)

func TestMapCallErrorUsesDocumentedDBusNames(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		dbusName string
		want     domain.ErrorCode
	}{
		{
			name:     "invalid argument",
			dbusName: "org.freedesktop.ModemManager1.Error.Core.InvalidArgs",
			want:     domain.ErrorInvalidArgument,
		},
		{
			name:     "not found",
			dbusName: "org.freedesktop.ModemManager1.Error.MobileEquipment.PdnConnectionNonexistent",
			want:     domain.ErrorNotFound,
		},
		{
			name:     "not supported",
			dbusName: "org.freedesktop.ModemManager1.Error.MobileEquipment.FeatureNotSupported",
			want:     domain.ErrorNotSupported,
		},
		{
			name:     "permission denied",
			dbusName: "org.freedesktop.DBus.Error.AccessDenied",
			want:     domain.ErrorPermissionDenied,
		},
		{
			name:     "wrong modem state",
			dbusName: "org.freedesktop.ModemManager1.Error.Core.WrongState",
			want:     domain.ErrorFailedPrecondition,
		},
		{
			name:     "SIM PIN required",
			dbusName: "org.freedesktop.ModemManager1.Error.MobileEquipment.SimPin",
			want:     domain.ErrorFailedPrecondition,
		},
		{
			name:     "APN rejected",
			dbusName: "org.freedesktop.ModemManager1.Error.MobileEquipment.MissingOrUnknownApn",
			want:     domain.ErrorNetworkRejected,
		},
		{
			name:     "registration rejected",
			dbusName: "org.freedesktop.ModemManager1.Error.MobileEquipment.NetworkNotAllowed",
			want:     domain.ErrorNetworkRejected,
		},
		{
			name:     "conflict",
			dbusName: "org.freedesktop.ModemManager1.Error.Core.InProgress",
			want:     domain.ErrorConflict,
		},
		{
			name:     "service unavailable",
			dbusName: "org.freedesktop.DBus.Error.ServiceUnknown",
			want:     domain.ErrorUnavailable,
		},
		{
			name:     "network timeout",
			dbusName: "org.freedesktop.ModemManager1.Error.MobileEquipment.NetworkTimeout",
			want:     domain.ErrorUnavailable,
		},
		{
			name:     "documented reset and retry nick",
			dbusName: "org.freedesktop.ModemManager1.Error.Core.ResetRetry",
			want:     domain.ErrorUnavailable,
		},
		{
			name:     "serial response timeout",
			dbusName: "org.freedesktop.ModemManager1.Error.Serial.ResponseTimeout",
			want:     domain.ErrorUnavailable,
		},
		{
			name:     "message SIM precondition",
			dbusName: "org.freedesktop.ModemManager1.Error.Message.SimNotInserted",
			want:     domain.ErrorFailedPrecondition,
		},
		{
			name:     "CDMA authentication rejection",
			dbusName: "org.freedesktop.ModemManager1.Error.CdmaActivation.SecurityAuthenticationFailed",
			want:     domain.ErrorNetworkRejected,
		},
		{
			name:     "carrier lock input",
			dbusName: "org.freedesktop.ModemManager1.Error.CarrierLock.InvalidSignature",
			want:     domain.ErrorInvalidArgument,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			cause := dbus.NewError(test.dbusName, []any{"message text is not classified"})
			err := mapCallError("test_operation", "operation failed", fmt.Errorf("wrapped: %w", cause))
			assertMappedError(t, err, test.want)
		})
	}
}

func TestMapCallErrorDoesNotClassifyBySuffixOrMessage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
	}{
		{
			name: "foreign suffix",
			err: dbus.NewError(
				"com.example.Error.Core.WrongState",
				[]any{"same documented suffix under a foreign namespace"},
			),
		},
		{
			name: "message text",
			err: dbus.NewError(
				"org.freedesktop.ModemManager1.Error.MobileEquipment.VendorSpecific",
				[]any{"SIM PIN required; network not allowed"},
			),
		},
		{
			name: "non D-Bus error",
			err:  errors.New("org.freedesktop.ModemManager1.Error.Core.WrongState"),
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			assertMappedError(
				t,
				mapCallError("test_operation", "operation failed", test.err),
				domain.ErrorInternal,
			)
		})
	}
}

func TestMapCallErrorTreatsContextTerminationAsUnavailable(t *testing.T) {
	t.Parallel()

	for _, cause := range []error{context.Canceled, context.DeadlineExceeded} {
		err := mapCallError("test_operation", "operation failed", fmt.Errorf("wrapped: %w", cause))
		assertMappedError(t, err, domain.ErrorUnavailable)
	}
}

func assertMappedError(t *testing.T, err error, want domain.ErrorCode) {
	t.Helper()

	operationError, ok := domain.AsOperationError(err)
	if !ok {
		t.Fatalf("error = %T %v, want OperationError", err, err)
	}
	if operationError.Code != want {
		t.Fatalf("code = %q, want %q (error: %v)", operationError.Code, want, err)
	}
	if operationError.Operation != "test_operation" {
		t.Fatalf("operation = %q, want test_operation", operationError.Operation)
	}
}
