import assert from 'node:assert/strict'
import test from 'node:test'
import { callState } from '../src/state/call.ts'
import {
  callTestSession, callTestState, dismissCallTest, initializeCallTestRuntime,
  presentedCallSession, scheduleCallTest, shutdownCallTestRuntime, stopCallTest
} from '../src/state/callTestRuntime.ts'
import { minimizeCallSurface, uiState } from '../src/state/ui.ts'

test('test calls open the shared call presentation, survive navigation, and yield to a real call', async t => {
  t.mock.timers.enable({ apis: ['setTimeout'] })
  initializeCallTestRuntime()
  t.after(() => { shutdownCallTestRuntime(); callState.sessions = []; callState.session = null })
  minimizeCallSurface()
  scheduleCallTest()
  assert.equal(callTestSession.value, null)
  t.mock.timers.tick(5000)
  assert.equal(uiState.callMinimized, false)
  assert.equal(presentedCallSession.value, callTestSession.value)
  assert.equal(callTestSession.value.phase, 'ringing')
  assert.equal(callTestSession.value.control_state, 'available')
  assert.equal(callTestSession.value.line_id, '')
  assert.deepEqual(callState.sessions, [], 'tests never enter real call collections')

  // Minimizing or navigating does not end the app-owned test.
  minimizeCallSurface()
  assert.equal(callTestState.status, 'ringing')
  const incoming = {
    id: 'real', line_id: 'line-1', direction: 'incoming', phase: 'ringing',
    control_state: 'available', remote_number: '123', media_available: false, created_at: new Date().toISOString()
  }
  callState.sessions = [incoming]
  callState.session = incoming
  assert.equal(callTestSession.value, null)
  assert.equal(callTestState.status, 'error')
  assert.equal(presentedCallSession.value.id, 'real')
  callState.sessions = []
  callState.session = null
  scheduleCallTest()
  t.mock.timers.tick(5000)
  await stopCallTest()
  assert.equal(callTestSession.value.phase, 'ended')
  dismissCallTest()
  assert.equal(presentedCallSession.value, null)
})
