import type { Contact, ContactInput, ContactPhone } from '../api/types'

// The API replaces a contact as a whole, so every field the server keeps must be
// sent back. One mapping keeps a partial update from silently dropping a field.
export function contactPhoneInputs(phones: ContactPhone[]): ContactInput['phones'] {
  return phones.map(phone => ({
    id: phone.id,
    label: phone.label,
    number: phone.number,
    region: phone.region,
    primary: phone.primary
  }))
}

export function contactInputFromContact(
  contact: Contact,
  overrides: Partial<ContactInput> = {}
): ContactInput {
  return {
    display_name: contact.display_name,
    avatar: contact.avatar,
    favorite: contact.favorite,
    notes: contact.notes,
    preferred_line_id: contact.preferred_line_id,
    revision: contact.revision,
    phones: contactPhoneInputs(contact.phones),
    ...overrides
  }
}
