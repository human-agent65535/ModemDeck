package modemmanager

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/godbus/dbus/v5"
	"github.com/human-agent65535/modemdeck/agent/internal/domain"
)

const (
	dbusErrorPrefix             = "org.freedesktop.DBus.Error."
	modemManagerCoreErrorPrefix = "org.freedesktop.ModemManager1.Error.Core."
	mobileEquipmentErrorPrefix  = "org.freedesktop.ModemManager1.Error.MobileEquipment."
	connectionErrorPrefix       = "org.freedesktop.ModemManager1.Error.Connection."
	serialErrorPrefix           = "org.freedesktop.ModemManager1.Error.Serial."
	messageErrorPrefix          = "org.freedesktop.ModemManager1.Error.Message."
	cdmaActivationErrorPrefix   = "org.freedesktop.ModemManager1.Error.CdmaActivation."
	carrierLockErrorPrefix      = "org.freedesktop.ModemManager1.Error.CarrierLock."
)

func mapCallError(operation, message string, err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return domain.Unavailable(operation, message, err)
	}

	code, ok := classifyDBusError(dbusErrorName(err))
	if !ok {
		return domain.Internal(operation, message, err)
	}
	return domain.NewOperationError(code, operation, message, err)
}

func classifyDBusError(name string) (domain.ErrorCode, bool) {
	switch name {
	case dbusErrorPrefix + "InvalidArgs",
		modemManagerCoreErrorPrefix + "InvalidArgs",
		mobileEquipmentErrorPrefix + "InvalidIndex",
		mobileEquipmentErrorPrefix + "TextTooLong",
		mobileEquipmentErrorPrefix + "InvalidChars",
		mobileEquipmentErrorPrefix + "DialStringTooLong",
		mobileEquipmentErrorPrefix + "DialStringInvalid",
		mobileEquipmentErrorPrefix + "IncorrectParameters",
		messageErrorPrefix + "InvalidPduParameter",
		messageErrorPrefix + "InvalidTextParameter",
		messageErrorPrefix + "InvalidIndex",
		carrierLockErrorPrefix + "InvalidSignature",
		carrierLockErrorPrefix + "InvalidImei",
		carrierLockErrorPrefix + "InvalidTimestamp",
		carrierLockErrorPrefix + "NetworkListTooLarge",
		carrierLockErrorPrefix + "DecodeOrParsingError":
		return domain.ErrorInvalidArgument, true

	case dbusErrorPrefix + "UnknownObject",
		dbusErrorPrefix + "FileNotFound",
		modemManagerCoreErrorPrefix + "NotFound",
		mobileEquipmentErrorPrefix + "NotFound",
		mobileEquipmentErrorPrefix + "UnknownPdpContext",
		mobileEquipmentErrorPrefix + "PdnConnectionNonexistent":
		return domain.ErrorNotFound, true

	case dbusErrorPrefix + "UnknownMethod",
		dbusErrorPrefix + "UnknownInterface",
		dbusErrorPrefix + "UnknownProperty",
		dbusErrorPrefix + "NotSupported",
		modemManagerCoreErrorPrefix + "Unsupported",
		mobileEquipmentErrorPrefix + "NotSupported",
		mobileEquipmentErrorPrefix + "EapMethodNotSupported",
		mobileEquipmentErrorPrefix + "LanguageOrAlphabetNotSupported",
		mobileEquipmentErrorPrefix + "ServiceOptionNotSupported",
		mobileEquipmentErrorPrefix + "FeatureNotSupported",
		mobileEquipmentErrorPrefix + "MessageTypeNotImplemented",
		mobileEquipmentErrorPrefix + "BearerHandlingUnsupported",
		mobileEquipmentErrorPrefix + "UnsupportedQciOr5qiValue",
		mobileEquipmentErrorPrefix + "UnsupportedSscMode",
		mobileEquipmentErrorPrefix + "IeNotImplemented",
		messageErrorPrefix + "NotSupported",
		carrierLockErrorPrefix + "SignatureAlgorithmNotSupported",
		carrierLockErrorPrefix + "FeatureNotSupported":
		return domain.ErrorNotSupported, true

	case dbusErrorPrefix + "AccessDenied",
		dbusErrorPrefix + "AuthFailed",
		modemManagerCoreErrorPrefix + "Unauthorized":
		return domain.ErrorPermissionDenied, true

	case modemManagerCoreErrorPrefix + "WrongState",
		modemManagerCoreErrorPrefix + "Connected",
		modemManagerCoreErrorPrefix + "WrongSimState",
		mobileEquipmentErrorPrefix + "NotAllowed",
		mobileEquipmentErrorPrefix + "PhSimPin",
		mobileEquipmentErrorPrefix + "PhFsimPin",
		mobileEquipmentErrorPrefix + "PhFsimPuk",
		mobileEquipmentErrorPrefix + "SimNotInserted",
		mobileEquipmentErrorPrefix + "SimPin",
		mobileEquipmentErrorPrefix + "SimPuk",
		mobileEquipmentErrorPrefix + "SimFailure",
		mobileEquipmentErrorPrefix + "SimBusy",
		mobileEquipmentErrorPrefix + "SimWrong",
		mobileEquipmentErrorPrefix + "IncorrectPassword",
		mobileEquipmentErrorPrefix + "SimPin2",
		mobileEquipmentErrorPrefix + "SimPuk2",
		mobileEquipmentErrorPrefix + "NetworkPin",
		mobileEquipmentErrorPrefix + "NetworkPuk",
		mobileEquipmentErrorPrefix + "NetworkSubsetPin",
		mobileEquipmentErrorPrefix + "NetworkSubsetPuk",
		mobileEquipmentErrorPrefix + "ServicePin",
		mobileEquipmentErrorPrefix + "ServicePuk",
		mobileEquipmentErrorPrefix + "CorpPin",
		mobileEquipmentErrorPrefix + "CorpPuk",
		mobileEquipmentErrorPrefix + "HiddenKeyRequired",
		mobileEquipmentErrorPrefix + "CommandDisabled",
		mobileEquipmentErrorPrefix + "NotAttachedRestricted",
		mobileEquipmentErrorPrefix + "FixedDialNumberOnly",
		mobileEquipmentErrorPrefix + "LastPdnDisconnectionNotAllowedLegacy",
		mobileEquipmentErrorPrefix + "LastPdnDisconnectionNotAllowed",
		mobileEquipmentErrorPrefix + "NoBearerActivated",
		mobileEquipmentErrorPrefix + "MessageNotCompatibleWithProtocolState",
		mobileEquipmentErrorPrefix + "MessageTypeNotCompatibleWithProtocolState",
		messageErrorPrefix + "NotAllowed",
		messageErrorPrefix + "SimNotInserted",
		messageErrorPrefix + "SimPin",
		messageErrorPrefix + "PhSimPin",
		messageErrorPrefix + "SimFailure",
		messageErrorPrefix + "SimBusy",
		messageErrorPrefix + "SimWrong",
		messageErrorPrefix + "SimPuk",
		messageErrorPrefix + "SimPin2",
		messageErrorPrefix + "SimPuk2",
		messageErrorPrefix + "SmscAddressUnknown",
		messageErrorPrefix + "NoCnmaAckExpected",
		cdmaActivationErrorPrefix + "Roaming",
		cdmaActivationErrorPrefix + "WrongRadioInterface":
		return domain.ErrorFailedPrecondition, true

	case mobileEquipmentErrorPrefix + "NetworkNotAllowed",
		mobileEquipmentErrorPrefix + "NotAllowedEmergencyOnly",
		mobileEquipmentErrorPrefix + "NotAllowedRestricted",
		mobileEquipmentErrorPrefix + "CallBarred",
		mobileEquipmentErrorPrefix + "ImsiUnknownInHss",
		mobileEquipmentErrorPrefix + "IllegalUe",
		mobileEquipmentErrorPrefix + "ImsiUnknownInVlr",
		mobileEquipmentErrorPrefix + "ImeiNotAccepted",
		mobileEquipmentErrorPrefix + "IllegalMe",
		mobileEquipmentErrorPrefix + "PsServicesNotAllowed",
		mobileEquipmentErrorPrefix + "PsAndNonPsServicesNotAllowed",
		mobileEquipmentErrorPrefix + "UeIdentityNotDerivedFromNetwork",
		mobileEquipmentErrorPrefix + "PlmnNotAllowed",
		mobileEquipmentErrorPrefix + "AreaNotAllowed",
		mobileEquipmentErrorPrefix + "RoamingNotAllowedInArea",
		mobileEquipmentErrorPrefix + "PsServicesNotAllowedInPlmn",
		mobileEquipmentErrorPrefix + "NotAuthorizedForCsg",
		mobileEquipmentErrorPrefix + "MissingOrUnknownApn",
		mobileEquipmentErrorPrefix + "UnknownPdpAddressOrType",
		mobileEquipmentErrorPrefix + "UserAuthenticationFailed",
		mobileEquipmentErrorPrefix + "ActivationRejectedByGgsnOrGw",
		mobileEquipmentErrorPrefix + "ActivationRejectedUnspecified",
		mobileEquipmentErrorPrefix + "ServiceOptionNotSubscribed",
		mobileEquipmentErrorPrefix + "QosNotAccepted",
		mobileEquipmentErrorPrefix + "PdpAuthFailure",
		mobileEquipmentErrorPrefix + "OperatorDeterminedBarring",
		mobileEquipmentErrorPrefix + "RequestedApnNotSupported",
		mobileEquipmentErrorPrefix + "RequestRejectedBcmViolation",
		mobileEquipmentErrorPrefix + "ServiceOptionNotAuthorizedInPlmn",
		mobileEquipmentErrorPrefix + "ApnRestrictionIncompatible",
		mobileEquipmentErrorPrefix + "N1ModeNotAllowed",
		mobileEquipmentErrorPrefix + "RestrictedServiceArea",
		mobileEquipmentErrorPrefix + "MissingOrUnknownDnnInSlice",
		mobileEquipmentErrorPrefix + "Non3gppAccessTo5gcnNotAllowed",
		mobileEquipmentErrorPrefix + "ServingNetworkNotAuthorized",
		mobileEquipmentErrorPrefix + "DnnNotSupportedInSlice",
		mobileEquipmentErrorPrefix + "OutOfLadnServiceArea",
		mobileEquipmentErrorPrefix + "MaxDataRateForUserPlaneIntegrityTooLow",
		mobileEquipmentErrorPrefix + "TemporarilyUnauthorizedForSnpn",
		mobileEquipmentErrorPrefix + "PermanentlyUnauthorizedForSnpn",
		mobileEquipmentErrorPrefix + "UnauthorizedForCag",
		mobileEquipmentErrorPrefix + "WirelineAccessAreaNotAllowed",
		cdmaActivationErrorPrefix + "SecurityAuthenticationFailed",
		cdmaActivationErrorPrefix + "ProvisioningFailed":
		return domain.ErrorNetworkRejected, true

	case dbusErrorPrefix + "LimitsExceeded",
		dbusErrorPrefix + "ObjectPathInUse",
		modemManagerCoreErrorPrefix + "InProgress",
		modemManagerCoreErrorPrefix + "TooMany",
		modemManagerCoreErrorPrefix + "Exists",
		modemManagerCoreErrorPrefix + "Throttled",
		mobileEquipmentErrorPrefix + "LinkReserved",
		mobileEquipmentErrorPrefix + "MemoryFull",
		mobileEquipmentErrorPrefix + "NsapiOrPtiAlreadyInUse",
		mobileEquipmentErrorPrefix + "PdpContextWithoutTftAlreadyActivated",
		mobileEquipmentErrorPrefix + "MaximumNumberOfBearersReached",
		mobileEquipmentErrorPrefix + "CollisionWithNetworkInitiatedRequest",
		mobileEquipmentErrorPrefix + "MultipleAccessToPdnConnectionNotAllowed",
		mobileEquipmentErrorPrefix + "MultiplePdnConnectionSameApnNotAllowed",
		mobileEquipmentErrorPrefix + "NkgsiAlreadyInUse",
		connectionErrorPrefix + "Busy",
		messageErrorPrefix + "SmsServiceReserved",
		messageErrorPrefix + "MemoryFull":
		return domain.ErrorConflict, true

	case dbusErrorPrefix + "ServiceUnknown",
		dbusErrorPrefix + "NameHasNoOwner",
		dbusErrorPrefix + "NoReply",
		dbusErrorPrefix + "Timeout",
		dbusErrorPrefix + "Disconnected",
		modemManagerCoreErrorPrefix + "Cancelled",
		modemManagerCoreErrorPrefix + "Aborted",
		modemManagerCoreErrorPrefix + "NoPlugins",
		modemManagerCoreErrorPrefix + "Retry",
		modemManagerCoreErrorPrefix + "ResetRetry",
		modemManagerCoreErrorPrefix + "Timeout",
		modemManagerCoreErrorPrefix + "Protocol",
		mobileEquipmentErrorPrefix + "PhoneFailure",
		mobileEquipmentErrorPrefix + "NoConnection",
		mobileEquipmentErrorPrefix + "NoNetwork",
		mobileEquipmentErrorPrefix + "NetworkTimeout",
		mobileEquipmentErrorPrefix + "CommandAborted",
		mobileEquipmentErrorPrefix + "TemporarilyOutOfService",
		mobileEquipmentErrorPrefix + "ImplicitlyDetached",
		mobileEquipmentErrorPrefix + "NoCellsInArea",
		mobileEquipmentErrorPrefix + "MscTemporarilyNotReachable",
		mobileEquipmentErrorPrefix + "NetworkFailureAttach",
		mobileEquipmentErrorPrefix + "CsDomainUnavailable",
		mobileEquipmentErrorPrefix + "EsmFailure",
		mobileEquipmentErrorPrefix + "Congestion",
		mobileEquipmentErrorPrefix + "InsufficientResources",
		mobileEquipmentErrorPrefix + "ServiceOptionOutOfOrder",
		mobileEquipmentErrorPrefix + "RegularDeactivation",
		mobileEquipmentErrorPrefix + "CsServiceTemporarilyUnavailable",
		mobileEquipmentErrorPrefix + "MulticastGroupMembershipTimeout",
		mobileEquipmentErrorPrefix + "UserDataViaControlPlaneCongested",
		mobileEquipmentErrorPrefix + "RecoveryOnTimerExpiry",
		mobileEquipmentErrorPrefix + "NetworkFailureActivation",
		mobileEquipmentErrorPrefix + "ReactivationRequested",
		mobileEquipmentErrorPrefix + "SevereNetworkFailure",
		mobileEquipmentErrorPrefix + "InsufficientResourcesForSliceAndDnn",
		mobileEquipmentErrorPrefix + "InsufficientResourcesForSlice",
		mobileEquipmentErrorPrefix + "LadnUnavailable",
		mobileEquipmentErrorPrefix + "PayloadNotForwarded",
		mobileEquipmentErrorPrefix + "InsufficientUserPlaneResourcesForPduSession",
		mobileEquipmentErrorPrefix + "NoNetworkSlicesAvailable",
		connectionErrorPrefix + "NoCarrier",
		connectionErrorPrefix + "NoDialtone",
		connectionErrorPrefix + "NoAnswer",
		serialErrorPrefix + "OpenFailed",
		serialErrorPrefix + "SendFailed",
		serialErrorPrefix + "ResponseTimeout",
		serialErrorPrefix + "OpenFailedNoDevice",
		serialErrorPrefix + "NotOpen",
		messageErrorPrefix + "NoNetwork",
		messageErrorPrefix + "NetworkTimeout",
		cdmaActivationErrorPrefix + "CouldNotConnect",
		cdmaActivationErrorPrefix + "NoSignal",
		cdmaActivationErrorPrefix + "TimedOut",
		cdmaActivationErrorPrefix + "StartFailed":
		return domain.ErrorUnavailable, true
	}
	return "", false
}

