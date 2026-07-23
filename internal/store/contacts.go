package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

var (
	ErrContactNotFound         = errors.New("contact not found")
	ErrContactRevisionConflict = errors.New("contact revision conflict")
	ErrContactValidation       = errors.New("contact validation failed")
	ErrContactPhoneConflict    = errors.New("contact phone conflict")
)

type ContactNotFoundError struct {
	ContactID string
}

func (e *ContactNotFoundError) Error() string {
	if e == nil || e.ContactID == "" {
		return ErrContactNotFound.Error()
	}
	return fmt.Sprintf("%s: %s", ErrContactNotFound, e.ContactID)
}

func (e *ContactNotFoundError) Unwrap() error {
	return ErrContactNotFound
}

type ContactRevisionConflictError struct {
	ContactID        string
	ExpectedRevision int64
	ActualRevision   int64
}

func (e *ContactRevisionConflictError) Error() string {
	if e == nil {
		return ErrContactRevisionConflict.Error()
	}
	return fmt.Sprintf(
		"%s: %s expected %d, actual %d",
		ErrContactRevisionConflict,
		e.ContactID,
		e.ExpectedRevision,
		e.ActualRevision,
	)
}

func (e *ContactRevisionConflictError) Unwrap() error {
	return ErrContactRevisionConflict
}

type ContactValidationError struct {
	Field string
	Code  string
}

func (e *ContactValidationError) Error() string {
	if e == nil {
		return ErrContactValidation.Error()
	}
	if e.Field == "" {
		return fmt.Sprintf("%s: %s", ErrContactValidation, e.Code)
	}
	return fmt.Sprintf("%s: %s (%s)", ErrContactValidation, e.Field, e.Code)
}

func (e *ContactValidationError) Unwrap() error {
	return ErrContactValidation
}

type ContactPhoneConflictError struct {
	PhoneID           string
	CanonicalE164     string
	ExistingContactID string
}

func (e *ContactPhoneConflictError) Error() string {
	if e == nil {
		return ErrContactPhoneConflict.Error()
	}
	phone := e.CanonicalE164
	if phone == "" {
		phone = e.PhoneID
	}
	if e.ExistingContactID == "" {
		return fmt.Sprintf("%s: %s", ErrContactPhoneConflict, phone)
	}
	return fmt.Sprintf("%s: %s belongs to %s", ErrContactPhoneConflict, phone, e.ExistingContactID)
}

func (e *ContactPhoneConflictError) Unwrap() error {
	return ErrContactPhoneConflict
}

type normalizedContactInput struct {
	displayName string
	notes       string
	revision    int64
	phones      []normalizedContactPhone
}

type normalizedContactPhone struct {
	id             string
	label          string
	originalNumber string
	canonicalE164  string
	primary        bool
}

type contactQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func (s *Store) Contact(ctx context.Context, id string) (Contact, error) {
	contactID, err := validateContactID(id)
	if err != nil {
		return Contact{}, err
	}
	return contactByID(ctx, s.database, contactID)
}

func (s *Store) CreateContact(ctx context.Context, input ContactInput) (Contact, error) {
	normalized, err := normalizeContactInput(input, true)
	if err != nil {
		return Contact{}, err
	}
	for index := range normalized.phones {
		normalized.phones[index].id, err = newContactIdentifier()
		if err != nil {
			return Contact{}, fmt.Errorf("generate contact phone id: %w", err)
		}
	}
	contactID, err := newContactIdentifier()
	if err != nil {
		return Contact{}, fmt.Errorf("generate contact id: %w", err)
	}

	transaction, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return Contact{}, fmt.Errorf("begin create contact: %w", err)
	}
	defer transaction.Rollback()

	if err := findContactPhoneConflict(ctx, transaction, normalized.phones, ""); err != nil {
		return Contact{}, err
	}
	if _, err := transaction.ExecContext(
		ctx,
		`INSERT INTO contacts (id, display_name, notes, revision, created_at, updated_at)
		 VALUES (?, ?, ?, 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`,
		contactID,
		normalized.displayName,
		normalized.notes,
	); err != nil {
		_ = transaction.Rollback()
		return Contact{}, s.classifyContactWriteError(ctx, fmt.Errorf("insert contact: %w", err), normalized.phones, "")
	}
	if err := insertContactPhones(ctx, transaction, contactID, normalized.phones); err != nil {
		_ = transaction.Rollback()
		return Contact{}, s.classifyContactWriteError(ctx, err, normalized.phones, "")
	}

	contact, err := contactByID(ctx, transaction, contactID)
	if err != nil {
		return Contact{}, err
	}
	if err := transaction.Commit(); err != nil {
		return Contact{}, fmt.Errorf("commit create contact: %w", err)
	}
	return contact, nil
}

