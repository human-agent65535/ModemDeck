package httpapi

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/human-agent65535/modemdeck/internal/auth"
	"github.com/human-agent65535/modemdeck/internal/store"
)

type fakeUserRepository struct {
	*fakeRepository
	createInput store.CreateMemberInput
	createUser  store.User
	users       []store.User
	updateID    string
	updateInput store.UpdateMemberInput
	updateUser  store.User
	updateError error
	updateCalls int
}

func (repository *fakeUserRepository) Users(context.Context) ([]store.User, error) {
	return append([]store.User(nil), repository.users...), nil
}

func (repository *fakeUserRepository) CreateMember(
	_ context.Context,
	input store.CreateMemberInput,
) (store.User, error) {
	repository.createInput = input
	return repository.createUser, nil
}

func (repository *fakeUserRepository) UpdateMember(
	_ context.Context,
	userID string,
	input store.UpdateMemberInput,
) (store.User, error) {
	repository.updateCalls++
	repository.updateID = userID
	repository.updateInput = input
	return repository.updateUser, repository.updateError
}

func (*fakeUserRepository) SetMemberPassword(context.Context, string, string) error {
	return nil
}

func (*fakeUserRepository) SetProfileContact(context.Context, string) error {
	return nil
}

func TestCreateMemberAcceptsEightCharacterFinalPassword(t *testing.T) {
	t.Parallel()

	repository := &fakeUserRepository{
		fakeRepository: &fakeRepository{},
		createUser: store.User{
			ID:       "member-1",
			Username: "alice",
			Role:     auth.RoleMember,
			Enabled:  true,
			Revision: 1,
			LineIDs:  []string{},
		},
	}
	api, err := New(repository, Options{disableAuthentication: true})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/users",
		bytes.NewBufferString(`{
			"username":"alice",
			"password":"密码密码密码密码",
			"line_ids":[]
		}`),
	)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	api.ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d; body = %s", response.Code, response.Body.String())
	}
	matches, err := auth.VerifyPassword(
		"密码密码密码密码",
		repository.createInput.PasswordHash,
	)
	if err != nil || !matches {
		t.Fatalf("created password matches = %t, error = %v", matches, err)
	}
	if !bytes.Contains(response.Body.Bytes(), []byte(`"must_change_password":false`)) {
		t.Fatalf("response did not disable deprecated password-change state: %s", response.Body.String())
	}

	shortRequest := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/users",
		bytes.NewBufferString(`{
			"username":"bob",
			"password":"密码密码密码密",
			"line_ids":[]
		}`),
	)
	shortRequest.Header.Set("Content-Type", "application/json")
	shortResponse := httptest.NewRecorder()
	api.ServeHTTP(shortResponse, shortRequest)
	if shortResponse.Code != http.StatusUnprocessableEntity {
		t.Fatalf(
			"short password status = %d; body = %s",
			shortResponse.Code,
			shortResponse.Body.String(),
		)
	}
}

func TestMemberAccessUpdateReloadsTelegramRuntimeAfterCommit(t *testing.T) {
	t.Parallel()

	repository := &fakeUserRepository{
		fakeRepository: &fakeRepository{},
		updateUser: store.User{
			ID:       "member-1",
			Username: "alice",
			Role:     auth.RoleMember,
			Enabled:  true,
			Revision: 2,
			LineIDs:  []string{"line-1"},
		},
	}
	settings := &fakeTelegramSettings{}
	api, err := New(repository, Options{
		TelegramSettings:      settings,
		disableAuthentication: true,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	request := httptest.NewRequest(
		http.MethodPut,
		"/api/v1/users/member-1",
		bytes.NewBufferString(`{
			"username":"alice",
			"enabled":true,
			"ios_pairing_enabled":true,
			"line_ids":["line-1"],
			"revision":1
		}`),
	)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	api.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", response.Code, response.Body.String())
	}
	if repository.updateID != "member-1" {
		t.Fatalf("updated user ID = %q", repository.updateID)
	}
	wantInput := store.UpdateMemberInput{
		Username:          "alice",
		Enabled:           true,
		IOSPairingEnabled: true,
		LineIDs:           []string{"line-1"},
		Revision:          1,
	}
	if !reflect.DeepEqual(repository.updateInput, wantInput) {
		t.Fatalf("update input = %+v, want %+v", repository.updateInput, wantInput)
	}
	if settings.accessNotifications != 1 {
		t.Fatalf("Telegram access notifications = %d, want 1", settings.accessNotifications)
	}
}

func TestUpdateMemberHashesOptionalPassword(t *testing.T) {
	t.Parallel()

	repository := &fakeUserRepository{
		fakeRepository: &fakeRepository{},
		updateUser: store.User{
			ID:       "member-1",
			Username: "alice",
			Role:     auth.RoleMember,
			Enabled:  true,
			Revision: 2,
			LineIDs:  []string{"line-1"},
		},
	}
	api, err := New(repository, Options{disableAuthentication: true})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	request := httptest.NewRequest(
		http.MethodPut,
		"/api/v1/users/member-1",
		bytes.NewBufferString(`{
			"username":"alice",
			"password":"密码密码密码密码",
			"enabled":true,
			"ios_pairing_enabled":true,
			"line_ids":["line-1"],
			"revision":1
		}`),
	)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	api.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", response.Code, response.Body.String())
	}
	if repository.updateCalls != 1 {
		t.Fatalf("UpdateMember() calls = %d, want 1", repository.updateCalls)
	}
	matches, err := auth.VerifyPassword(
		"密码密码密码密码",
		repository.updateInput.PasswordHash,
	)
	if err != nil || !matches {
		t.Fatalf("updated password matches = %t, error = %v", matches, err)
	}
}

func TestUpdateMemberRejectsShortOptionalPasswordBeforeWrite(t *testing.T) {
	t.Parallel()

	repository := &fakeUserRepository{fakeRepository: &fakeRepository{}}
	api, err := New(repository, Options{disableAuthentication: true})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	request := httptest.NewRequest(
		http.MethodPut,
		"/api/v1/users/member-1",
		bytes.NewBufferString(`{
			"username":"alice",
			"password":"密码密码密码密",
			"enabled":true,
			"line_ids":[],
			"revision":1
		}`),
	)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	api.ServeHTTP(response, request)

	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d; body = %s", response.Code, response.Body.String())
	}
	if repository.updateCalls != 0 {
		t.Fatalf("UpdateMember() calls = %d, want 0", repository.updateCalls)
	}
}

func TestFailedMemberAccessUpdateDoesNotReloadTelegramRuntime(t *testing.T) {
	t.Parallel()

	repository := &fakeUserRepository{
		fakeRepository: &fakeRepository{},
		updateError:    errors.New("update failed"),
	}
	settings := &fakeTelegramSettings{}
	api, err := New(repository, Options{
		TelegramSettings:      settings,
		disableAuthentication: true,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	request := httptest.NewRequest(
		http.MethodPut,
		"/api/v1/users/member-1",
		bytes.NewBufferString(`{
			"username":"alice",
			"enabled":true,
			"line_ids":["line-1"],
			"revision":1
		}`),
	)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	api.ServeHTTP(response, request)

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d; body = %s", response.Code, response.Body.String())
	}
	if settings.accessNotifications != 0 {
		t.Fatalf("Telegram access notifications = %d, want 0", settings.accessNotifications)
	}
}
