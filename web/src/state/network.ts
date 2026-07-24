import { reactive } from 'vue'
import { gateway } from '../api/client'
import type {
  NetworkStatus,
  ProxyApplyStatus,
  ProxyInstance,
  ProxyMode,
  ResourceStatus
} from '../api/types'
import { ApiError } from '../api/types'
import { isIPAddress, isLoopbackAddress } from '../utils/ipAddress'
import { proxyCredentialError } from '../utils/proxyCredentials'

export type ProxyDraft = {
  line_id: string
  mode: ProxyMode
  listen_address: string
  listen_port: number
  username: string
  password: string
}

export const networkState = reactive<{
  status: ResourceStatus
  snapshot: NetworkStatus | null
  proxies: ProxyInstance[]
  error: string
  busyID: string
  notice: string
}>({
  status: 'idle',
  snapshot: null,
  proxies: [],
  error: '',
  busyID: '',
  notice: ''
})

let loadGeneration = 0
let loadRequest: Promise<void> | undefined

function failureMessage(error: unknown, fallback: string): string {
  if (error instanceof ApiError && error.status === 403) return '无权管理网络与代理'
  return error instanceof Error ? error.message : fallback
}

function proxyName(draft: ProxyDraft, lineLabel: string): string {
  const protocol = draft.mode === 'http' ? 'HTTP' : 'SOCKS5'
  return `${lineLabel.trim() || '线路'} ${protocol} ${draft.listen_port}`.slice(0, 100)
}

function validateDraft(draft: ProxyDraft, existing?: ProxyInstance): string {
  const username = draft.username.trim()
  const password = draft.password
  if (!draft.line_id.trim()) return '请选择线路'
  if (draft.mode !== 'http' && draft.mode !== 'socks5') return '请选择代理协议'
  if (!draft.listen_address.trim()) return '请输入监听地址'
  if (!isIPAddress(draft.listen_address)) return '监听地址必须是 IPv4 或 IPv6 地址'
  if (
    !Number.isSafeInteger(draft.listen_port) ||
    draft.listen_port < 1024 ||
    draft.listen_port > 65535
  ) {
    return '端口范围为 1024–65535'
  }
  const credentialError = proxyCredentialError(
    draft.mode,
    username,
    password,
    existing?.has_password
  )
  if (credentialError) return credentialError
  if (!isLoopbackAddress(draft.listen_address) && !username) {
    return '非本机监听必须设置用户名和密码'
  }
  return ''
}

function applyNotice(applied: boolean, status: ProxyApplyStatus): string {
  if (applied) return ''
  switch (status) {
    case 'agent_unavailable':
      return '配置已保存，主机服务当前不可用'
    case 'agent_rejected':
      return '配置已保存，但主机服务拒绝了配置'
    case 'runtime_unavailable':
      return '配置已保存，但网络运行时暂不可用'
    default:
      return '配置已保存，等待主机服务同步'
  }
}

export function loadNetwork(force = false, silent = false): Promise<void> {
  if (!force && networkState.status === 'ready') return Promise.resolve()
  if (!force && loadRequest) return loadRequest

  const token = ++loadGeneration
  if (!silent || !networkState.snapshot) networkState.status = 'loading'
  networkState.error = ''
  let request: Promise<void>
  request = Promise.all([gateway.getNetworkStatus(), gateway.listProxies()])
    .then(([snapshot, proxies]) => {
      if (token !== loadGeneration) return
      networkState.snapshot = snapshot
      networkState.proxies = proxies
      networkState.status = 'ready'
      networkState.notice = ''
    })
    .catch(error => {
      if (token !== loadGeneration) return
      networkState.status =
        error instanceof ApiError && error.status === 403 ? 'forbidden' : 'error'
      networkState.error = failureMessage(error, '无法载入流量数据')
    })
    .finally(() => {
      if (loadRequest === request) loadRequest = undefined
    })
  loadRequest = request
  return request
}

async function handleMutationFailure(error: unknown, fallback: string): Promise<void> {
  const message = failureMessage(error, fallback)
  if (error instanceof ApiError && error.status === 409) {
    await loadNetwork(true, true)
    networkState.error = '代理已由其他操作更新，请确认后重试'
    return
  }
  networkState.error = message
}

export async function saveProxy(
  draft: ProxyDraft,
  lineLabel: string,
  existing?: ProxyInstance
): Promise<boolean> {
  if (networkState.busyID) return false
  const validationError = validateDraft(draft, existing)
  if (validationError) {
    networkState.error = validationError
    return false
  }

  const username = draft.username.trim()
  const password = draft.password
  const authEnabled = Boolean(username)
  const common = {
    name: proxyName(draft, lineLabel),
    line_id: draft.line_id.trim(),
    enabled: existing?.enabled ?? true,
    mode: draft.mode,
    listen_address: draft.listen_address.trim(),
    listen_port: draft.listen_port,
    auth_enabled: authEnabled,
    username: authEnabled ? username : ''
  }

  networkState.busyID = existing?.id || 'new'
  networkState.error = ''
  networkState.notice = ''
  try {
    const result = existing
      ? await gateway.updateProxy(existing.id, {
          ...common,
          revision: existing.revision,
          ...(password ? { password } : {})
        })
      : await gateway.createProxy({
          ...common,
          password: authEnabled ? password : ''
        })
    const index = networkState.proxies.findIndex(proxy => proxy.id === result.proxy.id)
    if (index >= 0) networkState.proxies[index] = result.proxy
    else networkState.proxies.push(result.proxy)
    await loadNetwork(true, true)
    networkState.notice = applyNotice(result.applied, result.status)
    return true
  } catch (error) {
    await handleMutationFailure(error, '无法保存代理')
    return false
  } finally {
    networkState.busyID = ''
  }
}

export async function setProxyEnabled(
  proxy: ProxyInstance,
  enabled: boolean
): Promise<boolean> {
  if (networkState.busyID) return false
  networkState.busyID = proxy.id
  networkState.error = ''
  networkState.notice = ''
  try {
    const result = await gateway.updateProxy(proxy.id, {
      revision: proxy.revision,
      name: proxy.name,
      line_id: proxy.line_id,
      enabled,
      mode: proxy.mode,
      listen_address: proxy.listen_address,
      listen_port: proxy.listen_port,
      auth_enabled: proxy.auth_enabled,
      username: proxy.username
    })
    const index = networkState.proxies.findIndex(item => item.id === proxy.id)
    if (index >= 0) networkState.proxies[index] = result.proxy
    await loadNetwork(true, true)
    networkState.notice = applyNotice(result.applied, result.status)
    return true
  } catch (error) {
    await handleMutationFailure(error, '无法切换代理')
    return false
  } finally {
    networkState.busyID = ''
  }
}

export async function removeProxy(proxy: ProxyInstance): Promise<boolean> {
  if (networkState.busyID) return false
  networkState.busyID = proxy.id
  networkState.error = ''
  networkState.notice = ''
  try {
    const result = await gateway.deleteProxy(proxy.id, proxy.revision)
    networkState.proxies = networkState.proxies.filter(item => item.id !== proxy.id)
    await loadNetwork(true, true)
    networkState.notice = applyNotice(result.applied, result.status)
    return true
  } catch (error) {
    await handleMutationFailure(error, '无法删除代理')
    return false
  } finally {
    networkState.busyID = ''
  }
}