func (s *Store) UpdateContact(ctx context.Context, id string, input ContactInput) (Contact, error) {
	contactID, err := validateContactID(id)
	if err != nil {
		return Contact{}, err
	}
	normalized, err := normalizeContactInput(input, false)
	if err != nil {
		return Contact{}, err
	}

	transaction, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return Contact{}, fmt.Errorf("begin update contact: %w", err)
	}
	defer transaction.Rollback()

	current, err := contactByID(ctx, transaction, contactID)
	if err != nil {
		return Contact{}, err
	}
	if current.Revision != normalized.revision {
		return Contact{}, &ContactRevisionConflictError{
			ContactID:        contactID,
			ExpectedRevision: normalized.revision,
			ActualRevision:   current.Revision,
		}
	}
	if err := resolveUpdatedPhoneIDs(ctx, transaction, current.Phones, normalized.phones); err != nil {
		return Contact{}, err
	}
	if err := findContactPhoneConflict(ctx, transaction, normalized.phones, contactID); err != nil {
		return Contact{}, err
	}

	result, err := transaction.ExecContext(
		ctx,
		`UPDATE contacts
		 SET display_name = ?, notes = ?, revision = revision + 1, updated_at = CURRENT_TIMESTAMP
		 WHERE id = ? AND revision = ?`,
		normalized.displayName,
		normalized.notes,
		contactID,
		normalized.revision,
	)
	if err != nil {
		return Contact{}, fmt.Errorf("update contact: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return Contact{}, fmt.Errorf("read updated contact count: %w", err)
	}
	if affected != 1 {
		return Contact{}, contactWriteMiss(ctx, transaction, contactID, normalized.revision)
	}
	if _, err := transaction.ExecContext(ctx, "DELETE FROM contact_phones WHERE contact_id = ?", contactID); err != nil {
		return Contact{}, fmt.Errorf("replace contact phones: %w", err)
	}
	if err := insertContactPhones(ctx, transaction, contactID, normalized.phones); err != nil {
		_ = transaction.Rollback()
		return Contact{}, s.classifyContactWriteError(ctx, err, normalized.phones, contactID)
	}

	contact, err := contactByID(ctx, transaction, contactID)
	if err != nil {
		return Contact{}, err
	}
	if err := transaction.Commit(); err != nil {
		return Contact{}, fmt.Errorf("commit update contact: %w", err)
	}
	return contact, nil
}

func (s *Store) DeleteContact(ctx context.Context, id string, revision int64) error {
	contactID, err := validateContactID(id)
	if err != nil {
		return err
	}
	if revision <= 0 {
		return contactValidation("revision", "must_be_positive")
	}

	transaction, err := s.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin delete contact: %w", err)
	}
	defer transaction.Rollback()

	actualRevision, err := contactRevision(ctx, transaction, contactID)
	if err != nil {
		return err
	}
	if actualRevision != revision {
		return &ContactRevisionConflictError{
			ContactID:        contactID,
			ExpectedRevision: revision,
			ActualRevision:   actualRevision,
		}
	}
	if _, err := transaction.ExecContext(ctx, "DELETE FROM contact_phones WHERE contact_id = ?", contactID); err != nil {
		return fmt.Errorf("delete contact phones: %w", err)
	}
	result, err := transaction.ExecContext(
		ctx,
		"DELETE FROM contacts WHERE id = ? AND revision = ?",
		contactID,
		revision,
	)
	if err != nil {
		return fmt.Errorf("delete contact: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read deleted contact count: %w", err)
	}
	if affected != 1 {
		return contactWriteMiss(ctx, transaction, contactID, revision)
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit delete contact: %w", err)
	}
	return nil
}

