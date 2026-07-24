import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import test from 'node:test'
import {
  createNetworkSelectionPayload,
  mobileNetworkScanPath,
  networkSelectionContract,
  networkSelectionPath,
  parseMobileNetworkScanResponse,
  parseNetworkSelectionResponse
} from '../src/api/contract.ts'
import { gateway } from '../src/api/client.ts'
import { createFixtureGateway } from '../src/api/fixture.ts'
import {
  activateNetworkSelection,
  enterManualNetworkSelection,
  loadNetworkSelection,
  networkSelectionResource,
  networkSelectionState,
  selectManualNetwork,
  useAutomaticNetworkSelection
} from '../src/state/networkSelection.ts'

const panelSource = readFileSync(
  new URL('../src/components/DeviceConfigurationPanel.vue', import.meta.url),
  'utf8'
)
const stateSource = readFileSync(
  new URL('../src/state/networkSelection.ts', import.meta.url),
  'utf8'
)

function deferred() {
  let resolve
  const promise = new Promise(done => {
    resolve = done
  })
  return { promise, resolve }
}

function availableNetwork(operatorCode = '00102') {
  return {
    status: 'available',
    operator_code: operatorCode,
    operator_long: 'Pine Wireless',
    operator_short: 'Pine',
    access_technologies: 16384,
    access_technology_names: ['LTE']
  }
}

test('network selection endpoints encode line ids and use the explicit REST contract', () => {
  assert.equal(
    networkSelectionPath(' line / main '),
    '/api/v1/devices/line%20%2F%20main/network-selection'
  )
  assert.equal(
    mobileNetworkScanPath(' line / main '),
    '/api/v1/devices/line%20%2F%20main/network-scan'
  )
  assert.deepEqual(networkSelectionContract('line-main'), {
    get: {
      method: 'GET',
      path: '/api/v1/devices/line-main/network-selection',
      successStatus: 200
    },
    update: {
      method: 'PUT',
      path: '/api/v1/devices/line-main/network-selection',
      successStatus: 200
    },
    scan: {
      method: 'POST',
      path: '/api/v1/devices/line-main/network-scan',
      successStatus: 200
    }
  })
})

test('network selection payload only accepts a complete manual target', () => {
  assert.deepEqual(
    createNetworkSelectionPayload({
      mode: 'auto',
      operator_code: '00101',
      expected_revision: 2
    }),
    { mode: 'auto', expected_revision: 2 }
  )
  assert.deepEqual(
    createNetworkSelectionPayload({
      mode: 'manual',
      operator_code: ' 00101 ',
      expected_revision: 3
    }),
    { mode: 'manual', operator_code: '00101', expected_revision: 3 }
  )
  assert.throws(
    () => createNetworkSelectionPayload({ mode: 'manual', expected_revision: 3 }),
    /MCCMNC/
  )
})

test('network selection and scan responses are parsed without inventing state', () => {
  assert.deepEqual(
    parseNetworkSelectionResponse({
      line_id: 'line-main',
      mode: 'manual',
      operator_code: '00101',
      revision: 4,
      applied: false,
      last_error: 'registration failed'
    }),
    {
      line_id: 'line-main',
      mode: 'manual',
      operator_code: '00101',
      revision: 4,
      applied: false,
      last_error: 'registration failed',
      applied_at: undefined
    }
  )
  assert.deepEqual(
    parseMobileNetworkScanResponse({
      line_id: 'line-main',
      observed_at: '2026-07-24T12:00:00Z',
      networks: [
        {
          status: 'available',
          operator_code: '00102',
          operator_long: 'Pine Wireless',
          operator_short: 'Pine',
          access_technologies: 16384,
          access_technology_names: ['LTE']
        }
      ]
    }).networks[0],
    {
      status: 'available',
      operator_code: '00102',
      operator_long: 'Pine Wireless',
      operator_short: 'Pine',
      access_technologies: 16384,
      access_technology_names: ['LTE']
    }
  )
  assert.throws(
    () =>
      parseNetworkSelectionResponse({
        line_id: 'line-main',
        mode: 'manual',
        revision: 1,
        applied: true
      }),
    /operator_code/
  )
})

