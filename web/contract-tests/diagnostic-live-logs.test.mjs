import assert from 'node:assert/strict'
import test from 'node:test'
import { createFixtureGateway } from '../src/api/fixture.ts'

test('fixture diagnostic stream emits sanitized operational events', async () => {
  const gateway = createFixtureGateway()
  const initial = await gateway.listDiagnosticLogs()
  const entries = []
  let opened = false
  const close = gateway.subscribeDiagnosticLogs(
    { after: initial.newest_id },
    {
      onOpen() {
        opened = true
      },
      onEntry(entry) {
        entries.push(entry)
      },
      onReset() {},
      onError(error) {
        assert.fail(error)
      }
    }
  )

  await gateway.sendMessage({
    line_id: 'line-fixture-main',
    to: '+818012345678',
    content: 'private fixture body'
  })
  close()

  assert.equal(opened, true)
  assert.equal(entries.length, 1)
  assert.equal(entries[0]?.source, 'application')
  assert.equal(entries[0]?.component, 'messages')
  assert.equal(entries[0]?.message, 'SMS submitted')
  assert.doesNotMatch(JSON.stringify(entries), /818012345678|private fixture body/)
})