func (s *Store) Contacts(ctx context.Context, query ContactQuery) ([]Contact, error) {
	limit := boundedLimit(query.Limit)
	statement := `SELECT id, display_name, notes, revision, created_at, updated_at
		FROM contacts`
	arguments := []any{}
	if strings.TrimSpace(query.Search) != "" {
		pattern := searchPattern(query.Search)
		statement += ` WHERE
			LOWER(COALESCE(display_name, '')) LIKE ? ESCAPE '\' OR
			LOWER(COALESCE(notes, '')) LIKE ? ESCAPE '\' OR
			EXISTS (
				SELECT 1 FROM contact_phones
				WHERE contact_phones.contact_id = contacts.id
				AND (
					LOWER(COALESCE(contact_phones.label, '')) LIKE ? ESCAPE '\' OR
					LOWER(COALESCE(contact_phones.original_number, '')) LIKE ? ESCAPE '\' OR
					LOWER(COALESCE(contact_phones.canonical_e164, '')) LIKE ? ESCAPE '\'
				)
			)`
		arguments = append(arguments, pattern, pattern, pattern, pattern, pattern)
	}
	statement += ` ORDER BY LOWER(COALESCE(display_name, '')) ASC, id ASC LIMIT ?`
	arguments = append(arguments, limit)

	rows, err := s.database.QueryContext(ctx, statement, arguments...)
	if err != nil {
		return nil, fmt.Errorf("query contacts: %w", err)
	}
	defer rows.Close()

	contacts := make([]Contact, 0)
	for rows.Next() {
		var (
			contact              Contact
			displayName, notes   sql.NullString
			revision             sql.NullInt64
			createdAt, updatedAt sql.NullString
		)
		if err := rows.Scan(&contact.ID, &displayName, &notes, &revision, &createdAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("scan contact: %w", err)
		}
		contact.DisplayName = stringValue(displayName)
		contact.Notes = stringValue(notes)
		contact.Revision = intValue(revision)
		contact.CreatedAt = stringValue(createdAt)
		contact.UpdatedAt = stringValue(updatedAt)
		contact.Phones = []ContactPhone{}
		contacts = append(contacts, contact)
	}
	if err := rows.Err(); err != nil {
		return nil, rowsError("read contacts", err)
	}
	if len(contacts) == 0 {
		return contacts, nil
	}
	if err := s.loadContactPhones(ctx, contacts); err != nil {
		return nil, err
	}
	return contacts, nil
}

func (s *Store) loadContactPhones(ctx context.Context, contacts []Contact) error {
	arguments := make([]any, 0, len(contacts))
	contactIndex := make(map[string]int, len(contacts))
	for index := range contacts {
		arguments = append(arguments, contacts[index].ID)
		contactIndex[contacts[index].ID] = index
	}
	statement := `SELECT id, contact_id, label, original_number, canonical_e164, is_primary
		FROM contact_phones
		WHERE contact_id IN (` + placeholders(len(arguments)) + `)
		ORDER BY contact_id ASC, is_primary DESC, id ASC`
	rows, err := s.database.QueryContext(ctx, statement, arguments...)
	if err != nil {
		return fmt.Errorf("query contact phones: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			phone                                 ContactPhone
			contactID, label, original, canonical sql.NullString
			primary                               sql.NullInt64
		)
		if err := rows.Scan(&phone.ID, &contactID, &label, &original, &canonical, &primary); err != nil {
			return fmt.Errorf("scan contact phone: %w", err)
		}
		phone.Label = stringValue(label)
		phone.OriginalNumber = stringValue(original)
		phone.CanonicalE164 = stringValue(canonical)
		phone.Primary = boolValue(primary)
		if index, exists := contactIndex[stringValue(contactID)]; exists {
			contacts[index].Phones = append(contacts[index].Phones, phone)
		}
	}
	return rowsError("read contact phones", rows.Err())
}

func validateContactID(id string) (string, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return "", contactValidation("id", "required")
	}
	return id, nil
}