test('fixture keeps automatic and manual preferences revisioned per line', async () => {
  const gateway = createFixtureGateway()
  const automatic = await gateway.getNetworkSelection('line-fixture-main')
  const manual = await gateway.getNetworkSelection('line-fixture-travel')
  assert.equal(automatic.mode, 'auto')
  assert.equal(manual.mode, 'manual')
  assert.equal(manual.operator_code, '00101')

  const scan = await gateway.scanMobileNetworks('line-fixture-main')
  assert.equal(scan.networks.length, 3)
  assert.equal(scan.networks.at(-1)?.status, 'forbidden')

  const selected = await gateway.updateNetworkSelection('line-fixture-main', {
    mode: 'manual',
    operator_code: '00102',
    expected_revision: automatic.revision
  })
  assert.equal(selected.mode, 'manual')
  assert.equal(selected.operator_code, '00102')

  const restored = await gateway.updateNetworkSelection('line-fixture-main', {
    mode: 'auto',
    expected_revision: selected.revision
  })
  assert.equal(restored.mode, 'auto')
  assert.equal(restored.operator_code, undefined)
})

test('mobile network UI follows phone-style automatic and manual selection', () => {
  const start = panelSource.indexOf('<div class="network-selection">')
  const end = panelSource.indexOf('</section>', start)
  const section = panelSource.slice(start, end)
  assert.ok(start >= 0 && end > start)
  assert.match(section, /type="radio"\s+value="auto"/)
  assert.match(section, /type="radio"\s+value="manual"/)
  assert.match(section, /changeNetworkSelectionMode\('manual'\)/)
  assert.match(section, /selectedNetworkSelection\.mode === 'manual'/)
  assert.match(section, /重新搜索/)
  assert.match(section, /正在搜索网络/)
  assert.match(section, /未找到可用网络/)
  assert.match(section, /network\.status === 'forbidden'/)
  assert.doesNotMatch(section, /<select/)
  assert.doesNotMatch(section, /requestConfirmation/)
})

test('manual entry scans locally while writes require a selected operator', () => {
  const enterStart = stateSource.indexOf('export function enterManualNetworkSelection')
  const saveStart = stateSource.indexOf('async function saveNetworkSelection', enterStart)
  const enterBody = stateSource.slice(enterStart, saveStart)
  assert.match(enterBody, /target\.mode = 'manual'/)
  assert.match(enterBody, /scanMobileNetworks\(lineID\)/)
  assert.doesNotMatch(enterBody, /updateNetworkSelection/)

  assert.match(stateSource, /operator\.status === 'forbidden'/)
  assert.match(stateSource, /expected_revision: policy\.revision/)
  assert.match(stateSource, /networkSelectionState\.activation === context\.activation/)
  assert.match(stateSource, /networkSelectionState\.activeLineID === lineID/)
  assert.match(
    stateSource,
    /!updated\.applied && updated\.last_error \? updated\.last_error : ''/
  )
})

test('network selection state scans and writes only at the intended transitions', async () => {
  const lineID = 'line-state-transitions'
  const originalGet = gateway.getNetworkSelection
  const originalScan = gateway.scanMobileNetworks
  const originalUpdate = gateway.updateNetworkSelection
  const scans = []
  const writes = []
  let policy = {
    line_id: lineID,
    mode: 'auto',
    revision: 1,
    applied: true
  }

  gateway.getNetworkSelection = async requestedLineID => {
    assert.equal(requestedLineID, lineID)
    return structuredClone(policy)
  }
  gateway.scanMobileNetworks = async requestedLineID => {
    scans.push(requestedLineID)
    return {
      line_id: requestedLineID,
      observed_at: '2026-07-24T12:00:00Z',
      networks: [availableNetwork()]
    }
  }
  gateway.updateNetworkSelection = async (requestedLineID, input) => {
    writes.push({ lineID: requestedLineID, input: structuredClone(input) })
    policy = {
      line_id: requestedLineID,
      mode: input.mode,
      ...(input.mode === 'manual' ? { operator_code: input.operator_code } : {}),
      revision: policy.revision + 1,
      applied: true
    }
    return structuredClone(policy)
  }

  try {
    activateNetworkSelection(lineID)
    assert.equal(await loadNetworkSelection(lineID, true), true)
    assert.equal(scans.length, 0, 'automatic policy load must not scan')
    assert.equal(writes.length, 0)

    assert.equal(await enterManualNetworkSelection(lineID), true)
    assert.equal(networkSelectionResource(lineID).mode, 'manual')
    assert.equal(networkSelectionResource(lineID).policy.mode, 'auto')
    assert.deepEqual(scans, [lineID])
    assert.equal(writes.length, 0, 'entering manual mode must not write an empty target')

    assert.equal(await selectManualNetwork(lineID, availableNetwork()), true)
    assert.deepEqual(writes[0], {
      lineID,
      input: {
        mode: 'manual',
        operator_code: '00102',
        expected_revision: 1
      }
    })

    assert.equal(await useAutomaticNetworkSelection(lineID), true)
    assert.deepEqual(writes[1], {
      lineID,
      input: {
        mode: 'auto',
        expected_revision: 2
      }
    })
  } finally {
    gateway.getNetworkSelection = originalGet
    gateway.scanMobileNetworks = originalScan
    gateway.updateNetworkSelection = originalUpdate
    delete networkSelectionState.resources[lineID]
    activateNetworkSelection('')
  }
})

