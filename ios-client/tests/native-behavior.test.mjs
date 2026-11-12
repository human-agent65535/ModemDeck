import assert from 'node:assert/strict'
import { execFile } from 'node:child_process'
import { mkdtemp, rm } from 'node:fs/promises'
import os from 'node:os'
import path from 'node:path'
import { promisify } from 'node:util'
import test from 'node:test'
import { fileURLToPath } from 'node:url'

const run = promisify(execFile)
const root = path.dirname(path.dirname(fileURLToPath(import.meta.url)))

test('media exchange retries preserve the offer, deadline and hangup cancellation', {
  skip: process.platform !== 'darwin', timeout: 60_000
}, async () => {
  const temporary = await mkdtemp(path.join(os.tmpdir(), 'modemdeck-media-request-test-'))
  try {
    const executable = path.join(temporary, 'media-request-tests')
    const env = { ...process.env, DEVELOPER_DIR: '/Applications/Xcode.app/Contents/Developer' }
    await run('xcrun', ['swiftc', '-swift-version', '5', '-parse-as-library',
      path.join(root, 'ios/App/App/ModemDeckDiagnostics.swift'),
      path.join(root, 'ios/App/App/ModemDeckMediaRequest.swift'),
      path.join(root, 'tests/media-request.swift'), '-o', executable], { env })
    const { stdout } = await run(executable, [], { env, timeout: 10_000 })
    assert.match(stdout, /7 media request behavior tests passed/)
  } finally { await rm(temporary, { recursive: true, force: true }) }
})

test('all native diagnostics honor consent, redaction, retries, persistence and pairing isolation', {
  skip: process.platform !== 'darwin', timeout: 60_000
}, async () => {
  const temporary = await mkdtemp(path.join(os.tmpdir(), 'modemdeck-diagnostics-test-'))
  try {
    const executable = path.join(temporary, 'diagnostics-tests')
    const env = { ...process.env, DEVELOPER_DIR: process.env.DEVELOPER_DIR || '/Applications/Xcode.app/Contents/Developer' }
    await run('xcrun', ['swiftc', '-swift-version', '5', '-parse-as-library',
      path.join(root, 'ios/App/App/ModemDeckDiagnostics.swift'),
      path.join(root, 'tests/diagnostics.swift'), '-o', executable
    ], { env })
    const { stdout } = await run(executable, [], { env, timeout: 15_000, maxBuffer: 2 * 1024 * 1024 })
    assert.match(stdout, /8 diagnostics behavior tests passed/)
  } finally { await rm(temporary, { recursive: true, force: true }) }
})

test('foreground recovery executes automatic retries, coalescing and cancellation', {
  skip: process.platform !== 'darwin', timeout: 60_000
}, async () => {
  const temporary = await mkdtemp(path.join(os.tmpdir(), 'modemdeck-recovery-test-'))
  try {
    const executable = path.join(temporary, 'recovery-tests')
    const env = { ...process.env, DEVELOPER_DIR: process.env.DEVELOPER_DIR || '/Applications/Xcode.app/Contents/Developer' }
    await run('xcrun', ['swiftc', '-swift-version', '6', '-parse-as-library',
      path.join(root, 'ios/App/App/ModemDeckConnectionRecovery.swift'),
      path.join(root, 'tests/connection-recovery.swift'), '-o', executable
    ], { env })
    const { stdout } = await run(executable, [], { env, timeout: 10_000 })
    assert.match(stdout, /6 connection recovery behavior tests passed/)
  } finally {
    await rm(temporary, { recursive: true, force: true })
  }
})

