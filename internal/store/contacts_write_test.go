package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	platformdb "github.com/human-agent65535/modemdeck/internal/platform/database"
)

func TestContactCRUD(t *testing.T) {
	t.Parallel()

	repository, _ := newContactTestStore(t)
	ctx := context.Background()

	created, err := repository.CreateContact(ctx, ContactInput{
		DisplayName: "  Ada Lovelace  ",
		Notes:       "  first programmer  ",
		Phones: []ContactPhoneInput{
			{Label: " mobile ", Number: " +44 (20) 7946-0958 ", Primary: true},
			{Label: "work", Number: "+1 212-555-0198"},
		},
	})
	if err != nil {
		t.Fatalf("CreateContact() error = %v", err)
	}
	if created.ID == "" {
		t.Fatal("CreateContact() returned an empty id")
	}
	if created.DisplayName != "Ada Lovelace" || created.Notes != "first programmer" {
		t.Fatalf("CreateContact() text = %q / %q", created.DisplayName, created.Notes)
	}
	if created.Revision != 1 {
		t.Fatalf("CreateContact() revision = %d, want 1", created.Revision)
	}
	if created.CreatedAt == "" || created.UpdatedAt == "" {
		t.Fatalf("CreateContact() timestamps = %q / %q", created.CreatedAt, created.UpdatedAt)
	}
	if len(created.Phones) != 2 {
		t.Fatalf("CreateContact() phones = %d, want 2", len(created.Phones))
	}
	primary := contactPhoneByCanonical(t, created, "+442079460958")
	if primary.OriginalNumber != "+44 (20) 7946-0958" || !primary.Primary {
		t.Fatalf("primary phone = %#v", primary)
	}
	work := contactPhoneByCanonical(t, created, "+12125550198")

	read, err := repository.Contact(ctx, created.ID)
	if err != nil {
		t.Fatalf("Contact() error = %v", err)
	}
	if read.ID != created.ID || read.Revision != created.Revision || len(read.Phones) != 2 {
		t.Fatalf("Contact() = %#v, want created contact", read)
	}

	updated, err := repository.UpdateContact(ctx, created.ID, ContactInput{
		DisplayName: "Ada Byron",
		Notes:       "updated",
		Revision:    created.Revision,
		Phones: []ContactPhoneInput{
			{
				ID:      primary.ID,
				Label:   "mobile",
				Number:  "+44 7700 900-123",
				Primary: true,
			},
		},
	})
	if err != nil {
		t.Fatalf("UpdateContact() error = %v", err)
	}
	if updated.Revision != 2 {
		t.Fatalf("UpdateContact() revision = %d, want 2", updated.Revision)
	}
	if updated.DisplayName != "Ada Byron" || updated.Notes != "updated" {
		t.Fatalf("UpdateContact() text = %q / %q", updated.DisplayName, updated.Notes)
	}
	if len(updated.Phones) != 1 {
		t.Fatalf("UpdateContact() phones = %d, want whole-resource replacement with 1", len(updated.Phones))
	}
	if updated.Phones[0].ID != primary.ID || updated.Phones[0].CanonicalE164 != "+447700900123" {
		t.Fatalf("UpdateContact() phone = %#v", updated.Phones[0])
	}
	for _, phone := range updated.Phones {
		if phone.ID == work.ID {
			t.Fatalf("UpdateContact() retained omitted phone %s", work.ID)
		}
	}

	err = repository.DeleteContact(ctx, updated.ID, created.Revision)
	assertContactErrorIs(t, "DeleteContact(stale)", err, ErrContactRevisionConflict)
	if _, err := repository.Contact(ctx, updated.ID); err != nil {
		t.Fatalf("contact disappeared after stale delete: %v", err)
	}

	if err := repository.DeleteContact(ctx, updated.ID, updated.Revision); err != nil {
		t.Fatalf("DeleteContact() error = %v", err)
	}
	_, err = repository.Contact(ctx, updated.ID)
	assertContactErrorIs(t, "Contact(deleted)", err, ErrContactNotFound)
}

