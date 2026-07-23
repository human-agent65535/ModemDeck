export function initials(name: string): string {
  const parts = name.trim().split(/\s+/).filter(Boolean)
  if (parts.length === 0) return '#'
  if (parts.length === 1) return Array.from(parts[0] || '#').slice(0, 2).join('').toUpperCase()
  return `${Array.from(parts[0] || '#')[0] || ''}${Array.from(parts.at(-1) || '#')[0] || ''}`.toUpperCase()
}

export function formatRelativeDate(value: string): string {
  const date = new Date(value)
  if (!Number.isFinite(date.getTime())) return value
  const now = new Date()
  const sameDay = date.toDateString() === now.toDateString()
  if (sameDay) return new Intl.DateTimeFormat('zh-CN', { hour: '2-digit', minute: '2-digit' }).format(date)
  const yesterday = new Date(now)
  yesterday.setDate(now.getDate() - 1)
  if (date.toDateString() === yesterday.toDateString()) return '昨天'
  if (date.getFullYear() === now.getFullYear()) {
    return new Intl.DateTimeFormat('zh-CN', { month: 'numeric', day: 'numeric' }).format(date)
  }
  return new Intl.DateTimeFormat('zh-CN', { year: 'numeric', month: 'numeric', day: 'numeric' }).format(date)
}

export function formatDateTime(value: string): string {
  const date = new Date(value)
  if (!Number.isFinite(date.getTime())) return value
  return new Intl.DateTimeFormat('zh-CN', {
    year: 'numeric',
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit'
  }).format(date)
}

export function formatDuration(seconds: number): string {
  const safe = Math.max(0, Math.floor(seconds))
  const minutes = Math.floor(safe / 60)
  const remainder = safe % 60
  return minutes > 0 ? `${minutes}:${String(remainder).padStart(2, '0')}` : `${remainder} 秒`
}

export function primaryPhone(phones: Array<{ number: string; primary: boolean }>): string {
  return phones.find(phone => phone.primary)?.number || phones[0]?.number || ''
}
