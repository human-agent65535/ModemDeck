package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/human-agent65535/modemdeck/internal/mobilepairing"
)

type mobilePushRepository interface {
	UpdateIOSPushRegistration(
		context.Context,
		mobilepairing.TokenDigest,
		mobilepairing.PushRegistration,
	) error
	ClearIOSPushRegistration(context.Context, mobilepairing.TokenDigest) error
}

func (api *API) mobilePush(response http.ResponseWriter, request *http.Request) {
	authentication, ok := mobileAuthenticationFromContext(request.Context())
	if !ok {
		writeError(
			response,
			http.StatusUnauthorized,
			"authentication_required",
			"Authentication is required",
			"",
		)
		return
	}
	repository, ok := api.repository.(mobilePushRepository)
	if !ok {
		writeError(
			response,
			http.StatusServiceUnavailable,
			"push_registration_unavailable",
			"iOS push registration is unavailable",
			"",
		)
		return
	}

	switch request.Method {
	case http.MethodPut:
		var input mobilepairing.PushRegistration
		if !decodeJSONBody(response, request, &input) {
			return
		}
		registration, err := input.Normalize()
		if errors.Is(err, mobilepairing.ErrInvalidPushToken) {
			writeError(
				response,
				http.StatusBadRequest,
				"invalid_push_registration",
				"The Apple push registration is invalid",
				"",
			)
			return
		}
		if err != nil {
			writeError(
				response,
				http.StatusBadRequest,
				"invalid_push_registration",
				err.Error(),
				"",
			)
			return
		}
		if err := repository.UpdateIOSPushRegistration(
			request.Context(),
			authentication.Digest,
			registration,
		); err != nil {
			api.logger.Error("store iOS push registration", "error", err)
			writeError(
				response,
				http.StatusServiceUnavailable,
				"push_registration_unavailable",
				"iOS push registration could not be stored",
				"",
			)
			return
		}
	case http.MethodDelete:
		if err := repository.ClearIOSPushRegistration(
			request.Context(),
			authentication.Digest,
		); err != nil {
			api.logger.Error("clear iOS push registration", "error", err)
			writeError(
				response,
				http.StatusServiceUnavailable,
				"push_registration_unavailable",
				"iOS push registration could not be cleared",
				"",
			)
			return
		}
	default:
		response.Header().Set("Allow", http.MethodPut+", "+http.MethodDelete)
		writeError(
			response,
			http.StatusMethodNotAllowed,
			"method_not_allowed",
			"Only PUT and DELETE are supported",
			"",
		)
		return
	}

	response.Header().Set("Cache-Control", "no-store")
	response.WriteHeader(http.StatusNoContent)
}