func TestContactTypedConflictsAndPhoneOwnership(t *testing.T) {
	t.Parallel()

	repository, _ := newContactTestStore(t)
	ctx := context.Background()
	first := mustCreateContact(t, repository, ContactInput{
		DisplayName: "First",
		Phones: []ContactPhoneInput{
			{Label: "mobile", Number: "+81 (90) 1234-5678", Primary: true},
		},
	})

	_, err := repository.CreateContact(ctx, ContactInput{
		DisplayName: "Duplicate",
		Phones: []ContactPhoneInput{
			{Label: "mobile", Number: "+81 90-1234-5678", Primary: true},
		},
	})
	assertContactErrorIs(t, "CreateContact(duplicate canonical)", err, ErrContactPhoneConflict)
	var phoneConflict *ContactPhoneConflictError
	if !errors.As(err, &phoneConflict) || phoneConflict.CanonicalE164 != "+819012345678" {
		t.Fatalf("CreateContact() conflict = %#v, want canonical detail", err)
	}

	second := mustCreateContact(t, repository, ContactInput{
		DisplayName: "Second",
		Phones: []ContactPhoneInput{
			{Label: "mobile", Number: "+82 10 1234 5678", Primary: true},
		},
	})
	_, err = repository.UpdateContact(ctx, second.ID, ContactInput{
		DisplayName: "Takeover",
		Revision:    second.Revision,
		Phones: []ContactPhoneInput{
			{
				ID:      first.Phones[0].ID,
				Label:   "mobile",
				Number:  "+82 10 9876 5432",
				Primary: true,
			},
		},
	})
	assertContactErrorIs(t, "UpdateContact(phone id takeover)", err, ErrContactPhoneConflict)
	phoneConflict = nil
	if !errors.As(err, &phoneConflict) || phoneConflict.ExistingContactID != first.ID {
		t.Fatalf("UpdateContact() takeover conflict = %#v, want owner %s", err, first.ID)
	}
	unchanged, err := repository.Contact(ctx, second.ID)
	if err != nil {
		t.Fatalf("Contact(second) error = %v", err)
	}
	if unchanged.DisplayName != second.DisplayName ||
		unchanged.Revision != second.Revision ||
		unchanged.Phones[0].ID != second.Phones[0].ID {
		t.Fatalf("second contact changed after rejected takeover: %#v", unchanged)
	}

	_, err = repository.UpdateContact(ctx, second.ID, ContactInput{
		DisplayName: "Stale",
		Revision:    second.Revision + 1,
		Phones: []ContactPhoneInput{
			{
				ID:      second.Phones[0].ID,
				Label:   "mobile",
				Number:  second.Phones[0].OriginalNumber,
				Primary: true,
			},
		},
	})
	assertContactErrorIs(t, "UpdateContact(stale)", err, ErrContactRevisionConflict)
	var revisionConflict *ContactRevisionConflictError
	if !errors.As(err, &revisionConflict) ||
		revisionConflict.ExpectedRevision != second.Revision+1 ||
		revisionConflict.ActualRevision != second.Revision {
		t.Fatalf("UpdateContact() revision conflict = %#v", err)
	}

	_, err = repository.UpdateContact(ctx, second.ID, ContactInput{
		DisplayName: "Unknown phone id",
		Revision:    second.Revision,
		Phones: []ContactPhoneInput{
			{ID: "phone-does-not-exist", Label: "mobile", Number: "+82 10 5555 5555", Primary: true},
		},
	})
	assertContactErrorIs(t, "UpdateContact(unknown phone id)", err, ErrContactValidation)
}

func TestContactNotFoundErrors(t *testing.T) {
	t.Parallel()

	repository, _ := newContactTestStore(t)
	ctx := context.Background()
	input := ContactInput{
		DisplayName: "Missing",
		Revision:    1,
		Phones: []ContactPhoneInput{
			{Label: "mobile", Number: "+81 90 0000 0000", Primary: true},
		},
	}

	_, err := repository.Contact(ctx, "missing-contact")
	assertContactErrorIs(t, "Contact(missing)", err, ErrContactNotFound)
	_, err = repository.UpdateContact(ctx, "missing-contact", input)
	assertContactErrorIs(t, "UpdateContact(missing)", err, ErrContactNotFound)
	err = repository.DeleteContact(ctx, "missing-contact", 1)
	assertContactErrorIs(t, "DeleteContact(missing)", err, ErrContactNotFound)
}

