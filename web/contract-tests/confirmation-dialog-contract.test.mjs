import assert from 'node:assert/strict'
import { readdirSync, readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import test from 'node:test'

const srcRoot = fileURLToPath(new URL('../src/', import.meta.url))

function sourceFiles(directory) {
  return readdirSync(directory, { withFileTypes: true }).flatMap(entry => {
    const path = `${directory}/${entry.name}`
    if (entry.isDirectory()) return sourceFiles(path)
    return /\.(?:ts|vue)$/.test(entry.name) ? [path] : []
  })
}

function source(relativePath) {
  return readFileSync(new URL(relativePath, import.meta.url), 'utf8')
}

test('application uses one accessible confirmation dialog instead of browser confirms', () => {
  for (const path of sourceFiles(srcRoot)) {
    assert.doesNotMatch(
      readFileSync(path, 'utf8'),
      /\bwindow\.confirm\s*\(/,
      `${path} 仍在使用浏览器确认框`
    )
  }

  const app = source('../src/App.vue')
  const dialog = source('../src/components/ConfirmationDialog.vue')
  assert.match(app, /<ConfirmationDialog \/>/)
  assert.match(dialog, /role="dialog"/)
  assert.match(dialog, /aria-modal="true"/)
  assert.match(dialog, /event\.key === 'Escape'/)
  assert.match(dialog, /event\.key !== 'Tab'/)
  assert.match(dialog, /cancelButton\.value\?\.focus\(\)/)
  assert.match(dialog, /previousFocus\?\.focus\(\)/)
  assert.match(dialog, /answerConfirmation\(false\)/)
  assert.match(dialog, /answerConfirmation\(true\)/)
})

test('dangerous device, contact, and proxy actions await the shared confirmation', () => {
  for (const relativePath of [
    '../src/components/DeviceConfigurationPanel.vue',
    '../src/views/ContactsView.vue',
    '../src/views/TrafficView.vue'
  ]) {
    const component = source(relativePath)
    assert.match(component, /await requestConfirmation\(/)
    assert.doesNotMatch(component, /\bwindow\.confirm\s*\(/)
  }

  const devicePanel = source('../src/components/DeviceConfigurationPanel.vue')
  assert.match(devicePanel, /启用 VoLTE（重启生效）/)
  assert.match(devicePanel, /保存（重启生效）/)
})
