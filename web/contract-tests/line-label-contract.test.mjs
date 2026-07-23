import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import {
  createLineLabelPayload,
  lineLabelPath,
  parseLineLabelResponse
} from '../src/api/contract.ts'
import { createFixtureGateway } from '../src/api/fixture.ts'

const devicePanelSource = readFileSync(
  new URL('../src/components/DeviceConfigurationPanel.vue', import.meta.url),
  'utf8'
)
const lineTagSource = readFileSync(
  new URL('../src/components/LineTag.vue', import.meta.url),
  'utf8'
)
const workspaceSource = readFileSync(
  new URL('../src/state/workspace.ts', import.meta.url),
  'utf8'
)

const line = {
  id: 'line-main',
  iccid: '8986012345678900001',
  imsi: '460011234567890',
  phone_number: '+86 138 0000 0000',
  operator: 'China Unicom',
  device_imei: '860000000000001',
  device_alias: '客厅模组',
  line_label: '主卡',
  state: 'registered'
}

test('line label API uses ICCID identity and a bounded payload', () => {
  assert.equal(
    lineLabelPath(' 8986012345678900001 '),
    '/api/v1/lines/8986012345678900001/label'
  )
  assert.deepEqual(createLineLabelPayload({ line_label: '  副卡  ' }), {
    line_label: '副卡'
  })
  assert.deepEqual(createLineLabelPayload({ line_label: '   ' }), {
    line_label: ''
  })
  assert.throws(
    () => createLineLabelPayload({ line_label: '一二三四五六七八九十一二三四五六七' }),
    /16/
  )
})

test('line label response accepts the documented direct and envelope forms', () => {
  const direct = parseLineLabelResponse(line)
  const enveloped = parseLineLabelResponse({ line: { ...line, line_label: '副卡' } })
  assert.equal(direct.id, line.id)
  assert.equal(direct.iccid, line.iccid)
  assert.equal(direct.device_alias, line.device_alias)
  assert.equal(direct.line_label, '主卡')
  assert.equal(enveloped.id, line.id)
  assert.equal(enveloped.line_label, '副卡')
})

test('fixture keeps module aliases separate from editable line labels', async () => {
  const gateway = createFixtureGateway()
  const initial = await gateway.getBootstrap()

  assert.deepEqual(
    initial.lines.slice(0, 2).map(item => item.line_label),
    ['主卡', '副卡']
  )

  const main = initial.lines[0]
  const saved = await gateway.updateLineLabel(main.iccid, { line_label: '工作' })
  assert.equal(saved.line_label, '工作')
  assert.equal(saved.device_alias, main.device_alias)
  assert.equal((await gateway.getBootstrap()).lines[0].line_label, '工作')

  const cleared = await gateway.updateLineLabel(main.iccid, { line_label: '' })
  assert.equal(cleared.line_label, '')
  await assert.rejects(
    () => gateway.updateLineLabel(main.iccid, { line_label: '一二三四五六七八九十一二三四五六七' }),
    error => error?.status === 400 && error?.code === 'invalid_line_label'
  )
})

test('settings edit an ICCID-backed label without changing module alias', () => {
  assert.match(devicePanelSource, /maxlength="16"/)
  assert.match(devicePanelSource, /!selectedLine\?\.iccid/)
  assert.match(devicePanelSource, /updateLineLabel\(line\.iccid,\s*\{\s*line_label: value\s*\}\)/)
  assert.match(devicePanelSource, /lineLabelDraft\.value = selectedLine\.value\?\.line_label \|\| ''/)
  assert.match(devicePanelSource, /`\$\{selectedLineFallback\}（建议）`/)
  assert.match(workspaceSource, /gateway\.updateLineLabel\(iccid, input\)/)
  assert.match(workspaceSource, /Object\.assign\(line, saved\)/)
  assert.match(lineTagSource, /line\.line_label\.trim\(\) \|\| props\.fallback\.trim\(\)/)
  assert.match(lineTagSource, /stableHash\(stableKey\) % 6/)
})