test('a path segment never lets an identifier reach another endpoint', {
  skip: process.platform !== 'darwin', timeout: 60_000
}, async () => {
  const { readFile, writeFile } = await import('node:fs/promises')
  const api = await readFile(path.join(root, 'ios/App/App/ModemDeckAPI.swift'), 'utf8')
  const start = api.indexOf('func modemDeckPathSegment(')
  const end = api.indexOf('\n}\n', start)
  assert.ok(start >= 0 && end > start, 'modemDeckPathSegment is missing')

  // Every identifier that goes into a request path uses the one function.
  for (const file of ['ModemDeckAPI.swift', 'ModemDeckCallAudio.swift', 'ModemDeckNative.swift',
    'ModemDeckSession.swift']) {
    const source = await readFile(path.join(root, 'ios/App/App', file), 'utf8')
    assert.doesNotMatch(source, /urlPathAllowed/, `${file} builds a path segment by hand`)
  }

  const temporary = await mkdtemp(path.join(os.tmpdir(), 'modemdeck-path-segment-test-'))
  try {
    const sourceFile = path.join(temporary, 'main.swift')
    await writeFile(sourceFile, `import Foundation
${api.slice(start, end + 3)}
let cases: [(String, String)] = [
  ("call_0123abcDEF-._~", "call_0123abcDEF-._~"),
  ("a/b", "a%2Fb"),
  ("../active", "..%2Factive"),
  ("..", "%2E%2E"),
  (".", "%2E"),
  ("id?x=1#y", "id%3Fx%3D1%23y"),
  ("a b%", "a%20b%25"),
  ("线路", "%E7%BA%BF%E8%B7%AF"),
  ("", "")
]
for (input, expected) in cases {
  precondition(modemDeckPathSegment(input) == expected, "\\(input) -> \\(modemDeckPathSegment(input))")
}
print("path segment cases passed")
`)
    const executable = path.join(temporary, 'path-segment-tests')
    const env = { ...process.env, DEVELOPER_DIR: process.env.DEVELOPER_DIR || '/Applications/Xcode.app/Contents/Developer' }
    await run('xcrun', ['swiftc', '-swift-version', '5', sourceFile, '-o', executable], { env })
    const { stdout } = await run(executable, [], { env, timeout: 10_000 })
    assert.match(stdout, /path segment cases passed/)
  } finally { await rm(temporary, { recursive: true, force: true }) }
})

// Shared pure helpers: each test compiles the product declaration with a small Swift main and
// pins every behaviour that differed between the copies it replaced.
function swiftDeclaration(source, marker) {
  const start = source.indexOf(marker)
  assert.ok(start >= 0, `missing ${marker}`)
  const body = source.indexOf('{', start)
  let depth = 0
  for (let index = body; index < source.length; index++) {
    if (source[index] === '{') depth++
    if (source[index] === '}' && --depth === 0) return source.slice(start, index + 1)
  }
  assert.fail(`unclosed ${marker}`)
}

async function runSwiftHelper(name, declarations, main) {
  const { writeFile } = await import('node:fs/promises')
  const temporary = await mkdtemp(path.join(os.tmpdir(), `modemdeck-${name}-test-`))
  try {
    const sourceFile = path.join(temporary, 'helpers.swift')
    await writeFile(sourceFile, `import Foundation\n${declarations.join('\n')}\n${main}\n`)
    const executable = path.join(temporary, name)
    const env = { ...process.env, DEVELOPER_DIR: process.env.DEVELOPER_DIR || '/Applications/Xcode.app/Contents/Developer' }
    await run('xcrun', ['swiftc', '-swift-version', '5', '-parse-as-library', sourceFile, '-o', executable], { env })
    const { stdout } = await run(executable, [], { env, timeout: 10_000 })
    return stdout
  } finally { await rm(temporary, { recursive: true, force: true }) }
}

async function productSource(file) {
  const { readFile } = await import('node:fs/promises')
  return readFile(path.join(root, 'ios/App/App', file), 'utf8')
}

test('shared date text accepts both timestamp forms and keeps each screen\'s duration form', {
  skip: process.platform !== 'darwin', timeout: 60_000
}, async () => {
  const app = await productSource('ModemDeckApp.swift')
  const stdout = await runSwiftHelper('date-text', [
    swiftDeclaration(app, 'struct ModemDeckISO8601Parser'), swiftDeclaration(app, 'enum ModemDeckDateText')
  ], `@main struct Main { static func main() {
  func check(_ actual: String, _ expected: String) { precondition(actual == expected, "\\(actual) != \\(expected)") }
  // Every form any former copy accepted: with or without fractional seconds, Z or an offset.
  let second = Date(timeIntervalSince1970: 1_762_916_645)
  precondition(ModemDeckDateText.date("2025-11-12T03:04:05Z") == second)
  precondition(ModemDeckDateText.date("2025-11-12T12:04:05+09:00") == second)
  precondition(ModemDeckDateText.date("2025-11-12T03:04:05.250Z") == second.addingTimeInterval(0.25))
  precondition(ModemDeckISO8601Parser().date("2025-11-12T03:04:05.250Z") == second.addingTimeInterval(0.25))
  for rejected in ["", "2025-11-12", "2025-11-12 03:04:05", "garbage"] {
    precondition(ModemDeckDateText.date(rejected) == nil, rejected)
  }
  // Written timestamps: the call state payload keeps milliseconds, the UAT fixture does not.
  let written = Date(timeIntervalSince1970: 1_700_000_000.25)
  check(ModemDeckDateText.timestamp(written, fractionalSeconds: true), "2023-11-14T22:13:20.250Z")
  check(ModemDeckDateText.timestamp(written, fractionalSeconds: false), "2023-11-14T22:13:20Z")
  // History and recording lengths: unpadded minutes that keep counting past an hour.
  for (seconds, text) in [(-5, "0:00"), (0, "0:00"), (65, "1:05"), (3_725, "62:05"), (4_500, "75:00")] {
    check(ModemDeckDateText.duration(seconds), text)
    check(ModemDeckDateText.duration(seconds, style: .minutes), text)
  }
  // The live call clock pads minutes and switches to hours.
  for (seconds, text) in [(-5, "00:00"), (0, "00:00"), (65, "01:05"), (3_599, "59:59"), (3_725, "1:02:05")] {
    check(ModemDeckDateText.duration(seconds, style: .callClock), text)
  }
  // The recording player does not pad minutes and switches to hours.
  for (seconds, text) in [(-5, "0:00"), (0, "0:00"), (65, "1:05"), (3_599, "59:59"), (3_725, "1:02:05")] {
    check(ModemDeckDateText.duration(seconds, style: .playback), text)
  }
  print("date text cases passed")
} }`)
  assert.match(stdout, /date text cases passed/)
})

