package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/human-agent65535/modemdeck/internal/store"
)

func TestCreateContact(t *testing.T) {
	t.Parallel()

	repository := &fakeRepository{contact: store.Contact{ID: "contact-1", DisplayName: "Aiko", Revision: 1}}
	api, err := New(repository, Options{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	body := `{"display_name":"Aiko","notes":"Tokyo","phones":[{"label":"mobile","number":"+81 90-1234-5678","primary":true}]}`
	request := httptest.NewRequest(http.MethodPost, "/api/v1/contacts", bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	api.ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body = %s", response.Code, response.Body.String())
	}
	if repository.createContactInput.DisplayName != "Aiko" || len(repository.createContactInput.Phones) != 1 {
		t.Fatalf("create input = %+v", repository.createContactInput)
	}
	var result contactResponse
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if result.Contact.ID != "contact-1" {
		t.Fatalf("contact ID = %q", result.Contact.ID)
	}
}

func TestUpdateAndDeleteContactCarryRevision(t *testing.T) {
	t.Parallel()

	repository := &fakeRepository{contact: store.Contact{ID: "contact-1", Revision: 8}}
	api, err := New(repository, Options{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	updateBody := `{"display_name":"Aiko","revision":7,"phones":[{"id":"phone-1","label":"mobile","number":"+819012345678","primary":true}]}`
	update := httptest.NewRequest(http.MethodPut, "/api/v1/contacts/contact-1", bytes.NewBufferString(updateBody))
	update.Header.Set("Content-Type", "application/json; charset=utf-8")
	updateResponse := httptest.NewRecorder()
	api.ServeHTTP(updateResponse, update)
	if updateResponse.Code != http.StatusOK {
		t.Fatalf("update status = %d, want 200; body = %s", updateResponse.Code, updateResponse.Body.String())
	}
	if repository.updateContactID != "contact-1" || repository.updateContactInput.Revision != 7 {
		t.Fatalf("update = id %q input %+v", repository.updateContactID, repository.updateContactInput)
	}

	remove := httptest.NewRequest(http.MethodDelete, "/api/v1/contacts/contact-1?revision=8", nil)
	removeResponse := httptest.NewRecorder()
	api.ServeHTTP(removeResponse, remove)
	if removeResponse.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, want 204; body = %s", removeResponse.Code, removeResponse.Body.String())
	}
	if repository.deleteContactID != "contact-1" || repository.deleteContactRev != 8 {
		t.Fatalf("delete = id %q revision %d", repository.deleteContactID, repository.deleteContactRev)
	}
}

func TestContactWriteValidationAndErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		method      string
		path        string
		contentType string
		body        string
		repository  *fakeRepository
		wantStatus  int
		wantCode    string
	}{
		{name: "missing content type", method: http.MethodPost, path: "/api/v1/contacts", body: `{}`, repository: &fakeRepository{}, wantStatus: 415, wantCode: "unsupported_media_type"},
		{name: "unknown field", method: http.MethodPost, path: "/api/v1/contacts", contentType: "application/json", body: `{"display_name":"Aiko","unknown":true}`, repository: &fakeRepository{}, wantStatus: 400, wantCode: "invalid_json"},
		{name: "create revision forbidden", method: http.MethodPost, path: "/api/v1/contacts", contentType: "application/json", body: `{"revision":1}`, repository: &fakeRepository{}, wantStatus: 400, wantCode: "invalid_argument"},
		{name: "update revision required", method: http.MethodPut, path: "/api/v1/contacts/contact-1", contentType: "application/json", body: `{}`, repository: &fakeRepository{}, wantStatus: 400, wantCode: "invalid_argument"},
		{name: "delete revision required", method: http.MethodDelete, path: "/api/v1/contacts/contact-1", repository: &fakeRepository{}, wantStatus: 400, wantCode: "invalid_argument"},
		{name: "not found", method: http.MethodGet, path: "/api/v1/contacts/contact-1", repository: &fakeRepository{contactError: store.ErrContactNotFound}, wantStatus: 404, wantCode: "contact_not_found"},
		{name: "revision conflict", method: http.MethodPut, path: "/api/v1/contacts/contact-1", contentType: "application/json", body: `{"revision":2}`, repository: &fakeRepository{updateContactError: store.ErrContactRevisionConflict}, wantStatus: 409, wantCode: "revision_conflict"},
		{name: "phone conflict", method: http.MethodPost, path: "/api/v1/contacts", contentType: "application/json", body: `{}`, repository: &fakeRepository{createContactError: store.ErrContactPhoneConflict}, wantStatus: 409, wantCode: "phone_conflict"},
		{name: "invalid contact", method: http.MethodPost, path: "/api/v1/contacts", contentType: "application/json", body: `{}`, repository: &fakeRepository{createContactError: errors.Join(store.ErrContactValidation, errors.New("display_name is required"))}, wantStatus: 400, wantCode: "invalid_contact"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			api, err := New(test.repository, Options{})
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			request := httptest.NewRequest(test.method, test.path, bytes.NewBufferString(test.body))
			if test.contentType != "" {
				request.Header.Set("Content-Type", test.contentType)
			}
			response := httptest.NewRecorder()
			api.ServeHTTP(response, request)
			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", response.Code, test.wantStatus, response.Body.String())
			}
			var body errorResponse
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode error response: %v", err)
			}
			if body.Code != test.wantCode {
				t.Fatalf("code = %q, want %q", body.Code, test.wantCode)
			}
		})
	}
}
