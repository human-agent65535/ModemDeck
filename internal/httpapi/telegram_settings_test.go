package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/human-agent65535/modemdeck/internal/auth"
	"github.com/human-agent65535/modemdeck/internal/telegramsettings"
)

type fakeTelegramSettings struct {
	units       []telegramsettings.Unit
	createInput telegramsettings.CreateInput
	createError error
	updateID    string
	updateInput telegramsettings.UpdateInput
	updateError error
	deleteID    string
	deleteRev   int64
	deleteError error
}

func (settings *fakeTelegramSettings) List(context.Context) ([]telegramsettings.Unit, error) {
	return settings.units, nil
}

func (settings *fakeTelegramSettings) Create(
	_ context.Context,
	input telegramsettings.CreateInput,
) (telegramsettings.Unit, error) {
	settings.createInput = input
	if settings.createError != nil {
		return telegramsettings.Unit{}, settings.createError
	}
	return settings.units[0], nil
}

func (settings *fakeTelegramSettings) Update(
	_ context.Context,
	id string,
	input telegramsettings.UpdateInput,
) (telegramsettings.Unit, error) {
	settings.updateID = id
	settings.updateInput = input
	if settings.updateError != nil {
		return telegramsettings.Unit{}, settings.updateError
	}
	return settings.units[0], nil
}

func (settings *fakeTelegramSettings) Delete(_ context.Context, id string, revision int64) error {
	settings.deleteID = id
	settings.deleteRev = revision
	return settings.deleteError
}

func TestTelegramSettingsCollectionAndWriteOnlyToken(t *testing.T) {
	t.Parallel()

	unit := telegramsettings.Unit{
		ID:              "telegram_unit_1",
		DisplayName:     "Primary",
		Enabled:         true,
		ChatID:          "-100123",
		AdminID:         "42",
		LineScopes:      []string{"line-1"},
		IncomingSMS:     true,
		MissedCalls:     true,
		TokenConfigured: true,
		TokenHint:       "123456789",
		Revision:        1,
	}
	settings := &fakeTelegramSettings{units: []telegramsettings.Unit{unit}}
	api, err := New(&fakeRepository{}, Options{
		TelegramSettings:      settings,
		disableAuthentication: true,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	listResponse := httptest.NewRecorder()
	api.ServeHTTP(
		listResponse,
		httptest.NewRequest(http.MethodGet, "/api/v1/settings/telegram", nil),
	)
	if listResponse.Code != http.StatusOK {
		t.Fatalf("list status = %d; body = %s", listResponse.Code, listResponse.Body.String())
	}
	var list telegramUnitsResponse
	if err := json.Unmarshal(listResponse.Body.Bytes(), &list); err != nil ||
		len(list.Units) != 1 || !list.Units[0].TokenConfigured {
		t.Fatalf("list response = %+v, error = %v", list, err)
	}

	const token = "123456789:ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghi"
	createRequest := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/settings/telegram",
		bytes.NewBufferString(`{
			"display_name":"Primary",
			"enabled":true,
			"bot_token":"`+token+`",
			"chat_id":"-100123",
			"admin_id":"42",
			"line_scopes":["line-1"],
			"incoming_sms":true,
			"missed_calls":true
		}`),
	)
	createRequest.Header.Set("Content-Type", "application/json")
	createResponse := httptest.NewRecorder()
	api.ServeHTTP(createResponse, createRequest)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("create status = %d; body = %s", createResponse.Code, createResponse.Body.String())
	}
	if settings.createInput.BotToken != token ||
		settings.createInput.ChatID != "-100123" ||
		settings.createInput.AdminID != "42" {
		t.Fatalf("create input = %+v", settings.createInput)
	}
	if bytes.Contains(createResponse.Body.Bytes(), []byte(token)) {
		t.Fatalf("create response leaked token: %s", createResponse.Body.String())
	}
}

