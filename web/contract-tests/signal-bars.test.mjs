import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'

const source = await readFile(
  new URL('../src/components/SignalBars.vue', import.meta.url),
  'utf8'
)

test('signal bars derive four stable levels from the reported percentage', () => {
  assert.match(source, /const barCount = 4/)
  assert.match(
    source,
    /Math\.ceil\(Math\.min\(props\.value, 100\) \/ \(100 \/ barCount\)\)/
  )
  assert.match(source, /v-for="bar in barCount"/)
  assert.match(source, /'is-active': bar <= activeBars/)
  assert.match(source, /width: 19px/)
  assert.match(source, /height: 14px/)
  assert.match(source, /flex: 0 0 19px/)
})

test('unknown and zero signal have distinct visual and accessible states', () => {
  assert.match(source, /if \(!isKnown\.value\) return 'is-unknown'/)
  assert.match(source, /if \(activeBars\.value === 0\) return 'is-zero'/)
  assert.match(source, /if \(!isKnown\.value\) return '信号质量未知'/)
  assert.match(source, /无信号/)
  assert.match(source, /\.signal-bars\.is-unknown \.signal-bars__bar/)
  assert.match(source, /border: 1px dashed/)
  assert.match(source, /\.signal-bars\.is-zero::after/)
})

test('signal bars expose the reported percentage without inventing dBm', () => {
  assert.match(source, /role="img"/)
  assert.match(source, /:aria-label="accessibleLabel"/)
  assert.match(source, /信号质量 \$\{props\.value\}%/)
  assert.doesNotMatch(source, /dBm|RSRP|RSRQ|RSSI/)
})
