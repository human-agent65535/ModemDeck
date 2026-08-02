import assert from 'node:assert/strict'
import { readdir, readFile } from 'node:fs/promises'
import test from 'node:test'

const sourceRoot = new URL('../src/', import.meta.url)

async function sourceFiles(directory = sourceRoot) {
  const entries = await readdir(directory, { withFileTypes: true })
  const nested = await Promise.all(
    entries.map(async entry => {
      const url = new URL(entry.name, directory)
      if (entry.isDirectory()) {
        return sourceFiles(new URL(`${entry.name}/`, directory))
      }
      return /\.(css|vue)$/.test(entry.name) ? [url] : []
    })
  )
  return nested.flat()
}

test('responsive media queries use the shared viewport bands', async () => {
  const allowedWidths = new Set([420, 560, 860, 861, 1100, 1479, 1480])
  const files = await sourceFiles()

  for (const file of files) {
    const contents = await readFile(file, 'utf8')
    for (const media of contents.matchAll(/@media\s*([^\{]+)\{/g)) {
      for (const width of media[1].matchAll(/(?:min|max)-width:\s*(\d+)px/g)) {
        const value = Number(width[1])
        assert.ok(
          allowedWidths.has(value),
          `${file.pathname} uses an unshared viewport width in @media ${media[1].trim()}`
        )
      }
    }
  }
})

test('semantic UI colors come from the shared design tokens', async () => {
  const styleURL = new URL('../src/style.css', import.meta.url)
  const style = await readFile(styleURL, 'utf8')
  const requiredTokens = [
    '--accent-border',
    '--success-border',
    '--warning',
    '--warning-soft',
    '--warning-border',
    '--favorite',
    '--favorite-border',
    '--danger-strong',
    '--danger-border',
    '--on-accent',
    '--control-muted',
    '--skeleton'
  ]

  for (const token of requiredTokens) {
    assert.match(style, new RegExp(`${token}:`), `missing ${token}`)
  }

  const legacySemanticColors = new Set([
    '#725200',
    '#7a420c',
    '#7a5200',
    '#7a5700',
    '#8a4b10',
    '#8a5a25',
    '#8a5b00',
    '#9b3340',
    '#a85b10',
    '#a92e3b',
    '#b9ddd5',
    '#c7352d',
    '#d6a400',
    '#d99a18',
    '#e9bd72',
    '#ead9a7',
    '#eed98b',
    '#f0d2d6',
    '#f2caca',
    '#f4cbd1',
    '#fff0c2',
    '#fff3c6',
    '#fff3da',
    '#fff4f4',
    '#fff4f5',
    '#fff6d8',
    '#fff6dd',
    '#fff7e8',
    '#fff8e8'
  ])
  const files = await sourceFiles()

  for (const file of files) {
    let contents = await readFile(file, 'utf8')
    if (file.pathname === styleURL.pathname) {
      contents = contents.slice(contents.indexOf('\n}') + 2)
    }
    for (const color of contents.matchAll(/#[\da-f]{6}\b/gi)) {
      assert.ok(
        !legacySemanticColors.has(color[0].toLowerCase()),
        `${file.pathname} bypasses a shared semantic color token with ${color[0]}`
      )
    }
  }
})

test('the production entry stays compatible with the strict style CSP', async () => {
  const index = await readFile(new URL('../index.html', import.meta.url), 'utf8')

  assert.doesNotMatch(index, /<style(?:\s|>)/i)
  assert.doesNotMatch(index, /\sstyle\s*=/i)
})

test('interaction motion uses the shared timing and easing language', async () => {
  const style = await readFile(new URL('../src/style.css', import.meta.url), 'utf8')
  const files = await sourceFiles()

  for (const token of [
    '--motion-fast',
    '--motion-base',
    '--motion-slow',
    '--ease-standard',
    '--ease-emphasized'
  ]) {
    assert.match(style, new RegExp(`${token}:`), `missing ${token}`)
  }

  for (const file of files) {
    const contents = await readFile(file, 'utf8')
    for (const declaration of contents.matchAll(/\btransition\s*:\s*([^;]+);/g)) {
      assert.doesNotMatch(
        declaration[1],
        /\b\d+(?:\.\d+)?ms\b/,
        `${file.pathname} bypasses the shared interaction motion tokens`
      )
    }
    assert.doesNotMatch(
      contents,
      /<Transition(?:Group)?\s+name="fade"/,
      `${file.pathname} uses an unowned generic fade transition`
    )
  }
})

test('binary switches share the global visual primitive', async () => {
  const [style, proxy] = await Promise.all([
    readFile(new URL('../src/style.css', import.meta.url), 'utf8'),
    readFile(new URL('../src/components/ProxyCard.vue', import.meta.url), 'utf8')
  ])

  assert.match(style, /\.ui-switch\s*\{/)
  assert.match(proxy, /class="ui-switch ui-switch--compact"/)
  assert.doesNotMatch(proxy, /class="proxy-switch"/)
  assert.doesNotMatch(style, /\.settings-toggle-row\s*\{/)
})
