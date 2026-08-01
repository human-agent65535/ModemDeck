import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import test from 'node:test'
import {
  accessTechnologyLabel,
  detailedServingTechnologyLabel,
  servingBandLabel,
  servingChannelLabel
} from '../src/utils/radioAccess.ts'

const [moduleCard, devicePanel, diagnosticsPanel] = await Promise.all([
  readFile(new URL('../src/components/ModuleCard.vue', import.meta.url), 'utf8'),
  readFile(
    new URL('../src/components/DeviceConfigurationPanel.vue', import.meta.url),
    'utf8'
  ),
  readFile(new URL('../src/components/DiagnosticsPanel.vue', import.meta.url), 'utf8')
])

const servingRadio = {
  access_technology: 'lte',
  duplex_mode: 'fdd',
  band: 'B1',
  channel: 100,
  channel_type: 'earfcn',
  source: 'quectel-qnwinfo'
}

test('radio labels keep overview technology separate from serving-band evidence', () => {
  assert.equal(accessTechnologyLabel(1 << 14), 'LTE')
  assert.equal(detailedServingTechnologyLabel(servingRadio, 1 << 14), 'FDD LTE')
  assert.equal(servingBandLabel(servingRadio), 'LTE Band 1')
  assert.equal(servingChannelLabel(servingRadio), 'EARFCN 100')
  assert.equal(servingBandLabel(undefined), '')
})

test('the module card shows LTE without polling-derived band data', () => {
  assert.match(moduleCard, /accessTechnologyLabel\(props\.line\.access_technologies\)/)
  assert.doesNotMatch(moduleCard, /serving_radio|servingBandLabel|currentBand/)
})

test('device detail and diagnostics show structured serving-radio evidence', () => {
  assert.match(devicePanel, /detailedServingTechnologyLabel\(/)
  assert.match(devicePanel, /servingBandLabel\(hardware\.details\.serving_radio\)/)
  assert.match(diagnosticsPanel, /lineAccessTechnologyEvidence\(selectedDiagnosticLine\)/)
  assert.match(diagnosticsPanel, /lineBandEvidence\(selectedDiagnosticLine\)/)
  assert.match(diagnosticsPanel, /lineChannelEvidence\(selectedDiagnosticLine\)/)
  assert.match(diagnosticsPanel, /lineAccessMask\(selectedDiagnosticLine\)/)
})