func TestContactValidation(t *testing.T) {
	t.Parallel()

	repository, _ := newContactTestStore(t)
	valid := ContactInput{
		DisplayName: "Valid",
		Phones: []ContactPhoneInput{
			{Label: "mobile", Number: "+81 90 1234 5678", Primary: true},
		},
	}

	tests := []struct {
		name   string
		mutate func(*ContactInput)
	}{
		{name: "create revision", mutate: func(input *ContactInput) { input.Revision = 1 }},
		{name: "display name required", mutate: func(input *ContactInput) { input.DisplayName = "  " }},
		{name: "display name too long", mutate: func(input *ContactInput) {
			input.DisplayName = strings.Repeat("名", MaxContactDisplayNameLength+1)
		}},
		{name: "notes too long", mutate: func(input *ContactInput) {
			input.Notes = strings.Repeat("n", MaxContactNotesLength+1)
		}},
		{name: "phones required", mutate: func(input *ContactInput) { input.Phones = nil }},
		{name: "too many phones", mutate: func(input *ContactInput) {
			input.Phones = make([]ContactPhoneInput, MaxContactPhones+1)
		}},
		{name: "primary required", mutate: func(input *ContactInput) { input.Phones[0].Primary = false }},
		{name: "exactly one primary", mutate: func(input *ContactInput) {
			input.Phones = append(input.Phones, ContactPhoneInput{
				Label: "work", Number: "+1 212 555 0198", Primary: true,
			})
		}},
		{name: "label required", mutate: func(input *ContactInput) { input.Phones[0].Label = " " }},
		{name: "label too long", mutate: func(input *ContactInput) {
			input.Phones[0].Label = strings.Repeat("l", MaxContactPhoneLabelLength+1)
		}},
		{name: "number too long", mutate: func(input *ContactInput) {
			input.Phones[0].Number = "+" + strings.Repeat("-", MaxContactPhoneNumberLength)
		}},
		{name: "plus required", mutate: func(input *ContactInput) { input.Phones[0].Number = "819012345678" }},
		{name: "invalid character", mutate: func(input *ContactInput) { input.Phones[0].Number = "+81.90.1234.5678" }},
		{name: "too few digits", mutate: func(input *ContactInput) { input.Phones[0].Number = "+1234567" }},
		{name: "too many digits", mutate: func(input *ContactInput) { input.Phones[0].Number = "+1234567890123456" }},
		{name: "invalid country code", mutate: func(input *ContactInput) { input.Phones[0].Number = "+01234567" }},
		{name: "phone id not allowed", mutate: func(input *ContactInput) { input.Phones[0].ID = "caller-id" }},
		{name: "duplicate canonical", mutate: func(input *ContactInput) {
			input.Phones = append(input.Phones, ContactPhoneInput{
				Label: "same", Number: "+8190-1234-5678",
			})
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := cloneContactInput(valid)
			test.mutate(&input)
			_, err := repository.CreateContact(context.Background(), input)
			assertContactErrorIs(t, "CreateContact()", err, ErrContactValidation)
		})
	}

	_, err := repository.UpdateContact(context.Background(), "contact", ContactInput{
		DisplayName: valid.DisplayName,
		Phones:      valid.Phones,
	})
	assertContactErrorIs(t, "UpdateContact(revision zero)", err, ErrContactValidation)
	err = repository.DeleteContact(context.Background(), "contact", 0)
	assertContactErrorIs(t, "DeleteContact(revision zero)", err, ErrContactValidation)
	_, err = repository.Contact(context.Background(), " ")
	assertContactErrorIs(t, "Contact(blank id)", err, ErrContactValidation)
}

