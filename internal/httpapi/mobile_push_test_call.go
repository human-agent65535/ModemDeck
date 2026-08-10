package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/human-agent65535/modemdeck/internal/applepush"
	"github.com/human-agent65535/modemdeck/internal/auth"
	"github.com/human-agent65535/modemdeck/internal/mobilepairing"
	"github.com/human-agent65535/modemdeck/internal/store"
)

type iosTestCallRepository interface {
	IOSPairingStatus(context.Context, string) (store.IOSPairingStatus, error)
	IOSPairingCredentialIDByTokenDigest(
		context.Context,
		mobilepairing.TokenDigest,
	) (string, bool, error)
}

type iosTestCallResponse struct {
	ID         string `json:"id"`
	AcceptedAt string `json:"accepted_at"`
}

func (api *API) mobilePushTestCall(
	response http.ResponseWriter,
	request *http.Request,
) {
	if api.iosCallTests == nil {
		writeError(
			response,
			http.StatusServiceUnavailable,
			"apple_push_unavailable",
			"Apple push delivery is unavailable",
			"",
		)
		return
	}
	userID := auth.InitialAdminUserID
	if principal, ok := auth.PrincipalFromContext(request.Context()); ok {
		userID = principal.UserID
	}
	repository, ok := api.repository.(iosTestCallRepository)
	if !ok {
		writeError(
			response,
			http.StatusServiceUnavailable,
			"apple_push_unavailable",
			"Apple push delivery is unavailable",
			"",
		)
		return
	}
	credentialID := strings.TrimSpace(request.URL.Query().Get("credential_id"))
	if authentication, mobile := mobileAuthenticationFromContext(request.Context()); mobile {
		var found bool
		var err error
		credentialID, found, err = repository.IOSPairingCredentialIDByTokenDigest(
			request.Context(),
			authentication.Digest,
		)
		if err != nil {
			api.writeInternalError(response, request, "read current iOS push target", err)
			return
		}
		if !found {
			credentialID = ""
		}
	} else if credentialID == "" {
		status, err := repository.IOSPairingStatus(request.Context(), userID)
		if err != nil {
			api.writeInternalError(response, request, "read iOS push targets", err)
			return
		}
		if len(status.Devices) == 1 {
			credentialID = status.Devices[0].ID
		} else if len(status.Devices) > 1 {
			writeError(
				response,
				http.StatusUnprocessableEntity,
				"ios_pairing_credential_required",
				"Choose an Apple device for the test call",
				"credential_id",
			)
			return
		}
	}
	result, err := api.iosCallTests.SendTestCall(
		request.Context(),
		userID,
		credentialID,
	)
	if err != nil {
		switch {
		case errors.Is(err, applepush.ErrPushTargetUnavailable):
			writeError(
				response,
				http.StatusConflict,
				"pushkit_not_registered",
				"The paired Apple device has not registered for incoming calls",
				"",
			)
		case errors.Is(err, applepush.ErrPushTopicMismatch):
			writeError(
				response,
				http.StatusConflict,
				"pushkit_topic_mismatch",
				"The paired iPhone push topic does not match this server",
				"",
			)
		default:
			api.logger.Warn(
				"send iOS test call",
				"component", "apple_push",
				"user_id", userID,
				"error", err,
			)
			writeError(
				response,
				http.StatusBadGateway,
				"test_call_delivery_failed",
				"Apple did not accept the test call",
				"",
			)
		}
		return
	}
	response.Header().Set("Cache-Control", "no-store")
	writeJSON(response, http.StatusAccepted, iosTestCallResponse{
		ID:         result.ID,
		AcceptedAt: result.AcceptedAt.UTC().Format(time.RFC3339Nano),
	})
}