func normalizeContactInput(input ContactInput, creating bool) (normalizedContactInput, error) {
	if creating && input.Revision != 0 {
		return normalizedContactInput{}, contactValidation("revision", "not_allowed")
	}
	if !creating && input.Revision <= 0 {
		return normalizedContactInput{}, contactValidation("revision", "must_be_positive")
	}

	displayName := strings.TrimSpace(input.DisplayName)
	if displayName == "" {
		return normalizedContactInput{}, contactValidation("display_name", "required")
	}
	if len([]rune(displayName)) > MaxContactDisplayNameLength {
		return normalizedContactInput{}, contactValidation("display_name", "too_long")
	}
	notes := strings.TrimSpace(input.Notes)
	if len([]rune(notes)) > MaxContactNotesLength {
		return normalizedContactInput{}, contactValidation("notes", "too_long")
	}
	if len(input.Phones) == 0 {
		return normalizedContactInput{}, contactValidation("phones", "required")
	}
	if len(input.Phones) > MaxContactPhones {
		return normalizedContactInput{}, contactValidation("phones", "too_many")
	}

	normalized := normalizedContactInput{
		displayName: displayName,
		notes:       notes,
		revision:    input.Revision,
		phones:      make([]normalizedContactPhone, 0, len(input.Phones)),
	}
	canonicalSeen := make(map[string]struct{}, len(input.Phones))
	requestedIDSeen := make(map[string]struct{}, len(input.Phones))
	primaryCount := 0
	for index, phone := range input.Phones {
		field := fmt.Sprintf("phones[%d]", index)
		id := strings.TrimSpace(phone.ID)
		if creating && id != "" {
			return normalizedContactInput{}, contactValidation(field+".id", "not_allowed")
		}
		if id != "" {
			if _, duplicate := requestedIDSeen[id]; duplicate {
				return normalizedContactInput{}, contactValidation(field+".id", "duplicate")
			}
			requestedIDSeen[id] = struct{}{}
		}
		label := strings.TrimSpace(phone.Label)
		if label == "" {
			return normalizedContactInput{}, contactValidation(field+".label", "required")
		}
		if len([]rune(label)) > MaxContactPhoneLabelLength {
			return normalizedContactInput{}, contactValidation(field+".label", "too_long")
		}
		original, canonical, err := normalizeContactNumber(phone.Number, field+".number")
		if err != nil {
			return normalizedContactInput{}, err
		}
		if _, duplicate := canonicalSeen[canonical]; duplicate {
			return normalizedContactInput{}, contactValidation(field+".number", "duplicate")
		}
		canonicalSeen[canonical] = struct{}{}
		if phone.Primary {
			primaryCount++
		}
		normalized.phones = append(normalized.phones, normalizedContactPhone{
			id:             id,
			label:          label,
			originalNumber: original,
			canonicalE164:  canonical,
			primary:        phone.Primary,
		})
	}
	if primaryCount != 1 {
		return normalizedContactInput{}, contactValidation("phones", "exactly_one_primary_required")
	}
	return normalized, nil
}

func normalizeContactNumber(number, field string) (string, string, error) {
	number = strings.TrimSpace(number)
	if number == "" {
		return "", "", contactValidation(field, "required")
	}
	if len([]rune(number)) > MaxContactPhoneNumberLength {
		return "", "", contactValidation(field, "too_long")
	}
	if number[0] != '+' {
		return "", "", contactValidation(field, "international_prefix_required")
	}

	var digits strings.Builder
	digits.Grow(len(number) - 1)
	for _, character := range number[1:] {
		switch {
		case character >= '0' && character <= '9':
			digits.WriteRune(character)
		case character == ' ', character == '(', character == ')', character == '-':
		default:
			return "", "", contactValidation(field, "invalid_character")
		}
	}
	canonicalDigits := digits.String()
	if len(canonicalDigits) < 8 || len(canonicalDigits) > 15 {
		return "", "", contactValidation(field, "invalid_length")
	}
	if canonicalDigits[0] == '0' {
		return "", "", contactValidation(field, "invalid_country_code")
	}
	return number, "+" + canonicalDigits, nil
}

func resolveUpdatedPhoneIDs(
	ctx context.Context,
	queryer contactQueryer,
	current []ContactPhone,
	phones []normalizedContactPhone,
) error {
	currentByID := make(map[string]ContactPhone, len(current))
	currentByCanonical := make(map[string]ContactPhone, len(current))
	for _, phone := range current {
		currentByID[phone.ID] = phone
		currentByCanonical[phone.CanonicalE164] = phone
	}

	resolvedIDs := make(map[string]struct{}, len(phones))
	for index := range phones {
		phone := &phones[index]
		switch {
		case phone.id != "":
			if _, owned := currentByID[phone.id]; !owned {
				owner, exists, err := contactPhoneOwner(ctx, queryer, phone.id)
				if err != nil {
					return err
				}
				if exists {
					return &ContactPhoneConflictError{
						PhoneID:           phone.id,
						CanonicalE164:     phone.canonicalE164,
						ExistingContactID: owner,
					}
				}
				return contactValidation(fmt.Sprintf("phones[%d].id", index), "unknown")
			}
		case currentByCanonical[phone.canonicalE164].ID != "":
			phone.id = currentByCanonical[phone.canonicalE164].ID
		default:
			id, err := newContactIdentifier()
			if err != nil {
				return fmt.Errorf("generate contact phone id: %w", err)
			}
			phone.id = id
		}
		if _, duplicate := resolvedIDs[phone.id]; duplicate {
			return contactValidation(fmt.Sprintf("phones[%d].id", index), "duplicate")
		}
		resolvedIDs[phone.id] = struct{}{}
	}
	return nil
}

