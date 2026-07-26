import type { ProxyMode } from '../api/types'

const MAX_USERNAME_BYTES = 128
const MAX_PASSWORD_BYTES = 512
const MAX_SOCKS5_PASSWORD_BYTES = 255

type Translator = (key: string) => string

export function utf8ByteLength(value: string): number {
  return new TextEncoder().encode(value).byteLength
}

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
    return message('proxy.usernameTooLong', '用户名不能超过 128 个 UTF-8 字节')
  }
  if (mode === 'http' && normalizedUsername.includes(':')) {
    return message('proxy.httpUsernameColon', 'HTTP Basic 用户名不能包含冒号')
  }
  if (utf8ByteLength(password) > MAX_PASSWORD_BYTES) {
    return message('proxy.passwordTooLong', '密码不能超过 512 个 UTF-8 字节')
  }
  if (
    mode === 'socks5' &&
    utf8ByteLength(password) > MAX_SOCKS5_PASSWORD_BYTES
  ) {
    return message('proxy.socksPasswordTooLong', 'SOCKS5 密码不能超过 255 个 UTF-8 字节')
  }
  if (password && !normalizedUsername) {
    return message('proxy.usernameRequired', '填写密码前需要用户名')
  }
  if (normalizedUsername && !password && !hasExistingPassword) {
    return message('proxy.passwordRequired', '请输入密码')
  }
  return ''
}
