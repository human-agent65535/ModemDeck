import assert from 'node:assert/strict'
import { readdirSync, readFileSync } from 'node:fs'
import test from 'node:test'

const componentPath = new URL('../src/components/LineSelector.vue', import.meta.url)
const component = readFileSync(componentPath, 'utf8')
const sourceRoot = new URL('../src/', import.meta.url)

function vueSources(directory) {
  return readdirSync(directory, { withFileTypes: true }).flatMap(entry => {
    const url = new URL(entry.name + (entry.isDirectory() ? '/' : ''), directory)
    if (entry.isDirectory()) return vueSources(url)
    return entry.name.endsWith('.vue')
      ? [{ path: url.pathname, source: readFileSync(url, 'utf8') }]
      : []
  })
}

test('line selector is a single non-native listbox control', () => {
  assert.match(component, /aria-haspopup="listbox"/)
  assert.match(component, /role="listbox"/)
  assert.match(component, /role="option"/)
  assert.doesNotMatch(component, /<select/)
})

test('line selector commits model updates and exposes a change hook', () => {
  assert.match(component, /emit\('update:modelValue', value\)/)
  assert.match(component, /emit\('change', value\)/)
  assert.match(component, /@click="selectOption\(option\.value\)"/)
})

test('line selector closes after focus moves outside instead of racing option clicks', () => {
  assert.match(component, /document\.addEventListener\('focusin', onDocumentFocusIn\)/)
  assert.match(component, /document\.removeEventListener\('focusin', onDocumentFocusIn\)/)
  assert.match(
    component,
    /open\.value && !root\.value\?\.contains\(event\.target as Node\)/
  )
  assert.doesNotMatch(component, /@focusout=/)
  assert.doesNotMatch(component, /document\.activeElement/)
})

test('line selector supports complete keyboard navigation', () => {
  for (const key of [
    "'ArrowDown'",
    "'ArrowUp'",
    "'Home'",
    "'End'",
    "'Enter'",
    "' '",
    "'Escape'",
    "'Tab'"
  ]) {
    assert.match(component, new RegExp(`case ${key.replace(/[.*+?^${}()|[\\]\\\\]/g, '\\\\$&')}:`))
  }
  assert.match(component, /focusOption\(\)/)
  assert.match(component, /closeMenu\(true\)/)
})

test('all line selector call sites use v-model instead of one-way values', () => {
  const users = vueSources(sourceRoot).filter(file =>
    /<LineSelector(?:\s|>)/.test(file.source)
  )
  assert.ok(users.length > 0)
  for (const file of users) {
    const uses = file.source.match(/<LineSelector[\s\S]*?\/>/g) || []
    assert.ok(uses.length > 0, file.path)
    for (const use of uses) {
      assert.match(use, /\bv-model="/, file.path)
      assert.doesNotMatch(use, /:\s*model-value=/, file.path)
    }
  }
})

test('VoLTE is a styled binary switch and never a native select', () => {
  const panel = readFileSync(
    new URL('../src/components/DeviceConfigurationPanel.vue', import.meta.url),
    'utf8'
  )
  const start = panel.indexOf('<h4>VoLTE</h4>')
  const end = panel.indexOf('</section>', start)
  const section = panel.slice(start, end)

  assert.ok(start >= 0)
  assert.match(section, /type="checkbox"/)
  assert.match(section, /role="switch"/)
  assert.match(section, /aria-label="启用 VoLTE（重启生效）"/)
  assert.doesNotMatch(section, /<select/)
  assert.match(panel, /-webkit-appearance: none/)
  assert.match(panel, /\.configuration-toggle input:focus-visible/)
})