func TestTelegramSettingsResourceUsesRevision(t *testing.T) {
	t.Parallel()

	unit := telegramsettings.Unit{ID: "telegram_unit_1", DisplayName: "Primary", Revision: 4}
	settings := &fakeTelegramSettings{units: []telegramsettings.Unit{unit}}
	api, err := New(&fakeRepository{}, Options{
		TelegramSettings:      settings,
		disableAuthentication: true,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	updateRequest := httptest.NewRequest(
		http.MethodPut,
		"/api/v1/settings/telegram/telegram_unit_1",
		bytes.NewBufferString(`{
			"revision":4,
			"display_name":"Primary",
			"enabled":false,
			"chat_id":"",
			"admin_id":"",
			"line_scopes":[],
			"incoming_sms":false,
			"missed_calls":false
		}`),
	)
	updateRequest.Header.Set("Content-Type", "application/json")
	updateResponse := httptest.NewRecorder()
	api.ServeHTTP(updateResponse, updateRequest)
	if updateResponse.Code != http.StatusOK {
		t.Fatalf("update status = %d; body = %s", updateResponse.Code, updateResponse.Body.String())
	}
	if settings.updateID != "telegram_unit_1" ||
		settings.updateInput.Revision != 4 ||
		settings.updateInput.BotToken != nil {
		t.Fatalf("update = %q %+v", settings.updateID, settings.updateInput)
	}

	deleteResponse := httptest.NewRecorder()
	api.ServeHTTP(
		deleteResponse,
		httptest.NewRequest(
			http.MethodDelete,
			"/api/v1/settings/telegram/telegram_unit_1?revision=4",
			nil,
		),
	)
	if deleteResponse.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d; body = %s", deleteResponse.Code, deleteResponse.Body.String())
	}
	if settings.deleteID != "telegram_unit_1" || settings.deleteRev != 4 {
		t.Fatalf("delete = %q revision %d", settings.deleteID, settings.deleteRev)
	}
}

func TestTelegramSettingsErrorsAreTypedAndDoNotLeakCauses(t *testing.T) {
	t.Parallel()

	settings := &fakeTelegramSettings{
		units: []telegramsettings.Unit{{ID: "telegram_unit_1"}},
		updateError: &telegramsettings.Error{
			Code:    telegramsettings.CodeUnavailable,
			Message: "Stored Bot token could not be decrypted",
			Cause:   errors.New("secret key bytes"),
		},
	}
	api, err := New(&fakeRepository{}, Options{
		TelegramSettings:      settings,
		disableAuthentication: true,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	request := httptest.NewRequest(
		http.MethodPut,
		"/api/v1/settings/telegram/telegram_unit_1",
		bytes.NewBufferString(`{"revision":1,"display_name":"Primary","enabled":false}`),
	)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	api.ServeHTTP(response, request)
	assertAPIError(t, response, http.StatusServiceUnavailable, "telegram_settings_unavailable")
	if bytes.Contains(response.Body.Bytes(), []byte("secret key bytes")) {
		t.Fatalf("response leaked cause: %s", response.Body.String())
	}
}

func TestTelegramSettingsMembersManageOnlyTheirOwnBots(t *testing.T) {
	t.Parallel()

	settings := &fakeTelegramSettings{units: []telegramsettings.Unit{
		{
			ID:             "own-bot",
			DisplayName:    "Own",
			AssignedUserID: "member-1",
			Revision:       1,
		},
		{
			ID:             "other-bot",
			DisplayName:    "Other",
			AssignedUserID: "member-2",
			Revision:       1,
		},
	}}
	api, err := New(&fakeRepository{}, Options{
		TelegramSettings:      settings,
		disableAuthentication: true,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	member := auth.Principal{UserID: "member-1", Role: auth.RoleMember}
	memberRequest := func(method, path, body string) *http.Request {
		request := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		request.Header.Set("Content-Type", "application/json")
		return request.WithContext(auth.ContextWithPrincipal(request.Context(), member))
	}

	listResponse := httptest.NewRecorder()
	api.ServeHTTP(listResponse, memberRequest(http.MethodGet, "/api/v1/settings/telegram", ""))
	if listResponse.Code != http.StatusOK {
		t.Fatalf("list status = %d; body = %s", listResponse.Code, listResponse.Body.String())
	}
	var list telegramUnitsResponse
	if err := json.Unmarshal(listResponse.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list.Units) != 1 || list.Units[0].ID != "own-bot" {
		t.Fatalf("member-visible units = %+v", list.Units)
	}

	const payload = `{
		"revision":1,
		"display_name":"Member bot",
		"enabled":false,
		"assigned_user_id":"member-2",
		"line_scopes":[],
		"incoming_sms":true,
		"missed_calls":true
	}`
	createResponse := httptest.NewRecorder()
	api.ServeHTTP(
		createResponse,
		memberRequest(http.MethodPost, "/api/v1/settings/telegram", payload),
	)
	if createResponse.Code != http.StatusCreated ||
		settings.createInput.AssignedUserID != member.UserID {
		t.Fatalf(
			"create status = %d input = %+v body = %s",
			createResponse.Code,
			settings.createInput,
			createResponse.Body.String(),
		)
	}

	updateResponse := httptest.NewRecorder()
	api.ServeHTTP(
		updateResponse,
		memberRequest(http.MethodPut, "/api/v1/settings/telegram/own-bot", payload),
	)
	if updateResponse.Code != http.StatusOK ||
		settings.updateInput.AssignedUserID != member.UserID {
		t.Fatalf(
			"update status = %d input = %+v body = %s",
			updateResponse.Code,
			settings.updateInput,
			updateResponse.Body.String(),
		)
	}

	deniedUpdate := httptest.NewRecorder()
	api.ServeHTTP(
		deniedUpdate,
		memberRequest(http.MethodPut, "/api/v1/settings/telegram/other-bot", payload),
	)
	assertAPIError(t, deniedUpdate, http.StatusNotFound, "not_found")

	deniedDelete := httptest.NewRecorder()
	api.ServeHTTP(
		deniedDelete,
		memberRequest(
			http.MethodDelete,
			"/api/v1/settings/telegram/other-bot?revision=1",
			"",
		),
	)
	assertAPIError(t, deniedDelete, http.StatusNotFound, "not_found")
}