test('failed device apply reloads the persisted desired policy and concise error', async () => {
  const lineID = 'line-state-apply-error'
  const originalGet = gateway.getNetworkSelection
  const originalUpdate = gateway.updateNetworkSelection
  let reads = 0

  gateway.getNetworkSelection = async requestedLineID => {
    reads += 1
    if (reads === 1) {
      return {
        line_id: requestedLineID,
        mode: 'auto',
        revision: 4,
        applied: true
      }
    }
    return {
      line_id: requestedLineID,
      mode: 'manual',
      operator_code: '00102',
      revision: 5,
      applied: false,
      last_error: '运营商注册失败'
    }
  }
  gateway.updateNetworkSelection = async () => {
    throw new Error('设备应用失败')
  }

  try {
    activateNetworkSelection(lineID)
    assert.equal(await loadNetworkSelection(lineID, true), true)
    assert.equal(await selectManualNetwork(lineID, availableNetwork()), false)

    const resource = networkSelectionResource(lineID)
    assert.equal(reads, 2)
    assert.equal(resource.policy.mode, 'manual')
    assert.equal(resource.policy.operator_code, '00102')
    assert.equal(resource.policy.revision, 5)
    assert.equal(resource.mode, 'manual')
    assert.equal(resource.policyError, '运营商注册失败')
  } finally {
    gateway.getNetworkSelection = originalGet
    gateway.updateNetworkSelection = originalUpdate
    delete networkSelectionState.resources[lineID]
    activateNetworkSelection('')
  }
})

test('late scan and save responses never cross the active line boundary', async () => {
  const scanLineID = 'line-stale-scan'
  const saveLineID = 'line-stale-save'
  const currentLineID = 'line-current'
  const originalGet = gateway.getNetworkSelection
  const originalScan = gateway.scanMobileNetworks
  const originalUpdate = gateway.updateNetworkSelection
  const pendingScan = deferred()
  const pendingSave = deferred()

  gateway.getNetworkSelection = async requestedLineID => ({
    line_id: requestedLineID,
    mode: 'auto',
    revision: 1,
    applied: true
  })
  gateway.scanMobileNetworks = async () => pendingScan.promise
  gateway.updateNetworkSelection = async () => pendingSave.promise

  try {
    activateNetworkSelection(scanLineID)
    await loadNetworkSelection(scanLineID, true)
    const scanRequest = enterManualNetworkSelection(scanLineID)
    activateNetworkSelection(currentLineID)
    await loadNetworkSelection(currentLineID, true)
    pendingScan.resolve({
      line_id: scanLineID,
      observed_at: '2026-07-24T12:01:00Z',
      networks: [availableNetwork()]
    })
    assert.equal(await scanRequest, false)
    assert.equal(networkSelectionResource(currentLineID).scan, null)
    assert.equal(networkSelectionResource(currentLineID).mode, 'auto')

    activateNetworkSelection(saveLineID)
    await loadNetworkSelection(saveLineID, true)
    const saveRequest = selectManualNetwork(saveLineID, availableNetwork())
    activateNetworkSelection(currentLineID)
    pendingSave.resolve({
      line_id: saveLineID,
      mode: 'manual',
      operator_code: '00102',
      revision: 2,
      applied: true
    })
    assert.equal(await saveRequest, false)
    assert.equal(networkSelectionResource(currentLineID).policy.line_id, currentLineID)
    assert.equal(networkSelectionResource(currentLineID).policy.mode, 'auto')
  } finally {
    gateway.getNetworkSelection = originalGet
    gateway.scanMobileNetworks = originalScan
    gateway.updateNetworkSelection = originalUpdate
    delete networkSelectionState.resources[scanLineID]
    delete networkSelectionState.resources[saveLineID]
    delete networkSelectionState.resources[currentLineID]
    activateNetworkSelection('')
  }
})
