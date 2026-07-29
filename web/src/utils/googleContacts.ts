import type { TransferContact } from './contactTransfer'
import { normalizeTransferContact } from './contactTransfer'

export const GOOGLE_CONTACTS_SCOPE =
  'https://www.googleapis.com/auth/contacts.readonly'

type GoogleTokenResponse = {
  access_token?: string
  error?: string
  error_description?: string
  scope?: string
}

type GoogleTokenClient = {
  requestAccessToken(options?: { prompt?: string }): void
}

type GoogleOAuth = {
  initTokenClient(options: {
    client_id: string
    scope: string
    callback(response: GoogleTokenResponse): void
    error_callback?(error: { type?: string }): void
  }): GoogleTokenClient
  revoke(token: string, callback?: () => void): void
}

type GoogleIdentityWindow = Window & {
  google?: {
    accounts?: {
      oauth2?: GoogleOAuth
    }
  }
}

type GooglePerson = {
  names?: Array<{ displayName?: string }>
  phoneNumbers?: Array<{
    canonicalForm?: string
    value?: string
    type?: string
    formattedType?: string
  }>
  biographies?: Array<{ value?: string }>
}

type GoogleConnectionsResponse = {
  connections?: GooglePerson[]
  nextPageToken?: string
}

let googleIdentityPromise: Promise<GoogleOAuth> | undefined

export function validGoogleClientID(value: string): boolean {
  return /^[0-9]+-[A-Za-z0-9_-]+\.apps\.googleusercontent\.com$/.test(value.trim())
}

function googleOAuth(): GoogleOAuth | undefined {
  return (window as GoogleIdentityWindow).google?.accounts?.oauth2
}

export function loadGoogleIdentityServices(): Promise<GoogleOAuth> {
  const loaded = googleOAuth()
  if (loaded) return Promise.resolve(loaded)
  if (googleIdentityPromise) return googleIdentityPromise

  const loading = new Promise<GoogleOAuth>((resolve, reject) => {
    const script = document.createElement('script')
    script.src = 'https://accounts.google.com/gsi/client'
    script.async = true
    script.onload = () => {
      const api = googleOAuth()
      if (api) resolve(api)
      else reject(new Error('Google Identity Services did not initialize'))
    }
    script.onerror = () => reject(new Error('Google Identity Services could not be loaded'))
    document.head.append(script)
  }).catch(error => {
    googleIdentityPromise = undefined
    throw error
  })
  googleIdentityPromise = loading
  return loading
}

export async function requestGoogleContactsToken(
  clientID: string,
  prompt = 'consent'
): Promise<string> {
  if (!validGoogleClientID(clientID)) throw new Error('Google OAuth client ID is invalid')
  const oauth = await loadGoogleIdentityServices()
  return new Promise((resolve, reject) => {
    const client = oauth.initTokenClient({
      client_id: clientID.trim(),
      scope: GOOGLE_CONTACTS_SCOPE,
      callback(response) {
        if (response.error || !response.access_token) {
          reject(
            new Error(
              response.error_description || response.error || 'Google authorization failed'
            )
          )
          return
        }
        const scopes = new Set((response.scope || '').split(/\s+/).filter(Boolean))
        if (!scopes.has(GOOGLE_CONTACTS_SCOPE)) {
          reject(new Error('Google Contacts permission was not granted'))
          return
        }
        resolve(response.access_token)
      },
      error_callback(error) {
        reject(new Error(error.type || 'Google authorization was cancelled'))
      }
    })
    client.requestAccessToken({ prompt })
  })
}

export async function revokeGoogleContactsToken(token: string): Promise<void> {
  if (!token) return
  const oauth = await loadGoogleIdentityServices()
  await new Promise<void>(resolve => oauth.revoke(token, resolve))
}

export function transferContactFromGooglePerson(
  person: GooglePerson,
  defaultRegion = ''
): TransferContact | undefined {
  return normalizeTransferContact(
    {
      display_name: person.names?.find(name => name.displayName?.trim())?.displayName || '',
      notes: person.biographies?.find(item => item.value?.trim())?.value,
      phones: (person.phoneNumbers || []).map(phone => ({
        label: phone.formattedType || phone.type || 'Phone',
        number: phone.canonicalForm || phone.value || ''
      }))
    },
    defaultRegion
  )
}

export async function fetchGoogleContacts(
  token: string,
  defaultRegion = '',
  request: typeof fetch = fetch
): Promise<TransferContact[]> {
  const contacts: TransferContact[] = []
  let pageToken = ''
  do {
    const url = new URL('https://people.googleapis.com/v1/people/me/connections')
    url.searchParams.set('pageSize', '1000')
    url.searchParams.set('personFields', 'names,phoneNumbers,biographies')
    if (pageToken) url.searchParams.set('pageToken', pageToken)
    const response = await request(url, {
      headers: {
        Accept: 'application/json',
        Authorization: `Bearer ${token}`
      }
    })
    if (!response.ok) {
      let message = `Google Contacts returned ${response.status}`
      try {
        const body = (await response.json()) as {
          error?: { message?: string }
        }
        message = body.error?.message || message
      } catch {
        // Keep the status-based message when Google did not return JSON.
      }
      throw new Error(message)
    }
    const body = (await response.json()) as GoogleConnectionsResponse
    for (const person of body.connections || []) {
      const contact = transferContactFromGooglePerson(person, defaultRegion)
      if (contact) contacts.push(contact)
    }
    pageToken = body.nextPageToken || ''
  } while (pageToken)
  return contacts
}
