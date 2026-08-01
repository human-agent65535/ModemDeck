export type ReleaseNoteSection = {
  title: string
  lines: string[]
}

const summaryLineLimit = 3
const summaryCharacterLimit = 220

function cleanInlineMarkdown(value: string): string {
  return value
    .replace(/!\[([^\]]*)\]\([^)]+\)/g, '$1')
    .replace(/\[([^\]]+)\]\([^)]+\)/g, '$1')
    .replace(/\*\*([^*]+)\*\*/g, '$1')
    .replace(/__([^_]+)__/g, '$1')
    .replace(/`([^`]+)`/g, '$1')
    .replace(/^>\s*/, '')
    .trim()
}

function releaseContentLine(value: string): string {
  const withoutListMarker = value.replace(/^\s*(?:[-*+]\s+|\d+[.)]\s+)/, '')
  return cleanInlineMarkdown(withoutListMarker)
}

function isFullChangelog(value: string): boolean {
  return /^\*{0,2}full changelog\*{0,2}\s*:/i.test(value.trim())
}

function boundedSummaryLine(value: string): string {
  const characters = Array.from(value)
  if (characters.length <= summaryCharacterLimit) return value
  return `${characters.slice(0, summaryCharacterLimit).join('').trimEnd()}…`
}

export function parseReleaseNotes(value?: string): ReleaseNoteSection[] {
  if (!value?.trim()) return []
  const sections: ReleaseNoteSection[] = []
  let current: ReleaseNoteSection = { title: '', lines: [] }

  const commitSection = () => {
    if (current.title || current.lines.length) sections.push(current)
    current = { title: '', lines: [] }
  }

  for (const rawLine of value.replace(/\r\n?/g, '\n').split('\n')) {
    const line = rawLine.trim()
    if (!line || isFullChangelog(line)) continue
    const heading = /^#{1,6}\s+(.+)$/.exec(line)
    if (heading) {
      commitSection()
      current.title = cleanInlineMarkdown(heading[1] || '')
      continue
    }
    const content = releaseContentLine(line)
    if (content) current.lines.push(content)
  }
  commitSection()
  return sections
}

export function releaseNoteSummary(value?: string): string[] {
  const lines = parseReleaseNotes(value).flatMap(section => section.lines)
  return lines.slice(0, summaryLineLimit).map(boundedSummaryLine)
}

export function releaseNoteRemainder(value?: string): ReleaseNoteSection[] {
  let skipped = 0
  return parseReleaseNotes(value).flatMap(section => {
    const lines = section.lines.filter(() => {
      if (skipped >= summaryLineLimit) return true
      skipped += 1
      return false
    })
    return lines.length ? [{ title: section.title, lines }] : []
  })
}
