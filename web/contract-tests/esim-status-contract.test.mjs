import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import { parseSIMStatusResponse } from '../src/api/contract.ts'
import { createFixtureGateway } from '../src/api/fixture.ts'

function esimResponse() {
  return {
    sim: {
      line_id: 'line-esim',
      present: true,
      active: true,
      identifier: '8986012345678900001',
      imsi: '001010000000001',
      sim_type: 'esim',
      esim_status: 'with_profiles',
      eid: '****5678',
      sim_slots: [
        {
          index: 1,
          present: false,
          current: false,
          sim_type: 'unknown',
          esim_status: 'unknown'
        },
        {
          index: 2,
          present: true,
          current: true,
          sim_type: 'esim',
          esim_status: 'with_profiles',
          eid: '****5678'
        }
      ],
      sim_slots_known: true,
      primary_sim_slot: 2,
      primary_sim_slot_known: true,
      current_sim_slot: 2,
      current_sim_slot_known: true,
      profile_management: {
        supported: false,
        reason: 'eUICC profile management is unavailable'
      },
      home_operator_code: '00101',
      home_operator_name: 'Aurora Mobile',
      serving_operator_code: '00101',
      serving_operator_name: 'Aurora Mobile',
      registration_state_known: true,
      registration_state_code: 1,
      registration_state: 'home',
      roaming: false,
      operator_identifier: '00101',
      operator_name: 'Aurora Mobile',
      unlock_required: 'none',
      unlock_required_code: 1,
      unlock_retries: { 'sim-pin': 3, 'sim-puk': 10 },
      observed_at: '2026-07-24T08:00:00Z'
    }
  }
}

test('SIM decoder preserves typed eSIM and slot facts', () => {
  const parsed = parseSIMStatusResponse(esimResponse())

  assert.equal(parsed.sim_type, 'esim')
  assert.equal(parsed.esim_status, 'with_profiles')
  assert.equal(parsed.eid, '****5678')
  assert.equal(parsed.current_sim_slot, 2)
  assert.equal(parsed.primary_sim_slot, 2)
  assert.equal(parsed.sim_slots[1]?.sim_type, 'esim')
  assert.equal(parsed.sim_slots[1]?.eid, '****5678')
  assert.equal(parsed.profile_management.supported, false)
})

test('SIM decoder rejects unknown enum values and unmasked EIDs', () => {
  const invalidSIMType = esimResponse()
  invalidSIMType.sim.sim_type = 'embedded'
  assert.throws(() => parseSIMStatusResponse(invalidSIMType), /sim_type/)

  const invalidESIMStatus = esimResponse()
  invalidESIMStatus.sim.esim_status = 'ready'
  assert.throws(() => parseSIMStatusResponse(invalidESIMStatus), /esim_status/)

  const invalidSlotType = esimResponse()
  invalidSlotType.sim.sim_slots[1].sim_type = 'embedded'
  assert.throws(() => parseSIMStatusResponse(invalidSlotType), /sim_type/)

  const invalidSlotStatus = esimResponse()
  invalidSlotStatus.sim.sim_slots[1].esim_status = 'ready'
  assert.throws(() => parseSIMStatusResponse(invalidSlotStatus), /esim_status/)

  const rawEID = esimResponse()
  rawEID.sim.eid = '89049032000000000000000012345678'
  assert.throws(() => parseSIMStatusResponse(rawEID), /掩码 EID/)

  const rawSlotEID = esimResponse()
  rawSlotEID.sim.sim_slots[1].eid = '89049032000000000000000012345678'
  assert.throws(() => parseSIMStatusResponse(rawSlotEID), /掩码 EID/)
})

test('empty EID remains absent rather than becoming display data', () => {
  const response = esimResponse()
  response.sim.eid = ''
  response.sim.sim_slots[1].eid = ''

  const parsed = parseSIMStatusResponse(response)
  assert.equal(parsed.eid, undefined)
  assert.equal(parsed.sim_slots[1]?.eid, undefined)
})

test('fixture distinguishes a physical SIM from an eSIM', async () => {
  const gateway = createFixtureGateway()
  const physical = await gateway.getSIMStatus('line-fixture-main')
  const esim = await gateway.getSIMStatus('line-fixture-travel')

  assert.equal(physical.sim_type, 'physical')
  assert.equal(physical.eid, undefined)
  assert.equal(esim.sim_type, 'esim')
  assert.equal(esim.esim_status, 'with_profiles')
  assert.match(esim.eid || '', /^\*{4}\d{4}$/)
  assert.equal(esim.current_sim_slot, 2)
  assert.equal(esim.sim_slots[1]?.current, true)
})

test('SIM settings render read-only eSIM facts through the shared decoder', () => {
  const panel = readFileSync(
    new URL('../src/components/DeviceConfigurationPanel.vue', import.meta.url),
    'utf8'
  )
  const client = readFileSync(new URL('../src/api/client.ts', import.meta.url), 'utf8')

  for (const label of ['SIM 类型', 'eSIM 状态', 'EID', '当前卡槽', '主卡槽', 'Profile 管理']) {
    assert.match(panel, new RegExp(label))
  }
  assert.match(panel, /simStatus\.sim_slots/)
  assert.doesNotMatch(panel, /profile_management\.reason/)
  assert.match(client, /parseSIMStatusResponse/)
  assert.doesNotMatch(client, /function parseSIMStatus\(/)
})
