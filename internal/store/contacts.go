package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

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