func TestContactWritesAreAtomic(t *testing.T) {
	t.Parallel()

	repository, database := newContactTestStore(t)
	ctx := context.Background()
	original := mustCreateContact(t, repository, ContactInput{
		DisplayName: "Original",
		Notes:       "unchanged",
		Phones: []ContactPhoneInput{
			{Label: "mobile", Number: "+49 30 1234 5678", Primary: true},
		},
	})

	_, err := database.ExecContext(ctx, `
		CREATE TRIGGER reject_exploding_phone
		BEFORE INSERT ON contact_phones
		WHEN NEW.label = 'explode'
		BEGIN
			SELECT RAISE(ABORT, 'forced contact phone failure');
		END
	`)
	if err != nil {
		t.Fatalf("create phone failure trigger: %v", err)
	}

	_, err = repository.CreateContact(ctx, ContactInput{
		DisplayName: "Rolled back create",
		Phones: []ContactPhoneInput{
			{Label: "explode", Number: "+33 1 2345 6789", Primary: true},
		},
	})
	if err == nil {
		t.Fatal("CreateContact() error = nil, want forced insert failure")
	}
	assertContactCount(t, database, "Rolled back create", 0)

	_, err = repository.UpdateContact(ctx, original.ID, ContactInput{
		DisplayName: "Rolled back update",
		Notes:       "changed",
		Revision:    original.Revision,
		Phones: []ContactPhoneInput{
			{
				ID:      original.Phones[0].ID,
				Label:   "explode",
				Number:  "+49 30 9999 9999",
				Primary: true,
			},
		},
	})
	if err == nil {
		t.Fatal("UpdateContact() error = nil, want forced insert failure")
	}
	afterUpdate, err := repository.Contact(ctx, original.ID)
	if err != nil {
		t.Fatalf("Contact() after failed update error = %v", err)
	}
	if afterUpdate.DisplayName != original.DisplayName ||
		afterUpdate.Notes != original.Notes ||
		afterUpdate.Revision != original.Revision ||
		len(afterUpdate.Phones) != 1 ||
		afterUpdate.Phones[0].ID != original.Phones[0].ID ||
		afterUpdate.Phones[0].CanonicalE164 != original.Phones[0].CanonicalE164 {
		t.Fatalf("failed update was not rolled back: %#v", afterUpdate)
	}

	_, err = database.ExecContext(ctx, `
		CREATE TRIGGER reject_contact_delete
		BEFORE DELETE ON contacts
		BEGIN
			SELECT RAISE(ABORT, 'forced contact delete failure');
		END
	`)
	if err != nil {
		t.Fatalf("create delete failure trigger: %v", err)
	}
	if err := repository.DeleteContact(ctx, original.ID, original.Revision); err == nil {
		t.Fatal("DeleteContact() error = nil, want forced delete failure")
	}
	afterDelete, err := repository.Contact(ctx, original.ID)
	if err != nil {
		t.Fatalf("Contact() after failed delete error = %v", err)
	}
	if len(afterDelete.Phones) != 1 || afterDelete.Phones[0].ID != original.Phones[0].ID {
		t.Fatalf("failed delete did not restore contact phones: %#v", afterDelete)
	}
}

func newContactTestStore(t *testing.T) (*Store, *sql.DB) {
	t.Helper()

	directory := t.TempDir()
	database, _, err := platformdb.Open(context.Background(), platformdb.Config{
		TargetPath: filepath.Join(directory, "modemdeck.db"),
		LegacyPath: filepath.Join(directory, "vohive.db"),
	})
	if err != nil {
		t.Fatalf("open contact test database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	repository, err := New(database)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return repository, database
}

func mustCreateContact(t *testing.T, repository *Store, input ContactInput) Contact {
	t.Helper()

	contact, err := repository.CreateContact(context.Background(), input)
	if err != nil {
		t.Fatalf("CreateContact() error = %v", err)
	}
	return contact
}

func contactPhoneByCanonical(t *testing.T, contact Contact, canonical string) ContactPhone {
	t.Helper()

	for _, phone := range contact.Phones {
		if phone.CanonicalE164 == canonical {
			return phone
		}
	}
	t.Fatalf("contact %s has no phone %s: %#v", contact.ID, canonical, contact.Phones)
	return ContactPhone{}
}

func assertContactErrorIs(t *testing.T, operation string, err, target error) {
	t.Helper()

	if !errors.Is(err, target) {
		t.Fatalf("%s error = %v, want errors.Is(_, %v)", operation, err, target)
	}
}

func cloneContactInput(input ContactInput) ContactInput {
	input.Phones = append([]ContactPhoneInput(nil), input.Phones...)
	return input
}

func assertContactCount(t *testing.T, database *sql.DB, displayName string, want int) {
	t.Helper()

	var count int
	if err := database.QueryRow(
		"SELECT COUNT(*) FROM contacts WHERE display_name = ?",
		displayName,
	).Scan(&count); err != nil {
		t.Fatalf("count contacts named %q: %v", displayName, err)
	}
	if count != want {
		t.Fatalf("contacts named %q = %d, want %d", displayName, count, want)
	}
}