func contactPhoneOwner(ctx context.Context, queryer contactQueryer, phoneID string) (string, bool, error) {
	var owner sql.NullString
	err := queryer.QueryRowContext(
		ctx,
		"SELECT contact_id FROM contact_phones WHERE id = ?",
		phoneID,
	).Scan(&owner)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("query contact phone owner: %w", err)
	}
	return stringValue(owner), true, nil
}

func findContactPhoneConflict(
	ctx context.Context,
	queryer contactQueryer,
	phones []normalizedContactPhone,
	excludeContactID string,
) error {
	if len(phones) == 0 {
		return nil
	}

	canonicalArguments := make([]any, 0, len(phones)+1)
	for _, phone := range phones {
		canonicalArguments = append(canonicalArguments, phone.canonicalE164)
	}
	statement := `SELECT canonical_e164, contact_id
		FROM contact_phones
		WHERE canonical_e164 IN (` + placeholders(len(phones)) + `)`
	if excludeContactID != "" {
		statement += " AND contact_id <> ?"
		canonicalArguments = append(canonicalArguments, excludeContactID)
	}
	rows, err := queryer.QueryContext(ctx, statement, canonicalArguments...)
	if err != nil {
		return fmt.Errorf("query canonical contact phone conflicts: %w", err)
	}
	canonicalOwners := make(map[string]string)
	for rows.Next() {
		var canonical, owner sql.NullString
		if err := rows.Scan(&canonical, &owner); err != nil {
			rows.Close()
			return fmt.Errorf("scan canonical contact phone conflict: %w", err)
		}
		canonicalOwners[stringValue(canonical)] = stringValue(owner)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close canonical contact phone conflicts: %w", err)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read canonical contact phone conflicts: %w", err)
	}
	for _, phone := range phones {
		if owner, conflict := canonicalOwners[phone.canonicalE164]; conflict {
			return &ContactPhoneConflictError{
				PhoneID:           phone.id,
				CanonicalE164:     phone.canonicalE164,
				ExistingContactID: owner,
			}
		}
	}

	idArguments := make([]any, 0, len(phones)+1)
	for _, phone := range phones {
		idArguments = append(idArguments, phone.id)
	}
	statement = `SELECT id, contact_id
		FROM contact_phones
		WHERE id IN (` + placeholders(len(phones)) + `)`
	if excludeContactID != "" {
		statement += " AND contact_id <> ?"
		idArguments = append(idArguments, excludeContactID)
	}
	rows, err = queryer.QueryContext(ctx, statement, idArguments...)
	if err != nil {
		return fmt.Errorf("query contact phone id conflicts: %w", err)
	}
	idOwners := make(map[string]string)
	for rows.Next() {
		var phoneID, owner sql.NullString
		if err := rows.Scan(&phoneID, &owner); err != nil {
			rows.Close()
			return fmt.Errorf("scan contact phone id conflict: %w", err)
		}
		idOwners[stringValue(phoneID)] = stringValue(owner)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close contact phone id conflicts: %w", err)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read contact phone id conflicts: %w", err)
	}
	for _, phone := range phones {
		if owner, conflict := idOwners[phone.id]; conflict {
			return &ContactPhoneConflictError{
				PhoneID:           phone.id,
				CanonicalE164:     phone.canonicalE164,
				ExistingContactID: owner,
			}
		}
	}
	return nil
}