test('phone identity keeps the contact, conversation, avatar and import rules apart', {
  skip: process.platform !== 'darwin', timeout: 60_000
}, async () => {
  const api = await productSource('ModemDeckAPI.swift')
  const stdout = await runSwiftHelper('phone-identity', [swiftDeclaration(api, 'enum ModemDeckPhoneIdentity')],
    `@main struct Main { static func main() {
  typealias P = ModemDeckPhoneIdentity
  func check(_ actual: String?, _ expected: String?) { precondition(actual == expected, "\\(String(describing: actual)) != \\(String(describing: expected))") }
  check(P.compact(" (+86) 138.0013/8000-1 "), "+86138001380001")
  // Contacts: only a + number of 8 to 15 ASCII digits not starting with 0.
  check(P.canonical(" +86 (138) 0013-8000 "), "+8613800138000")
  check(P.canonical("+1234567"), nil)
  check(P.canonical("+1234567890123456"), nil)
  check(P.canonical("+0123456789"), nil)
  check(P.canonical("008613800138000"), nil)
  check(P.canonical("13800138000"), nil)
  check(P.canonical("+１３８００１３８０００"), nil)
  // Conversations: canonical when both sides have one, otherwise the digits as written.
  precondition(P.sameThreadPeer("+86 138 0013 8000", "+8613800138000"))
  precondition(P.sameThreadPeer("138-0013-8000", "13800138000"))
  precondition(!P.sameThreadPeer("008613800138000", "+8613800138000"))
  precondition(!P.sameThreadPeer("13800138000", "+8613800138000"))
  precondition(P.sameThreadPeer("UAT-SERVICE", "UATSERVICE"))
  // Avatars: 00 reads as +, and text compares case- and width-insensitively.
  check(P.avatarKey("0086 138 0013 8000"), "+8613800138000")
  check(P.avatarKey("00"), "00")
  check(P.avatarKey("  Alice   Smith "), "alice smith")
  check(P.avatarKey("Ｂob"), "bob")
  check(P.avatarKey(""), "")
  // Import: + and every character Swift counts as a number, separators dropped.
  check(P.importKey("+86 (138) 0013-8000"), "+8613800138000")
  check(P.importKey("٨٦١٣٨"), "٨٦١٣٨")
  check(P.importKey("ext. abc"), "")
  print("phone identity cases passed")
} }`)
  assert.match(stdout, /phone identity cases passed/)
})