func dbusErrorName(err error) string {
	var pointer *dbus.Error
	if errors.As(err, &pointer) && pointer != nil {
		return pointer.Name
	}
	var value dbus.Error
	if errors.As(err, &value) {
		return value.Name
	}
	return ""
}

func deliveryReportCreateRejected(err error) bool {
	operationError, ok := domain.AsOperationError(err)
	if !ok {
		return false
	}
	return operationError.Code == domain.ErrorInvalidArgument ||
		operationError.Code == domain.ErrorNotSupported
}

func deliveryReportSendRejected(err error) bool {
	operationError, ok := domain.AsOperationError(err)
	if ok && operationError.Code == domain.ErrorNotSupported {
		return true
	}
	details := strings.ToLower(dbusErrorDetails(err))
	if details == "" {
		return false
	}
	for _, marker := range []string{
		"requested facility not subscribed",
		"requested facility not implemented",
		"requested service option not subscribed",
		"service option not supported",
	} {
		if strings.Contains(details, marker) {
			return true
		}
	}
	return containsExactErrorCode(details, "unknown message error:", "50") ||
		containsExactErrorCode(details, "unknown message error:", "69") ||
		containsExactErrorCode(details, "+cms error:", "50") ||
		containsExactErrorCode(details, "+cms error:", "69")
}

func containsExactErrorCode(details, marker, code string) bool {
	for {
		index := strings.Index(details, marker)
		if index < 0 {
			return false
		}
		remainder := strings.TrimLeft(details[index+len(marker):], " \t")
		if strings.HasPrefix(remainder, code) &&
			(len(remainder) == len(code) ||
				remainder[len(code)] < '0' ||
				remainder[len(code)] > '9') {
			return true
		}
		details = details[index+len(marker):]
	}
}

func dbusErrorDetails(err error) string {
	var pointer *dbus.Error
	if errors.As(err, &pointer) && pointer != nil {
		return dbusErrorBody(pointer)
	}
	var value dbus.Error
	if errors.As(err, &value) {
		return dbusErrorBody(&value)
	}
	return ""
}

func dbusErrorBody(err *dbus.Error) string {
	if err == nil {
		return ""
	}
	parts := make([]string, 0, len(err.Body)+1)
	parts = append(parts, err.Name)
	for _, value := range err.Body {
		parts = append(parts, fmt.Sprint(value))
	}
	return strings.Join(parts, " ")
}
