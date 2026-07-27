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
import { LINE_TONE_PRESETS, lineTone } from '../src/utils/lineTone.ts'

const devicePanelSource = readFileSync(
  new URL('../src/components/DeviceConfigurationPanel.vue', import.meta.url),
  'utf8'
)
const lineTagSource = readFileSync(
  new URL('../src/components/LineTag.vue', import.meta.url),
  'utf8'
)
const lineToneSource = readFileSync(
  new URL('../src/utils/lineTone.ts', import.meta.url),
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
  device_name: '客厅模组',
  line_label: '主卡',
  line_color: 'violet',
  state: 'registered'
}

test('line label API uses stable line_id identity and a bounded payload', () => {
  assert.equal(
    lineLabelPath(' line-main '),
    '/api/v1/lines/line-main/label'
  )
  assert.deepEqual(createLineLabelPayload({ line_label: '  副卡  ' }), {
    line_label: '副卡'
  })
  assert.deepEqual(createLineLabelPayload({ line_label: '   ' }), {
    line_label: ''
  })
  assert.deepEqual(
    createLineLabelPayload({ line_label: '  工作  ', line_color: 'orange' }),
    {
      line_label: '工作',
      line_color: 'orange'
    }
  )
  assert.throws(
    () => createLineLabelPayload({ line_label: '一二三四五六七八九十一二三四五六七' }),
    /16/
  )
  assert.throws(
    () => createLineLabelPayload({ line_label: '主卡', line_color: 'magenta' }),
    /预设/
  )
})

test('line label response accepts the documented direct and envelope forms', () => {
  const direct = parseLineLabelResponse({
    line_id: line.id,
    line_label: line.line_label,
    line_color: line.line_color
  })
  const enveloped = parseLineLabelResponse({
    line: { line_id: line.id, line_label: '副卡', line_color: 'teal' }
  })
  assert.equal(direct.line_id, line.id)
  assert.equal(direct.line_label, '主卡')
  assert.equal(direct.line_color, 'violet')
  assert.equal(enveloped.line_id, line.id)
  assert.equal(enveloped.line_label, '副卡')
  assert.equal(enveloped.line_color, 'teal')
  assert.equal('state' in enveloped, false)
  assert.throws(
    () =>
      parseLineLabelResponse({
        line: { line_id: line.id, line_label: '副卡', line_color: 'magenta' }
      }),
    /预设/
  )
})

test('fixture keeps module names separate from editable line labels', async () => {
  const gateway = createFixtureGateway()
  const initial = await gateway.getBootstrap()

  assert.deepEqual(
    initial.lines.slice(0, 2).map(item => [item.line_label, item.line_color]),
    [
      ['主卡', 'violet'],
      ['副卡', 'teal']
    ]
  )

  const main = initial.lines[0]
  const saved = await gateway.updateLineLabel(main.id, {
    line_label: '工作',
    line_color: 'orange'
  })
  assert.equal(saved.line_label, '工作')
  assert.deepEqual(saved, {
    line_id: main.id,
    line_label: '工作',
    line_color: 'orange'
  })
  assert.equal((await gateway.getBootstrap()).lines[0].line_label, '工作')
  assert.equal((await gateway.getBootstrap()).lines[0].line_color, 'orange')

  const cleared = await gateway.updateLineLabel(main.id, { line_label: '' })
  assert.equal(cleared.line_label, '')
  assert.equal(cleared.line_color, 'orange')
  await assert.rejects(
    () => gateway.updateLineLabel(main.id, { line_label: '一二三四五六七八九十一二三四五六七' }),
    error => error?.status === 400 && error?.code === 'invalid_line_label'
  )
  await assert.rejects(
    () => gateway.updateLineLabel(main.id, { line_label: '主卡', line_color: 'magenta' }),
    error => error?.status === 400 && error?.code === 'invalid_line_color'
  )
})

test('line names prefer the line label and otherwise use the module name', () => {
  assert.equal(lineLabel(line), '主卡')
  assert.equal(
    lineLabel({
      ...line,
      id: 'line-secondary',
      iccid: '8986012345678901937',
      device_name: '楼上模组',
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
      device_name: '',
      model: 'EC25',
      line_label: ''
    }),
    'EC25'
  )
})

test('settings edit the stable line identity from preset colors', () => {
  assert.match(devicePanelSource, /maxlength="16"/)
  assert.match(devicePanelSource, /const lineID = line \? lineKey\(line\) : ''/)
  assert.match(
    devicePanelSource,
    /updateLineLabel\(lineID,\s*\{\s*line_label: value,\s*line_color: lineColorDraft\.value\s*\}\)/
  )
  assert.match(devicePanelSource, /lineLabelDraft\.value = selectedLine\.value\?\.line_label \|\| ''/)
  assert.match(devicePanelSource, /lineColorDraft\.value = selectedLineColor\.value/)
  assert.match(devicePanelSource, /class="line-color-picker"/)
  assert.match(devicePanelSource, /v-for="preset in lineTonePresets"/)
  assert.match(
    devicePanelSource,
    /\.line-color-picker__options\s*\{[^}]*grid-template-columns: repeat\(8, 30px\)/s
  )
  assert.match(devicePanelSource, /\.line-label-form__controls\s*\{[^}]*flex-wrap: wrap/s)
  assert.match(devicePanelSource, /\.line-label-form__actions\s*\{[^}]*margin-left: auto/s)
  assert.match(
    devicePanelSource,
    /\.line-label-form__actions > :deep\(\.line-tag\)\s*\{[^}]*height: 34px/s
  )
  assert.doesNotMatch(devicePanelSource, /repeat\(4, 30px\)/)
  assert.match(devicePanelSource, /t\('device\.suggested', \{ label: selectedLineFallback \}\)/)
  assert.match(workspaceSource, /gateway\.updateLineLabel\(lineID, input\)/)
  assert.match(workspaceSource, /if \(saved\.line_id !== normalizedLineID\)/)
  assert.match(workspaceSource, /line\.line_label = saved\.line_label/)
  assert.match(workspaceSource, /line\.line_color = saved\.line_color/)
  assert.doesNotMatch(workspaceSource, /Object\.assign\(line, saved\)/)
  assert.match(devicePanelSource, /class="module-name-edit-button"/)
  assert.match(devicePanelSource, /<Pencil :size="15" \/>/)
  assert.match(devicePanelSource, /await renameDevice\(imei, \{ name \}\)/)
  assert.match(devicePanelSource, /selectedLine\.value\?\.device_imei\.trim\(\)/)
  assert.match(devicePanelSource, /maxlength="100"/)
  assert.match(lineTagSource, /line\.line_label\.trim\(\) \|\| props\.fallback\.trim\(\)/)
  assert.match(lineTagSource, /lineTone\(props\.line\)/)
  assert.match(lineToneSource, /preset\.id === line\.line_color/)
  assert.match(
    lineToneSource,
    /stableHash\(stableKey\) % AUTO_LINE_TONE_PRESETS\.length/
  )
  assert.match(lineTagSource, /flex:\s*0 0 auto/)
  assert.equal(LINE_TONE_PRESETS.length, 8)
  assert.equal(lineTone(line).foreground, '#6b3287')
})
