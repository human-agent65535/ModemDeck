import type { ProxyMode } from '../api/types'
import { utf8ByteLength } from './utf8'

const MAX_USERNAME_BYTES = 128
const MAX_PASSWORD_BYTES = 512
const MAX_SOCKS5_PASSWORD_BYTES = 255

type Translator = (key: string) => string

export function proxyCredentialError(
  mode: ProxyMode,
  username: string,
  password: string,
  hasExistingPassword = false,
  translator?: Translator
): string {
  const message = (key: string, fallback: string) => translator?.(key) || fallback
  const normalizedUsername = username.trim()
  if (utf8ByteLength(normalizedUsername) > MAX_USERNAME_BYTES) {
    return message('proxy.usernameTooLong', 'Username cannot exceed 128 UTF-8 bytes')
  }
  if (mode === 'http' && normalizedUsername.includes(':')) {
    return message('proxy.httpUsernameColon', 'An HTTP Basic username cannot contain a colon')
  }
  if (utf8ByteLength(password) > MAX_PASSWORD_BYTES) {
    return message('proxy.passwordTooLong', 'Password cannot exceed 512 UTF-8 bytes')
  }
  if (
    mode === 'socks5' &&
    utf8ByteLength(password) > MAX_SOCKS5_PASSWORD_BYTES
  ) {
    return message('proxy.socksPasswordTooLong', 'A SOCKS5 password cannot exceed 255 UTF-8 bytes')
  }
  if (password && !normalizedUsername) {
    return message('proxy.usernameRequired', 'Enter a username before entering a password')
  }
  if (normalizedUsername && !password && !hasExistingPassword) {
    return message('proxy.passwordRequired', 'Enter a password')
  }
  return ''
}
