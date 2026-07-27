import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import { createFixtureGateway } from '../src/api/fixture.ts'
import { parseDevice } from '../src/api/normalize.ts'

const panelSource = readFileSync(
  new URL('../src/components/DeviceConfigurationPanel.vue', import.meta.url),
  'utf8'
)
const workspaceSource = readFileSync(
  new URL('../src/state/workspace.ts', import.meta.url),
  'utf8'
)
const clientSource = readFileSync(
  new URL('../src/api/client.ts', import.meta.url),
  'utf8'
)

test('device contracts expose a device-bound name instead of an alias', () => {
  assert.equal(
    parseDevice({
      imei: '860000000000001',
      name: '机房模组',
      model: 'QDC507'
    }).name,
    '机房模组'
  )
  assert.match(clientSource, /\{ name: input\.name\.trim\(\) \}/)
  assert.doesNotMatch(clientSource, /\{ alias: input\.alias/)
  assert.match(
    workspaceSource,
    /if \(line\.device_imei === saved\.imei\) line\.device_name = saved\.name/
  )
})

test('fixture rename follows the modem IMEI without changing the line label', async () => {
  const gateway = createFixtureGateway({ lineCount: 2 })
  const before = await gateway.getBootstrap()
  const target = before.lines[0]
  const renamed = await gateway.renameDevice(target.device_imei, { name: '机房模组' })
  const devices = await gateway.listDevices()
  const after = await gateway.getBootstrap()

  assert.equal(renamed.name, '机房模组')
  assert.equal(
    devices.find(device => device.imei === target.device_imei)?.name,
    '机房模组'
  )
  assert.equal(after.lines[0].line_label, target.line_label)
})

test('current modem heading provides compact inline name editing', () => {
  assert.match(panelSource, /class="module-name-edit-button"/)
  assert.match(panelSource, /<Pencil :size="15" \/>/)
  assert.match(panelSource, /class="module-name-editor"/)
  assert.match(panelSource, /maxlength="100"/)
  assert.match(panelSource, /await renameDevice\(imei, \{ name \}\)/)
  assert.match(panelSource, /selectedLine\.value\?\.device_imei\.trim\(\)/)
  assert.match(panelSource, /<LineTag[\s\S]*selectedExplicitLineLabel/)
})
