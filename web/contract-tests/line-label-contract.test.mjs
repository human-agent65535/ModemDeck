import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import {
  createLineLabelPayload,
  lineLabelPath,
  parseLineLabelResponse
} from '../src/api/contract.ts'
import { createFixtureGateway } from '../src/api/fixture.ts'
import { lineLabel } from '../src/state/workspace.ts'

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
  const direct = parseLineLabelResponse({
    iccid: line.iccid,
    line_label: line.line_label
  })
  const enveloped = parseLineLabelResponse({
    line: { iccid: line.iccid, line_label: '副卡' }
  })
  assert.equal(direct.iccid, line.iccid)
  assert.equal(direct.line_label, '主卡')
  assert.equal(enveloped.iccid, line.iccid)
  assert.equal(enveloped.line_label, '副卡')
  assert.equal('state' in enveloped, false)
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
  assert.deepEqual(saved, { iccid: main.iccid, line_label: '工作' })
  assert.equal((await gateway.getBootstrap()).lines[0].line_label, '工作')

  const cleared = await gateway.updateLineLabel(main.iccid, { line_label: '' })
  assert.equal(cleared.line_label, '')
  await assert.rejects(
    () => gateway.updateLineLabel(main.iccid, { line_label: '一二三四五六七八九十一二三四五六七' }),
    error => error?.status === 400 && error?.code === 'invalid_line_label'
  )
})

test('line names prefer the line label and otherwise use the module name', () => {
  assert.equal(lineLabel(line), '主卡')
  assert.equal(
    lineLabel({
      ...line,
      id: 'line-secondary',
      iccid: '8986012345678901937',
      device_alias: '楼上模组',
      model: 'EC25',
      line_label: ''
    }),
    '楼上模组'
  )
  assert.equal(
    lineLabel({
      ...line,
      id: 'line-third',
      iccid: '8986012345678901942',
      device_alias: '',
      model: 'EC25',
      line_label: ''
    }),
    'EC25'
  )
})

test('settings edit only the ICCID-backed line label', () => {
  assert.match(devicePanelSource, /maxlength="16"/)
  assert.match(devicePanelSource, /!selectedLine\?\.iccid/)
  assert.match(devicePanelSource, /updateLineLabel\(line\.iccid,\s*\{\s*line_label: value\s*\}\)/)
  assert.match(devicePanelSource, /lineLabelDraft\.value = selectedLine\.value\?\.line_label \|\| ''/)
  assert.match(devicePanelSource, /`\$\{selectedLineFallback\}（建议）`/)
  assert.match(workspaceSource, /gateway\.updateLineLabel\(iccid, input\)/)
  assert.match(workspaceSource, /if \(saved\.iccid !== normalizedICCID\)/)
  assert.match(workspaceSource, /line\.line_label = saved\.line_label/)
  assert.doesNotMatch(workspaceSource, /Object\.assign\(line, saved\)/)
  assert.doesNotMatch(devicePanelSource, /renameDevice|修改模组名称|@rename/)
  assert.match(lineTagSource, /line\.line_label\.trim\(\) \|\| props\.fallback\.trim\(\)/)
  assert.match(lineTagSource, /stableHash\(stableKey\) % 6/)
  assert.match(lineTagSource, /flex:\s*0 0 auto/)
})
