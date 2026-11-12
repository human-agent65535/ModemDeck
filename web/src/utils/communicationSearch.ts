/** A search box value prepared once per filter pass. */
export type CommunicationQuery = {
  /** Trimmed, locale-lowercased text; empty means no search. */
  text: string
  /** Digits of the text, matched against phone numbers with formatting ignored. */
  digits: string
}

export function communicationQuery(value: string): CommunicationQuery {
  const text = value.trim().toLocaleLowerCase()
  return { text, digits: text.replace(/\D/g, '') }
}

/**
 * True when any text field contains the query, or any number contains its digits.
 * Only the fields the record carries are searched; callers decide what an empty
 * query means, since lists show everything and suggestions show nothing.
 */
export function matchesCommunicationQuery(
  query: CommunicationQuery,
  record: {
    text?: ReadonlyArray<string | undefined>
    numbers?: ReadonlyArray<string>
  }
): boolean {
  if (record.text?.some(value => (value || '').toLocaleLowerCase().includes(query.text))) {
    return true
  }
  return (
    query.digits.length > 0 &&
    (record.numbers?.some(number => number.replace(/\D/g, '').includes(query.digits)) ?? false)
  )
}