test('page following stops on a repeated or missing cursor and returns the first unread cursor', {
  skip: process.platform !== 'darwin', timeout: 60_000
}, async () => {
  const api = await productSource('ModemDeckAPI.swift')
  const stdout = await runSwiftHelper('follow-pages', [swiftDeclaration(api, 'func modemDeckFollowPages(')],
    `struct ModemDeckPageMeta { let limit: Int; let nextCursor: String; let hasMore: Bool }
enum ModemDeckAPIError: Error { case invalidResponse }
func pages(_ next: [String: (String, Bool)], emptyCursorEnds: Bool, stopAfter: Int = .max) async -> (fetched: [String], result: Result<String, Error>) {
  var fetched: [String] = []
  do {
    let cursor = try await modemDeckFollowPages(emptyCursorEnds: emptyCursorEnds, wantsMore: { fetched.count < stopAfter }) { cursor in
      fetched.append(cursor)
      let (nextCursor, hasMore) = next[cursor] ?? ("", false)
      return ModemDeckPageMeta(limit: 100, nextCursor: nextCursor, hasMore: hasMore)
    }
    return (fetched, .success(cursor))
  } catch { return (fetched, .failure(error)) }
}
func invalid(_ result: Result<String, Error>) -> Bool {
  if case .failure(ModemDeckAPIError.invalidResponse) = result { return true }; return false
}
@main struct Main { static func main() async {
  let chain = ["": ("a", true), "a": ("b", true), "b": ("", false)]
  for ends in [true, false] {
    let all = await pages(chain, emptyCursorEnds: ends)
    precondition(all.fetched == ["", "a", "b"] && (try? all.result.get()) == "")
    // A server that hands out a cursor again stops the listing instead of looping.
    let looping = await pages(["": ("a", true), "a": ("b", true), "b": ("a", true)], emptyCursorEnds: ends)
    precondition(looping.fetched == ["", "a", "b"] && invalid(looping.result))
    let same = await pages(["": ("a", true), "a": ("a", true)], emptyCursorEnds: ends)
    precondition(same.fetched == ["", "a"] && invalid(same.result))
    // Stopping early returns the next cursor unfetched, and a repeat there is not checked.
    let early = await pages(chain, emptyCursorEnds: ends, stopAfter: 1)
    precondition(early.fetched == [""] && (try? early.result.get()) == "a")
    let earlyRepeat = await pages(["": ("a", true), "a": ("a", true)], emptyCursorEnds: ends, stopAfter: 2)
    precondition(earlyRepeat.fetched == ["", "a"] && (try? earlyRepeat.result.get()) == "a")
  }
  // A page that has more but no cursor: the end of the listing, or an invalid response.
  let missing = ["": ("a", true), "a": ("", true)]
  let ended = await pages(missing, emptyCursorEnds: true)
  precondition(ended.fetched == ["", "a"] && (try? ended.result.get()) == "")
  let rejected = await pages(missing, emptyCursorEnds: false)
  precondition(rejected.fetched == ["", "a"] && invalid(rejected.result))
  print("page following cases passed")
} }`)
  assert.match(stdout, /page following cases passed/)
})

test('retry schedules keep each loop\'s delays', {
  skip: process.platform !== 'darwin', timeout: 60_000
}, async () => {
  const recovery = await productSource('ModemDeckConnectionRecovery.swift')
  const stdout = await runSwiftHelper('backoff', [swiftDeclaration(recovery, 'enum ModemDeckBackoff')],
    `@main struct Main { static func main() {
  let connection = (0..<7).map { ModemDeckBackoff.delay(ModemDeckBackoff.connectionSeconds, attempt: $0) }
  precondition(connection == [1, 2, 5, 10, 30, 30, 30], "\\(connection)")
  let push = (0..<6).map { ModemDeckBackoff.delay(ModemDeckBackoff.pushSeconds, attempt: $0) }
  precondition(push == [5, 15, 30, 60, 60, 60], "\\(push)")
  let streams = (0..<7).map { ModemDeckBackoff.eventStreamDelay(attempt: $0) }
  precondition(streams == [2, 4, 8, 16, 30, 30, 30], "\\(streams)")
  print("backoff cases passed")
} }`)
  assert.match(stdout, /backoff cases passed/)
})

test('Select All toggles only the visible items', {
  skip: process.platform !== 'darwin', timeout: 60_000
}, async () => {
  const interaction = await productSource('ModemDeckListInteraction.swift')
  const stdout = await runSwiftHelper('select-all', [swiftDeclaration(interaction, 'extension Set where Element == String {')],
    `@main struct Main { static func main() {
  func toggled(_ selected: Set<String>, _ visible: [String]) -> Set<String> {
    var value = selected; value.modemDeckToggleAll(visible); return value
  }
  precondition(toggled([], ["a", "b"]) == ["a", "b"])
  precondition(toggled(["a"], ["a", "b"]) == ["a", "b"])
  precondition(toggled(["a", "b", "hidden"], ["a", "b"]) == ["hidden"])
  precondition(toggled(["hidden"], []) == ["hidden"])
  print("select all cases passed")
} }`)
  assert.match(stdout, /select all cases passed/)
})
