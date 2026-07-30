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
	users       []store.User
	updateID    string
	updateInput store.UpdateMemberInput
	updateUser  store.User
	updateError error
}

func (repository *fakeUserRepository) Users(context.Context) ([]store.User, error) {
	return append([]store.User(nil), repository.users...), nil
}

func (repository *fakeUserRepository) CreateMember(
	context.Context,
	store.CreateMemberInput,
) (store.User, error) {
	return store.User{}, nil
}

func (repository *fakeUserRepository) UpdateMember(
	_ context.Context,
	userID string,
	input store.UpdateMemberInput,
) (store.User, error) {
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
		Username: "alice",
		Enabled:  true,
		LineIDs:  []string{"line-1"},
		Revision: 1,
	}
	if !reflect.DeepEqual(repository.updateInput, wantInput) {
		t.Fatalf("update input = %+v, want %+v", repository.updateInput, wantInput)
	}
	if settings.accessNotifications != 1 {
		t.Fatalf("Telegram access notifications = %d, want 1", settings.accessNotifications)
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