func insertContactPhones(
	ctx context.Context,
	transaction *sql.Tx,
	contactID string,
	phones []normalizedContactPhone,
) error {
	for _, phone := range phones {
		if _, err := transaction.ExecContext(
			ctx,
			`INSERT INTO contact_phones
				(id, contact_id, label, original_number, canonical_e164, is_primary)
			 VALUES (?, ?, ?, ?, ?, ?)`,
			phone.id,
			contactID,
			phone.label,
			phone.originalNumber,
			phone.canonicalE164,
			phone.primary,
		); err != nil {
			return fmt.Errorf("insert contact phone: %w", err)
		}
	}
	return nil
}

func contactByID(ctx context.Context, queryer contactQueryer, contactID string) (Contact, error) {
	var (
		contact              Contact
		displayName, notes   sql.NullString
		revision             sql.NullInt64
		createdAt, updatedAt sql.NullString
	)
	err := queryer.QueryRowContext(
		ctx,
		`SELECT id, display_name, notes, revision, created_at, updated_at
		 FROM contacts
		 WHERE id = ?`,
		contactID,
	).Scan(&contact.ID, &displayName, &notes, &revision, &createdAt, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Contact{}, &ContactNotFoundError{ContactID: contactID}
	}
	if err != nil {
		return Contact{}, fmt.Errorf("query contact: %w", err)
	}
	contact.DisplayName = stringValue(displayName)
	contact.Notes = stringValue(notes)
	contact.Revision = intValue(revision)
	contact.CreatedAt = stringValue(createdAt)
	contact.UpdatedAt = stringValue(updatedAt)
	contact.Phones = []ContactPhone{}

	rows, err := queryer.QueryContext(
		ctx,
		`SELECT id, label, original_number, canonical_e164, is_primary
		 FROM contact_phones
		 WHERE contact_id = ?
		 ORDER BY is_primary DESC, id ASC`,
		contactID,
	)
	if err != nil {
		return Contact{}, fmt.Errorf("query contact phones: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			phone                      ContactPhone
			label, original, canonical sql.NullString
			primary                    sql.NullInt64
		)
		if err := rows.Scan(&phone.ID, &label, &original, &canonical, &primary); err != nil {
			return Contact{}, fmt.Errorf("scan contact phone: %w", err)
		}
		phone.Label = stringValue(label)
		phone.OriginalNumber = stringValue(original)
		phone.CanonicalE164 = stringValue(canonical)
		phone.Primary = boolValue(primary)
		contact.Phones = append(contact.Phones, phone)
	}
	if err := rows.Err(); err != nil {
		return Contact{}, fmt.Errorf("read contact phones: %w", err)
	}
	return contact, nil
}

func contactRevision(ctx context.Context, queryer contactQueryer, contactID string) (int64, error) {
	var revision sql.NullInt64
	err := queryer.QueryRowContext(
		ctx,
		"SELECT revision FROM contacts WHERE id = ?",
		contactID,
	).Scan(&revision)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, &ContactNotFoundError{ContactID: contactID}
	}
	if err != nil {
		return 0, fmt.Errorf("query contact revision: %w", err)
	}
	return intValue(revision), nil
}

func contactWriteMiss(
	ctx context.Context,
	queryer contactQueryer,
	contactID string,
	expectedRevision int64,
) error {
	actualRevision, err := contactRevision(ctx, queryer, contactID)
	if err != nil {
		return err
	}
	return &ContactRevisionConflictError{
		ContactID:        contactID,
		ExpectedRevision: expectedRevision,
		ActualRevision:   actualRevision,
	}
}

func (s *Store) classifyContactWriteError(
	ctx context.Context,
	err error,
	phones []normalizedContactPhone,
	excludeContactID string,
) error {
	if err == nil ||
		errors.Is(err, ErrContactNotFound) ||
		errors.Is(err, ErrContactRevisionConflict) ||
		errors.Is(err, ErrContactValidation) ||
		errors.Is(err, ErrContactPhoneConflict) {
		return err
	}
	if conflict := findContactPhoneConflict(ctx, s.database, phones, excludeContactID); conflict != nil {
		if errors.Is(conflict, ErrContactPhoneConflict) {
			return conflict
		}
	}
	return err
}

func contactValidation(field, code string) error {
	return &ContactValidationError{Field: field, Code: code}
}

func newContactIdentifier() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	return fmt.Sprintf(
		"%x-%x-%x-%x-%x",
		value[0:4],
		value[4:6],
		value[6:8],
		value[8:10],
		value[10:16],
	), nil
}
